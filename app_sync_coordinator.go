package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gamesync/internal/core"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const maxManifestCASAttempts = 3

func (a *App) RunSyncAll() (core.SyncBatchResult, error) {
	if err := a.ensureReady(); err != nil {
		return core.SyncBatchResult{}, err
	}
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return core.SyncBatchResult{}, err
	}
	defer finish()
	return a.runSyncBatch(nil)
}

func (a *App) runSyncBatch(requests []core.SyncRunRequest) (core.SyncBatchResult, error) {
	a.syncCoordinatorMu.Lock()
	defer a.syncCoordinatorMu.Unlock()
	result := core.SyncBatchResult{
		Catalog: core.SyncCatalogResult{Status: "success"},
		Covers:  []core.SyncCoverResult{},
		Saves:   []core.SyncGameResult{},
	}

	changed, pullErr := a.pullRemoteCatalog()
	if pullErr != nil {
		result.Catalog.Status = "failed"
		result.Catalog.Message = pullErr.Error()
	} else if changed {
		a.emitStateUpdated()
	}
	if err := a.refreshAllSyncTracking(false); err != nil && a.ctx != nil {
		wailsruntime.LogWarningf(a.ctx, "refresh save tracking before sync failed: %v", err)
	}

	targets, err := a.syncTargets(requests)
	if err != nil {
		return core.SyncBatchResult{}, err
	}
	for _, target := range targets {
		coverResult := a.syncCoverForCoordinator(target.GameID)
		result.Covers = append(result.Covers, coverResult)
	}

	for _, target := range targets {
		saveResult, stats := a.syncSaveForCoordinator(target)
		result.Saves = append(result.Saves, saveResult)
		addSyncResourceStats(&result.Stats, stats)
	}

	if pushErr := a.syncRemoteCatalog(); pushErr != nil {
		result.Catalog.Status = "failed"
		result.Catalog.Message = pushErr.Error()
	} else if result.Catalog.Status != "failed" {
		result.Catalog.Message = "云端目录已同步"
	}
	result.Catalog.Revision = a.store.LastKnownCatalogRevision()

	snapshot, snapshotErr := a.snapshot()
	if snapshotErr != nil {
		return core.SyncBatchResult{}, snapshotErr
	}
	result.Snapshot = snapshot
	return result, nil
}

func (a *App) syncTargets(requests []core.SyncRunRequest) ([]core.SyncRunRequest, error) {
	state := a.store.Snapshot()
	if requests == nil {
		result := make([]core.SyncRunRequest, 0, len(state.Games))
		for _, game := range state.Games {
			result = append(result, core.SyncRunRequest{GameID: game.ID})
		}
		return result, nil
	}
	result := make([]core.SyncRunRequest, 0, len(requests))
	for _, request := range requests {
		request.GameID = strings.TrimSpace(request.GameID)
		if request.GameID == "" {
			return nil, errors.New(msgGameIDRequired)
		}
		if _, err := findGame(state, request.GameID); err != nil {
			return nil, err
		}
		result = append(result, request)
	}
	return result, nil
}

func (a *App) syncCoverForCoordinator(gameID string) core.SyncCoverResult {
	state := a.store.Snapshot()
	game, err := findGame(state, gameID)
	if err != nil {
		return core.SyncCoverResult{GameID: gameID, Status: "pending", Message: err.Error()}
	}
	result := core.SyncCoverResult{GameID: game.ID, GameName: game.Name, Status: "skipped"}
	if strings.TrimSpace(game.CoverPath) == "" && strings.TrimSpace(game.CoverSource) == "" &&
		strings.TrimSpace(game.CoverLocalPath) == "" && strings.TrimSpace(game.CoverCloudKey) == "" {
		return result
	}
	if target, ok := selectCoverStorageAccount(state, game); ok {
		accountID, objectKey := coverCloudLocation(game)
		if accountID == target.ID && objectKey != "" {
			if index, indexErr := a.ensureDeviceIndex(); indexErr == nil {
				if indexed, exists := index.Game(game.ID); exists && indexed.Cover == (core.DeviceCoverIndex{
					SourceFingerprint: coverFingerprintFromObjectKey(objectKey),
					AccountID:         accountID,
					ObjectKey:         objectKey,
				}) && indexed.Cover.SourceFingerprint != "" {
					return result
				}
			}
		}
	}

	beforeAccount, beforeKey := coverCloudLocation(game)
	a.emitSyncProgress(game.ID, msgCoverSyncing)
	if err := a.syncGameCover(state, game); err != nil {
		result.Status = "pending"
		result.Message = err.Error()
		return result
	}
	updated, err := findGame(a.store.Snapshot(), game.ID)
	if err != nil {
		result.Status = "pending"
		result.Message = err.Error()
		return result
	}
	accountID, objectKey := coverCloudLocation(updated)
	if accountID == "" || objectKey == "" {
		result.Status = "pending"
		result.Message = msgCoverLocalOnlyNoCloudAccount
		return result
	}
	if accountID != beforeAccount || objectKey != beforeKey {
		result.Status = "uploaded"
	}
	if index, indexErr := a.ensureDeviceIndex(); indexErr == nil {
		_, _ = index.UpdateCover(updated.ID, core.DeviceCoverIndex{
			SourceFingerprint: coverFingerprintFromObjectKey(objectKey),
			AccountID:         accountID,
			ObjectKey:         objectKey,
		})
	}
	return result
}

