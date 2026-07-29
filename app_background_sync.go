package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gamesync/internal/core"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	backgroundSyncBusyRetryDelay = time.Second
	backgroundGameExitPollDelay  = 5 * time.Second
)

var errBackgroundSyncBusy = errors.New("sync coordinator is busy")

type backgroundTimer interface {
	Stop() bool
}

type backgroundSyncOutcome struct {
	Checked         bool
	ChangedGameIDs  []string
	ConflictGameIDs []string
}

func (a *App) startBackgroundSyncScheduler() {
	a.backgroundSyncMu.Lock()
	a.backgroundSyncStarted = true
	a.backgroundSyncStopped = false
	a.backgroundSyncMu.Unlock()
	a.requestBackgroundSync("startup")
}

func (a *App) resetBackgroundSyncScheduler() {
	a.backgroundSyncMu.Lock()
	a.backgroundSyncScheduleID++
	if a.backgroundSyncTimer != nil {
		a.backgroundSyncTimer.Stop()
		a.backgroundSyncTimer = nil
	}
	if !a.backgroundSyncStarted {
		a.backgroundSyncMu.Unlock()
		return
	}
	if a.backgroundSyncInterval() == 0 {
		a.backgroundSyncQueued = false
		if a.backgroundDeferredTimer != nil {
			a.backgroundDeferredTimer.Stop()
			a.backgroundDeferredTimer = nil
		}
		clear(a.backgroundDeferredGames)
		a.backgroundSyncMu.Unlock()
		return
	}
	if a.backgroundSyncActive {
		a.backgroundSyncQueued = true
		a.backgroundSyncMu.Unlock()
		return
	}
	a.backgroundSyncMu.Unlock()
	a.requestBackgroundSync("preferences changed")
}

func (a *App) stopBackgroundSyncScheduler() {
	a.backgroundSyncMu.Lock()
	defer a.backgroundSyncMu.Unlock()
	a.backgroundSyncStopped = true
	a.backgroundSyncStarted = false
	a.backgroundSyncQueued = false
	a.backgroundSyncScheduleID++
	if a.backgroundSyncTimer != nil {
		a.backgroundSyncTimer.Stop()
		a.backgroundSyncTimer = nil
	}
	if a.backgroundDeferredTimer != nil {
		a.backgroundDeferredTimer.Stop()
		a.backgroundDeferredTimer = nil
	}
	clear(a.backgroundDeferredGames)
}

func (a *App) backgroundSyncInterval() time.Duration {
	if a.store == nil {
		return 0
	}
	seconds := a.store.Snapshot().Preferences.BackgroundSyncIntervalSeconds
	if !core.IsValidBackgroundSyncInterval(seconds) {
		seconds = core.DefaultBackgroundSyncIntervalSeconds
	}
	return time.Duration(seconds) * time.Second
}

func (a *App) requestBackgroundSync(reason string) {
	a.backgroundSyncMu.Lock()
	if !a.backgroundSyncStarted || a.backgroundSyncStopped || a.backgroundSyncInterval() == 0 {
		a.backgroundSyncMu.Unlock()
		return
	}
	if a.backgroundSyncActive {
		a.backgroundSyncQueued = true
		a.backgroundSyncMu.Unlock()
		return
	}
	a.backgroundSyncActive = true
	if a.backgroundSyncTimer != nil {
		a.backgroundSyncTimer.Stop()
		a.backgroundSyncTimer = nil
	}
	a.backgroundSyncMu.Unlock()
	go a.executeBackgroundSync(reason)
}

func (a *App) executeBackgroundSync(reason string) {
	outcome, err := a.runBackgroundSyncOnce(reason)
	retrySoon := errors.Is(err, errBackgroundSyncBusy)
	if err != nil && !retrySoon && !errors.Is(err, errStorageMigrationInProgress) {
		if a.store != nil {
			_ = a.store.MarkCatalogCheckFailed(err.Error())
			a.emitStateUpdated()
		}
		a.emitBackgroundSyncState("offline", err.Error(), outcome.ChangedGameIDs, outcome.ConflictGameIDs)
	} else if err == nil && outcome.Checked {
		a.emitBackgroundSyncState("succeeded", backgroundSyncSuccessMessage(outcome), outcome.ChangedGameIDs, outcome.ConflictGameIDs)
	}
	a.finishBackgroundSync(retrySoon)
}

