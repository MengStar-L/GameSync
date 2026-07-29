package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gamesync/internal/core"
)

const (
	storageSwitchCompleted = "completed"
	storageSwitchPaused    = "paused"
	storageSwitchRetryable = "retryable"
)

var errPendingStorageHandoff = errors.New("检测到已提交的存储交接，但目标连接尚不可用")
var errStorageMigrationInProgress = errors.New("存储迁移尚未完成，请先继续或取消切换")

func (a *App) beginRemoteOperation() (func(), error) {
	a.handoffMu.Lock()
	a.remoteOpsMu.RLock()
	if a.store != nil && a.store.Snapshot().StorageMigration != nil {
		a.remoteOpsMu.RUnlock()
		a.handoffMu.Unlock()
		return nil, errStorageMigrationInProgress
	}
	err := a.followCommittedStorageHandoffs()
	a.handoffMu.Unlock()
	if err != nil {
		a.remoteOpsMu.RUnlock()
		return nil, err
	}
	return a.remoteOpsMu.RUnlock, nil
}

func (a *App) beginLocalAccountMutation() (func(), error) {
	a.handoffMu.Lock()
	a.remoteOpsMu.Lock()
	if a.store != nil && a.store.Snapshot().StorageMigration != nil {
		a.remoteOpsMu.Unlock()
		a.handoffMu.Unlock()
		return nil, errStorageMigrationInProgress
	}
	a.handoffMu.Unlock()
	return a.remoteOpsMu.Unlock, nil
}

func (a *App) followCommittedStorageHandoffs() error {
	const maxHops = 8
	visited := make(map[string]bool)
	for hop := 0; hop < maxHops; hop++ {
		state := a.store.Snapshot()
		primary, err := findPrimaryAccount(state)
		if err != nil {
			return nil
		}
		if visited[primary.ID] {
			return fmt.Errorf("%w: storage handoff loop", errPendingStorageHandoff)
		}
		visited[primary.ID] = true
		sourceCatalog, err := a.catalogStoreFor(primary)
		if err != nil {
			return err
		}
		handoff, err := sourceCatalog.LoadStorageHandoff(a.syncContext())
		if err != nil {
			return err
		}
		if handoff.State != core.StorageHandoffCommitted || handoff.Generation <= state.StorageGeneration {
			return nil
		}
		if handoff.SourceAccountID != primary.ID || handoff.TargetAccountID == "" {
			return fmt.Errorf("%w: invalid source handoff", errPendingStorageHandoff)
		}
		if strings.TrimSpace(a.recoveryPassword) == "" {
			return fmt.Errorf("%w: %s", errPendingStorageHandoff, msgRecoveryPasswordRequired)
		}

		sourceRemote, sourceCredentials, err := sourceCatalog.LoadRemoteCatalog(a.syncContext())
		if err != nil {
			return err
		}
		sourceRemote, failures := decryptCatalogCredentials(sourceRemote, sourceCredentials, a.recoveryPassword)
		if credentialErr := failures[handoff.TargetAccountID]; credentialErr != nil {
			return fmt.Errorf("%w: %v", errPendingStorageHandoff, credentialErr)
		}
		target, err := findAccount(core.AppState{Accounts: sourceRemote.Accounts}, handoff.TargetAccountID)
		if err != nil || !hasObjectCredentials(target) || !catalogAccountHasWritableCredentials(target) {
			return fmt.Errorf("%w: target credentials are missing", errPendingStorageHandoff)
		}
		verified, verifyErr := a.verifyStorageAccountFor(a.syncContext(), target)
		if verifyErr != nil || verified.LastError != "" {
			if verifyErr == nil {
				verifyErr = errors.New(verified.LastError)
			}
			return fmt.Errorf("%w: %v", errPendingStorageHandoff, verifyErr)
		}
		verified.ID = target.ID
		verified.IsPrimary = true
		verified.Enabled = true

		targetCatalog, err := a.catalogStoreFor(verified)
		if err != nil {
			return err
		}
		targetRemote, targetCredentials, err := targetCatalog.LoadRemoteCatalog(a.syncContext())
		if err != nil {
			return err
		}
		if targetRemote.Handoff != nil && targetRemote.Handoff.TransactionID == handoff.TransactionID && targetRemote.Handoff.Generation == handoff.Generation && targetRemote.Handoff.State == core.StorageHandoffPrepared {
			if err := targetCatalog.SaveStorageHandoffIfGeneration(a.syncContext(), handoff, handoff.Generation); err != nil {
				return fmt.Errorf("%w: %v", errPendingStorageHandoff, err)
			}
			targetRemote.Handoff = &handoff
			targetRemote.StorageGeneration = handoff.Generation
		}
		if targetRemote.Handoff == nil || targetRemote.Handoff.State != core.StorageHandoffCommitted || targetRemote.Handoff.Generation < handoff.Generation {
			return fmt.Errorf("%w: target handoff is not prepared", errPendingStorageHandoff)
		}
		targetRemote, failures = decryptCatalogCredentials(targetRemote, targetCredentials, a.recoveryPassword)
		if credentialErr := failures[verified.ID]; credentialErr != nil {
			return fmt.Errorf("%w: %v", errPendingStorageHandoff, credentialErr)
		}
		if err := a.store.MergeRemoteCatalog(targetRemote); err != nil {
			return err
		}
		merged := a.store.Snapshot()
		if err := a.store.FollowStorageHandoff(verified, merged.Games, handoff); err != nil {
			return err
		}
		a.emitStateUpdated()
	}
	return fmt.Errorf("%w: too many chained handoffs", errPendingStorageHandoff)
}