func (a *App) syncPendingGameCovers() []core.SyncCoverResult {
	state := a.store.Snapshot()
	results := make([]core.SyncCoverResult, 0, len(state.Games))
	for _, game := range state.Games {
		results = append(results, a.syncCoverForCoordinator(game.ID))
	}
	return results
}

func coverFingerprintFromObjectKey(objectKey string) string {
	base := strings.TrimSuffix(filepath.Base(strings.TrimSpace(objectKey)), filepath.Ext(objectKey))
	if len(base) != 64 {
		return ""
	}
	for _, char := range strings.ToLower(base) {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return ""
		}
	}
	return strings.ToLower(base)
}

func (a *App) updateCoverIndexForGame(game core.Game) {
	index, err := a.ensureDeviceIndex()
	if err != nil {
		return
	}
	accountID, objectKey := coverCloudLocation(game)
	_, _ = index.UpdateCover(game.ID, core.DeviceCoverIndex{
		SourceFingerprint: coverFingerprintFromObjectKey(objectKey),
		AccountID:         accountID,
		ObjectKey:         objectKey,
	})
}

func (a *App) syncSaveForCoordinator(request core.SyncRunRequest) (core.SyncGameResult, core.SyncResourceStats) {
	request.GameID = strings.TrimSpace(request.GameID)
	unlock := a.lockGameSync(request.GameID)
	defer unlock()

	state := a.store.Snapshot()
	game, findErr := findGame(state, request.GameID)
	if findErr != nil {
		return core.SyncGameResult{GameID: request.GameID, Status: "failed", Message: findErr.Error()}, core.SyncResourceStats{}
	}
	startedAt := time.Now()
	a.emitSyncProgress(game.ID, msgSyncPreparing)

	if !game.Sync.Enabled {
		summary := core.SyncSummary{Status: "disabled", Message: "该游戏的同步已禁用。", SyncedAt: time.Now()}
		return a.persistCoordinatorSyncResult(game, "", game.Anchor, summary, startedAt), core.SyncResourceStats{}
	}
	if strings.TrimSpace(game.SavePath) == "" {
		summary := core.SyncSummary{Status: "unconfigured", Message: "当前设备未配置存档目录。", SyncedAt: time.Now()}
		return a.persistCoordinatorSyncResult(game, "", game.Anchor, summary, startedAt), core.SyncResourceStats{}
	}

	game, storageAccount, err := findGameAndAccount(state, game.ID)
	if err != nil {
		summary := failedSyncSummary(err)
		return a.persistCoordinatorSyncResult(game, "", game.Anchor, summary, startedAt), core.SyncResourceStats{}
	}
	primaryAccount, err := findPrimaryAccount(state)
	if err != nil {
		summary := failedSyncSummary(err)
		return a.persistCoordinatorSyncResult(game, storageAccount.ID, game.Anchor, summary, startedAt), core.SyncResourceStats{}
	}
	gateway, err := a.storageGatewayFor(a.syncContext(), primaryAccount, storageAccount)
	if err != nil {
		summary := failedSyncSummary(err)
		return a.persistCoordinatorSyncResult(game, storageAccount.ID, game.Anchor, summary, startedAt), core.SyncResourceStats{}
	}
	if err := gateway.Catalog.EnsureSchema(a.syncContext()); err != nil {
		summary := failedSyncSummary(err)
		return a.persistCoordinatorSyncResult(game, storageAccount.ID, game.Anchor, summary, startedAt), core.SyncResourceStats{}
	}
	remoteRecord, err := gateway.Catalog.LoadRemoteManifest(a.syncContext(), game.ID)
	if err != nil {
		summary := failedSyncSummary(err)
		return a.persistCoordinatorSyncResult(game, storageAccount.ID, game.Anchor, summary, startedAt), core.SyncResourceStats{}
	}

	index, err := a.ensureDeviceIndex()
	if err != nil {
		summary := failedSyncSummary(err)
		return a.persistCoordinatorSyncResult(game, storageAccount.ID, game.Anchor, summary, startedAt), core.SyncResourceStats{}
	}
	indexedGame, err := index.ConfigureGame(game.ID, game.SavePath, game.Sync.IncludePatterns, game.Sync.ExcludePatterns)
	if err != nil {
		summary := failedSyncSummary(err)
		return a.persistCoordinatorSyncResult(game, storageAccount.ID, game.Anchor, summary, startedAt), core.SyncResourceStats{}
	}
	buildResult, buildErr := core.BuildIncrementalManifest(
		game.SavePath, game.Sync.IncludePatterns, game.Sync.ExcludePatterns,
		indexedGame.Files, indexedGame.ScanState, indexedGame.DirtyPaths,
	)
	if errors.Is(buildErr, core.ErrManifestRebuildRequired) {
		_ = index.MarkRebuild(game.ID)
		indexedGame, _ = index.Game(game.ID)
		buildResult, buildErr = core.BuildIncrementalManifest(
			game.SavePath, game.Sync.IncludePatterns, game.Sync.ExcludePatterns,
			indexedGame.Files, core.ScanStateRebuild, nil,
		)
	}
	stats := core.SyncResourceStats{
		StattedFiles: buildResult.Stats.StattedFiles,
		HashedFiles:  buildResult.Stats.HashedFiles,
	}
	if indexedGame.ScanState == core.ScanStateRebuild {
		stats.EnumeratedGames = 1
	}
	if buildErr != nil {
		if errors.Is(buildErr, core.ErrManifestSourceChanged) {
			_ = index.MarkRebuild(game.ID)
		}
		summary := failedSyncSummary(buildErr)
		return a.persistCoordinatorSyncResult(game, storageAccount.ID, game.Anchor, summary, startedAt), stats
	}
	if indexedGame.ScanState == core.ScanStateClean && !indexedGame.GeneratedAt.IsZero() {
		buildResult.Manifest.GeneratedAt = indexedGame.GeneratedAt
	}

	choice := resolveSyncConflictChoice(game, state.Preferences, request.ConflictChoice)
	var summary core.SyncSummary
	anchor := game.Anchor
	for attempt := 0; attempt < maxManifestCASAttempts; attempt++ {
		summary, anchor, err = a.engine.SyncGameWithPreparedManifest(
			a.syncContext(), state.Device, game, gateway, choice, buildResult.Manifest, remoteRecord,
			func(message string) { a.emitSyncProgress(game.ID, message) },
		)
		if !errors.Is(err, core.ErrRemoteManifestChanged) || attempt == maxManifestCASAttempts-1 {
			break
		}
		remoteRecord, err = gateway.Catalog.LoadRemoteManifest(a.syncContext(), game.ID)
		if err != nil {
			break
		}
	}
	if err != nil {
		if errors.Is(err, core.ErrLocalFileChanged) {
			_ = index.MarkRebuild(game.ID)
		}
		anchor = game.Anchor
		summary = failedSyncSummary(err)
	} else if summary.Status == "success" {
		if _, commitErr := index.CommitGame(game.ID, anchor.LastRemoteVersion, anchor.LastManifest.Hash, anchor.LastManifest); commitErr != nil {
			_ = index.MarkRebuild(game.ID)
			summary = failedSyncSummary(commitErr)
		}
	}
	stats.UploadedObjects += summary.Uploaded
	stats.DownloadedObjects += summary.Downloaded
	return a.persistCoordinatorSyncResult(game, storageAccount.ID, anchor, summary, startedAt), stats
}