func (a *App) finishBackgroundSync(retrySoon bool) {
	a.backgroundSyncMu.Lock()
	a.backgroundSyncActive = false
	if a.backgroundSyncStopped || a.backgroundSyncInterval() == 0 {
		a.backgroundSyncQueued = false
		a.backgroundSyncMu.Unlock()
		return
	}
	if a.backgroundSyncQueued {
		a.backgroundSyncQueued = false
		a.backgroundSyncActive = true
		a.backgroundSyncMu.Unlock()
		go a.executeBackgroundSync("queued")
		return
	}
	delay := a.backgroundSyncInterval()
	if retrySoon {
		delay = backgroundSyncBusyRetryDelay
	}
	a.scheduleBackgroundSyncLocked(delay)
	a.backgroundSyncMu.Unlock()
}

func (a *App) scheduleBackgroundSyncLocked(delay time.Duration) {
	if delay <= 0 || a.backgroundSyncStopped {
		return
	}
	a.backgroundSyncScheduleID++
	scheduleID := a.backgroundSyncScheduleID
	afterFn := a.backgroundSyncAfterFn
	if afterFn == nil {
		afterFn = func(delay time.Duration, callback func()) backgroundTimer {
			return time.AfterFunc(delay, callback)
		}
	}
	a.backgroundSyncTimer = afterFn(delay, func() {
		a.backgroundSyncMu.Lock()
		if a.backgroundSyncStopped || scheduleID != a.backgroundSyncScheduleID {
			a.backgroundSyncMu.Unlock()
			return
		}
		a.backgroundSyncTimer = nil
		a.backgroundSyncMu.Unlock()
		a.requestBackgroundSync("scheduled")
	})
}

func (a *App) runBackgroundSyncOnce(_ string) (backgroundSyncOutcome, error) {
	if err := a.ensureReady(); err != nil {
		return backgroundSyncOutcome{}, err
	}
	finishRemote, err := a.beginRemoteOperation()
	if err != nil {
		return backgroundSyncOutcome{}, err
	}
	defer finishRemote()
	if !a.syncCoordinatorMu.TryLock() {
		return backgroundSyncOutcome{}, errBackgroundSyncBusy
	}
	defer a.syncCoordinatorMu.Unlock()

	a.emitBackgroundSyncState("checking", "正在检查云端更新", nil, nil)
	changed, err := a.pullRemoteCatalog()
	if err != nil {
		return backgroundSyncOutcome{}, err
	}
	if changed {
		if refreshErr := a.refreshAllSyncTracking(false); refreshErr != nil {
			return backgroundSyncOutcome{}, refreshErr
		}
		a.emitStateUpdated()
	}

	state := a.store.Snapshot()
	if len(state.Accounts) == 0 {
		return backgroundSyncOutcome{}, nil
	}
	primary, err := findPrimaryAccount(state)
	if err != nil {
		return backgroundSyncOutcome{}, nil
	}
	catalog, err := a.catalogStoreFor(primary)
	if err != nil {
		return backgroundSyncOutcome{}, err
	}
	heads, err := loadRemoteManifestHeads(a, catalog, state.Games)
	if err != nil {
		return backgroundSyncOutcome{}, err
	}
	index, err := a.ensureDeviceIndex()
	if err != nil {
		return backgroundSyncOutcome{}, err
	}
	candidates := a.backgroundSyncCandidates(state, index, heads)
	if len(candidates) == 0 {
		if a.store.HasPendingCatalogSync() {
			if err := a.syncRemoteCatalog(); err != nil {
				return backgroundSyncOutcome{}, err
			}
			a.emitStateUpdated()
		} else if state.CatalogSync.LastError != "" {
			if err := a.store.MarkCatalogSynced(a.store.LastKnownCatalogRevision()); err != nil {
				return backgroundSyncOutcome{}, err
			}
			a.emitStateUpdated()
		}
		return backgroundSyncOutcome{Checked: true}, nil
	}

	running := a.runningGameIDs(candidates)
	requests := make([]core.SyncRunRequest, 0, len(candidates))
	changedGameIDs := make([]string, 0, len(candidates))
	for _, game := range candidates {
		if running[game.ID] {
			a.deferBackgroundGame(game.ID)
			continue
		}
		requests = append(requests, core.SyncRunRequest{GameID: game.ID})
		changedGameIDs = append(changedGameIDs, game.ID)
	}
	if len(requests) == 0 {
		return backgroundSyncOutcome{Checked: true}, nil
	}
	outcome := backgroundSyncOutcome{Checked: true, ChangedGameIDs: changedGameIDs}
	a.emitBackgroundSyncState("syncing", "正在同步云端存档", changedGameIDs, nil)
	batch, err := a.runSyncBatchLocked(requests, syncBatchOptions{skipCatalogPull: true, forceManualConflict: true})
	if err != nil {
		return outcome, err
	}
	if batch.Catalog.Status == "failed" {
		message := strings.TrimSpace(batch.Catalog.Message)
		if message == "" {
			message = "云端目录同步失败"
		}
		return outcome, errors.New(message)
	}
	conflictGameIDs := make([]string, 0)
	for _, result := range batch.Saves {
		if result.Status == "conflict" {
			conflictGameIDs = append(conflictGameIDs, result.GameID)
		}
	}
	outcome.ConflictGameIDs = conflictGameIDs
	a.refreshObservedManifestTokens(catalog, batch.Saves)
	a.emitStateUpdated()
	return outcome, nil
}

