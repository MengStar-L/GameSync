package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"gamesync/internal/core"
)

type headCatalogStore struct {
	*countingCatalogStore
	mu        sync.Mutex
	headCalls int
	headErr   error
}

func (c *headCatalogStore) ListRemoteManifestHeads(context.Context) ([]core.RemoteManifestHead, error) {
	c.mu.Lock()
	c.headCalls++
	err := c.headErr
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	c.migrationCatalogStore.mu.Lock()
	defer c.migrationCatalogStore.mu.Unlock()
	heads := make([]core.RemoteManifestHead, 0, len(c.manifests))
	for gameID, record := range c.manifests {
		heads = append(heads, core.RemoteManifestHead{
			GameID: gameID, Version: record.Version,
			Token: fmt.Sprintf("test:%d:%s", record.Version, record.Manifest.Hash),
		})
	}
	sort.Slice(heads, func(i, j int) bool { return heads[i].GameID < heads[j].GameID })
	return heads, nil
}

func (c *headCatalogStore) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.headCalls
}

type fakeBackgroundTimer struct {
	mu      sync.Mutex
	stopped bool
}

func (t *fakeBackgroundTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	wasActive := !t.stopped
	t.stopped = true
	return wasActive
}

func (t *fakeBackgroundTimer) isStopped() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stopped
}

func TestBackgroundSyncNoChangesOnlyListsManifestHeads(t *testing.T) {
	app, store, catalog, objects := newSyncCoordinatorFixture(t)
	game := addSyncCoordinatorGame(t, store, "background-clean", "baseline")
	if _, err := app.RunSync(core.SyncRunRequest{GameID: game.ID}); err != nil {
		t.Fatal(err)
	}
	headCatalog := &headCatalogStore{countingCatalogStore: catalog}
	app.catalogStoreFn = func(core.CloudflareAccount) (core.CatalogStore, error) { return headCatalog, nil }
	loadsBefore := catalog.manifestLoadCount(game.ID)
	putsBefore := objects.putCount

	if _, err := app.runBackgroundSyncOnce("test"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.runBackgroundSyncOnce("test"); err != nil {
		t.Fatal(err)
	}
	if headCatalog.callCount() != 2 {
		t.Fatalf("head calls = %d, want 2", headCatalog.callCount())
	}
	if got := catalog.manifestLoadCount(game.ID); got != loadsBefore {
		t.Fatalf("clean polls loaded manifest %d extra times", got-loadsBefore)
	}
	if objects.putCount != putsBefore {
		t.Fatalf("clean polls uploaded objects: before=%d after=%d", putsBefore, objects.putCount)
	}
	index, err := app.ensureDeviceIndex()
	if err != nil {
		t.Fatal(err)
	}
	indexed, _ := index.Game(game.ID)
	if indexed.RemoteManifestToken == "" || indexed.ScanState != core.ScanStateClean {
		t.Fatalf("clean poll index = %+v", indexed)
	}
}

func TestBackgroundSyncForcesManualConflictAndDoesNotRepeat(t *testing.T) {
	app, store, catalog, objects := newSyncCoordinatorFixture(t)
	game := addSyncCoordinatorGame(t, store, "background-conflict", "baseline")
	if _, err := app.RunSync(core.SyncRunRequest{GameID: game.ID}); err != nil {
		t.Fatal(err)
	}
	headCatalog := &headCatalogStore{countingCatalogStore: catalog}
	app.catalogStoreFn = func(core.CloudflareAccount) (core.CatalogStore, error) { return headCatalog, nil }
	prefs := store.Snapshot().Preferences
	prefs.ConflictPolicy = "local"
	if err := store.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}

	localContent := []byte("local-change")
	if err := os.WriteFile(filepath.Join(game.SavePath, "profile.dat"), localContent, 0o600); err != nil {
		t.Fatal(err)
	}
	index, err := app.ensureDeviceIndex()
	if err != nil {
		t.Fatal(err)
	}
	if err := index.MarkDirty(game.ID, "profile.dat"); err != nil {
		t.Fatal(err)
	}

	remoteContent := []byte("remote-change")
	remoteHash := fmt.Sprintf("%x", sha256.Sum256(remoteContent))
	remote := catalog.remoteManifest(game.ID)
	remote.Version++
	remote.Manifest.Version = remote.Version
	remote.Manifest.Hash = "remote-v2"
	remote.Manifest.GeneratedAt = time.Now().UTC()
	remote.Manifest.Files = []core.ManifestFile{{
		Path: "profile.dat", Size: int64(len(remoteContent)), ModifiedAt: time.Now().UTC(), SHA256: remoteHash,
	}}
	catalog.migrationCatalogStore.mu.Lock()
	catalog.manifests[game.ID] = remote
	catalog.migrationCatalogStore.mu.Unlock()
	objects.mu.Lock()
	objects.objects[fmt.Sprintf("games/%s/objects/%s", game.ID, remoteHash)] = remoteContent
	objects.mu.Unlock()

	outcome, err := app.runBackgroundSyncOnce("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.ConflictGameIDs) != 1 || outcome.ConflictGameIDs[0] != game.ID {
		t.Fatalf("outcome = %+v", outcome)
	}
	stored, err := findGame(store.Snapshot(), game.ID)
	if err != nil || stored.LastSync == nil || stored.LastSync.Status != "conflict" {
		t.Fatalf("stored game = %+v, err=%v", stored, err)
	}
	content, err := os.ReadFile(filepath.Join(game.SavePath, "profile.dat"))
	if err != nil || string(content) != string(localContent) {
		t.Fatalf("local content = %q, err=%v", content, err)
	}
	if got := catalog.remoteManifest(game.ID); got.Version != remote.Version || got.Manifest.Hash != remote.Manifest.Hash {
		t.Fatalf("remote manifest was overwritten: %+v", got)
	}
	activitiesBefore := len(store.Snapshot().Activities)
	if _, err := app.runBackgroundSyncOnce("test"); err != nil {
		t.Fatal(err)
	}
	if len(store.Snapshot().Activities) != activitiesBefore {
		t.Fatal("unresolved background conflict was retried")
	}
}