func failedSyncSummary(err error) core.SyncSummary {
	message := "同步失败"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	return core.SyncSummary{Status: "failed", Message: message, SyncedAt: time.Now()}
}

func (a *App) persistCoordinatorSyncResult(game core.Game, accountID string, anchor core.SyncAnchor, summary core.SyncSummary, startedAt time.Time) core.SyncGameResult {
	if summary.SyncedAt.IsZero() {
		summary.SyncedAt = time.Now()
	}
	if err := a.store.UpdateGameSync(game.ID, anchor, summary); err != nil {
		summary = failedSyncSummary(err)
	}
	endedAt := time.Now()
	_ = a.store.RecordActivity(core.SyncActivity{
		ID: core.NewID(), GameID: game.ID, GameName: game.Name, AccountID: accountID,
		Status: summary.Status, Message: summary.Message, Uploaded: summary.Uploaded,
		Downloaded: summary.Downloaded, Conflicts: summary.Conflicts, StartedAt: startedAt, EndedAt: &endedAt,
	})
	a.emitSyncProgress(game.ID, summary.Message)
	return core.SyncGameResult{
		GameID: game.ID, GameName: game.Name, Status: summary.Status, Message: summary.Message,
		Uploaded: summary.Uploaded, Downloaded: summary.Downloaded, Conflicts: summary.Conflicts,
	}
}

