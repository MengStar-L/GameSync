package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const defaultSaveChangeDebounce = 100 * time.Millisecond

var ErrSaveChangeTrackerClosed = errors.New("save change tracker is closed")

type SaveChangeEvent struct {
	GameID     string
	DirtyPaths []string
	Rebuild    bool
}

type SaveChangeTracker struct {
	watcher  *fsnotify.Watcher
	debounce time.Duration
	onChange func(SaveChangeEvent)
	commands chan saveChangeCommand
	done     chan struct{}

	closeOnce sync.Once
	closeErr  error
}

type saveChangeCommand struct {
	kind             saveChangeCommandKind
	gameID           string
	savePath         string
	rulesFingerprint string
	reply            chan error
}

type saveChangeCommandKind uint8

const (
	saveChangeRegister saveChangeCommandKind = iota
	saveChangeUnregister
	saveChangeClose
)

type trackedSaveGame struct {
	root             string
	rootKey          string
	rulesFingerprint string
	watchReady       bool
}

type watchedSaveDirectory struct {
	path   string
	owners map[string]struct{}
}

type pendingSaveChange struct {
	rebuild bool
	paths   map[string]struct{}
}

type saveChangeLoop struct {
	tracker     *SaveChangeTracker
	games       map[string]trackedSaveGame
	watchedDirs map[string]*watchedSaveDirectory
	pending     map[string]*pendingSaveChange
	timer       *time.Timer
	timerC      <-chan time.Time
}

func NewSaveChangeTracker(onChange func(SaveChangeEvent)) (*SaveChangeTracker, error) {
	return newSaveChangeTracker(defaultSaveChangeDebounce, onChange)
}

func newSaveChangeTracker(debounce time.Duration, onChange func(SaveChangeEvent)) (*SaveChangeTracker, error) {
	if debounce <= 0 {
		return nil, fmt.Errorf("save change debounce must be positive")
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create save change watcher: %w", err)
	}
	if onChange == nil {
		onChange = func(SaveChangeEvent) {}
	}
	tracker := &SaveChangeTracker{
		watcher:  watcher,
		debounce: debounce,
		onChange: onChange,
		commands: make(chan saveChangeCommand),
		done:     make(chan struct{}),
	}
	go tracker.run()
	return tracker, nil
}

func (t *SaveChangeTracker) RegisterGame(gameID, savePath, rulesFingerprint string) error {
	return t.configureGame(gameID, savePath, rulesFingerprint)
}

func (t *SaveChangeTracker) UpdateGame(gameID, savePath, rulesFingerprint string) error {
	return t.configureGame(gameID, savePath, rulesFingerprint)
}

func (t *SaveChangeTracker) UnregisterGame(gameID string) error {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return fmt.Errorf("save change game id is empty")
	}
	return t.sendCommand(saveChangeCommand{
		kind:   saveChangeUnregister,
		gameID: gameID,
		reply:  make(chan error, 1),
	})
}

func (t *SaveChangeTracker) Close() error {
	t.closeOnce.Do(func() {
		command := saveChangeCommand{kind: saveChangeClose, reply: make(chan error, 1)}
		select {
		case t.commands <- command:
			t.closeErr = <-command.reply
		case <-t.done:
		}
		<-t.done
	})
	return t.closeErr
}

func (t *SaveChangeTracker) configureGame(gameID, savePath, rulesFingerprint string) error {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return fmt.Errorf("save change game id is empty")
	}
	savePath = strings.TrimSpace(savePath)
	if savePath == "" {
		return fmt.Errorf("save change path is empty for game %s", gameID)
	}
	return t.sendCommand(saveChangeCommand{
		kind:             saveChangeRegister,
		gameID:           gameID,
		savePath:         savePath,
		rulesFingerprint: rulesFingerprint,
		reply:            make(chan error, 1),
	})
}

func (t *SaveChangeTracker) sendCommand(command saveChangeCommand) error {
	select {
	case t.commands <- command:
	case <-t.done:
		return ErrSaveChangeTrackerClosed
	}
	select {
	case err := <-command.reply:
		return err
	case <-t.done:
		return ErrSaveChangeTrackerClosed
	}
}

func (t *SaveChangeTracker) run() {
	loop := saveChangeLoop{
		tracker:     t,
		games:       make(map[string]trackedSaveGame),
		watchedDirs: make(map[string]*watchedSaveDirectory),
		pending:     make(map[string]*pendingSaveChange),
	}
	defer close(t.done)

	for {
		select {
		case command := <-t.commands:
			if loop.handleCommand(command) {
				return
			}
		case event, ok := <-t.watcher.Events:
			if !ok {
				loop.handleWatcherShutdown()
				return
			}
			loop.handleEvent(event)
		case _, ok := <-t.watcher.Errors:
			if !ok {
				loop.handleWatcherShutdown()
				return
			}
			loop.markAllGamesRebuild()
		case <-loop.timerC:
			loop.timerC = nil
			loop.flushPending()
		}
	}
}