func (a *App) catalogStoreFor(account core.CloudflareAccount) (core.CatalogStore, error) {
	if a.catalogStoreFn != nil {
		return a.catalogStoreFn(account)
	}
	return newCatalogStore(account)
}

func (a *App) objectStoreFor(ctx context.Context, account core.CloudflareAccount) (core.ObjectStore, error) {
	if a.objectStoreFn != nil {
		return a.objectStoreFn(ctx, account)
	}
	return newObjectStore(ctx, account)
}

func (a *App) verifyStorageAccountFor(ctx context.Context, account core.CloudflareAccount) (core.CloudflareAccount, error) {
	if a.verifyStorageFn != nil {
		return a.verifyStorageFn(ctx, account)
	}
	return verifyStorageAccount(ctx, account)
}

func (a *App) storageGatewayFor(ctx context.Context, catalogAccount core.CloudflareAccount, objectAccount core.CloudflareAccount) (*core.StorageGateway, error) {
	catalog, err := a.catalogStoreFor(catalogAccount)
	if err != nil {
		return nil, err
	}
	objects, err := a.objectStoreFor(ctx, objectAccount)
	if err != nil {
		return nil, err
	}
	return &core.StorageGateway{Catalog: catalog, Objects: objects}, nil
}

func (a *App) storageSwitchResult(status, transactionID, conflictGameID, message string) core.StorageSwitchResult {
	snapshot, _ := a.snapshot()
	return core.StorageSwitchResult{
		Snapshot:       snapshot,
		Status:         status,
		TransactionID:  strings.TrimSpace(transactionID),
		ConflictGameID: strings.TrimSpace(conflictGameID),
		Message:        strings.TrimSpace(message),
	}
}

func (a *App) beginStorageSwitch() (func(), error) {
	a.switchStorageMu.Lock()
	if a.switchStorageBusy {
		a.switchStorageMu.Unlock()
		return nil, errors.New(msgStorageSwitchBusy)
	}
	a.switchStorageBusy = true
	a.switchStorageMu.Unlock()

	a.handoffMu.Lock()
	a.remoteOpsMu.Lock()
	if a.store != nil && a.store.Snapshot().StorageMigration == nil {
		if err := a.followCommittedStorageHandoffs(); err != nil {
			a.remoteOpsMu.Unlock()
			a.handoffMu.Unlock()
			a.switchStorageMu.Lock()
			a.switchStorageBusy = false
			a.switchStorageMu.Unlock()
			return nil, err
		}
	}
	a.handoffMu.Unlock()
	return func() {
		a.remoteOpsMu.Unlock()
		a.switchStorageMu.Lock()
		a.switchStorageBusy = false
		a.switchStorageMu.Unlock()
	}, nil
}

func (a *App) SwitchStoragePrimary(request core.StorageSwitchRequest) (core.StorageSwitchResult, error) {
	if err := a.ensureReady(); err != nil {
		return core.StorageSwitchResult{}, err
	}
	finish, err := a.beginStorageSwitch()
	if err != nil {
		return core.StorageSwitchResult{}, err
	}
	defer finish()

	if strings.TrimSpace(a.recoveryPassword) == "" {
		return core.StorageSwitchResult{}, errors.New(msgRecoveryPasswordRequired)
	}
	state := a.store.Snapshot()
	if state.StorageMigration != nil {
		return a.storageSwitchResult(storageSwitchRetryable, state.StorageMigration.TransactionID, state.StorageMigration.ConflictGameID, "已有存储迁移尚未完成，请继续或取消该迁移"), nil
	}
	source, err := findPrimaryAccount(state)
	if err != nil {
		return core.StorageSwitchResult{}, err
	}
	target, err := resolveStorageSwitchTarget(state, request)
	if err != nil {
		return core.StorageSwitchResult{}, err
	}

	a.emitStorageSwitchProgress("verify", msgStorageSwitchVerifying, 0, 0)
	verified, verifyErr := a.verifyStorageAccountFor(a.syncContext(), target)
	if verified.LastError != "" {
		return core.StorageSwitchResult{}, fmt.Errorf(msgStorageSwitchVerifyFailed, verified.LastError)
	}
	if verifyErr != nil {
		return core.StorageSwitchResult{}, fmt.Errorf(msgStorageSwitchVerifyFailed, verifyErr)
	}
	if target.ID != "" {
		verified.ID = target.ID
	}
	if verified.ID == "" {
		verified.ID = core.NewID()
	}
	verified.IsPrimary = false
	verified.Enabled = false
	verified.VerificationState = "valid"
	verified, err = a.store.UpsertAccount(verified)
	if err != nil {
		return core.StorageSwitchResult{}, err
	}

	migration := core.StorageMigrationState{
		TransactionID:   core.NewID(),
		SourceAccountID: source.ID,
		TargetAccountID: verified.ID,
		Phase:           core.MigrationPhaseCopying,
		Generation:      state.StorageGeneration + 1,
	}
	if err := a.store.BeginStorageMigration(migration); err != nil {
		return core.StorageSwitchResult{}, err
	}
	return a.continueStorageMigration(migration.TransactionID, request.UseLocalData)
}

