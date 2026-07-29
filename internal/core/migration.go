package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrNoStorageMigration        = errors.New("no storage migration is active")
	ErrStorageMigrationChanged   = errors.New("storage migration changed")
	ErrStorageMigrationCommitted = errors.New("storage migration is already committed")
)

func validateStorageHandoffUpdate(handoff StorageHandoff, expectedGeneration int64) error {
	handoff.TransactionID = strings.TrimSpace(handoff.TransactionID)
	handoff.SourceAccountID = strings.TrimSpace(handoff.SourceAccountID)
	handoff.TargetAccountID = strings.TrimSpace(handoff.TargetAccountID)
	if handoff.TransactionID == "" || handoff.SourceAccountID == "" || handoff.TargetAccountID == "" {
		return errors.New("storage handoff identifiers are incomplete")
	}
	if handoff.State != StorageHandoffPrepared && handoff.State != StorageHandoffCommitted {
		return errors.New("storage handoff state is invalid")
	}
	if handoff.Generation < expectedGeneration || handoff.Generation == 0 {
		return ErrStorageHandoffChanged
	}
	return nil
}

func (s *Store) BeginStorageMigration(migration StorageMigrationState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	migration.TransactionID = strings.TrimSpace(migration.TransactionID)
	migration.SourceAccountID = strings.TrimSpace(migration.SourceAccountID)
	migration.TargetAccountID = strings.TrimSpace(migration.TargetAccountID)
	if migration.TransactionID == "" || migration.SourceAccountID == "" || migration.TargetAccountID == "" {
		return errors.New("storage migration identifiers are incomplete")
	}
	if s.state.StorageMigration != nil && s.state.StorageMigration.TransactionID != migration.TransactionID {
		return ErrStorageMigrationChanged
	}
	if migration.Phase == "" {
		migration.Phase = MigrationPhaseCopying
	}
	if migration.Generation <= s.state.StorageGeneration {
		migration.Generation = s.state.StorageGeneration + 1
	}
	for index := range migration.Items {
		migration.Items[index] = normalizeStorageMigrationItem(migration.Items[index])
	}
	migration.UpdatedAt = time.Now()
	copy := migration
	s.state.StorageMigration = &copy
	return s.saveLocked()
}

func (s *Store) UpdateStorageMigrationItem(transactionID string, item StorageMigrationItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	migration, err := s.storageMigrationLocked(transactionID)
	if err != nil {
		return err
	}
	item = normalizeStorageMigrationItem(item)
	for index := range migration.Items {
		if storageMigrationItemID(migration.Items[index]) == storageMigrationItemID(item) {
			migration.Items[index] = item
			migration.UpdatedAt = time.Now()
			return s.saveLocked()
		}
	}
	migration.Items = append(migration.Items, item)
	migration.UpdatedAt = time.Now()
	return s.saveLocked()
}

func (s *Store) PauseStorageMigration(transactionID, gameID, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	migration, err := s.storageMigrationLocked(transactionID)
	if err != nil {
		return err
	}
	if migration.Phase == MigrationPhaseSourceCommitted || migration.Phase == MigrationPhaseLocalCommitted {
		return ErrStorageMigrationCommitted
	}
	migration.ConflictGameID = strings.TrimSpace(gameID)
	migration.LastError = strings.TrimSpace(message)
	migration.UpdatedAt = time.Now()
	return s.saveLocked()
}

func (s *Store) MarkStorageMigrationPhase(transactionID, phase string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	migration, err := s.storageMigrationLocked(transactionID)
	if err != nil {
		return err
	}
	if !validMigrationTransition(migration.Phase, phase) {
		return fmt.Errorf("invalid storage migration transition %s -> %s", migration.Phase, phase)
	}
	migration.Phase = phase
	migration.ConflictGameID = ""
	migration.LastError = ""
	migration.UpdatedAt = time.Now()
	return s.saveLocked()
}

func (s *Store) CancelStorageMigration(transactionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	migration, err := s.storageMigrationLocked(transactionID)
	if err != nil {
		return err
	}
	if migration.Phase == MigrationPhaseSourceCommitted || migration.Phase == MigrationPhaseLocalCommitted {
		return ErrStorageMigrationCommitted
	}
	s.state.StorageMigration = nil
	return s.saveLocked()
}

