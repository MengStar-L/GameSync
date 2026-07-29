package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gamesync/internal/core"
)

func TestSelectCoverStorageAccountSupportsWebdav(t *testing.T) {
	account := core.CloudflareAccount{
		ID: "dav", Provider: core.ProviderWebdav, WebdavURL: "https://dav.example.test",
		WebdavUsername: "u", WebdavPassword: "p", Enabled: true, IsPrimary: true,
	}
	selected, ok := selectCoverStorageAccount(core.AppState{Accounts: []core.CloudflareAccount{account}}, core.Game{StorageAccountID: account.ID})
	if !ok || selected.ID != account.ID {
		t.Fatalf("webdav cover account was not selected: %+v, %v", selected, ok)
	}
}

func TestBuildCoverObjectKeyUsesContentHash(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.jpg")
	second := filepath.Join(dir, "second.jpg")
	if err := os.WriteFile(first, []byte("first cover"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second cover"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstKey, err := buildCoverObjectKey("game", first)
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := buildCoverObjectKey("game", second)
	if err != nil {
		t.Fatal(err)
	}
	if firstKey == secondKey || filepath.Ext(firstKey) != ".jpg" || filepath.Ext(secondKey) != ".jpg" {
		t.Fatalf("content-addressed cover keys = %q, %q", firstKey, secondKey)
	}
}

func TestRunSyncBackfillsLegacyCoverToCurrentWebdav(t *testing.T) {
	dataDir := t.TempDir()
	store, err := core.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	target, err := store.UpsertAccount(core.CloudflareAccount{
		ID: "webdav", Provider: core.ProviderWebdav, Enabled: true, IsPrimary: true,
		WebdavURL: "https://dav.example.test", WebdavUsername: "user", WebdavPassword: "password", WebdavRoot: "GameSync",
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
	coverPath := filepath.Join(dataDir, "missing-original.jpg")
	coverCachePath := filepath.Join(dataDir, "covers", "game", "cover.jpg")
	if err := os.MkdirAll(filepath.Dir(coverCachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coverCachePath, []byte("cover-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	game, err := store.UpsertGame(core.Game{
		ID: "game", Name: "Game", SavePath: saveDir, StorageAccountID: target.ID,
		CoverPath: coverPath, CoverSourceType: coverSourceLocalFile, CoverSource: coverPath, CoverLocalPath: coverCachePath,
		CoverCloudAccountID: "legacy-r2", CoverCloudKey: "covers/game/cover.jpg",
		Sync: core.SyncConfig{Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	catalog := newMigrationCatalogStore()
	objects := newMigrationObjectStore()
	app := NewApp()
	app.store = store
	app.baseDir = dataDir
	app.catalogStoreFn = func(core.CloudflareAccount) (core.CatalogStore, error) { return catalog, nil }
	app.objectStoreFn = func(context.Context, core.CloudflareAccount) (core.ObjectStore, error) { return objects, nil }
	defer func() {
		app.catalogSyncMu.Lock()
		if app.catalogSyncTimer != nil {
			app.catalogSyncTimer.Stop()
		}
		app.catalogSyncMu.Unlock()
	}()

	if _, err := app.RunSync(core.SyncRunRequest{GameID: game.ID}); err != nil {
		t.Fatal(err)
	}
	updated, err := findGame(store.Snapshot(), game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastSync == nil || updated.LastSync.Status != "success" {
		t.Fatalf("sync result = %+v", updated.LastSync)
	}
	if updated.CoverCloudAccountID != target.ID || !strings.HasPrefix(updated.CoverCloudKey, "covers/game/") || updated.CoverCloudKey == "covers/game/cover.jpg" {
		t.Fatalf("cover route was not backfilled: %+v", updated)
	}
	if string(objects.objects[updated.CoverCloudKey]) != "cover-data" || objects.puts[updated.CoverCloudKey] != 1 {
		t.Fatalf("cover object = %q, puts = %d", objects.objects[updated.CoverCloudKey], objects.puts[updated.CoverCloudKey])
	}

	if _, err := app.RunSync(core.SyncRunRequest{GameID: game.ID}); err != nil {
		t.Fatal(err)
	}
	if objects.puts[updated.CoverCloudKey] != 1 {
		t.Fatalf("unchanged cover was uploaded %d times", objects.puts[updated.CoverCloudKey])
	}
}

func TestSyncCoverUsesValidatedCacheBeforeLegacyReference(t *testing.T) {
	dataDir := t.TempDir()
	store, err := core.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.UpsertAccount(core.CloudflareAccount{
		Provider: core.ProviderWebdav, Enabled: true, IsPrimary: true,
		WebdavURL: "https://dav.example.test", WebdavUsername: "user", WebdavPassword: "password",
	})
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(dataDir, "covers", "game")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(cacheDir, "cover.jpg")
	if err := os.WriteFile(cachePath, []byte("content-addressed-cover"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := sha256FileHex(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	objectKey := "covers/game/" + hash + ".jpg"
	legacyReference := makeCoverReference("legacy-r2", objectKey)
	game, err := store.UpsertGame(core.Game{
		ID: "game", Name: "Game", StorageAccountID: account.ID,
		CoverPath: legacyReference, CoverSourceType: coverSourceLocalFile, CoverSource: legacyReference,
		CoverLocalPath: cachePath, CoverCloudAccountID: account.ID, CoverCloudKey: objectKey,
	})
	if err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.store = store
	app.baseDir = dataDir
	t.Cleanup(app.stopCoverRetries)
	remoteCalls := 0
	app.objectStoreFn = func(context.Context, core.CloudflareAccount) (core.ObjectStore, error) {
		remoteCalls++
		return nil, errors.New("unexpected remote access")
	}
	var coverStatuses []string
	app.runtimeEventFn = func(name string, payload any) {
		if name == "cover:sync_state" {
			coverStatuses = append(coverStatuses, payload.(map[string]string)["status"])
		}
	}

	result := app.syncCoverForCoordinator(game.ID)
	if result.Status != "skipped" || result.Message != "" {
		t.Fatalf("cover result = %+v", result)
	}
	if len(coverStatuses) != 2 || coverStatuses[0] != "syncing" || coverStatuses[1] != "skipped" {
		t.Fatalf("cover status sequence = %v", coverStatuses)
	}
	if remoteCalls != 0 {
		t.Fatalf("validated local cache triggered %d remote calls", remoteCalls)
	}
	index, err := app.ensureDeviceIndex()
	if err != nil {
		t.Fatal(err)
	}
	indexed, ok := index.Game(game.ID)
	wantCover := core.DeviceCoverIndex{SourceFingerprint: hash, AccountID: account.ID, ObjectKey: objectKey}
	if !ok || indexed.Cover != wantCover {
		t.Fatalf("device cover index = %+v, exists=%v, want=%+v", indexed.Cover, ok, wantCover)
	}
}

func TestSaveGameSameCoverPathContentChangeUsesNewObjectKey(t *testing.T) {
	dataDir := t.TempDir()
	store, err := core.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.UpsertAccount(core.CloudflareAccount{
		ID: "webdav", Provider: core.ProviderWebdav, Enabled: true, IsPrimary: true,
		WebdavURL: "https://dav.example.test", WebdavUsername: "user", WebdavPassword: "password",
	})
	if err != nil {
		t.Fatal(err)
	}
	coverPath := filepath.Join(dataDir, "cover.jpg")
	if err := os.WriteFile(coverPath, []byte("first-cover"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := newMigrationCatalogStore()
	objects := newMigrationObjectStore()
	app := NewApp()
	app.store = store
	app.baseDir = dataDir
	app.catalogStoreFn = func(core.CloudflareAccount) (core.CatalogStore, error) { return catalog, nil }
	app.objectStoreFn = func(context.Context, core.CloudflareAccount) (core.ObjectStore, error) { return objects, nil }
	t.Cleanup(app.closeSyncTracking)

	firstSnapshot, err := app.SaveGame(core.Game{
		Name: "Game", CoverPath: coverPath, StorageAccountID: account.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	first := firstSnapshot.State.Games[0]
	if first.CoverCloudKey == "" {
		t.Fatal("first cover did not receive a cloud key")
	}
	if err := os.WriteFile(coverPath, []byte("second-cover"), 0o600); err != nil {
		t.Fatal(err)
	}
	secondSnapshot, err := app.SaveGame(first)
	if err != nil {
		t.Fatal(err)
	}
	second := secondSnapshot.State.Games[0]
	if second.CoverCloudKey == first.CoverCloudKey {
		t.Fatalf("changed cover reused old object key %q", first.CoverCloudKey)
	}
	if got := string(objects.objects[second.CoverCloudKey]); got != "second-cover" {
		t.Fatalf("new cover object content = %q", got)
	}
}

func TestResolveCoverSourceRepairsContentAddressedCacheWithoutRemoteAccess(t *testing.T) {
	dataDir := t.TempDir()
	store, err := core.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.UpsertAccount(core.CloudflareAccount{
		ID: "primary", Provider: core.ProviderWebdav, Enabled: true, IsPrimary: true,
		WebdavURL: "https://unavailable.example.test", WebdavUsername: "user", WebdavPassword: "password",
	})
	if err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(dataDir, "covers", "game", "cover.jpg")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("cached-cover"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := sha256FileHex(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	game, err := store.UpsertGame(core.Game{
		ID: "game", Name: "Game", CoverPath: filepath.Join(dataDir, "missing-original.jpg"),
		CoverSourceType: coverSourceLocalFile, CoverSource: filepath.Join(dataDir, "missing-original.jpg"),
		CoverLocalPath: cachePath, CoverCloudAccountID: account.ID,
		CoverCloudKey: "covers/game/" + hash + ".jpg",
	})
	if err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.store = store
	remoteCalls := 0
	app.catalogStoreFn = func(core.CloudflareAccount) (core.CatalogStore, error) {
		remoteCalls++
		return nil, errors.New("injected 502")
	}

	source, err := app.ResolveCoverSource(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(source, "data:") || remoteCalls != 0 {
		t.Fatalf("source prefix=%t remote calls=%d", strings.HasPrefix(source, "data:"), remoteCalls)
	}
	metadataBytes, err := os.ReadFile(cachePath + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var metadata coverCacheMetadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.AccountID != account.ID || metadata.ObjectKey != game.CoverCloudKey || metadata.SHA256 != hash {
		t.Fatalf("repaired metadata = %+v", metadata)
	}
}

func TestCoverCacheFreshnessRejectsContentHashMismatch(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "cover.jpg")
	if err := os.WriteFile(cachePath, []byte("wrong-cover"), 0o600); err != nil {
		t.Fatal(err)
	}
	game := core.Game{
		CoverCloudAccountID: "primary",
		CoverCloudKey:       "covers/game/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg",
	}
	fresh, repairMetadata := coverCacheFreshnessForGame(game, cachePath)
	if fresh || repairMetadata {
		t.Fatalf("fresh=%v repair=%v", fresh, repairMetadata)
	}
}

func TestWriteCoverCachePreservesTargetMetadataAndRemovesOldVariant(t *testing.T) {
	store, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.store = store
	targetPath, err := app.writeCoverCache("game", ".jpg", []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath+".json", []byte(`{"sha256":"first"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	oldPath := app.coverCachePath("game", ".png")
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath+".json", []byte(`{"sha256":"old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.writeCoverCache("game", ".jpg", []byte("second")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(targetPath + ".json"); err != nil {
		t.Fatalf("target metadata was removed: %v", err)
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old image still exists: %v", err)
	}
	if _, err := os.Stat(oldPath + ".json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old metadata still exists: %v", err)
	}
}

func TestResolveCoverSourceCopiesLocalOriginalAndWritesMetadata(t *testing.T) {
	dataDir := t.TempDir()
	store, err := core.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.UpsertAccount(core.CloudflareAccount{
		ID: "primary", Provider: core.ProviderWebdav, Enabled: true, IsPrimary: true,
		WebdavURL: "https://unavailable.example.test", WebdavUsername: "user", WebdavPassword: "password",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(dataDir, "original.jpg")
	if err := os.WriteFile(sourcePath, []byte("local-original"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := sha256FileHex(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	game, err := store.UpsertGame(core.Game{
		ID: "game", Name: "Game", CoverPath: sourcePath,
		CoverSourceType: coverSourceLocalFile, CoverSource: sourcePath,
		CoverCloudAccountID: account.ID, CoverCloudKey: "covers/game/" + hash + ".jpg",
	})
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.store = store

	if _, err := app.ResolveCoverSource(game.ID); err != nil {
		t.Fatal(err)
	}
	cachePath := app.coverCachePath(game.ID, ".jpg")
	if _, err := os.Stat(cachePath + ".json"); err != nil {
		t.Fatalf("local recopy metadata missing: %v", err)
	}
}

func TestLocateCoverCacheSkipsMetadataSidecar(t *testing.T) {
	store, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.store = store
	metadataPath := app.coverCachePath("game", ".jpg") + ".json"
	if err := os.MkdirAll(filepath.Dir(metadataPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataPath, []byte(`{"sha256":"metadata-only"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if path := app.locateCoverCache(core.Game{ID: "game"}); path != "" {
		t.Fatalf("metadata sidecar selected as cover: %q", path)
	}
}