func loadRemoteManifestHeads(a *App, catalog core.CatalogStore, games []core.Game) ([]core.RemoteManifestHead, error) {
	if lister, ok := catalog.(core.RemoteManifestHeadLister); ok {
		return lister.ListRemoteManifestHeads(a.syncContext())
	}
	heads := make([]core.RemoteManifestHead, 0, len(games))
	for _, game := range games {
		record, err := catalog.LoadRemoteManifest(a.syncContext(), game.ID)
		if err != nil {
			return nil, err
		}
		if record.Version == 0 && len(record.Manifest.Files) == 0 {
			continue
		}
		heads = append(heads, core.RemoteManifestHead{
			GameID:          game.ID,
			Version:         record.Version,
			Token:           fmt.Sprintf("manifest:%d:%s", record.Version, strings.TrimSpace(record.Manifest.Hash)),
			UpdatedAt:       record.UpdatedAt,
			UpdatedByDevice: record.UpdatedByDevice,
		})
	}
	return heads, nil
}

func (a *App) backgroundSyncCandidates(state core.AppState, index *core.DeviceIndexStore, heads []core.RemoteManifestHead) []core.Game {
	headByGame := make(map[string]core.RemoteManifestHead, len(heads))
	for _, head := range heads {
		if gameID := strings.TrimSpace(head.GameID); gameID != "" {
			headByGame[gameID] = head
		}
	}
	candidates := make([]core.Game, 0)
	for _, game := range state.Games {
		if !game.Sync.Enabled || strings.TrimSpace(game.SavePath) == "" || (game.LastSync != nil && game.LastSync.Status == "conflict") {
			continue
		}
		indexed, exists := index.Game(game.ID)
		localChanged := !exists || indexed.ScanState != core.ScanStateClean
		head, remoteExists := headByGame[game.ID]
		remoteChanged := false
		if remoteExists {
			switch {
			case indexed.RemoteManifestToken != "":
				remoteChanged = indexed.RemoteManifestToken != head.Token
			case head.Version > 0 && indexed.RemoteVersion == head.Version:
				_ = index.UpdateRemoteManifestToken(game.ID, head.Token)
			case head.Token != "":
				remoteChanged = true
			}
		} else if exists && (indexed.RemoteVersion > 0 || indexed.RemoteManifestToken != "") {
			remoteChanged = true
		}
		if localChanged || remoteChanged {
			candidates = append(candidates, game)
		}
	}
	return candidates
}

func (a *App) refreshObservedManifestTokens(catalog core.CatalogStore, results []core.SyncGameResult) {
	if len(results) == 0 {
		return
	}
	heads, err := loadRemoteManifestHeads(a, catalog, a.store.Snapshot().Games)
	if err != nil {
		if a.ctx != nil {
			wailsruntime.LogWarningf(a.ctx, "refresh remote manifest tokens failed: %v", err)
		}
		return
	}
	headByGame := make(map[string]string, len(heads))
	for _, head := range heads {
		headByGame[head.GameID] = head.Token
	}
	index, err := a.ensureDeviceIndex()
	if err != nil {
		return
	}
	for _, result := range results {
		if result.Status == "success" || result.Status == "conflict" {
			_ = index.UpdateRemoteManifestToken(result.GameID, headByGame[result.GameID])
		}
	}
}