func (s *Store) FinishStorageMigration(transactionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	migration, err := s.storageMigrationLocked(transactionID)
	if err != nil {
		return err
	}
	if migration.Phase != MigrationPhaseLocalCommitted {
		return errors.New("storage migration is not locally committed")
	}
	s.state.StorageMigration = nil
	return s.saveLocked()
}

func (s *Store) CommitStorageMigration(transactionID string, target CloudflareAccount, games []Game, handoff StorageHandoff) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	migration, err := s.storageMigrationLocked(transactionID)
	if err != nil {
		return err
	}
	if migration.Phase != MigrationPhaseSourceCommitted {
		return fmt.Errorf("storage migration is not source-committed")
	}
	if handoff.State != StorageHandoffCommitted || handoff.TransactionID != migration.TransactionID || handoff.TargetAccountID != migration.TargetAccountID {
		return errors.New("storage handoff does not match migration")
	}
	if strings.TrimSpace(target.ID) != migration.TargetAccountID {
		return errors.New("storage migration target account changed")
	}

	now := time.Now()
	for index := range s.state.Accounts {
		if s.state.Accounts[index].ID == target.ID {
			continue
		}
		if s.state.Accounts[index].IsPrimary || s.state.Accounts[index].Enabled {
			s.state.Accounts[index].IsPrimary = false
			s.state.Accounts[index].Enabled = false
			s.state.Accounts[index].CatalogUpdatedAt = now
		}
	}
	target.IsPrimary = true
	target.Enabled = true
	target.CatalogUpdatedAt = now
	replaced := false
	for index := range s.state.Accounts {
		if s.state.Accounts[index].ID == target.ID {
			s.state.Accounts[index] = target
			replaced = true
			break
		}
	}
	if !replaced {
		s.state.Accounts = append(s.state.Accounts, target)
	}

	s.state.Games = cloneState(AppState{Games: games}).Games
	for index := range s.state.Games {
		game := &s.state.Games[index]
		game.StorageAccountID = target.ID
		game.BackupStorageAccountID = target.ID
		if strings.TrimSpace(game.AutoBackupAccountID) != "" {
			game.AutoBackupAccountID = target.ID
		}
		game.Anchor.StorageAccountID = target.ID
		game.StorageUpdatedAt = now
		normalizeGameCatalogTimestamps(game, now)
	}
	handoffCopy := handoff
	s.state.StorageGeneration = handoff.Generation
	s.state.LastStorageHandoff = &handoffCopy
	migration.Phase = MigrationPhaseLocalCommitted
	migration.UpdatedAt = now
	s.state.CatalogSync.Dirty = true
	s.state.CatalogSync.LastQueuedAt = &now
	s.normalizeAccountsLocked()
	s.reorderAccountsLocked()
	s.assignAccountNamesLocked()
	return s.saveLocked()
}

func (s *Store) FollowStorageHandoff(target CloudflareAccount, games []Game, handoff StorageHandoff) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if handoff.State != StorageHandoffCommitted || handoff.Generation <= s.state.StorageGeneration {
		return ErrStorageHandoffChanged
	}
	if strings.TrimSpace(target.ID) == "" || target.ID != handoff.TargetAccountID {
		return errors.New("storage handoff target account is invalid")
	}

	now := time.Now()
	for index := range s.state.Accounts {
		if s.state.Accounts[index].ID == target.ID {
			continue
		}
		if s.state.Accounts[index].IsPrimary || s.state.Accounts[index].Enabled {
			s.state.Accounts[index].IsPrimary = false
			s.state.Accounts[index].Enabled = false
			s.state.Accounts[index].CatalogUpdatedAt = now
		}
	}
	target.IsPrimary = true
	target.Enabled = true
	target.CatalogUpdatedAt = now
	replaced := false
	for index := range s.state.Accounts {
		if s.state.Accounts[index].ID == target.ID {
			s.state.Accounts[index] = target
			replaced = true
			break
		}
	}
	if !replaced {
		s.state.Accounts = append(s.state.Accounts, target)
	}

	s.state.Games = cloneState(AppState{Games: games}).Games
	for index := range s.state.Games {
		game := &s.state.Games[index]
		game.StorageAccountID = target.ID
		game.BackupStorageAccountID = target.ID
		if strings.TrimSpace(game.AutoBackupAccountID) != "" {
			game.AutoBackupAccountID = target.ID
		}
		game.Anchor.StorageAccountID = target.ID
		game.StorageUpdatedAt = now
		normalizeGameCatalogTimestamps(game, now)
	}
	handoffCopy := handoff
	s.state.StorageGeneration = handoff.Generation
	s.state.LastStorageHandoff = &handoffCopy
	s.state.CatalogSync.Dirty = true
	s.state.CatalogSync.LastQueuedAt = &now
	s.normalizeAccountsLocked()
	s.reorderAccountsLocked()
	s.assignAccountNamesLocked()
	return s.saveLocked()
}