func (a *App) ResumeStorageMigration(request core.StorageMigrationResumeRequest) (core.StorageSwitchResult, error) {
	if err := a.ensureReady(); err != nil {
		return core.StorageSwitchResult{}, err
	}
	finish, err := a.beginStorageSwitch()
	if err != nil {
		return core.StorageSwitchResult{}, err
	}
	defer finish()

	request.TransactionID = strings.TrimSpace(request.TransactionID)
	choice := strings.ToLower(strings.TrimSpace(request.ConflictChoice))
	state := a.store.Snapshot()
	if state.StorageMigration == nil || state.StorageMigration.TransactionID != request.TransactionID {
		return core.StorageSwitchResult{}, core.ErrStorageMigrationChanged
	}
	if state.StorageMigration.ConflictGameID != "" && choice != "local" {
		return core.StorageSwitchResult{}, errors.New("存储切换只能取消或明确使用本地数据")
	}
	return a.continueStorageMigration(request.TransactionID, choice == "local")
}

func (a *App) CancelStorageMigration(transactionID string) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}
	finish, err := a.beginStorageSwitch()
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	defer finish()

	transactionID = strings.TrimSpace(transactionID)
	if err := a.store.CancelStorageMigration(transactionID); err != nil {
		return core.DashboardSnapshot{}, err
	}
	_ = os.RemoveAll(filepath.Join(a.store.DataDir(), "migrations", transactionID))
	a.emitStateUpdated()
	return a.snapshot()
}