func (l *saveChangeLoop) handleCommand(command saveChangeCommand) bool {
	switch command.kind {
	case saveChangeRegister:
		command.reply <- l.configureGame(command.gameID, command.savePath, command.rulesFingerprint)
	case saveChangeUnregister:
		command.reply <- l.unregisterGame(command.gameID)
	case saveChangeClose:
		if l.timer != nil {
			l.timer.Stop()
		}
		l.pending = make(map[string]*pendingSaveChange)
		command.reply <- l.tracker.watcher.Close()
		return true
	}
	return false
}

func (l *saveChangeLoop) configureGame(gameID, savePath, rulesFingerprint string) error {
	root, err := filepath.Abs(savePath)
	if err != nil {
		l.queueRebuild(gameID)
		return fmt.Errorf("resolve save path for game %s: %w", gameID, err)
	}
	root = filepath.Clean(root)
	rootKey := saveChangePathKey(root)
	current, exists := l.games[gameID]
	changed := !exists || current.rootKey != rootKey || current.rulesFingerprint != rulesFingerprint
	if exists && !changed && current.watchReady {
		return nil
	}
	if exists && current.rootKey == rootKey && current.watchReady {
		current.rulesFingerprint = rulesFingerprint
		l.games[gameID] = current
		l.queueRebuild(gameID)
		return nil
	}

	if exists {
		_ = l.removeGameWatches(gameID)
	}
	l.games[gameID] = trackedSaveGame{
		root:             root,
		rootKey:          rootKey,
		rulesFingerprint: rulesFingerprint,
	}

	info, statErr := os.Stat(root)
	if statErr != nil {
		l.queueRebuild(gameID)
		return fmt.Errorf("stat save directory for game %s: %w", gameID, statErr)
	}
	if !info.IsDir() {
		l.queueRebuild(gameID)
		return fmt.Errorf("save path for game %s is not a directory: %s", gameID, root)
	}
	if err := l.addGameSubtree(gameID, root); err != nil {
		l.queueRebuild(gameID)
		return fmt.Errorf("watch save directory for game %s: %w", gameID, err)
	}

	game := l.games[gameID]
	game.watchReady = true
	l.games[gameID] = game
	if exists && (changed || !current.watchReady) {
		l.queueRebuild(gameID)
	}
	return nil
}

func (l *saveChangeLoop) unregisterGame(gameID string) error {
	if _, exists := l.games[gameID]; !exists {
		delete(l.pending, gameID)
		return nil
	}
	delete(l.games, gameID)
	delete(l.pending, gameID)
	return l.removeGameWatches(gameID)
}

func (l *saveChangeLoop) addGameSubtree(gameID, root string) error {
	addedKeys := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		path = filepath.Clean(path)
		key := saveChangePathKey(path)
		watched, exists := l.watchedDirs[key]
		if exists {
			if _, owned := watched.owners[gameID]; !owned {
				watched.owners[gameID] = struct{}{}
				addedKeys = append(addedKeys, key)
			}
			return nil
		}
		if err := l.tracker.watcher.Add(path); err != nil {
			return err
		}
		l.watchedDirs[key] = &watchedSaveDirectory{
			path:   path,
			owners: map[string]struct{}{gameID: {}},
		}
		addedKeys = append(addedKeys, key)
		return nil
	})
	if err == nil {
		return nil
	}
	for index := len(addedKeys) - 1; index >= 0; index-- {
		l.removeDirectoryOwner(addedKeys[index], gameID)
	}
	return err
}

