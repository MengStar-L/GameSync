package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gamesync/internal/core"
)

type migrationCatalogStore struct {
	mu          sync.Mutex
	catalog     core.RemoteCatalog
	credentials map[string]core.EncryptedCredentialBlob
	manifests   map[string]core.RemoteManifestRecord
	handoff     core.StorageHandoff
	revision    int64
}

func newMigrationCatalogStore() *migrationCatalogStore {
	return &migrationCatalogStore{credentials: map[string]core.EncryptedCredentialBlob{}, manifests: map[string]core.RemoteManifestRecord{}}
}

func (m *migrationCatalogStore) EnsureSchema(context.Context) error   { return nil }
func (m *migrationCatalogStore) ValidateAccess(context.Context) error { return nil }
func (m *migrationCatalogStore) ClearGameRecords(_ context.Context, gameID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.manifests, gameID)
	return nil
}
func (m *migrationCatalogStore) SaveRemoteCatalog(_ context.Context, catalog core.RemoteCatalog, credentials map[string]core.EncryptedCredentialBlob, _ core.DeviceInfo) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revision++
	catalog.Revision = m.revision
	if m.handoff.TransactionID != "" {
		handoff := m.handoff
		catalog.Handoff = &handoff
		catalog.StorageGeneration = handoff.Generation
	}
	m.catalog = catalog
	m.credentials = credentials
	return m.revision, nil
}
func (m *migrationCatalogStore) LoadRemoteCatalog(context.Context) (core.RemoteCatalog, map[string]core.EncryptedCredentialBlob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	catalog := m.catalog
	catalog.Revision = m.revision
	if m.handoff.TransactionID != "" {
		handoff := m.handoff
		catalog.Handoff = &handoff
		catalog.StorageGeneration = handoff.Generation
	}
	return catalog, m.credentials, nil
}
func (m *migrationCatalogStore) LoadCatalogRevision(context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.revision, nil
}
func (m *migrationCatalogStore) IncrementCatalogRevision(context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revision++
	return m.revision, nil
}
func (m *migrationCatalogStore) LoadStorageHandoff(context.Context) (core.StorageHandoff, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.handoff, nil
}
func (m *migrationCatalogStore) SaveStorageHandoffIfGeneration(_ context.Context, handoff core.StorageHandoff, expected int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.handoff.Generation != expected {
		return core.ErrStorageHandoffChanged
	}
	if handoff.Generation == expected && m.handoff.TransactionID != "" && m.handoff.TransactionID != handoff.TransactionID {
		return core.ErrStorageHandoffChanged
	}
	m.handoff = handoff
	handoffCopy := handoff
	m.catalog.Handoff = &handoffCopy
	m.catalog.StorageGeneration = handoff.Generation
	return nil
}
func (m *migrationCatalogStore) LoadRemoteManifest(_ context.Context, gameID string) (core.RemoteManifestRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.manifests[gameID], nil
}
func (m *migrationCatalogStore) SaveRemoteManifest(_ context.Context, record core.RemoteManifestRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.manifests[record.GameID] = record
	return nil
}
func (m *migrationCatalogStore) SaveRemoteManifestIfVersion(_ context.Context, record core.RemoteManifestRecord, expected int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.manifests[record.GameID].Version != expected {
		return errors.New("manifest changed")
	}
	m.manifests[record.GameID] = record
	return nil
}

type migrationObjectStore struct {
	mu       sync.Mutex
	objects  map[string][]byte
	puts     map[string]int
	putCount int
	failPut  int
}

func newMigrationObjectStore() *migrationObjectStore {
	return &migrationObjectStore{objects: map[string][]byte{}, puts: map[string]int{}}
}
func (m *migrationObjectStore) PutObjectFromFile(_ context.Context, key, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.putCount++
	if m.failPut > 0 && m.putCount == m.failPut {
		return errors.New("injected object upload failure")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	m.objects[key] = append([]byte(nil), content...)
	m.puts[key]++
	return nil
}
func (m *migrationObjectStore) DownloadObjectToFile(_ context.Context, key, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	content, ok := m.objects[key]
	if !ok {
		return os.ErrNotExist
	}
	return os.WriteFile(path, content, 0o600)
}
func (m *migrationObjectStore) GetObjectBytes(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	content, ok := m.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), content...), nil
}
func (m *migrationObjectStore) DeleteObject(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}
func (m *migrationObjectStore) ClearPrefix(_ context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.objects {
		if strings.HasPrefix(key, prefix) {
			delete(m.objects, key)
		}
	}
	return nil
}
func (m *migrationObjectStore) ClearGameFiles(ctx context.Context, gameID string) error {
	return m.ClearPrefix(ctx, "games/"+gameID+"/")
}
func (m *migrationObjectStore) FetchAccountUsageBytes(context.Context) (int64, error) { return 0, nil }
func (m *migrationObjectStore) ValidateBucketAccess(context.Context) error            { return nil }

