package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const DeviceIndexVersion = 1

const (
	ScanStateClean   = "clean"
	ScanStateDirty   = "dirty"
	ScanStateRebuild = "rebuild"
)

type DeviceIndex struct {
	Version  int                        `json:"version"`
	DeviceID string                     `json:"deviceId"`
	Games    map[string]DeviceGameIndex `json:"games"`
}

type DeviceGameIndex struct {
	SavePath              string                     `json:"savePath,omitempty"`
	SyncConfigFingerprint string                     `json:"syncConfigFingerprint,omitempty"`
	RemoteVersion         int                        `json:"remoteVersion,omitempty"`
	RemoteManifestHash    string                     `json:"remoteManifestHash,omitempty"`
	GeneratedAt           time.Time                  `json:"generatedAt,omitempty" ts_type:"string"`
	ScanState             string                     `json:"scanState"`
	DirtyPaths            []string                   `json:"dirtyPaths,omitempty"`
	Files                 map[string]DeviceIndexFile `json:"files,omitempty"`
	Cover                 DeviceCoverIndex           `json:"cover,omitempty"`
}

type DeviceIndexFile struct {
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt" ts_type:"string"`
	SHA256     string    `json:"sha256"`
}

type DeviceCoverIndex struct {
	SourceFingerprint string `json:"sourceFingerprint,omitempty"`
	AccountID         string `json:"accountId,omitempty"`
	ObjectKey         string `json:"objectKey,omitempty"`
}

type DeviceIndexStore struct {
	mu          sync.Mutex
	path        string
	index       DeviceIndex
	writeAtomic func(string, []byte) error
}

func NewDeviceIndexStore(dataDir string, deviceID string) (*DeviceIndexStore, error) {
	dataDir = strings.TrimSpace(dataDir)
	deviceID = strings.TrimSpace(deviceID)
	if dataDir == "" {
		return nil, errors.New("device index data directory is empty")
	}
	if deviceID == "" {
		return nil, errors.New("device index device ID is empty")
	}

	indexDir := filepath.Join(dataDir, "indexes")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		return nil, fmt.Errorf("create device index directory: %w", err)
	}
	indexPath := filepath.Join(indexDir, "device-index.json")
	index, err := loadDeviceIndex(indexPath, deviceID)
	if err != nil {
		return nil, err
	}
	return &DeviceIndexStore{
		path:        indexPath,
		index:       index,
		writeAtomic: writeDeviceIndexAtomic,
	}, nil
}

func (s *DeviceIndexStore) Path() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path
}

func (s *DeviceIndexStore) Clone() DeviceIndex {
	if s == nil {
		return DeviceIndex{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneDeviceIndex(s.index)
}

func (s *DeviceIndexStore) Game(gameID string) (DeviceGameIndex, bool) {
	if s == nil {
		return DeviceGameIndex{}, false
	}
	gameID = strings.TrimSpace(gameID)
	s.mu.Lock()
	defer s.mu.Unlock()
	game, ok := s.index.Games[gameID]
	return cloneDeviceGameIndex(game), ok
}

func (s *DeviceIndexStore) ConfigureGame(gameID string, savePath string, includePatterns []string, excludePatterns []string) (DeviceGameIndex, error) {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return DeviceGameIndex{}, errors.New("device index game ID is empty")
	}
	savePath = normalizeIndexedSavePath(savePath)
	fingerprint := SyncConfigFingerprint(includePatterns, excludePatterns)

	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.index.Games[gameID]
	if exists && current.SavePath == savePath && current.SyncConfigFingerprint == fingerprint {
		return cloneDeviceGameIndex(current), nil
	}

	next := cloneDeviceIndex(s.index)
	game := next.Games[gameID]
	pathChanged := game.SavePath != savePath
	game.SavePath = savePath
	game.SyncConfigFingerprint = fingerprint
	game.ScanState = ScanStateRebuild
	game.DirtyPaths = nil
	if pathChanged || savePath == "" {
		game.Files = nil
		game.GeneratedAt = time.Time{}
	}
	next.Games[gameID] = game
	if err := s.persistLocked(next); err != nil {
		return DeviceGameIndex{}, err
	}
	return cloneDeviceGameIndex(game), nil
}

func (s *DeviceIndexStore) MarkDirty(gameID string, dirtyPaths ...string) error {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return errors.New("device index game ID is empty")
	}
	if len(dirtyPaths) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(dirtyPaths))
	for _, dirtyPath := range dirtyPaths {
		value, err := normalizeIndexedRelativePath(dirtyPath)
		if err != nil {
			return err
		}
		normalized = append(normalized, value)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.index.Games[gameID]
	if current.ScanState == ScanStateRebuild {
		return nil
	}
	seen := make(map[string]bool, len(current.DirtyPaths)+len(normalized))
	for _, value := range current.DirtyPaths {
		seen[value] = true
	}
	changed := current.ScanState != ScanStateDirty
	for _, value := range normalized {
		if !seen[value] {
			seen[value] = true
			changed = true
		}
	}
	if !changed {
		return nil
	}

	next := cloneDeviceIndex(s.index)
	game := next.Games[gameID]
	game.ScanState = ScanStateDirty
	game.DirtyPaths = make([]string, 0, len(seen))
	for value := range seen {
		game.DirtyPaths = append(game.DirtyPaths, value)
	}
	sort.Strings(game.DirtyPaths)
	next.Games[gameID] = game
	return s.persistLocked(next)
}