func (a *App) continueStorageMigration(transactionID string, useLocalData bool) (core.StorageSwitchResult, error) {
	state := a.store.Snapshot()
	migration := state.StorageMigration
	if migration == nil || migration.TransactionID != transactionID {
		return core.StorageSwitchResult{}, core.ErrStorageMigrationChanged
	}
	source, err := findAccount(state, migration.SourceAccountID)
	if err != nil {
		return a.storageSwitchResult(storageSwitchRetryable, transactionID, "", err.Error()), nil
	}
	target, err := findAccount(state, migration.TargetAccountID)
	if err != nil {
		return a.storageSwitchResult(storageSwitchRetryable, transactionID, "", err.Error()), nil
	}

	if migration.Phase == core.MigrationPhaseCopying {
		if result := a.synchronizeMigrationSource(transactionID, source, useLocalData); result != nil {
			return *result, nil
		}
		state = a.store.Snapshot()
		migration = state.StorageMigration

		targetCatalog, catalogErr := a.catalogStoreFor(target)
		if catalogErr != nil {
			return a.retryStorageMigration(transactionID, catalogErr), nil
		}
		if err := targetCatalog.EnsureSchema(a.syncContext()); err != nil {
			return a.retryStorageMigration(transactionID, err), nil
		}
		remoteTarget, _, err := targetCatalog.LoadRemoteCatalog(a.syncContext())
		if err != nil {
			return a.retryStorageMigration(transactionID, err), nil
		}
		if !useLocalData && len(remoteTarget.Games) > 0 {
			conflictGameID := remoteTarget.Games[0].ID
			for _, remoteGame := range remoteTarget.Games {
				if _, findErr := findGame(state, remoteGame.ID); findErr == nil {
					conflictGameID = remoteGame.ID
					break
				}
			}
			message := "目标存储已有数据，请取消切换或明确使用本地数据覆盖目标端"
			_ = a.store.PauseStorageMigration(transactionID, conflictGameID, message)
			return a.storageSwitchResult(storageSwitchPaused, transactionID, conflictGameID, message), nil
		}

		a.emitStorageSwitchProgress("inventory", "正在生成存档、封面和历史备份清单...", 0, 0)
		inventory, manifests, migratedGames, manifestVersions, err := a.buildStorageMigrationInventory(state, source, target)
		if err != nil {
			return a.retryStorageMigration(transactionID, err), nil
		}
		inventory = preserveVerifiedMigrationItems(migration.Items, inventory)
		revision, err := a.loadCatalogRevision(source)
		if err != nil {
			return a.retryStorageMigration(transactionID, err), nil
		}
		localHashes := make(map[string]string, len(manifests))
		for gameID, manifest := range manifests {
			localHashes[gameID] = manifest.Hash
		}
		updated := *migration
		updated.Items = inventory
		updated.TargetGames = migratedGames
		updated.SourceRevision = revision
		updated.SourceManifestVersion = manifestVersions
		updated.LocalManifestHash = localHashes
		updated.ConflictGameID = ""
		updated.LastError = ""
		if err := a.store.BeginStorageMigration(updated); err != nil {
			return a.retryStorageMigration(transactionID, err), nil
		}

		if err := a.copyStorageMigrationItems(updated, source, target); err != nil {
			return a.retryStorageMigration(transactionID, err), nil
		}
		a.emitStorageSwitchProgress("target", "正在发布并复核目标端目录...", 0, 0)
		if err := a.publishPreparedTarget(updated, target, migratedGames, manifests); err != nil {
			return a.retryStorageMigration(transactionID, err), nil
		}
		if err := a.store.MarkStorageMigrationPhase(transactionID, core.MigrationPhaseTargetReady); err != nil {
			return a.retryStorageMigration(transactionID, err), nil
		}
		migration = a.store.Snapshot().StorageMigration
	}

	if migration.Phase == core.MigrationPhaseTargetReady {
		a.emitStorageSwitchProgress("handoff", "正在确认源端未发生变化并提交存储交接...", 0, 0)
		if err := a.revalidateMigrationSource(*migration, source); err != nil {
			restarted := *migration
			restarted.Phase = core.MigrationPhaseCopying
			restarted.SourceRevision = 0
			restarted.SourceManifestVersion = nil
			restarted.LocalManifestHash = nil
			restarted.LastError = err.Error()
			_ = a.store.BeginStorageMigration(restarted)
			return a.retryStorageMigration(transactionID, err), nil
		}
		handoff := storageMigrationHandoff(*migration, a.store.Snapshot().Device.ID, core.StorageHandoffCommitted)
		sourceCatalog, err := a.catalogStoreFor(source)
		if err != nil {
			return a.retryStorageMigration(transactionID, err), nil
		}
		current, err := sourceCatalog.LoadStorageHandoff(a.syncContext())
		if err != nil {
			return a.retryStorageMigration(transactionID, err), nil
		}
		if current.TransactionID != handoff.TransactionID || current.State != core.StorageHandoffCommitted || current.Generation != handoff.Generation {
			if current.Generation != handoff.Generation-1 {
				return a.retryStorageMigration(transactionID, core.ErrStorageHandoffChanged), nil
			}
			if err := sourceCatalog.SaveStorageHandoffIfGeneration(a.syncContext(), handoff, current.Generation); err != nil {
				return a.retryStorageMigration(transactionID, err), nil
			}
		}
		if err := a.store.MarkStorageMigrationPhase(transactionID, core.MigrationPhaseSourceCommitted); err != nil {
			return a.retryStorageMigration(transactionID, err), nil
		}
		migration = a.store.Snapshot().StorageMigration
	}

	if migration.Phase == core.MigrationPhaseSourceCommitted {
		handoff := storageMigrationHandoff(*migration, a.store.Snapshot().Device.ID, core.StorageHandoffCommitted)
		targetCatalog, err := a.catalogStoreFor(target)
		if err != nil {
			return a.storageSwitchResult(storageSwitchRetryable, transactionID, "", err.Error()), nil
		}
		current, err := targetCatalog.LoadStorageHandoff(a.syncContext())
		if err != nil {
			return a.storageSwitchResult(storageSwitchRetryable, transactionID, "", err.Error()), nil
		}
		if current.TransactionID != handoff.TransactionID || current.State != core.StorageHandoffCommitted {
			if current.TransactionID != handoff.TransactionID || current.State != core.StorageHandoffPrepared || current.Generation != handoff.Generation {
				return a.storageSwitchResult(storageSwitchRetryable, transactionID, "", core.ErrStorageHandoffChanged.Error()), nil
			}
			if err := targetCatalog.SaveStorageHandoffIfGeneration(a.syncContext(), handoff, handoff.Generation); err != nil {
				return a.storageSwitchResult(storageSwitchRetryable, transactionID, "", err.Error()), nil
			}
		}

		games := migration.TargetGames
		if len(games) == 0 {
			games = migratedGamesForTarget(a.store.Snapshot().Games, target.ID)
		}
		a.emitStorageSwitchProgress("commit", "正在原子切换本地连接和同步路由...", 0, 0)
		target.Enabled = true
		target.IsPrimary = true
		if err := a.store.CommitStorageMigration(transactionID, target, games, handoff); err != nil {
			return a.storageSwitchResult(storageSwitchRetryable, transactionID, "", err.Error()), nil
		}
		migration = a.store.Snapshot().StorageMigration
		a.emitStateUpdated()
	}

	if migration != nil && migration.Phase == core.MigrationPhaseLocalCommitted {
		a.emitStorageSwitchProgress("sync", "正在新存储上执行首次正常同步...", 0, 0)
		if err := a.synchronizeCommittedTarget(target); err != nil {
			return a.storageSwitchResult(storageSwitchRetryable, transactionID, "", err.Error()), nil
		}
		if err := a.store.FinishStorageMigration(transactionID); err != nil {
			return a.storageSwitchResult(storageSwitchRetryable, transactionID, "", err.Error()), nil
		}
		_ = os.RemoveAll(filepath.Join(a.store.DataDir(), "migrations", transactionID))
	}

	a.emitStorageSwitchProgress("done", msgStorageSwitchDone, 0, 0)
	a.emitStateUpdated()
	return a.storageSwitchResult(storageSwitchCompleted, transactionID, "", msgStorageSwitchDone), nil
}

func (a *App) retryStorageMigration(transactionID string, err error) core.StorageSwitchResult {
	message := "存储迁移可重试"
	if err != nil {
		message = err.Error()
		_ = a.store.PauseStorageMigration(transactionID, "", message)
	}
	return a.storageSwitchResult(storageSwitchRetryable, transactionID, "", message)
}