func (s *Store) storageMigrationLocked(transactionID string) (*StorageMigrationState, error) {
	if s.state.StorageMigration == nil {
		return nil, ErrNoStorageMigration
	}
	if strings.TrimSpace(transactionID) == "" || s.state.StorageMigration.TransactionID != strings.TrimSpace(transactionID) {
		return nil, ErrStorageMigrationChanged
	}
	return s.state.StorageMigration, nil
}

func validMigrationTransition(current, next string) bool {
	if current == next {
		return true
	}
	switch current {
	case MigrationPhaseCopying:
		return next == MigrationPhaseTargetReady
	case MigrationPhaseTargetReady:
		return next == MigrationPhaseSourceCommitted
	case MigrationPhaseSourceCommitted:
		return next == MigrationPhaseLocalCommitted
	default:
		return false
	}
}

func normalizeStorageMigrationItem(item StorageMigrationItem) StorageMigrationItem {
	item.Kind = strings.TrimSpace(item.Kind)
	item.GameID = strings.TrimSpace(item.GameID)
	item.SourceAccountID = strings.TrimSpace(item.SourceAccountID)
	item.SourceKey = strings.TrimSpace(item.SourceKey)
	item.LocalPath = strings.TrimSpace(item.LocalPath)
	item.TargetKey = strings.TrimSpace(item.TargetKey)
	item.SHA256 = strings.ToLower(strings.TrimSpace(item.SHA256))
	item.Status = strings.TrimSpace(item.Status)
	item.LastError = strings.TrimSpace(item.LastError)
	if item.Status == "" {
		item.Status = MigrationItemPending
	}
	return item
}

func storageMigrationItemID(item StorageMigrationItem) string {
	return strings.TrimSpace(item.Kind) + ":" + strings.TrimSpace(item.GameID) + ":" + strings.TrimSpace(item.TargetKey)
}

func CopyMigrationObject(ctx context.Context, source ObjectStore, target ObjectStore, item StorageMigrationItem, tempDir string) error {
	item = normalizeStorageMigrationItem(item)
	if target == nil || item.TargetKey == "" || item.SHA256 == "" {
		return errors.New("migration object target or hash is incomplete")
	}
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return err
	}

	sourcePath := item.LocalPath
	if sourcePath == "" {
		if source == nil || item.SourceKey == "" {
			return errors.New("migration object source is incomplete")
		}
		temp, err := os.CreateTemp(tempDir, "migration-source-*")
		if err != nil {
			return err
		}
		sourcePath = temp.Name()
		_ = temp.Close()
		defer os.Remove(sourcePath)
		if err := source.DownloadObjectToFile(ctx, item.SourceKey, sourcePath); err != nil {
			return err
		}
	}
	if err := verifyMigrationFile(sourcePath, item); err != nil {
		return err
	}
	if err := target.PutObjectFromFile(ctx, item.TargetKey, sourcePath); err != nil {
		return err
	}

	verified, err := os.CreateTemp(tempDir, "migration-target-*")
	if err != nil {
		return err
	}
	verifiedPath := verified.Name()
	_ = verified.Close()
	defer os.Remove(verifiedPath)
	if err := target.DownloadObjectToFile(ctx, item.TargetKey, verifiedPath); err != nil {
		return err
	}
	if err := verifyMigrationFile(verifiedPath, item); err != nil {
		return fmt.Errorf("verify target migration object: %w", err)
	}
	return nil
}

func verifyMigrationFile(path string, item StorageMigrationItem) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if item.Size > 0 && info.Size() != item.Size {
		return fmt.Errorf("migration object size mismatch: got %d want %d", info.Size(), item.Size)
	}
	hash, err := sha256File(filepath.Clean(path))
	if err != nil {
		return err
	}
	if !strings.EqualFold(hash, item.SHA256) {
		return fmt.Errorf("migration object sha256 mismatch")
	}
	return nil
}