func (s *DeviceIndexStore) MarkRebuild(gameID string) error {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return errors.New("device index game ID is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.index.Games[gameID]
	if current.ScanState == ScanStateRebuild && len(current.DirtyPaths) == 0 {
		return nil
	}
	next := cloneDeviceIndex(s.index)
	game := next.Games[gameID]
	game.ScanState = ScanStateRebuild
	game.DirtyPaths = nil
	next.Games[gameID] = game
	return s.persistLocked(next)
}

func (s *DeviceIndexStore) CommitGame(gameID string, remoteVersion int, remoteManifestHash string, manifest SyncManifest) (DeviceGameIndex, error) {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return DeviceGameIndex{}, errors.New("device index game ID is empty")
	}
	if remoteVersion < 0 {
		return DeviceGameIndex{}, errors.New("device index remote version is negative")
	}
	files := make(map[string]DeviceIndexFile, len(manifest.Files))
	for _, file := range manifest.Files {
		relPath, err := normalizeIndexedRelativePath(file.Path)
		if err != nil {
			return DeviceGameIndex{}, err
		}
		if file.Size < 0 {
			return DeviceGameIndex{}, fmt.Errorf("device index file size is negative: %s", relPath)
		}
		if _, exists := files[relPath]; exists {
			return DeviceGameIndex{}, fmt.Errorf("duplicate device index file path: %s", relPath)
		}
		hash := strings.ToLower(strings.TrimSpace(file.SHA256))
		if hash == "" {
			return DeviceGameIndex{}, fmt.Errorf("device index file hash is empty: %s", relPath)
		}
		files[relPath] = DeviceIndexFile{
			Size:       file.Size,
			ModifiedAt: file.ModifiedAt.UTC(),
			SHA256:     hash,
		}
	}
	if len(files) == 0 {
		files = nil
	}
	generatedAt := manifest.GeneratedAt.UTC()
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	remoteManifestHash = strings.TrimSpace(remoteManifestHash)
	if remoteManifestHash == "" {
		remoteManifestHash = strings.TrimSpace(manifest.Hash)
	}
	if remoteManifestHash == "" {
		remoteManifestHash = hashManifestFiles(manifest.Files)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneDeviceIndex(s.index)
	game := next.Games[gameID]
	game.RemoteVersion = remoteVersion
	game.RemoteManifestHash = remoteManifestHash
	game.GeneratedAt = generatedAt
	game.ScanState = ScanStateClean
	game.DirtyPaths = nil
	game.Files = files
	next.Games[gameID] = game
	if err := s.persistLocked(next); err != nil {
		return DeviceGameIndex{}, err
	}
	return cloneDeviceGameIndex(game), nil
}

func (s *DeviceIndexStore) UpdateCover(gameID string, cover DeviceCoverIndex) (DeviceGameIndex, error) {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return DeviceGameIndex{}, errors.New("device index game ID is empty")
	}
	cover.SourceFingerprint = strings.ToLower(strings.TrimSpace(cover.SourceFingerprint))
	cover.AccountID = strings.TrimSpace(cover.AccountID)
	cover.ObjectKey = strings.Trim(strings.TrimSpace(cover.ObjectKey), "/")

	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.index.Games[gameID]
	if current.Cover == cover {
		return cloneDeviceGameIndex(current), nil
	}
	next := cloneDeviceIndex(s.index)
	game := next.Games[gameID]
	if game.ScanState == "" {
		game.ScanState = ScanStateRebuild
	}
	game.Cover = cover
	next.Games[gameID] = game
	if err := s.persistLocked(next); err != nil {
		return DeviceGameIndex{}, err
	}
	return cloneDeviceGameIndex(game), nil
}