func TestBackgroundSyncDownloadsRemoteOnlyChange(t *testing.T) {
	app, store, catalog, objects := newSyncCoordinatorFixture(t)
	game := addSyncCoordinatorGame(t, store, "background-download", "baseline")
	if _, err := app.RunSync(core.SyncRunRequest{GameID: game.ID}); err != nil {
		t.Fatal(err)
	}
	headCatalog := &headCatalogStore{countingCatalogStore: catalog}
	app.catalogStoreFn = func(core.CloudflareAccount) (core.CatalogStore, error) { return headCatalog, nil }
	if _, err := app.runBackgroundSyncOnce("establish token"); err != nil {
		t.Fatal(err)
	}

	remoteContent := []byte("newer-remote-save")
	remoteHash := fmt.Sprintf("%x", sha256.Sum256(remoteContent))
	remote := catalog.remoteManifest(game.ID)
	remote.Version++
	remote.Manifest.Version = remote.Version
	remote.Manifest.Hash = "remote-download-v2"
	remote.Manifest.GeneratedAt = time.Now().UTC()
	remote.Manifest.Files = []core.ManifestFile{{
		Path: "profile.dat", Size: int64(len(remoteContent)), ModifiedAt: time.Now().UTC(), SHA256: remoteHash,
	}}
	catalog.migrationCatalogStore.mu.Lock()
	catalog.manifests[game.ID] = remote
	catalog.migrationCatalogStore.mu.Unlock()
	objects.mu.Lock()
	objects.objects[fmt.Sprintf("games/%s/objects/%s", game.ID, remoteHash)] = remoteContent
	objects.mu.Unlock()

	outcome, err := app.runBackgroundSyncOnce("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.ChangedGameIDs) != 1 || outcome.ChangedGameIDs[0] != game.ID {
		t.Fatalf("outcome = %+v", outcome)
	}
	content, err := os.ReadFile(filepath.Join(game.SavePath, "profile.dat"))
	if err != nil || string(content) != string(remoteContent) {
		t.Fatalf("downloaded content = %q, err=%v", content, err)
	}
}

func TestBackgroundSyncUploadsLocalOnlyChange(t *testing.T) {
	app, store, catalog, _ := newSyncCoordinatorFixture(t)
	game := addSyncCoordinatorGame(t, store, "background-upload", "baseline")
	if _, err := app.RunSync(core.SyncRunRequest{GameID: game.ID}); err != nil {
		t.Fatal(err)
	}
	headCatalog := &headCatalogStore{countingCatalogStore: catalog}
	app.catalogStoreFn = func(core.CloudflareAccount) (core.CatalogStore, error) { return headCatalog, nil }
	if _, err := app.runBackgroundSyncOnce("establish token"); err != nil {
		t.Fatal(err)
	}

	localContent := []byte("newer-local-save")
	if err := os.WriteFile(filepath.Join(game.SavePath, "profile.dat"), localContent, 0o600); err != nil {
		t.Fatal(err)
	}
	index, err := app.ensureDeviceIndex()
	if err != nil {
		t.Fatal(err)
	}
	if err := index.MarkDirty(game.ID, "profile.dat"); err != nil {
		t.Fatal(err)
	}

	outcome, err := app.runBackgroundSyncOnce("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.ChangedGameIDs) != 1 || outcome.ChangedGameIDs[0] != game.ID {
		t.Fatalf("outcome = %+v", outcome)
	}
	remote := catalog.remoteManifest(game.ID)
	wantHash := fmt.Sprintf("%x", sha256.Sum256(localContent))
	if remote.Version != 2 || len(remote.Manifest.Files) != 1 || remote.Manifest.Files[0].SHA256 != wantHash {
		t.Fatalf("remote manifest = %+v", remote)
	}
}