func addSyncResourceStats(target *core.SyncResourceStats, addition core.SyncResourceStats) {
	if target == nil {
		return
	}
	target.EnumeratedGames += addition.EnumeratedGames
	target.StattedFiles += addition.StattedFiles
	target.HashedFiles += addition.HashedFiles
	target.UploadedObjects += addition.UploadedObjects
	target.DownloadedObjects += addition.DownloadedObjects
}

func (a *App) ensureDeviceIndex() (*core.DeviceIndexStore, error) {
	a.syncInfraMu.Lock()
	defer a.syncInfraMu.Unlock()
	if a.deviceIndex != nil {
		return a.deviceIndex, nil
	}
	if a.store == nil {
		return nil, errors.New("store is not initialized")
	}
	index, err := core.NewDeviceIndexStore(a.store.DataDir(), a.store.Snapshot().Device.ID)
	if err != nil {
		return nil, err
	}
	a.deviceIndex = index
	return index, nil
}

func (a *App) startSyncTracking() error {
	index, err := a.ensureDeviceIndex()
	if err != nil {
		return err
	}
	a.syncInfraMu.Lock()
	if a.saveChangeTracker != nil {
		a.syncInfraMu.Unlock()
		return nil
	}
	tracker, err := core.NewSaveChangeTracker(func(event core.SaveChangeEvent) {
		if event.Rebuild {
			_ = index.MarkRebuild(event.GameID)
			return
		}
		if len(event.DirtyPaths) > 0 {
			_ = index.MarkDirty(event.GameID, event.DirtyPaths...)
		}
	})
	if err != nil {
		a.syncInfraMu.Unlock()
		return err
	}
	a.saveChangeTracker = tracker
	a.syncInfraMu.Unlock()
	return a.refreshAllSyncTracking(true)
}

func (a *App) refreshAllSyncTracking(markOfflineChanges bool) error {
	if a.store == nil {
		return nil
	}
	for _, game := range a.store.Snapshot().Games {
		if err := a.refreshGameSyncTracking(game, markOfflineChanges); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) refreshGameSyncTracking(game core.Game, markOfflineChanges bool) error {
	index, err := a.ensureDeviceIndex()
	if err != nil {
		return err
	}
	if _, err := index.ConfigureGame(game.ID, game.SavePath, game.Sync.IncludePatterns, game.Sync.ExcludePatterns); err != nil {
		return err
	}
	if markOfflineChanges && strings.TrimSpace(game.SavePath) != "" {
		if err := index.MarkRebuild(game.ID); err != nil {
			return err
		}
	}
	a.syncInfraMu.Lock()
	tracker := a.saveChangeTracker
	a.syncInfraMu.Unlock()
	if tracker == nil {
		return nil
	}
	if strings.TrimSpace(game.SavePath) == "" {
		if err := tracker.UnregisterGame(game.ID); err != nil && !errors.Is(err, core.ErrSaveChangeTrackerClosed) {
			return err
		}
		return nil
	}
	return tracker.UpdateGame(game.ID, game.SavePath, core.SyncConfigFingerprint(game.Sync.IncludePatterns, game.Sync.ExcludePatterns))
}

func (a *App) removeGameSyncTracking(gameID string) {
	a.syncInfraMu.Lock()
	tracker := a.saveChangeTracker
	index := a.deviceIndex
	a.syncInfraMu.Unlock()
	if tracker != nil {
		_ = tracker.UnregisterGame(gameID)
	}
	if index != nil {
		_ = index.RemoveGame(gameID)
	}
}

func (a *App) closeSyncTracking() {
	a.syncInfraMu.Lock()
	tracker := a.saveChangeTracker
	a.saveChangeTracker = nil
	a.syncInfraMu.Unlock()
	if tracker != nil {
		if err := tracker.Close(); err != nil && a.ctx != nil {
			wailsruntime.LogErrorf(a.ctx, "close save change tracker failed: %v", err)
		}
	}
}

func (a *App) logCoverSyncFailures(results []core.SyncCoverResult) {
	if a.ctx == nil {
		return
	}
	for _, result := range results {
		if result.Status == "pending" && strings.TrimSpace(result.Message) != "" {
			wailsruntime.LogWarningf(a.ctx, "cover sync pending for %s: %s", result.GameID, result.Message)
		}
	}
}

func syncBatchFailureMessage(result core.SyncBatchResult) string {
	if result.Catalog.Status == "failed" {
		return result.Catalog.Message
	}
	for _, save := range result.Saves {
		if save.Status == "failed" {
			return fmt.Sprintf("%s: %s", save.GameName, save.Message)
		}
	}
	return ""
}