func (s *DeviceIndexStore) RepointAccountIDs(aliases map[string]string) error {
	if s == nil || len(aliases) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneDeviceIndex(s.index)
	changed := false
	for gameID, game := range next.Games {
		if replacement := aliases[strings.TrimSpace(game.Cover.AccountID)]; replacement != "" && replacement != game.Cover.AccountID {
			game.Cover.AccountID = replacement
			next.Games[gameID] = game
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.persistLocked(next)
}

func (s *DeviceIndexStore) RemoveGame(gameID string) error {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return errors.New("device index game ID is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.index.Games[gameID]; !exists {
		return nil
	}
	next := cloneDeviceIndex(s.index)
	delete(next.Games, gameID)
	return s.persistLocked(next)
}

func SyncConfigFingerprint(includePatterns []string, excludePatterns []string) string {
	include := canonicalSyncPatterns(includePatterns)
	if len(include) == 0 {
		include = []string{"*"}
	}
	payload := struct {
		Include []string `json:"include"`
		Exclude []string `json:"exclude"`
	}{
		Include: include,
		Exclude: canonicalSyncPatterns(excludePatterns),
	}
	content, _ := json.Marshal(payload)
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func (s *DeviceIndexStore) persistLocked(next DeviceIndex) error {
	content, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal device index: %w", err)
	}
	if err := s.writeAtomic(s.path, content); err != nil {
		return fmt.Errorf("persist device index: %w", err)
	}
	s.index = next
	return nil
}

func loadDeviceIndex(indexPath string, deviceID string) (DeviceIndex, error) {
	empty := newDeviceIndex(deviceID)
	content, err := os.ReadFile(indexPath)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return DeviceIndex{}, fmt.Errorf("read device index: %w", err)
	}
	var index DeviceIndex
	if err := json.Unmarshal(content, &index); err != nil {
		if quarantineErr := quarantineDeviceIndex(indexPath); quarantineErr != nil {
			return DeviceIndex{}, fmt.Errorf("quarantine malformed device index: %w", quarantineErr)
		}
		return empty, nil
	}
	if err := normalizeAndValidateDeviceIndex(&index, deviceID); err != nil {
		if quarantineErr := quarantineDeviceIndex(indexPath); quarantineErr != nil {
			return DeviceIndex{}, fmt.Errorf("quarantine invalid device index: %w", quarantineErr)
		}
		return empty, nil
	}
	return index, nil
}

func normalizeAndValidateDeviceIndex(index *DeviceIndex, deviceID string) error {
	if index.Version != DeviceIndexVersion {
		return fmt.Errorf("unsupported device index version: %d", index.Version)
	}
	if strings.TrimSpace(index.DeviceID) != deviceID {
		return errors.New("device index belongs to another device")
	}
	if index.Games == nil {
		index.Games = make(map[string]DeviceGameIndex)
	}
	for rawID, rawGame := range index.Games {
		gameID := strings.TrimSpace(rawID)
		if gameID == "" || gameID != rawID {
			return errors.New("device index contains an invalid game ID")
		}
		game := cloneDeviceGameIndex(rawGame)
		game.SavePath = normalizeIndexedSavePath(game.SavePath)
		game.SyncConfigFingerprint = strings.TrimSpace(game.SyncConfigFingerprint)
		game.RemoteManifestHash = strings.TrimSpace(game.RemoteManifestHash)
		game.GeneratedAt = game.GeneratedAt.UTC()
		if game.RemoteVersion < 0 {
			return fmt.Errorf("device index remote version is negative: %s", gameID)
		}
		switch game.ScanState {
		case ScanStateClean, ScanStateDirty, ScanStateRebuild:
		case "":
			game.ScanState = ScanStateRebuild
		default:
			return fmt.Errorf("invalid device index scan state for %s: %s", gameID, game.ScanState)
		}
		dirtySet := make(map[string]bool, len(game.DirtyPaths))
		for _, rawPath := range game.DirtyPaths {
			relPath, err := normalizeIndexedRelativePath(rawPath)
			if err != nil {
				return err
			}
			dirtySet[relPath] = true
		}
		game.DirtyPaths = game.DirtyPaths[:0]
		for relPath := range dirtySet {
			game.DirtyPaths = append(game.DirtyPaths, relPath)
		}
		sort.Strings(game.DirtyPaths)
		if game.ScanState == ScanStateRebuild {
			game.DirtyPaths = nil
		}
		files := make(map[string]DeviceIndexFile, len(game.Files))
		for rawPath, rawFile := range game.Files {
			relPath, err := normalizeIndexedRelativePath(rawPath)
			if err != nil {
				return err
			}
			if rawFile.Size < 0 {
				return fmt.Errorf("device index file size is negative: %s", relPath)
			}
			if _, exists := files[relPath]; exists {
				return fmt.Errorf("duplicate device index file path: %s", relPath)
			}
			rawFile.ModifiedAt = rawFile.ModifiedAt.UTC()
			rawFile.SHA256 = strings.ToLower(strings.TrimSpace(rawFile.SHA256))
			files[relPath] = rawFile
		}
		if len(files) == 0 || game.SavePath == "" {
			files = nil
		}
		game.Files = files
		game.Cover.SourceFingerprint = strings.ToLower(strings.TrimSpace(game.Cover.SourceFingerprint))
		game.Cover.AccountID = strings.TrimSpace(game.Cover.AccountID)
		game.Cover.ObjectKey = strings.Trim(strings.TrimSpace(game.Cover.ObjectKey), "/")
		index.Games[gameID] = game
	}
	index.DeviceID = deviceID
	return nil
}

func newDeviceIndex(deviceID string) DeviceIndex {
	return DeviceIndex{
		Version:  DeviceIndexVersion,
		DeviceID: deviceID,
		Games:    make(map[string]DeviceGameIndex),
	}
}

func cloneDeviceIndex(index DeviceIndex) DeviceIndex {
	clone := DeviceIndex{
		Version:  index.Version,
		DeviceID: index.DeviceID,
		Games:    make(map[string]DeviceGameIndex, len(index.Games)),
	}
	for gameID, game := range index.Games {
		clone.Games[gameID] = cloneDeviceGameIndex(game)
	}
	return clone
}

func cloneDeviceGameIndex(game DeviceGameIndex) DeviceGameIndex {
	clone := game
	clone.DirtyPaths = append([]string(nil), game.DirtyPaths...)
	if game.Files != nil {
		clone.Files = make(map[string]DeviceIndexFile, len(game.Files))
		for relPath, file := range game.Files {
			clone.Files[relPath] = file
		}
	}
	return clone
}

func canonicalSyncPatterns(patterns []string) []string {
	seen := make(map[string]bool, len(patterns))
	result := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(strings.ReplaceAll(pattern, "\\", "/"))
		if pattern == "" || seen[pattern] {
			continue
		}
		seen[pattern] = true
		result = append(result, pattern)
	}
	sort.Strings(result)
	return result
}