func TestBackgroundSyncPullsRemoteCatalogChanges(t *testing.T) {
	app, store, catalog, _ := newSyncCoordinatorFixture(t)
	game := addSyncCoordinatorGame(t, store, "background-catalog", "baseline")
	if _, err := app.RunSync(core.SyncRunRequest{GameID: game.ID}); err != nil {
		t.Fatal(err)
	}
	headCatalog := &headCatalogStore{countingCatalogStore: catalog}
	app.catalogStoreFn = func(core.CloudflareAccount) (core.CatalogStore, error) { return headCatalog, nil }
	if _, err := app.runBackgroundSyncOnce("establish token"); err != nil {
		t.Fatal(err)
	}

	catalog.migrationCatalogStore.mu.Lock()
	remoteGame := catalog.catalog.Games[0]
	remoteGame.Name = "Remote Name"
	remoteGame.MetadataUpdatedAt = time.Now().Add(time.Hour)
	remoteGame.CatalogUpdatedAt = remoteGame.MetadataUpdatedAt
	catalog.catalog.Games[0] = remoteGame
	catalog.revision++
	catalog.migrationCatalogStore.mu.Unlock()

	if _, err := app.runBackgroundSyncOnce("test"); err != nil {
		t.Fatal(err)
	}
	updated, err := findGame(store.Snapshot(), game.ID)
	if err != nil || updated.Name != "Remote Name" {
		t.Fatalf("updated game = %+v, err=%v", updated, err)
	}
}

func TestBackgroundSyncOfflinePreservesLocalAndRecovers(t *testing.T) {
	app, store, catalog, _ := newSyncCoordinatorFixture(t)
	game := addSyncCoordinatorGame(t, store, "background-offline", "local-save")
	if _, err := app.RunSync(core.SyncRunRequest{GameID: game.ID}); err != nil {
		t.Fatal(err)
	}
	headCatalog := &headCatalogStore{countingCatalogStore: catalog, headErr: errors.New("injected offline")}
	app.catalogStoreFn = func(core.CloudflareAccount) (core.CatalogStore, error) { return headCatalog, nil }
	if _, err := app.runBackgroundSyncOnce("test"); err == nil {
		t.Fatal("offline manifest check succeeded")
	}
	content, err := os.ReadFile(filepath.Join(game.SavePath, "profile.dat"))
	if err != nil || string(content) != "local-save" {
		t.Fatalf("offline check changed local content: %q, err=%v", content, err)
	}
	headCatalog.mu.Lock()
	headCatalog.headErr = nil
	headCatalog.mu.Unlock()
	if _, err := app.runBackgroundSyncOnce("recovered"); err != nil {
		t.Fatalf("recovered check failed: %v", err)
	}
}

func TestBackgroundSyncDoesNotWaitBehindManualCoordinator(t *testing.T) {
	app, _, _, _ := newSyncCoordinatorFixture(t)
	app.syncCoordinatorMu.Lock()
	_, err := app.runBackgroundSyncOnce("test")
	app.syncCoordinatorMu.Unlock()
	if !errors.Is(err, errBackgroundSyncBusy) {
		t.Fatalf("busy result = %v", err)
	}
}