func (a *App) synchronizeMigrationSource(transactionID string, source core.CloudflareAccount, useLocalData bool) *core.StorageSwitchResult {
	state := a.store.Snapshot()
	for index, game := range state.Games {
		if !game.Sync.Enabled || strings.TrimSpace(game.SavePath) == "" {
			continue
		}
		a.emitStorageSwitchProgress("source_sync", fmt.Sprintf("正在同步源存储中的《%s》...", game.Name), index+1, len(state.Games))
		objectAccount, err := findAccount(state, game.StorageAccountID)
		if err != nil {
			objectAccount = source
		}
		gateway, err := a.storageGatewayFor(a.syncContext(), source, objectAccount)
		if err != nil {
			result := a.retryStorageMigration(transactionID, err)
			return &result
		}
		choice := ""
		if useLocalData {
			choice = "local"
		}
		unlock := a.lockGameSync(game.ID)
		summary, anchor, syncErr := a.engine.SyncGameWithGateway(a.syncContext(), state.Device, game, gateway, choice, func(message string) {
			a.emitSyncProgress(game.ID, message)
		})
		unlock()
		if syncErr != nil {
			result := a.retryStorageMigration(transactionID, syncErr)
			return &result
		}
		if summary.Status == "conflict" {
			message := "源存储同步存在冲突，请取消切换或使用本地数据"
			_ = a.store.PauseStorageMigration(transactionID, game.ID, message)
			result := a.storageSwitchResult(storageSwitchPaused, transactionID, game.ID, message)
			return &result
		}
		if summary.Status == "success" {
			if coverErr := a.syncGameCover(state, game); coverErr != nil {
				result := a.retryStorageMigration(transactionID, fmt.Errorf(msgCoverSyncFailed, coverErr))
				return &result
			}
		}
		if err := a.store.UpdateGameSync(game.ID, anchor, summary); err != nil {
			result := a.retryStorageMigration(transactionID, err)
			return &result
		}
	}
	if err := a.syncRemoteCatalog(); err != nil {
		result := a.retryStorageMigration(transactionID, err)
		return &result
	}
	return nil
}

func (a *App) buildStorageMigrationInventory(state core.AppState, source, target core.CloudflareAccount) ([]core.StorageMigrationItem, map[string]core.SyncManifest, []core.Game, map[string]int, error) {
	items := make([]core.StorageMigrationItem, 0)
	seen := make(map[string]bool)
	manifests := make(map[string]core.SyncManifest)
	manifestVersions := make(map[string]int)
	games := append([]core.Game(nil), state.Games...)
	accountLookup := make(map[string]core.CloudflareAccount, len(state.Accounts))
	for _, account := range state.Accounts {
		accountLookup[account.ID] = account
	}
	sourceCatalog, err := a.catalogStoreFor(source)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	for gameIndex := range games {
		sourceGame := state.Games[gameIndex]
		game := &games[gameIndex]
		if strings.TrimSpace(game.SavePath) != "" {
			manifest, err := core.BuildLocalManifest(game.SavePath, game.Sync.IncludePatterns, game.Sync.ExcludePatterns)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("build migration manifest for %s: %w", game.Name, err)
			}
			manifests[game.ID] = manifest
			for _, file := range manifest.Files {
				item := core.StorageMigrationItem{
					Kind:      "save",
					GameID:    game.ID,
					LocalPath: filepath.Join(game.SavePath, filepath.FromSlash(file.Path)),
					TargetKey: fmt.Sprintf("games/%s/objects/%s", game.ID, file.SHA256),
					SHA256:    file.SHA256,
					Size:      file.Size,
					Status:    core.MigrationItemPending,
				}
				appendMigrationItem(&items, seen, item)
			}
		}
		remoteManifest, err := sourceCatalog.LoadRemoteManifest(a.syncContext(), game.ID)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		manifestVersions[game.ID] = remoteManifest.Version
		if _, ok := manifests[game.ID]; !ok && len(remoteManifest.Manifest.Files) > 0 {
			manifests[game.ID] = remoteManifest.Manifest
			sourceObjectAccountID := strings.TrimSpace(sourceGame.StorageAccountID)
			if sourceObjectAccountID == "" {
				sourceObjectAccountID = source.ID
			}
			for _, file := range remoteManifest.Manifest.Files {
				appendMigrationItem(&items, seen, core.StorageMigrationItem{
					Kind:            "save",
					GameID:          game.ID,
					SourceAccountID: sourceObjectAccountID,
					SourceKey:       fmt.Sprintf("games/%s/objects/%s", game.ID, file.SHA256),
					TargetKey:       fmt.Sprintf("games/%s/objects/%s", game.ID, file.SHA256),
					SHA256:          file.SHA256,
					Size:            file.Size,
					Status:          core.MigrationItemPending,
				})
			}
		}

		if err := a.addCoverMigrationItem(sourceGame, game, target.ID, accountLookup, &items, seen); err != nil {
			return nil, nil, nil, nil, err
		}
		for recordIndex := range sourceGame.BackupRegistry {
			sourceRecord := sourceGame.BackupRegistry[recordIndex]
			targetRecord := &game.BackupRegistry[recordIndex]
			if sourceRecord.DeletedAt != nil || sourceRecord.PendingDelete || sourceRecord.Status == core.BackupStatusPendingDelete || sourceRecord.Status == core.BackupStatusDeleteFailed {
				continue
			}
			sourceRecord.ObjectKey = core.BackupObjectKeyForRecord(game.ID, sourceRecord)
			localPath := core.ExistingBackupLocalPathForRecord(a.store.DataDir(), game.ID, sourceRecord)
			item, err := a.migrationItemForLocalOrRemote("backup", game.ID, localPath, sourceRecord.AccountID, sourceRecord.ObjectKey, sourceRecord.ObjectKey, sourceRecord.SHA256, accountLookup)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("inventory backup %s: %w", core.BackupRecordID(sourceRecord), err)
			}
			appendMigrationItem(&items, seen, item)
			targetRecord.ObjectKey = sourceRecord.ObjectKey
			targetRecord.AccountID = target.ID
		}
		game.StorageAccountID = target.ID
		game.BackupStorageAccountID = target.ID
		if game.AutoBackupAccountID != "" {
			game.AutoBackupAccountID = target.ID
		}
	}
	return items, manifests, games, manifestVersions, nil
}