func normalizeIndexedSavePath(savePath string) string {
	savePath = strings.TrimSpace(savePath)
	if savePath == "" {
		return ""
	}
	return filepath.Clean(savePath)
}

func normalizeIndexedRelativePath(relPath string) (string, error) {
	relPath = strings.TrimSpace(strings.ReplaceAll(relPath, "\\", "/"))
	if relPath == "" || strings.HasPrefix(relPath, "/") || filepath.IsAbs(filepath.FromSlash(relPath)) {
		return "", fmt.Errorf("invalid device index relative path: %s", relPath)
	}
	relPath = path.Clean(relPath)
	if relPath == "." || relPath == ".." || strings.HasPrefix(relPath, "../") {
		return "", fmt.Errorf("invalid device index relative path: %s", relPath)
	}
	return relPath, nil
}

func quarantineDeviceIndex(indexPath string) error {
	quarantinePath := fmt.Sprintf("%s.invalid-%d", indexPath, time.Now().UnixNano())
	return os.Rename(indexPath, quarantinePath)
}

func writeDeviceIndexAtomic(indexPath string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(filepath.Dir(indexPath), ".device-index-*.tmp")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tempFile.Close()
		}
		_ = os.Remove(tempPath)
	}()
	if _, err := tempFile.Write(content); err != nil {
		return err
	}
	if err := tempFile.Sync(); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	closed = true
	if err := os.Chmod(tempPath, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tempPath, indexPath); err != nil {
		return err
	}
	return nil
}