func (a *App) runningGameIDs(games []core.Game) map[string]bool {
	detector := a.runningGameIDsFn
	if detector == nil {
		detector = core.RunningGameIDs
	}
	running := detector(games)
	if running == nil {
		running = make(map[string]bool)
	}
	a.runningGamesMu.Lock()
	for gameID := range a.runningGames {
		running[gameID] = true
	}
	a.runningGamesMu.Unlock()
	return running
}

func (a *App) markGameRunning(gameID string, running bool) {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return
	}
	a.runningGamesMu.Lock()
	if running {
		a.runningGames[gameID] = true
	} else {
		delete(a.runningGames, gameID)
	}
	a.runningGamesMu.Unlock()
}

func (a *App) deferBackgroundGame(gameID string) {
	a.backgroundSyncMu.Lock()
	defer a.backgroundSyncMu.Unlock()
	if a.backgroundSyncStopped || a.backgroundSyncInterval() == 0 {
		return
	}
	a.backgroundDeferredGames[gameID] = true
	if a.backgroundDeferredTimer != nil {
		return
	}
	afterFn := a.backgroundSyncAfterFn
	if afterFn == nil {
		afterFn = func(delay time.Duration, callback func()) backgroundTimer { return time.AfterFunc(delay, callback) }
	}
	a.backgroundDeferredTimer = afterFn(backgroundGameExitPollDelay, a.checkDeferredBackgroundGames)
}

func (a *App) checkDeferredBackgroundGames() {
	a.backgroundSyncMu.Lock()
	a.backgroundDeferredTimer = nil
	if a.backgroundSyncStopped || a.backgroundSyncInterval() == 0 {
		clear(a.backgroundDeferredGames)
		a.backgroundSyncMu.Unlock()
		return
	}
	deferredIDs := make(map[string]bool, len(a.backgroundDeferredGames))
	for gameID := range a.backgroundDeferredGames {
		deferredIDs[gameID] = true
	}
	a.backgroundSyncMu.Unlock()

	state := a.store.Snapshot()
	games := make([]core.Game, 0, len(deferredIDs))
	for _, game := range state.Games {
		if deferredIDs[game.ID] {
			games = append(games, game)
		}
	}
	running := a.runningGameIDs(games)
	exited := false
	a.backgroundSyncMu.Lock()
	for gameID := range deferredIDs {
		if !running[gameID] {
			delete(a.backgroundDeferredGames, gameID)
			exited = true
		}
	}
	remaining := len(a.backgroundDeferredGames) > 0
	if remaining && !a.backgroundSyncStopped {
		afterFn := a.backgroundSyncAfterFn
		if afterFn == nil {
			afterFn = func(delay time.Duration, callback func()) backgroundTimer { return time.AfterFunc(delay, callback) }
		}
		a.backgroundDeferredTimer = afterFn(backgroundGameExitPollDelay, a.checkDeferredBackgroundGames)
	}
	a.backgroundSyncMu.Unlock()
	if exited {
		a.requestBackgroundSync("game exited")
	}
}

func (a *App) emitBackgroundSyncState(status string, message string, changedGameIDs []string, conflictGameIDs []string) {
	changedGameIDs = append([]string{}, changedGameIDs...)
	conflictGameIDs = append([]string{}, conflictGameIDs...)
	sort.Strings(changedGameIDs)
	sort.Strings(conflictGameIDs)
	a.emitRuntimeEvent("sync:background_state", map[string]any{
		"status":          strings.TrimSpace(status),
		"message":         strings.TrimSpace(message),
		"checkedAt":       time.Now(),
		"changedGameIds":  changedGameIDs,
		"conflictGameIds": conflictGameIDs,
	})
}

func backgroundSyncSuccessMessage(outcome backgroundSyncOutcome) string {
	if len(outcome.ConflictGameIDs) > 0 {
		return "部分存档存在冲突，等待手动处理"
	}
	if len(outcome.ChangedGameIDs) > 0 {
		return "后台同步完成"
	}
	return "云端内容已是最新"
}