func (a *App) addCoverMigrationItem(sourceGame core.Game, targetGame *core.Game, targetAccountID string, accountLookup map[string]core.CloudflareAccount, items *[]core.StorageMigrationItem, seen map[string]bool) error {
	if targetGame == nil {
		return nil
	}
	sourceAccountID, sourceKey := coverCloudLocation(sourceGame)
	localPath := a.locateCoverCache(sourceGame)
	if localPath == "" && strings.TrimSpace(sourceGame.CoverLocalPath) != "" {
		if info, err := os.Stat(sourceGame.CoverLocalPath); err == nil && !info.IsDir() {
			localPath = sourceGame.CoverLocalPath
		}
	}
	if localPath == "" && sourceKey == "" {
		return nil
	}

	ext := filepath.Ext(sourceKey)
	if ext == "" {
		ext = filepath.Ext(localPath)
	}
	item, err := a.migrationItemForLocalOrRemote("cover", sourceGame.ID, localPath, sourceAccountID, sourceKey, sourceKey, "", accountLookup)
	if err != nil {
		return fmt.Errorf("inventory cover for %s: %w", sourceGame.Name, err)
	}
	item.TargetKey = fmt.Sprintf("covers/%s/%s%s", strings.TrimSpace(sourceGame.ID), strings.ToLower(item.SHA256), sanitizeCoverExtension(ext))
	appendMigrationItem(items, seen, item)
	targetGame.CoverCloudAccountID = targetAccountID
	targetGame.CoverCloudKey = item.TargetKey
	return nil
}

func (a *App) migrationItemForLocalOrRemote(kind, gameID, localPath, sourceAccountID, sourceKey, targetKey, expectedSHA string, accountLookup map[string]core.CloudflareAccount) (core.StorageMigrationItem, error) {
	item := core.StorageMigrationItem{Kind: kind, GameID: gameID, TargetKey: targetKey, Status: core.MigrationItemPending}
	if info, err := os.Stat(localPath); err == nil && !info.IsDir() {
		hash, hashErr := sha256FileHex(localPath)
		if hashErr == nil && (strings.TrimSpace(expectedSHA) == "" || strings.EqualFold(hash, expectedSHA)) {
			item.LocalPath = localPath
			item.SHA256 = hash
			item.Size = info.Size()
			return item, nil
		}
	}
	account, ok := accountLookup[strings.TrimSpace(sourceAccountID)]
	if !ok {
		return core.StorageMigrationItem{}, errors.New("source storage account is unavailable")
	}
	objects, err := a.objectStoreFor(a.syncContext(), account)
	if err != nil {
		return core.StorageMigrationItem{}, err
	}
	item.SourceAccountID = account.ID
	item.SourceKey = strings.TrimSpace(sourceKey)
	item.SHA256 = strings.ToLower(strings.TrimSpace(expectedSHA))
	if item.SourceKey == "" {
		return core.StorageMigrationItem{}, errors.New("source object key is empty")
	}
	if item.SHA256 == "" {
		content, err := objects.GetObjectBytes(a.syncContext(), item.SourceKey)
		if err != nil {
			return core.StorageMigrationItem{}, err
		}
		hash := sha256.Sum256(content)
		item.SHA256 = hex.EncodeToString(hash[:])
		item.Size = int64(len(content))
	}
	return item, nil
}

func appendMigrationItem(items *[]core.StorageMigrationItem, seen map[string]bool, item core.StorageMigrationItem) {
	identity := item.Kind + ":" + item.GameID + ":" + item.TargetKey
	if seen[identity] {
		return
	}
	seen[identity] = true
	*items = append(*items, item)
}

func preserveVerifiedMigrationItems(previous, current []core.StorageMigrationItem) []core.StorageMigrationItem {
	verified := make(map[string]core.StorageMigrationItem, len(previous))
	for _, item := range previous {
		if item.Status == core.MigrationItemVerified {
			verified[item.Kind+":"+item.GameID+":"+item.TargetKey] = item
		}
	}
	for index := range current {
		identity := current[index].Kind + ":" + current[index].GameID + ":" + current[index].TargetKey
		if old, ok := verified[identity]; ok && strings.EqualFold(old.SHA256, current[index].SHA256) && old.Size == current[index].Size {
			current[index].Status = core.MigrationItemVerified
			current[index].LastError = ""
		}
	}
	return current
}

