package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gamesync/internal/core"
)

type countingCatalogStore struct {
	*migrationCatalogStore
	countMu       sync.Mutex
	manifestLoads map[string]int
}

func newCountingCatalogStore() *countingCatalogStore {
	return &countingCatalogStore{
		migrationCatalogStore: newMigrationCatalogStore(),
		manifestLoads:         map[string]int{},
	}
}

func (c *countingCatalogStore) LoadRemoteManifest(ctx context.Context, gameID string) (core.RemoteManifestRecord, error) {
	c.countMu.Lock()
	c.manifestLoads[gameID]++
	c.countMu.Unlock()
	return c.migrationCatalogStore.LoadRemoteManifest(ctx, gameID)
}

func (c *countingCatalogStore) manifestLoadCount(gameID string) int {
	c.countMu.Lock()
	defer c.countMu.Unlock()
	return c.manifestLoads[gameID]
}

func (c *countingCatalogStore) remoteManifest(gameID string) core.RemoteManifestRecord {
	c.migrationCatalogStore.mu.Lock()
	defer c.migrationCatalogStore.mu.Unlock()
	return c.migrationCatalogStore.manifests[gameID]
}

type manifestConflictOnceCatalogStore struct {
	*countingCatalogStore
	mu        sync.Mutex
	saveCalls int
}

func (c *manifestConflictOnceCatalogStore) SaveRemoteManifestIfVersion(ctx context.Context, record core.RemoteManifestRecord, expectedVersion int) error {
	c.mu.Lock()
	c.saveCalls++
	shouldConflict := c.saveCalls == 1
	c.mu.Unlock()
	if shouldConflict {
		return core.ErrRemoteManifestChanged
	}
	return c.countingCatalogStore.SaveRemoteManifestIfVersion(ctx, record, expectedVersion)
}

func (c *manifestConflictOnceCatalogStore) manifestSaveCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveCalls
}

func newSyncCoordinatorFixture(t *testing.T) (*App, *core.Store, *countingCatalogStore, *migrationObjectStore) {
	t.Helper()
	dataDir := t.TempDir()
	store, err := core.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertAccount(core.CloudflareAccount{
		ID: "primary", Name: "Primary", Enabled: true, IsPrimary: true,
		AccountID: "account", APIToken: "token", D1DatabaseID: "database",
		R2Bucket: "bucket", R2AccessKeyID: "key", R2SecretAccessKey: "secret",
	}); err != nil {
		t.Fatal(err)
	}
	catalog := newCountingCatalogStore()
	objects := newMigrationObjectStore()
	app := NewApp()
	app.store = store
	app.baseDir = dataDir
	app.catalogStoreFn = func(core.CloudflareAccount) (core.CatalogStore, error) { return catalog, nil }
	app.objectStoreFn = func(context.Context, core.CloudflareAccount) (core.ObjectStore, error) { return objects, nil }
	t.Cleanup(app.closeSyncTracking)
	return app, store, catalog, objects
}

