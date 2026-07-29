package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type memoryObjectStore struct {
	objects map[string][]byte
}

func newMemoryObjectStore() *memoryObjectStore {
	return &memoryObjectStore{objects: map[string][]byte{}}
}

func (m *memoryObjectStore) PutObjectFromFile(_ context.Context, key, path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	m.objects[key] = append([]byte(nil), content...)
	return nil
}

func (m *memoryObjectStore) DownloadObjectToFile(_ context.Context, key, path string) error {
	content, ok := m.objects[key]
	if !ok {
		return os.ErrNotExist
	}
	return os.WriteFile(path, content, 0o600)
}

func (m *memoryObjectStore) GetObjectBytes(_ context.Context, key string) ([]byte, error) {
	content, ok := m.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), content...), nil
}

func (m *memoryObjectStore) DeleteObject(_ context.Context, key string) error {
	delete(m.objects, key)
	return nil
}

func (m *memoryObjectStore) ClearPrefix(_ context.Context, prefix string) error {
	for key := range m.objects {
		if strings.HasPrefix(key, prefix) {
			delete(m.objects, key)
		}
	}
	return nil
}

func (m *memoryObjectStore) ClearGameFiles(ctx context.Context, gameID string) error {
	return m.ClearPrefix(ctx, "games/"+gameID+"/")
}

func (m *memoryObjectStore) FetchAccountUsageBytes(context.Context) (int64, error) { return 0, nil }
func (m *memoryObjectStore) ValidateBucketAccess(context.Context) error            { return nil }

func TestStorageMigrationPersistsAndCannotCancelAfterSourceCommit(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	migration := StorageMigrationState{
		TransactionID:   "tx-1",
		SourceAccountID: "source",
		TargetAccountID: "target",
		Items:           []StorageMigrationItem{{Kind: "cover", GameID: "g", TargetKey: "covers/g/hash.jpg", SHA256: "abc"}},
	}
	if err := store.BeginStorageMigration(migration); err != nil {
		t.Fatal(err)
	}
	item := migration.Items[0]
	item.Status = MigrationItemVerified
	if err := store.UpdateStorageMigrationItem("tx-1", item); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkStorageMigrationPhase("tx-1", MigrationPhaseTargetReady); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkStorageMigrationPhase("tx-1", MigrationPhaseSourceCommitted); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	state := reopened.Snapshot()
	if state.StorageMigration == nil || state.StorageMigration.Items[0].Status != MigrationItemVerified {
		t.Fatalf("migration state did not survive restart: %+v", state.StorageMigration)
	}
	if err := reopened.CancelStorageMigration("tx-1"); !errors.Is(err, ErrStorageMigrationCommitted) {
		t.Fatalf("cancel after commit error = %v", err)
	}
}

func TestCommitStorageMigrationSwitchesAtomically(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertAccount(CloudflareAccount{
		AccountID: "cf", APIToken: "token", D1DatabaseID: "d1", R2Bucket: "bucket",
		R2AccessKeyID: "key", R2SecretAccessKey: "secret", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	target, err := store.UpsertAccount(CloudflareAccount{
		Provider: ProviderWebdav, WebdavURL: "https://dav.example.test", WebdavUsername: "u", WebdavPassword: "p",
	})
	if err != nil {
		t.Fatal(err)
	}
	game, err := store.UpsertGame(Game{Name: "Game", SavePath: t.TempDir(), StorageAccountID: source.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BeginStorageMigration(StorageMigrationState{TransactionID: "tx", SourceAccountID: source.ID, TargetAccountID: target.ID}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkStorageMigrationPhase("tx", MigrationPhaseTargetReady); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkStorageMigrationPhase("tx", MigrationPhaseSourceCommitted); err != nil {
		t.Fatal(err)
	}
	handoff := StorageHandoff{TransactionID: "tx", SourceAccountID: source.ID, TargetAccountID: target.ID, State: StorageHandoffCommitted, Generation: 1}
	if err := store.CommitStorageMigration("tx", target, []Game{game}, handoff); err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot()
	if state.StorageGeneration != 1 || state.Games[0].StorageAccountID != target.ID || state.Games[0].Anchor.StorageAccountID != target.ID {
		t.Fatalf("local migration commit was incomplete: %+v", state)
	}
	primary, ok := store.PrimaryAccount()
	if !ok || primary.ID != target.ID {
		t.Fatalf("primary account = %+v, %v", primary, ok)
	}
}

func TestCopyMigrationObjectVerifiesTarget(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.bin")
	content := []byte("verified migration data")
	if err := os.WriteFile(sourcePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := sha256File(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	target := newMemoryObjectStore()
	item := StorageMigrationItem{LocalPath: sourcePath, TargetKey: "objects/hash", SHA256: hash, Size: int64(len(content))}
	if err := CopyMigrationObject(context.Background(), nil, target, item, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if string(target.objects[item.TargetKey]) != string(content) {
		t.Fatalf("target content = %q", target.objects[item.TargetKey])
	}
}