func (a *App) copyStorageMigrationItems(migration core.StorageMigrationState, source, target core.CloudflareAccount) error {
	targetObjects, err := a.objectStoreFor(a.syncContext(), target)
	if err != nil {
		return err
	}
	stores := map[string]core.ObjectStore{}
	tempDir := filepath.Join(a.store.DataDir(), "migrations", migration.TransactionID, "temp")
	for index, item := range migration.Items {
		if item.Status == core.MigrationItemVerified {
			continue
		}
		a.emitStorageSwitchProgress("copy", fmt.Sprintf("正在迁移 %s (%d/%d)...", item.Kind, index+1, len(migration.Items)), index+1, len(migration.Items))
		var sourceObjects core.ObjectStore
		if item.LocalPath == "" {
			sourceObjects = stores[item.SourceAccountID]
			if sourceObjects == nil {
				account, findErr := findAccount(a.store.Snapshot(), item.SourceAccountID)
				if findErr != nil {
					return findErr
				}
				sourceObjects, err = a.objectStoreFor(a.syncContext(), account)
				if err != nil {
					return err
				}
				stores[item.SourceAccountID] = sourceObjects
			}
		}
		if err := core.CopyMigrationObject(a.syncContext(), sourceObjects, targetObjects, item, tempDir); err != nil {
			item.Status = core.MigrationItemPending
			item.LastError = err.Error()
			_ = a.store.UpdateStorageMigrationItem(migration.TransactionID, item)
			return err
		}
		item.Status = core.MigrationItemVerified
		item.LastError = ""
		if err := a.store.UpdateStorageMigrationItem(migration.TransactionID, item); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) publishPreparedTarget(migration core.StorageMigrationState, target core.CloudflareAccount, games []core.Game, manifests map[string]core.SyncManifest) error {
	targetCatalog, err := a.catalogStoreFor(target)
	if err != nil {
		return err
	}
	for _, game := range games {
		manifest, ok := manifests[game.ID]
		if !ok {
			continue
		}
		current, err := targetCatalog.LoadRemoteManifest(a.syncContext(), game.ID)
		if err != nil {
			return err
		}
		manifest.Version = current.Version + 1
		record := core.RemoteManifestRecord{GameID: game.ID, Version: manifest.Version, Manifest: manifest, UpdatedAt: time.Now(), UpdatedByDevice: a.store.Snapshot().Device.ID}
		if err := targetCatalog.SaveRemoteManifestIfVersion(a.syncContext(), record, current.Version); err != nil {
			return err
		}
	}

	prepared := storageMigrationHandoff(migration, a.store.Snapshot().Device.ID, core.StorageHandoffPrepared)
	catalog := a.migrationRemoteCatalog(target, games, prepared)
	credentials, err := encryptCatalogCredentials(catalog.Accounts, a.recoveryPassword)
	if err != nil {
		return err
	}
	if _, err := targetCatalog.SaveRemoteCatalog(a.syncContext(), catalog, credentials, a.store.Snapshot().Device); err != nil {
		return err
	}
	current, err := targetCatalog.LoadStorageHandoff(a.syncContext())
	if err != nil {
		return err
	}
	if current.TransactionID != prepared.TransactionID || current.State != core.StorageHandoffPrepared || current.Generation != prepared.Generation {
		if current.Generation >= prepared.Generation && current.TransactionID != prepared.TransactionID {
			return core.ErrStorageHandoffChanged
		}
		if err := targetCatalog.SaveStorageHandoffIfGeneration(a.syncContext(), prepared, current.Generation); err != nil {
			return err
		}
	}
	readBack, _, err := targetCatalog.LoadRemoteCatalog(a.syncContext())
	if err != nil {
		return err
	}
	if readBack.Handoff == nil || readBack.Handoff.TransactionID != prepared.TransactionID || readBack.Handoff.State != core.StorageHandoffPrepared || readBack.Handoff.Generation != prepared.Generation {
		return errors.New("target storage did not retain the prepared handoff")
	}
	for _, game := range readBack.Games {
		if game.StorageAccountID != target.ID || (game.CoverCloudKey != "" && game.CoverCloudAccountID != target.ID) {
			return fmt.Errorf("target catalog route verification failed for game %s", game.ID)
		}
	}
	return nil
}

func (a *App) migrationRemoteCatalog(target core.CloudflareAccount, games []core.Game, handoff core.StorageHandoff) core.RemoteCatalog {
	state := a.store.Snapshot()
	now := time.Now()
	accounts := append([]core.CloudflareAccount(nil), state.Accounts...)
	for index := range accounts {
		accounts[index].IsPrimary = accounts[index].ID == target.ID
		accounts[index].Enabled = accounts[index].ID == target.ID
		accounts[index].CatalogUpdatedAt = now
		if accounts[index].ID == target.ID {
			accounts[index] = target
			accounts[index].IsPrimary = true
			accounts[index].Enabled = true
			accounts[index].CatalogUpdatedAt = now
		}
	}
	for index := range games {
		games[index].CatalogUpdatedAt = now
		games[index].StorageUpdatedAt = now
	}
	handoffCopy := handoff
	return core.RemoteCatalog{
		Accounts: accounts,
		Games:    games,
		Preferences: &core.RemotePreferences{
			TagOrder:                 state.Preferences.TagOrder,
			TagOrderUpdatedAt:        state.Preferences.TagOrderUpdatedAt,
			PinnedTags:               state.Preferences.PinnedTags,
			PinnedTagsUpdatedAt:      state.Preferences.PinnedTagsUpdatedAt,
			SidebarNavOrder:          state.Preferences.SidebarNavOrder,
			SidebarNavOrderUpdatedAt: state.Preferences.SidebarNavOrderUpdatedAt,
			FavoriteGames:            state.Preferences.FavoriteGames,
			FavoriteGamesUpdatedAt:   state.Preferences.FavoriteGamesUpdatedAt,
			GameOrderUpdatedAt:       state.Preferences.GameOrderUpdatedAt,
		},
		Tombstones:        activeCatalogTombstones(state),
		StorageGeneration: handoff.Generation,
		Handoff:           &handoffCopy,
	}
}

func (a *App) revalidateMigrationSource(migration core.StorageMigrationState, source core.CloudflareAccount) error {
	revision, err := a.loadCatalogRevision(source)
	if err != nil {
		return err
	}
	if revision != migration.SourceRevision {
		return errors.New("源存储目录在迁移期间已变化，请重试以合并最新数据")
	}
	catalog, err := a.catalogStoreFor(source)
	if err != nil {
		return err
	}
	state := a.store.Snapshot()
	for gameID, expectedVersion := range migration.SourceManifestVersion {
		record, err := catalog.LoadRemoteManifest(a.syncContext(), gameID)
		if err != nil {
			return err
		}
		if record.Version != expectedVersion {
			return fmt.Errorf("源存储中的游戏 %s 在迁移期间已变化", gameID)
		}
		if expectedHash := migration.LocalManifestHash[gameID]; expectedHash != "" {
			game, findErr := findGame(state, gameID)
			if findErr != nil {
				return findErr
			}
			manifest, buildErr := core.BuildLocalManifest(game.SavePath, game.Sync.IncludePatterns, game.Sync.ExcludePatterns)
			if buildErr != nil || manifest.Hash != expectedHash {
				return fmt.Errorf("游戏 %s 的本地存档在迁移期间已变化", game.Name)
			}
		}
	}
	return nil
}

func (a *App) loadCatalogRevision(account core.CloudflareAccount) (int64, error) {
	catalog, err := a.catalogStoreFor(account)
	if err != nil {
		return 0, err
	}
	return catalog.LoadCatalogRevision(a.syncContext())
}

func storageMigrationHandoff(migration core.StorageMigrationState, deviceID, state string) core.StorageHandoff {
	handoff := core.StorageHandoff{
		TransactionID:      migration.TransactionID,
		SourceAccountID:    migration.SourceAccountID,
		TargetAccountID:    migration.TargetAccountID,
		InitiatingDeviceID: strings.TrimSpace(deviceID),
		State:              state,
		Generation:         migration.Generation,
	}
	if state == core.StorageHandoffCommitted {
		handoff.CommittedAt = time.Now()
	}
	return handoff
}

func migratedGamesForTarget(games []core.Game, targetAccountID string) []core.Game {
	result := append([]core.Game(nil), games...)
	for gameIndex := range result {
		game := &result[gameIndex]
		game.StorageAccountID = targetAccountID
		game.BackupStorageAccountID = targetAccountID
		if game.AutoBackupAccountID != "" {
			game.AutoBackupAccountID = targetAccountID
		}
		if game.CoverCloudKey != "" {
			game.CoverCloudAccountID = targetAccountID
		}
		for recordIndex := range game.BackupRegistry {
			record := &game.BackupRegistry[recordIndex]
			if record.DeletedAt == nil && !record.PendingDelete {
				record.AccountID = targetAccountID
				record.ObjectKey = core.BackupObjectKeyForRecord(game.ID, *record)
			}
		}
	}
	return result
}

func (a *App) synchronizeCommittedTarget(target core.CloudflareAccount) error {
	state := a.store.Snapshot()
	for _, game := range state.Games {
		if !game.Sync.Enabled || strings.TrimSpace(game.SavePath) == "" {
			continue
		}
		gateway, err := a.storageGatewayFor(a.syncContext(), target, target)
		if err != nil {
			return err
		}
		unlock := a.lockGameSync(game.ID)
		summary, anchor, syncErr := a.engine.SyncGameWithGateway(a.syncContext(), state.Device, game, gateway, "local", func(message string) {
			a.emitSyncProgress(game.ID, message)
		})
		unlock()
		if syncErr != nil {
			return syncErr
		}
		if summary.Status == "success" {
			if err := a.syncGameCover(state, game); err != nil {
				return fmt.Errorf(msgCoverSyncFailed, err)
			}
		}
		if err := a.store.UpdateGameSync(game.ID, anchor, summary); err != nil {
			return err
		}
	}
	if err := a.syncRemoteCatalog(); err != nil {
		return err
	}
	a.requeuePendingBackupOperations()
	return nil
}