func addSyncCoordinatorGame(t *testing.T, store *core.Store, gameID string, content string) core.Game {
	t.Helper()
	saveDir := filepath.Join(store.DataDir(), "save-"+gameID)
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(saveDir, "profile.dat"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	game, err := store.UpsertGame(core.Game{ID: gameID, Name: gameID, SavePath: saveDir})
	if err != nil {
		t.Fatal(err)
	}
	return game
}

func TestRunSyncAllIncludesPathlessGamesAndCleanShortCircuits(t *testing.T) {
	app, store, catalog, objects := newSyncCoordinatorFixture(t)
	saveDir := filepath.Join(store.DataDir(), "save")
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(saveDir, "profile.dat"), []byte("save-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	configured, err := store.UpsertGame(core.Game{ID: "configured", Name: "Configured", SavePath: saveDir})
	if err != nil {
		t.Fatal(err)
	}
	pathless, err := store.UpsertGame(core.Game{ID: "pathless", Name: "Pathless"})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := store.UpsertGame(core.Game{
		ID: "disabled", Name: "Disabled",
		Sync: core.SyncConfig{Enabled: false, IncludePatterns: []string{"*"}, ExcludePatterns: []string{}, ConflictStrategy: "manual"},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := app.RunSyncAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Saves) != 3 {
		t.Fatalf("save results = %d, want all 3 games: %+v", len(first.Saves), first.Saves)
	}
	statuses := map[string]string{}
	for _, result := range first.Saves {
		statuses[result.GameID] = result.Status
	}
	if statuses[configured.ID] != "success" || statuses[pathless.ID] != "unconfigured" || statuses[disabled.ID] != "disabled" {
		t.Fatalf("unexpected first statuses: %+v", statuses)
	}
	if catalog.manifestLoadCount(pathless.ID) != 0 || catalog.manifestLoadCount(disabled.ID) != 0 {
		t.Fatalf("pathless/disabled games touched remote manifests: pathless=%d disabled=%d",
			catalog.manifestLoadCount(pathless.ID), catalog.manifestLoadCount(disabled.ID))
	}
	if first.Stats.EnumeratedGames != 1 || first.Stats.HashedFiles != 1 {
		t.Fatalf("first sync stats = %+v, want one enumerated/hashed game file", first.Stats)
	}
	putsAfterFirst := objects.putCount

	second, err := app.RunSyncAll()
	if err != nil {
		t.Fatal(err)
	}
	if second.Stats.EnumeratedGames != 0 || second.Stats.StattedFiles != 0 || second.Stats.HashedFiles != 0 ||
		second.Stats.UploadedObjects != 0 || second.Stats.DownloadedObjects != 0 {
		t.Fatalf("clean sync accessed local/object resources: %+v", second.Stats)
	}
	if objects.putCount != putsAfterFirst {
		t.Fatalf("clean sync wrote objects: before=%d after=%d", putsAfterFirst, objects.putCount)
	}
}

func TestRunSyncAllUploadsCoverForPathlessGame(t *testing.T) {
	app, store, catalog, objects := newSyncCoordinatorFixture(t)
	coverPath := filepath.Join(store.DataDir(), "pathless-cover.jpg")
	if err := os.WriteFile(coverPath, []byte("cover-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	game, err := store.UpsertGame(core.Game{
		ID: "pathless-cover", Name: "Pathless Cover", CoverPath: coverPath,
		CoverSourceType: coverSourceLocalFile, CoverSource: coverPath,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := app.RunSyncAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Covers) != 1 || result.Covers[0].Status != "uploaded" {
		t.Fatalf("cover result = %+v", result.Covers)
	}
	if len(result.Saves) != 1 || result.Saves[0].Status != "unconfigured" {
		t.Fatalf("save result = %+v", result.Saves)
	}
	if catalog.manifestLoadCount(game.ID) != 0 {
		t.Fatalf("pathless cover sync loaded save manifest %d times", catalog.manifestLoadCount(game.ID))
	}
	updated, err := findGame(store.Snapshot(), game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CoverCloudKey == "" || string(objects.objects[updated.CoverCloudKey]) != "cover-data" {
		t.Fatalf("pathless cover was not uploaded: game=%+v objects=%+v", updated, objects.objects)
	}
}

func TestRunSyncAllContinuesAfterObjectUploadFailure(t *testing.T) {
	app, store, catalog, objects := newSyncCoordinatorFixture(t)
	firstGame := addSyncCoordinatorGame(t, store, "first-game", "first-save")
	secondGame := addSyncCoordinatorGame(t, store, "second-game", "second-save")
	objects.failPut = 1

	result, err := app.RunSyncAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Saves) != 2 || result.Saves[0].GameID != firstGame.ID || result.Saves[1].GameID != secondGame.ID {
		t.Fatalf("save order/results = %+v", result.Saves)
	}
	if result.Saves[0].Status != "failed" || result.Saves[1].Status != "success" {
		t.Fatalf("failure did not stay isolated: %+v", result.Saves)
	}
	if objects.putCount != 2 {
		t.Fatalf("object upload attempts = %d, want failed first and successful second", objects.putCount)
	}
	if result.Stats.UploadedObjects != 1 {
		t.Fatalf("successful uploaded object count = %d, want 1", result.Stats.UploadedObjects)
	}
	if catalog.manifestLoadCount(firstGame.ID) != 1 || catalog.manifestLoadCount(secondGame.ID) != 1 {
		t.Fatalf("manifest loads: first=%d second=%d",
			catalog.manifestLoadCount(firstGame.ID), catalog.manifestLoadCount(secondGame.ID))
	}
	if remote := catalog.remoteManifest(firstGame.ID); remote.Version != 0 {
		t.Fatalf("failed game committed remote manifest: %+v", remote)
	}
	secondRemote := catalog.remoteManifest(secondGame.ID)
	if secondRemote.Version != 1 || secondRemote.Manifest.Hash == "" {
		t.Fatalf("second game remote manifest = %+v", secondRemote)
	}

	index, err := app.ensureDeviceIndex()
	if err != nil {
		t.Fatal(err)
	}
	firstIndex, ok := index.Game(firstGame.ID)
	if !ok || firstIndex.ScanState != core.ScanStateRebuild {
		t.Fatalf("failed game index = %+v, exists=%v", firstIndex, ok)
	}
	secondIndex, ok := index.Game(secondGame.ID)
	if !ok || secondIndex.ScanState != core.ScanStateClean || secondIndex.RemoteVersion != 1 ||
		secondIndex.RemoteManifestHash != secondRemote.Manifest.Hash {
		t.Fatalf("successful game index = %+v, exists=%v", secondIndex, ok)
	}

	firstStored, err := findGame(store.Snapshot(), firstGame.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondStored, err := findGame(store.Snapshot(), secondGame.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstStored.LastSync == nil || firstStored.LastSync.Status != "failed" || firstStored.Anchor.LastRemoteVersion != 0 {
		t.Fatalf("failed game persisted state = %+v", firstStored)
	}
	if secondStored.LastSync == nil || secondStored.LastSync.Status != "success" || secondStored.Anchor.LastRemoteVersion != 1 {
		t.Fatalf("successful game persisted state = %+v", secondStored)
	}
}

func TestRunSyncAllRetriesRemoteManifestConflict(t *testing.T) {
	app, store, catalog, objects := newSyncCoordinatorFixture(t)
	retryingCatalog := &manifestConflictOnceCatalogStore{countingCatalogStore: catalog}
	app.catalogStoreFn = func(core.CloudflareAccount) (core.CatalogStore, error) {
		return retryingCatalog, nil
	}
	game := addSyncCoordinatorGame(t, store, "retry-game", "retry-save")

	result, err := app.RunSyncAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Saves) != 1 || result.Saves[0].GameID != game.ID || result.Saves[0].Status != "success" {
		t.Fatalf("retry result = %+v", result.Saves)
	}
	if catalog.manifestLoadCount(game.ID) != 2 {
		t.Fatalf("manifest load count = %d, want initial load plus one bounded reload", catalog.manifestLoadCount(game.ID))
	}
	if retryingCatalog.manifestSaveCount() != 2 {
		t.Fatalf("manifest save count = %d, want conflict plus successful retry", retryingCatalog.manifestSaveCount())
	}
	if objects.putCount != 2 {
		t.Fatalf("object upload attempts = %d, want one per manifest attempt", objects.putCount)
	}

	remote := catalog.remoteManifest(game.ID)
	if remote.Version != 1 || remote.Manifest.Version != 1 || remote.Manifest.Hash == "" {
		t.Fatalf("remote manifest after retry = %+v", remote)
	}
	index, err := app.ensureDeviceIndex()
	if err != nil {
		t.Fatal(err)
	}
	indexed, ok := index.Game(game.ID)
	if !ok || indexed.ScanState != core.ScanStateClean || indexed.RemoteVersion != remote.Version ||
		indexed.RemoteManifestHash != remote.Manifest.Hash || len(indexed.Files) != 1 {
		t.Fatalf("index after retry = %+v, exists=%v", indexed, ok)
	}
	stored, err := findGame(store.Snapshot(), game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LastSync == nil || stored.LastSync.Status != "success" ||
		stored.Anchor.LastRemoteVersion != remote.Version || stored.Anchor.LastManifest.Hash != remote.Manifest.Hash {
		t.Fatalf("persisted game after retry = %+v", stored)
	}
}

func TestCoverRetryDelayIsBounded(t *testing.T) {
	want := []time.Duration{30 * time.Second, 60 * time.Second, 2 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for attempt, expected := range want {
		if got := coverRetryDelay(attempt); got != expected {
			t.Fatalf("attempt %d delay = %s, want %s", attempt, got, expected)
		}
	}
}

func TestCoverSyncEmitsOneTerminalStateAfterStarting(t *testing.T) {
	app, store, _, objects := newSyncCoordinatorFixture(t)
	t.Cleanup(app.stopCoverRetries)
	coverPath := filepath.Join(store.DataDir(), "terminal-cover.jpg")
	if err := os.WriteFile(coverPath, []byte("cover-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	game, err := store.UpsertGame(core.Game{
		ID: "terminal-cover", Name: "Terminal Cover", CoverPath: coverPath,
		CoverSourceType: coverSourceLocalFile, CoverSource: coverPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	objects.failPut = 1
	var events []map[string]string
	app.runtimeEventFn = func(name string, payload any) {
		if name == "cover:sync_state" {
			events = append(events, payload.(map[string]string))
		}
	}

	result := app.syncCoverForCoordinator(game.ID)
	if result.Status != "pending" {
		t.Fatalf("cover result = %+v", result)
	}
	if len(events) != 2 || events[0]["status"] != "syncing" || events[1]["status"] != "pending" {
		t.Fatalf("cover event sequence = %+v", events)
	}
}

func TestPendingCoverRetriesAndClearsAfterSuccess(t *testing.T) {
	app, store, _, objects := newSyncCoordinatorFixture(t)
	t.Cleanup(app.stopCoverRetries)
	app.coverRetryDelayFn = func(int) time.Duration { return 10 * time.Millisecond }
	coverPath := filepath.Join(store.DataDir(), "retry-cover.jpg")
	if err := os.WriteFile(coverPath, []byte("retry-cover-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	game, err := store.UpsertGame(core.Game{
		ID: "retry-cover", Name: "Retry Cover", CoverPath: coverPath,
		CoverSourceType: coverSourceLocalFile, CoverSource: coverPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	objects.failPut = 1
	succeeded := make(chan struct{}, 1)
	app.runtimeEventFn = func(name string, payload any) {
		if name != "cover:sync_state" {
			return
		}
		if payload.(map[string]string)["status"] == "succeeded" {
			select {
			case succeeded <- struct{}{}:
			default:
			}
		}
	}

	if result := app.syncCoverForCoordinator(game.ID); result.Status != "pending" {
		t.Fatalf("initial cover result = %+v", result)
	}
	select {
	case <-succeeded:
	case <-time.After(2 * time.Second):
		t.Fatal("pending cover was not retried successfully")
	}
	updated, err := findGame(store.Snapshot(), game.ID)
	if err != nil || updated.CoverCloudKey == "" || objects.puts[updated.CoverCloudKey] != 1 {
		t.Fatalf("retried cover = %+v, puts=%v, err=%v", updated, objects.puts, err)
	}
	app.coverRetryMu.Lock()
	pendingTimers := len(app.coverRetryTimers)
	pendingAttempts := len(app.coverRetryAttempts)
	app.coverRetryMu.Unlock()
	if pendingTimers != 0 || pendingAttempts != 0 {
		t.Fatalf("retry state was not cleared: timers=%d attempts=%d", pendingTimers, pendingAttempts)
	}
}