func TestBackgroundSyncStartupRunsImmediatelyAndSchedulesConfiguredInterval(t *testing.T) {
	for _, seconds := range []int{30, 60, 300} {
		t.Run(fmt.Sprintf("%d seconds", seconds), func(t *testing.T) {
			store, err := core.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			prefs := store.Snapshot().Preferences
			prefs.BackgroundSyncIntervalSeconds = seconds
			if err := store.SavePreferences(prefs); err != nil {
				t.Fatal(err)
			}
			app := NewApp()
			app.store = store
			app.baseDir = store.DataDir()
			t.Cleanup(app.stopBackgroundSyncScheduler)
			scheduled := make(chan time.Duration, 2)
			app.backgroundSyncAfterFn = func(delay time.Duration, _ func()) backgroundTimer {
				scheduled <- delay
				return &fakeBackgroundTimer{}
			}
			app.startBackgroundSyncScheduler()
			select {
			case delay := <-scheduled:
				if delay != time.Duration(seconds)*time.Second {
					t.Fatalf("scheduled delay = %s", delay)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("next background check was not scheduled")
			}
		})
	}
}

func TestBackgroundSyncRuntimeIntervalResetChecksImmediately(t *testing.T) {
	store, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.store = store
	app.baseDir = store.DataDir()
	t.Cleanup(app.stopBackgroundSyncScheduler)
	type scheduledTimer struct {
		delay time.Duration
		timer *fakeBackgroundTimer
	}
	scheduled := make(chan scheduledTimer, 2)
	app.backgroundSyncAfterFn = func(delay time.Duration, _ func()) backgroundTimer {
		timer := &fakeBackgroundTimer{}
		scheduled <- scheduledTimer{delay: delay, timer: timer}
		return timer
	}
	app.startBackgroundSyncScheduler()

	first := <-scheduled
	if first.delay != time.Minute {
		t.Fatalf("initial delay = %s", first.delay)
	}
	prefs := store.Snapshot().Preferences
	prefs.BackgroundSyncIntervalSeconds = 30
	if err := store.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	app.resetBackgroundSyncScheduler()

	select {
	case next := <-scheduled:
		if next.delay != 30*time.Second {
			t.Fatalf("reset delay = %s", next.delay)
		}
		if !first.timer.isStopped() {
			t.Fatal("previous schedule was not cancelled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("interval reset did not trigger an immediate check")
	}
}

func TestBackgroundSyncDisabledRejectsStaleRequests(t *testing.T) {
	store, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.store = store
	timer := &fakeBackgroundTimer{}
	app.backgroundSyncMu.Lock()
	app.backgroundSyncStarted = true
	app.backgroundSyncTimer = timer
	app.backgroundSyncMu.Unlock()

	prefs := store.Snapshot().Preferences
	prefs.BackgroundSyncIntervalSeconds = 0
	if err := store.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	app.resetBackgroundSyncScheduler()
	app.requestBackgroundSync("stale timer")

	app.backgroundSyncMu.Lock()
	active := app.backgroundSyncActive
	queued := app.backgroundSyncQueued
	installedTimer := app.backgroundSyncTimer
	app.backgroundSyncMu.Unlock()
	if active || queued || installedTimer != nil {
		t.Fatalf("disabled scheduler accepted stale request: active=%v queued=%v timer=%v", active, queued, installedTimer)
	}
	if !timer.isStopped() {
		t.Fatal("disabled scheduler did not cancel its timer")
	}
}

func TestBackgroundSyncSkipsRunningGame(t *testing.T) {
	app, store, catalog, _ := newSyncCoordinatorFixture(t)
	game := addSyncCoordinatorGame(t, store, "background-running", "baseline")
	if _, err := app.RunSync(core.SyncRunRequest{GameID: game.ID}); err != nil {
		t.Fatal(err)
	}
	headCatalog := &headCatalogStore{countingCatalogStore: catalog}
	app.catalogStoreFn = func(core.CloudflareAccount) (core.CatalogStore, error) { return headCatalog, nil }
	index, err := app.ensureDeviceIndex()
	if err != nil {
		t.Fatal(err)
	}
	if err := index.MarkDirty(game.ID, "profile.dat"); err != nil {
		t.Fatal(err)
	}
	app.runningGameIDsFn = func([]core.Game) map[string]bool { return map[string]bool{game.ID: true} }
	app.backgroundSyncStarted = true
	deferredDelay := make(chan time.Duration, 1)
	app.backgroundSyncAfterFn = func(delay time.Duration, _ func()) backgroundTimer {
		deferredDelay <- delay
		return &fakeBackgroundTimer{}
	}
	activitiesBefore := len(store.Snapshot().Activities)

	outcome, err := app.runBackgroundSyncOnce("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.ChangedGameIDs) != 0 || len(store.Snapshot().Activities) != activitiesBefore {
		t.Fatalf("running game was synchronized: outcome=%+v", outcome)
	}
	select {
	case delay := <-deferredDelay:
		if delay != 5*time.Second {
			t.Fatalf("exit poll delay = %s", delay)
		}
	case <-time.After(time.Second):
		t.Fatal("running game was not deferred")
	}
	app.stopBackgroundSyncScheduler()
}

func TestBackgroundSyncIntervalUsesSupportedPreferenceValues(t *testing.T) {
	store, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.store = store
	for _, seconds := range []int{0, 30, 60, 300} {
		prefs := store.Snapshot().Preferences
		prefs.BackgroundSyncIntervalSeconds = seconds
		if err := store.SavePreferences(prefs); err != nil {
			t.Fatal(err)
		}
		if got := app.backgroundSyncInterval(); got != time.Duration(seconds)*time.Second {
			t.Fatalf("interval for %d = %s", seconds, got)
		}
	}
}