type migrationFixture struct {
	app           *App
	sourceCatalog *migrationCatalogStore
	targetCatalog *migrationCatalogStore
	sourceObjects *migrationObjectStore
	targetObjects *migrationObjectStore
	game          core.Game
}

func newMigrationFixture(t *testing.T) migrationFixture {
	t.Helper()
	dataDir := t.TempDir()
	store, err := core.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertAccount(core.CloudflareAccount{
		ID: "source", Provider: core.ProviderCloudflare, Enabled: true,
		AccountID: "cf-account", APIToken: "token", D1DatabaseID: "d1", R2Bucket: "bucket", R2AccessKeyID: "key", R2SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	saveDir := filepath.Join(dataDir, "save")
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(saveDir, "profile.dat"), []byte("save-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	coverPath := filepath.Join(dataDir, "cover.png")
	if err := os.WriteFile(coverPath, []byte("cover-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	deviceID := store.Snapshot().Device.ID
	backupRecord := core.BackupRecord{Filename: "backup_manual_test.zip", SourceDeviceID: deviceID, AccountID: source.ID, Type: "manual", Status: core.BackupStatusReady}
	backupPath := core.BackupLocalPathForRecord(dataDir, "game", backupRecord)
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, []byte("backup-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	backupHash, err := sha256FileHex(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	backupRecord.SHA256 = backupHash
	backupRecord.ObjectKey = core.BackupObjectKeyForRecord("game", backupRecord)
	game, err := store.UpsertGame(core.Game{
		ID: "game", Name: "Game", SavePath: saveDir, CoverLocalPath: coverPath,
		StorageAccountID: source.ID, Sync: core.SyncConfig{Enabled: true}, BackupRegistry: []core.BackupRecord{backupRecord},
	})
	if err != nil {
		t.Fatal(err)
	}

	sourceCatalog := newMigrationCatalogStore()
	targetCatalog := newMigrationCatalogStore()
	sourceObjects := newMigrationObjectStore()
	targetObjects := newMigrationObjectStore()
	app := NewApp()
	app.store = store
	app.baseDir = dataDir
	app.recoveryPassword = "recovery-password"
	app.catalogStoreFn = func(account core.CloudflareAccount) (core.CatalogStore, error) {
		if core.AccountProvider(account) == core.ProviderWebdav {
			return targetCatalog, nil
		}
		return sourceCatalog, nil
	}
	app.objectStoreFn = func(_ context.Context, account core.CloudflareAccount) (core.ObjectStore, error) {
		if core.AccountProvider(account) == core.ProviderWebdav {
			return targetObjects, nil
		}
		return sourceObjects, nil
	}
	app.verifyStorageFn = func(_ context.Context, account core.CloudflareAccount) (core.CloudflareAccount, error) {
		account.LastError = ""
		return account, nil
	}
	return migrationFixture{app, sourceCatalog, targetCatalog, sourceObjects, targetObjects, game}
}

func migrationTargetRequest() core.StorageSwitchRequest {
	return core.StorageSwitchRequest{
		UseLocalData: true,
		NewAccount: &core.CloudflareAccount{
			Provider: core.ProviderWebdav, WebdavURL: "https://dav.example.test", WebdavUsername: "user", WebdavPassword: "secret",
		},
	}
}

func TestStorageMigrationCopiesAllObjectsAndCommitsBothEnds(t *testing.T) {
	fixture := newMigrationFixture(t)
	result, err := fixture.app.SwitchStoragePrimary(migrationTargetRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != storageSwitchCompleted {
		t.Fatalf("switch result = %+v", result)
	}
	state := fixture.app.store.Snapshot()
	primary, err := findPrimaryAccount(state)
	if err != nil || core.AccountProvider(primary) != core.ProviderWebdav {
		t.Fatalf("primary = %+v, %v", primary, err)
	}
	if primary.LastError != "" || primary.VerificationState != "valid" {
		t.Fatalf("migrated WebDAV primary retained an error: %+v", primary)
	}
	if state.StorageMigration != nil || state.StorageGeneration != 1 {
		t.Fatalf("migration was not finalized: %+v", state.StorageMigration)
	}
	if fixture.sourceCatalog.handoff.State != core.StorageHandoffCommitted || fixture.targetCatalog.handoff.State != core.StorageHandoffCommitted {
		t.Fatalf("handoffs source=%+v target=%+v", fixture.sourceCatalog.handoff, fixture.targetCatalog.handoff)
	}
	if len(fixture.targetObjects.objects) != 3 {
		t.Fatalf("target objects = %v", fixture.targetObjects.objects)
	}
	game, err := findGame(state, fixture.game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if game.CoverCloudAccountID != primary.ID || !strings.Contains(game.CoverCloudKey, "/") {
		t.Fatalf("cover route = %s %s", game.CoverCloudAccountID, game.CoverCloudKey)
	}
	if game.BackupRegistry[0].AccountID != primary.ID || game.BackupRegistry[0].ObjectKey == "" {
		t.Fatalf("backup route = %+v", game.BackupRegistry[0])
	}
}

func TestStorageMigrationResumeSkipsVerifiedObjects(t *testing.T) {
	fixture := newMigrationFixture(t)
	fixture.targetObjects.failPut = 2
	result, err := fixture.app.SwitchStoragePrimary(migrationTargetRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != storageSwitchRetryable {
		t.Fatalf("initial result = %+v", result)
	}
	migration := fixture.app.store.Snapshot().StorageMigration
	if migration == nil {
		t.Fatal("migration state was not persisted")
	}
	var verifiedKey string
	for _, item := range migration.Items {
		if item.Status == core.MigrationItemVerified {
			verifiedKey = item.TargetKey
			break
		}
	}
	if verifiedKey == "" || fixture.targetObjects.puts[verifiedKey] != 1 {
		t.Fatalf("verified object before resume = %q, puts=%v", verifiedKey, fixture.targetObjects.puts)
	}
	fixture.targetObjects.failPut = 0
	result, err = fixture.app.ResumeStorageMigration(core.StorageMigrationResumeRequest{TransactionID: migration.TransactionID, ConflictChoice: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != storageSwitchCompleted {
		t.Fatalf("resume result = %+v", result)
	}
	if fixture.targetObjects.puts[verifiedKey] != 1 {
		t.Fatalf("verified object was uploaded again: %v", fixture.targetObjects.puts)
	}
}

func TestStorageMigrationTargetDataRequiresExplicitLocalChoiceOrCancel(t *testing.T) {
	fixture := newMigrationFixture(t)
	fixture.targetCatalog.catalog = core.RemoteCatalog{Games: []core.Game{{ID: "remote-game", Name: "Remote"}}}
	request := migrationTargetRequest()
	request.UseLocalData = false
	result, err := fixture.app.SwitchStoragePrimary(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != storageSwitchPaused || result.TransactionID == "" {
		t.Fatalf("paused result = %+v", result)
	}
	primary, err := findPrimaryAccount(fixture.app.store.Snapshot())
	if err != nil || primary.ID != "source" {
		t.Fatalf("primary changed before explicit choice: %+v, %v", primary, err)
	}
	if _, err := fixture.app.CancelStorageMigration(result.TransactionID); err != nil {
		t.Fatal(err)
	}
	state := fixture.app.store.Snapshot()
	if state.StorageMigration != nil {
		t.Fatalf("migration was not canceled: %+v", state.StorageMigration)
	}
	primary, err = findPrimaryAccount(state)
	if err != nil || primary.ID != "source" {
		t.Fatalf("cancel changed primary: %+v, %v", primary, err)
	}
}

func TestRemoteDeviceFollowsCommittedStorageHandoff(t *testing.T) {
	fixture := newMigrationFixture(t)
	result, err := fixture.app.SwitchStoragePrimary(migrationTargetRequest())
	if err != nil || result.Status != storageSwitchCompleted {
		t.Fatalf("prepare migration: result=%+v err=%v", result, err)
	}

	followerStore, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	finalState := fixture.app.store.Snapshot()
	source, err := findAccount(finalState, "source")
	if err != nil {
		t.Fatal(err)
	}
	source.Enabled = true
	source.IsPrimary = true
	if _, err := followerStore.UpsertAccount(source); err != nil {
		t.Fatal(err)
	}
	followerGame := fixture.game
	followerGame.StorageAccountID = source.ID
	if _, err := followerStore.UpsertGame(followerGame); err != nil {
		t.Fatal(err)
	}

	follower := NewApp()
	follower.store = followerStore
	follower.recoveryPassword = "recovery-password"
	follower.catalogStoreFn = fixture.app.catalogStoreFn
	follower.objectStoreFn = fixture.app.objectStoreFn
	follower.verifyStorageFn = fixture.app.verifyStorageFn
	finish, err := follower.beginRemoteOperation()
	if err != nil {
		t.Fatal(err)
	}
	finish()
	state := followerStore.Snapshot()
	primary, err := findPrimaryAccount(state)
	if err != nil || core.AccountProvider(primary) != core.ProviderWebdav || state.StorageGeneration != 1 {
		t.Fatalf("follower primary=%+v generation=%d err=%v", primary, state.StorageGeneration, err)
	}
}

func TestRemoteDeviceDoesNotFollowHandoffWithoutRecoveryPassword(t *testing.T) {
	fixture := newMigrationFixture(t)
	result, err := fixture.app.SwitchStoragePrimary(migrationTargetRequest())
	if err != nil || result.Status != storageSwitchCompleted {
		t.Fatalf("prepare migration: result=%+v err=%v", result, err)
	}

	followerStore, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source, err := findAccount(fixture.app.store.Snapshot(), "source")
	if err != nil {
		t.Fatal(err)
	}
	source.Enabled = true
	source.IsPrimary = true
	if _, err := followerStore.UpsertAccount(source); err != nil {
		t.Fatal(err)
	}
	follower := NewApp()
	follower.store = followerStore
	follower.catalogStoreFn = fixture.app.catalogStoreFn
	follower.objectStoreFn = fixture.app.objectStoreFn
	follower.verifyStorageFn = fixture.app.verifyStorageFn

	if _, err := follower.beginRemoteOperation(); !errors.Is(err, errPendingStorageHandoff) {
		t.Fatalf("missing password error = %v", err)
	}
	primary, err := findPrimaryAccount(followerStore.Snapshot())
	if err != nil || primary.ID != source.ID || followerStore.Snapshot().StorageGeneration != 0 {
		t.Fatalf("source primary changed without password: %+v, %v", primary, err)
	}
}

func TestRemoteDeviceFollowsChainedCommittedStorageHandoffs(t *testing.T) {
	fixture := newMigrationFixture(t)
	thirdCatalog := newMigrationCatalogStore()
	thirdObjects := newMigrationObjectStore()
	fixture.app.catalogStoreFn = func(account core.CloudflareAccount) (core.CatalogStore, error) {
		if core.AccountProvider(account) == core.ProviderWebdav {
			return fixture.targetCatalog, nil
		}
		if account.AccountID == "cf-account-c" {
			return thirdCatalog, nil
		}
		return fixture.sourceCatalog, nil
	}
	fixture.app.objectStoreFn = func(_ context.Context, account core.CloudflareAccount) (core.ObjectStore, error) {
		if core.AccountProvider(account) == core.ProviderWebdav {
			return fixture.targetObjects, nil
		}
		if account.AccountID == "cf-account-c" {
			return thirdObjects, nil
		}
		return fixture.sourceObjects, nil
	}

	first, err := fixture.app.SwitchStoragePrimary(migrationTargetRequest())
	if err != nil || first.Status != storageSwitchCompleted {
		t.Fatalf("A to B migration: result=%+v err=%v", first, err)
	}
	second, err := fixture.app.SwitchStoragePrimary(core.StorageSwitchRequest{
		UseLocalData: true,
		NewAccount: &core.CloudflareAccount{
			Provider: core.ProviderCloudflare, AccountID: "cf-account-c", APIToken: "token-c", D1DatabaseID: "d1-c",
			R2Bucket: "bucket-c", R2AccessKeyID: "key-c", R2SecretAccessKey: "secret-c",
		},
	})
	if err != nil || second.Status != storageSwitchCompleted {
		t.Fatalf("B to C migration: result=%+v err=%v", second, err)
	}
	if fixture.sourceCatalog.handoff.Generation != 1 || fixture.targetCatalog.handoff.Generation != 2 || thirdCatalog.handoff.Generation != 2 {
		t.Fatalf("unexpected handoff generations: A=%+v B=%+v C=%+v", fixture.sourceCatalog.handoff, fixture.targetCatalog.handoff, thirdCatalog.handoff)
	}

	followerStore, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	finalState := fixture.app.store.Snapshot()
	source, err := findAccount(finalState, "source")
	if err != nil {
		t.Fatal(err)
	}
	source.Enabled = true
	source.IsPrimary = true
	if _, err := followerStore.UpsertAccount(source); err != nil {
		t.Fatal(err)
	}
	followerGame := fixture.game
	followerGame.StorageAccountID = source.ID
	if _, err := followerStore.UpsertGame(followerGame); err != nil {
		t.Fatal(err)
	}

	follower := NewApp()
	follower.store = followerStore
	follower.recoveryPassword = "recovery-password"
	follower.catalogStoreFn = fixture.app.catalogStoreFn
	follower.objectStoreFn = fixture.app.objectStoreFn
	follower.verifyStorageFn = fixture.app.verifyStorageFn
	finish, err := follower.beginRemoteOperation()
	if err != nil {
		t.Fatal(err)
	}
	finish()

	state := followerStore.Snapshot()
	primary, err := findPrimaryAccount(state)
	if err != nil || primary.AccountID != "cf-account-c" || state.StorageGeneration != 2 {
		t.Fatalf("follower did not reach C: primary=%+v generation=%d err=%v", primary, state.StorageGeneration, err)
	}
	if state.LastStorageHandoff == nil || state.LastStorageHandoff.SourceAccountID == source.ID || state.LastStorageHandoff.TargetAccountID != primary.ID {
		t.Fatalf("follower did not record the B to C handoff: %+v", state.LastStorageHandoff)
	}
}

func TestStorageSwitchFollowsCommittedHandoffBeforeStartingAnotherMigration(t *testing.T) {
	fixture := newMigrationFixture(t)
	result, err := fixture.app.SwitchStoragePrimary(migrationTargetRequest())
	if err != nil || result.Status != storageSwitchCompleted {
		t.Fatalf("prepare migration: result=%+v err=%v", result, err)
	}

	followerStore, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source, err := findAccount(fixture.app.store.Snapshot(), "source")
	if err != nil {
		t.Fatal(err)
	}
	source.Enabled = true
	source.IsPrimary = true
	if _, err := followerStore.UpsertAccount(source); err != nil {
		t.Fatal(err)
	}

	follower := NewApp()
	follower.store = followerStore
	follower.recoveryPassword = "recovery-password"
	follower.catalogStoreFn = fixture.app.catalogStoreFn
	follower.objectStoreFn = fixture.app.objectStoreFn
	follower.verifyStorageFn = fixture.app.verifyStorageFn

	result, err = follower.SwitchStoragePrimary(migrationTargetRequest())
	if err == nil || !strings.Contains(err.Error(), msgStorageSwitchSameProvider) {
		t.Fatalf("switch after pending handoff: result=%+v err=%v", result, err)
	}
	state := followerStore.Snapshot()
	primary, findErr := findPrimaryAccount(state)
	if findErr != nil || core.AccountProvider(primary) != core.ProviderWebdav || state.StorageGeneration != 1 {
		t.Fatalf("pending handoff was not followed first: primary=%+v generation=%d err=%v", primary, state.StorageGeneration, findErr)
	}
	if state.StorageMigration != nil {
		t.Fatalf("started a stale migration after following handoff: %+v", state.StorageMigration)
	}
}

func TestStorageSwitchDoesNotBypassCommittedHandoffWithoutRecoveryPassword(t *testing.T) {
	fixture := newMigrationFixture(t)
	result, err := fixture.app.SwitchStoragePrimary(migrationTargetRequest())
	if err != nil || result.Status != storageSwitchCompleted {
		t.Fatalf("prepare migration: result=%+v err=%v", result, err)
	}

	followerStore, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source, err := findAccount(fixture.app.store.Snapshot(), "source")
	if err != nil {
		t.Fatal(err)
	}
	source.Enabled = true
	source.IsPrimary = true
	if _, err := followerStore.UpsertAccount(source); err != nil {
		t.Fatal(err)
	}

	follower := NewApp()
	follower.store = followerStore
	follower.catalogStoreFn = fixture.app.catalogStoreFn
	follower.objectStoreFn = fixture.app.objectStoreFn
	follower.verifyStorageFn = fixture.app.verifyStorageFn

	result, err = follower.SwitchStoragePrimary(migrationTargetRequest())
	if !errors.Is(err, errPendingStorageHandoff) || !strings.Contains(err.Error(), msgRecoveryPasswordRequired) {
		t.Fatalf("switch without recovery password: result=%+v err=%v", result, err)
	}
	state := followerStore.Snapshot()
	primary, findErr := findPrimaryAccount(state)
	if findErr != nil || primary.ID != source.ID || state.StorageGeneration != 0 {
		t.Fatalf("source changed without recovery password: primary=%+v generation=%d err=%v", primary, state.StorageGeneration, findErr)
	}
	if state.StorageMigration != nil || len(state.Accounts) != 1 {
		t.Fatalf("created stale migration state without recovery password: migration=%+v accounts=%+v", state.StorageMigration, state.Accounts)
	}
}