func (l *saveChangeLoop) removeGameWatches(gameID string) error {
	keys := make([]string, 0)
	for key, watched := range l.watchedDirs {
		if _, owned := watched.owners[gameID]; owned {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	var removeErr error
	for _, key := range keys {
		if err := l.removeDirectoryOwner(key, gameID); err != nil {
			removeErr = errors.Join(removeErr, err)
		}
	}
	return removeErr
}

func (l *saveChangeLoop) removeDirectoryOwner(key, gameID string) error {
	watched, exists := l.watchedDirs[key]
	if !exists {
		return nil
	}
	delete(watched.owners, gameID)
	if len(watched.owners) > 0 {
		return nil
	}
	delete(l.watchedDirs, key)
	err := l.tracker.watcher.Remove(watched.path)
	if errors.Is(err, fsnotify.ErrNonExistentWatch) || errors.Is(err, fsnotify.ErrClosed) {
		return nil
	}
	return err
}

func (l *saveChangeLoop) handleEvent(event fsnotify.Event) {
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
		return
	}
	eventPath := filepath.Clean(event.Name)
	eventKey := saveChangePathKey(eventPath)
	exactDirectory, wasWatchedDirectory := l.watchedDirs[eventKey]

	if wasWatchedDirectory && event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		owners := saveChangeOwnerIDs(exactDirectory.owners)
		for _, gameID := range owners {
			l.queueRebuild(gameID)
		}
		l.removeWatchedSubtree(eventKey)
		return
	}
	if wasWatchedDirectory {
		// Directory writes are redundant metadata noise on Windows and kqueue.
		// Child create/remove/rename events carry the actionable path.
		return
	}

	parent := l.watchedDirs[saveChangePathKey(filepath.Dir(eventPath))]
	if parent == nil {
		return
	}
	owners := saveChangeOwnerIDs(parent.owners)

	if event.Op&fsnotify.Create != 0 {
		info, err := os.Stat(eventPath)
		if err != nil {
			for _, gameID := range owners {
				l.queueRebuild(gameID)
			}
			return
		}
		if info.IsDir() {
			for _, gameID := range owners {
				if err := l.addGameSubtree(gameID, eventPath); err != nil {
					game := l.games[gameID]
					game.watchReady = false
					l.games[gameID] = game
				}
				l.queueRebuild(gameID)
			}
			return
		}
	}

	for _, gameID := range owners {
		game, exists := l.games[gameID]
		if !exists {
			continue
		}
		relativePath, err := filepath.Rel(game.root, eventPath)
		if err != nil || relativePath == "." || saveChangePathEscapes(relativePath) {
			l.queueRebuild(gameID)
			continue
		}
		l.queueDirty(gameID, filepath.ToSlash(filepath.Clean(relativePath)))
	}
}

func (l *saveChangeLoop) removeWatchedSubtree(rootKey string) {
	prefix := rootKey + string(filepath.Separator)
	keys := make([]string, 0)
	for key := range l.watchedDirs {
		if key == rootKey || strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		watched := l.watchedDirs[key]
		delete(l.watchedDirs, key)
		_ = l.tracker.watcher.Remove(watched.path)
	}
	for gameID, game := range l.games {
		if game.rootKey == rootKey || strings.HasPrefix(game.rootKey, prefix) {
			game.watchReady = false
			l.games[gameID] = game
		}
	}
}

func (l *saveChangeLoop) handleWatcherShutdown() {
	l.markAllGamesRebuild()
	if l.timer != nil {
		l.timer.Stop()
	}
	l.timerC = nil
	l.flushPending()
	_ = l.tracker.watcher.Close()
}

func (l *saveChangeLoop) markAllGamesRebuild() {
	for gameID := range l.games {
		l.queueRebuild(gameID)
	}
}

func (l *saveChangeLoop) queueDirty(gameID, path string) {
	pending := l.pendingChange(gameID)
	if pending.rebuild {
		return
	}
	pending.paths[path] = struct{}{}
	l.resetTimer()
}

func (l *saveChangeLoop) queueRebuild(gameID string) {
	pending := l.pendingChange(gameID)
	pending.rebuild = true
	pending.paths = make(map[string]struct{})
	l.resetTimer()
}

func (l *saveChangeLoop) pendingChange(gameID string) *pendingSaveChange {
	pending := l.pending[gameID]
	if pending == nil {
		pending = &pendingSaveChange{paths: make(map[string]struct{})}
		l.pending[gameID] = pending
	}
	return pending
}

func (l *saveChangeLoop) resetTimer() {
	if l.timer == nil {
		l.timer = time.NewTimer(l.tracker.debounce)
	} else {
		if !l.timer.Stop() {
			select {
			case <-l.timer.C:
			default:
			}
		}
		l.timer.Reset(l.tracker.debounce)
	}
	l.timerC = l.timer.C
}

func (l *saveChangeLoop) flushPending() {
	gameIDs := make([]string, 0, len(l.pending))
	for gameID := range l.pending {
		gameIDs = append(gameIDs, gameID)
	}
	slices.Sort(gameIDs)
	pendingChanges := l.pending
	l.pending = make(map[string]*pendingSaveChange)
	for _, gameID := range gameIDs {
		pending := pendingChanges[gameID]
		paths := make([]string, 0, len(pending.paths))
		for path := range pending.paths {
			paths = append(paths, path)
		}
		slices.Sort(paths)
		l.tracker.onChange(SaveChangeEvent{
			GameID:     gameID,
			DirtyPaths: paths,
			Rebuild:    pending.rebuild,
		})
	}
}

func saveChangeOwnerIDs(owners map[string]struct{}) []string {
	result := make([]string, 0, len(owners))
	for gameID := range owners {
		result = append(result, gameID)
	}
	slices.Sort(result)
	return result
}

func saveChangePathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func saveChangePathEscapes(relativePath string) bool {
	return relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) || filepath.IsAbs(relativePath)
}
