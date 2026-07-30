package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeSyncDownloader struct {
	files  map[string][]byte
	errors map[string]error
}

func (f fakeSyncDownloader) DownloadObjectToFile(_ context.Context, key string, destinationPath string) error {
	if err := f.errors[key]; err != nil {
		return err
	}
	data, ok := f.files[key]
	if !ok {
		return errors.New("object not found")
	}
	return os.WriteFile(destinationPath, data, 0o644)
}

func testSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func TestSyncGameWithPreparedManifestUnconfiguredDoesNotRequireGateway(t *testing.T) {
	game := Game{ID: "game-1", Name: "Pathless", Sync: DefaultSyncConfig()}
	summary, anchor, err := NewEngine().SyncGameWithPreparedManifest(
		context.Background(), DeviceInfo{ID: "device-1"}, game, nil, "", SyncManifest{}, RemoteManifestRecord{}, nil,
	)
	if err != nil {
		t.Fatalf("sync pathless game: %v", err)
	}
	if summary.Status != "unconfigured" {
		t.Fatalf("status = %q, want unconfigured", summary.Status)
	}
	if anchor.LastRemoteVersion != 0 || len(anchor.LastManifest.Files) != 0 {
		t.Fatalf("pathless sync changed anchor: %+v", anchor)
	}
}

func TestStableUploadSnapshotRejectsChangedMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "save.dat")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := &ManifestFile{
		Path:       "save.dat",
		Size:       info.Size(),
		ModifiedAt: info.ModTime().UTC(),
		SHA256:     testSHA256([]byte("before")),
	}
	if err := os.WriteFile(path, []byte("changed-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := stableUploadSnapshot(path, expected); !errors.Is(err, ErrLocalFileChanged) {
		_ = os.Remove(snapshot)
		t.Fatalf("stableUploadSnapshot error = %v, want ErrLocalFileChanged", err)
	}
}

func TestRemoteCatalogGameStripsLocalSyncState(t *testing.T) {
	now := time.Now()
	game := Game{
		ID:                  "game-1",
		Name:                "Game",
		InstallPath:         `C:\Games\Game.exe`,
		SavePath:            `C:\Users\Player\Saves`,
		CoverPath:           `C:\Users\Player\Pictures\cover.png`,
		CoverSourceType:     "local_file",
		CoverSource:         `C:\Users\Player\Pictures\cover.png`,
		CoverLocalPath:      `C:\Users\Player\AppData\Roaming\GameSync\covers\game-1\cover.png`,
		CoverCloudAccountID: "account-1",
		CoverCloudKey:       "covers/game-1/cover.png",
		Anchor: SyncAnchor{
			LastRemoteVersion: 3,
			LastManifest:      SyncManifest{Hash: "local-anchor"},
			StorageAccountID:  "storage-a",
		},
		LastSync:         &SyncSummary{Status: "success", SyncedAt: now},
		RuntimeUpdatedAt: now,
		CatalogUpdatedAt: now,
	}

	publicGame := remoteCatalogGame(game, now)

	if publicGame.InstallPath != "" || publicGame.SavePath != "" {
		t.Fatalf("remote catalog leaked local paths: install=%q save=%q", publicGame.InstallPath, publicGame.SavePath)
	}
	if publicGame.CoverLocalPath != "" {
		t.Fatalf("remote catalog leaked local cover cache path: %q", publicGame.CoverLocalPath)
	}
	if publicGame.CoverPath != "r2cover://account-1/covers/game-1/cover.png" ||
		publicGame.CoverSource != "r2cover://account-1/covers/game-1/cover.png" {
		t.Fatalf("remote catalog did not convert local cover to cloud reference: path=%q source=%q", publicGame.CoverPath, publicGame.CoverSource)
	}
	if publicGame.Anchor.LastRemoteVersion != 0 || publicGame.Anchor.LastManifest.Hash != "" || publicGame.Anchor.StorageAccountID != "" {
		t.Fatalf("remote catalog leaked sync anchor: %+v", publicGame.Anchor)
	}
	if publicGame.LastSync != nil {
		t.Fatalf("remote catalog leaked last sync: %+v", publicGame.LastSync)
	}
	// M4 修复后 RuntimeUpdatedAt 必须随 PlayTime 一起上云，不得清零
	if !publicGame.RuntimeUpdatedAt.Equal(now) {
		t.Fatalf("remote catalog dropped runtime timestamp: %s", publicGame.RuntimeUpdatedAt)
	}
}

func TestRemoteCatalogAccountStripsSecrets(t *testing.T) {
	now := time.Now()
	account := CloudflareAccount{
		ID:                "account-1",
		Name:              "Primary",
		AccountID:         "cloudflare-account",
		APIToken:          "api-token",
		D1DatabaseID:      "database",
		R2Bucket:          "bucket",
		R2AccessKeyID:     "r2-access",
		R2SecretAccessKey: "r2-secret",
		IsPrimary:         true,
		Enabled:           true,
	}

	publicAccount := remoteCatalogAccount(account, now)

	if publicAccount.APIToken != "" || publicAccount.R2AccessKeyID != "" || publicAccount.R2SecretAccessKey != "" {
		t.Fatalf("remote catalog leaked account secrets: %+v", publicAccount)
	}
	if publicAccount.AccountID != account.AccountID || publicAccount.D1DatabaseID != account.D1DatabaseID || publicAccount.R2Bucket != account.R2Bucket {
		t.Fatalf("remote catalog stripped non-secret account config: %+v", publicAccount)
	}
}

func TestMergeRemoteCatalogKeepsLocalSyncAnchor(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	localTime := time.Now()
	remoteTime := localTime.Add(time.Hour)
	localSummary := &SyncSummary{Status: "success", Message: "local", SyncedAt: localTime}
	localAnchor := SyncAnchor{
		LastRemoteVersion: 5,
		LastManifest:      SyncManifest{Hash: "local-anchor"},
		StorageAccountID:  "storage-a",
	}
	store.state.Games = []Game{{
		ID:               "game-1",
		Name:             "Game",
		InstallPath:      filepath.Join(dir, "game.exe"),
		SavePath:         filepath.Join(dir, "saves"),
		Anchor:           localAnchor,
		LastSync:         localSummary,
		RuntimeUpdatedAt: localTime,
		CatalogUpdatedAt: localTime,
	}}

	remoteSummary := &SyncSummary{Status: "success", Message: "remote", SyncedAt: remoteTime}
	remoteAnchor := SyncAnchor{
		LastRemoteVersion: 9,
		LastManifest:      SyncManifest{Hash: "remote-anchor"},
		StorageAccountID:  "storage-b",
	}
	err = store.MergeRemoteCatalog(RemoteCatalog{
		Games: []Game{{
			ID:               "game-1",
			Name:             "Game",
			Anchor:           remoteAnchor,
			LastSync:         remoteSummary,
			RuntimeUpdatedAt: remoteTime,
			CatalogUpdatedAt: remoteTime,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	merged := store.state.Games[0]
	if merged.Anchor.LastRemoteVersion != localAnchor.LastRemoteVersion ||
		merged.Anchor.LastManifest.Hash != localAnchor.LastManifest.Hash ||
		merged.Anchor.StorageAccountID != localAnchor.StorageAccountID {
		t.Fatalf("remote catalog overwrote local anchor: got %+v want %+v", merged.Anchor, localAnchor)
	}
	if merged.LastSync == nil || merged.LastSync.Message != localSummary.Message {
		t.Fatalf("remote catalog overwrote local last sync: %+v", merged.LastSync)
	}
}

func TestMergeRemoteCatalogStripsIncomingLocalCoverPath(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = store.MergeRemoteCatalog(RemoteCatalog{Games: []Game{{
		ID: "remote-game", Name: "Remote", CoverLocalPath: `C:\\OtherDevice\\cover.jpg`, CatalogUpdatedAt: time.Now(),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	game := store.Snapshot().Games[0]
	if game.CoverLocalPath != "" {
		t.Fatalf("remote local cover path leaked into this device: %q", game.CoverLocalPath)
	}
}

func TestMergeRemoteCatalogAppliesNewerAPIKeyDeletion(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	localTime := time.Now()
	remoteTime := localTime.Add(time.Hour)
	store.state.Preferences.RawgAPIKey = "local-rawg"
	store.state.Preferences.SteamGridDBAPIKey = "local-sgdb"
	store.state.Preferences.RawgAPIKeyUpdatedAt = localTime
	store.state.Preferences.SteamGridDBAPIKeyUpdatedAt = localTime

	err = store.MergeRemoteCatalog(RemoteCatalog{
		Preferences: &RemotePreferences{
			RawgAPIKey:                 "",
			SteamGridDBAPIKey:          "",
			RawgAPIKeyUpdatedAt:        remoteTime,
			SteamGridDBAPIKeyUpdatedAt: remoteTime,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if store.state.Preferences.RawgAPIKey != "" ||
		store.state.Preferences.SteamGridDBAPIKey != "" {
		t.Fatalf("newer remote API key deletion was ignored: %+v", store.state.Preferences)
	}
	if !store.state.Preferences.RawgAPIKeyUpdatedAt.Equal(remoteTime) ||
		!store.state.Preferences.SteamGridDBAPIKeyUpdatedAt.Equal(remoteTime) {
		t.Fatalf("newer remote API key timestamps were ignored: %+v", store.state.Preferences)
	}
}

func TestMergeRemoteCatalogPinnedTagsUsesNewestTimestamp(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	localTime := time.Now()
	remoteTime := localTime.Add(time.Hour)
	store.state.Preferences.PinnedTags = []string{"local-tag"}
	store.state.Preferences.PinnedTagsUpdatedAt = localTime

	err = store.MergeRemoteCatalog(RemoteCatalog{
		Preferences: &RemotePreferences{
			PinnedTags:          []string{"remote-tag", "remote-tag", "other-tag"},
			PinnedTagsUpdatedAt: remoteTime,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(store.state.Preferences.PinnedTags) != 2 ||
		store.state.Preferences.PinnedTags[0] != "remote-tag" ||
		store.state.Preferences.PinnedTags[1] != "other-tag" {
		t.Fatalf("remote pinned tags were not merged and normalized: %+v", store.state.Preferences.PinnedTags)
	}
	if !store.state.Preferences.PinnedTagsUpdatedAt.Equal(remoteTime) {
		t.Fatalf("remote pinned tag timestamp was not merged: %s", store.state.Preferences.PinnedTagsUpdatedAt)
	}

	newerLocal := remoteTime.Add(time.Hour)
	store.state.Preferences.PinnedTags = []string{"newer-local-tag"}
	store.state.Preferences.PinnedTagsUpdatedAt = newerLocal
	err = store.MergeRemoteCatalog(RemoteCatalog{
		Preferences: &RemotePreferences{
			PinnedTags:          []string{"older-remote-tag"},
			PinnedTagsUpdatedAt: remoteTime,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(store.state.Preferences.PinnedTags) != 1 ||
		store.state.Preferences.PinnedTags[0] != "newer-local-tag" {
		t.Fatalf("older remote pinned tags overwrote local value: %+v", store.state.Preferences.PinnedTags)
	}
	if !store.state.Preferences.PinnedTagsUpdatedAt.Equal(newerLocal) {
		t.Fatalf("older remote pinned tag timestamp overwrote local timestamp: %s", store.state.Preferences.PinnedTagsUpdatedAt)
	}
}

func TestMergeRemoteCatalogSidebarNavOrderUsesNewestTimestamp(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	localTime := time.Now()
	remoteTime := localTime.Add(time.Hour)
	store.state.Preferences.SidebarNavOrder = []string{"page:all-games", "tag:local-tag"}
	store.state.Preferences.SidebarNavOrderUpdatedAt = localTime

	err = store.MergeRemoteCatalog(RemoteCatalog{
		Preferences: &RemotePreferences{
			SidebarNavOrder:          []string{"tag:remote-tag", "tag:remote-tag", "page:all-tags"},
			SidebarNavOrderUpdatedAt: remoteTime,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(store.state.Preferences.SidebarNavOrder) != 2 ||
		store.state.Preferences.SidebarNavOrder[0] != "tag:remote-tag" ||
		store.state.Preferences.SidebarNavOrder[1] != "page:all-tags" {
		t.Fatalf("remote sidebar nav order was not merged and normalized: %+v", store.state.Preferences.SidebarNavOrder)
	}
	if !store.state.Preferences.SidebarNavOrderUpdatedAt.Equal(remoteTime) {
		t.Fatalf("remote sidebar nav order timestamp was not merged: %s", store.state.Preferences.SidebarNavOrderUpdatedAt)
	}

	newerLocal := remoteTime.Add(time.Hour)
	store.state.Preferences.SidebarNavOrder = []string{"page:favorite-games"}
	store.state.Preferences.SidebarNavOrderUpdatedAt = newerLocal
	err = store.MergeRemoteCatalog(RemoteCatalog{
		Preferences: &RemotePreferences{
			SidebarNavOrder:          []string{"page:all-tags"},
			SidebarNavOrderUpdatedAt: remoteTime,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(store.state.Preferences.SidebarNavOrder) != 1 ||
		store.state.Preferences.SidebarNavOrder[0] != "page:favorite-games" {
		t.Fatalf("older remote sidebar nav order overwrote local value: %+v", store.state.Preferences.SidebarNavOrder)
	}
	if !store.state.Preferences.SidebarNavOrderUpdatedAt.Equal(newerLocal) {
		t.Fatalf("older remote sidebar nav order timestamp overwrote local timestamp: %s", store.state.Preferences.SidebarNavOrderUpdatedAt)
	}
}

func TestSafeSaveFilePathRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	if _, err := safeSaveFilePath(root, "profile/save.dat"); err != nil {
		t.Fatalf("safe nested path rejected: %v", err)
	}

	unsafePaths := []string{
		"../outside.dat",
		"profile/../../outside.dat",
		"/absolute.dat",
		"",
		".",
	}
	for _, relPath := range unsafePaths {
		if _, err := safeSaveFilePath(root, relPath); err == nil {
			t.Fatalf("unsafe path %q was accepted", relPath)
		}
	}
}

func TestApplyDownloadsRollsBackOnDownloadFailure(t *testing.T) {
	root := t.TempDir()
	game := Game{ID: "game-1", SavePath: root}
	if err := os.WriteFile(filepath.Join(root, "a.dat"), []byte("old-a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.dat"), []byte("old-b"), 0o644); err != nil {
		t.Fatal(err)
	}

	newA := []byte("new-a")
	newC := []byte("new-c")
	newASHA := testSHA256(newA)
	newCSHA := testSHA256(newC)
	failingSHA := testSHA256([]byte("missing"))
	downloader := fakeSyncDownloader{
		files: map[string][]byte{
			objectKey(game.ID, newASHA): newA,
			objectKey(game.ID, newCSHA): newC,
		},
		errors: map[string]error{
			objectKey(game.ID, failingSHA): errors.New("download failed"),
		},
	}

	// applyDownloads 现返回未提交的回滚句柄（A4/M8）：失败路径句柄为 nil 且已自行回滚
	rollback, err := (&Engine{}).applyDownloads(context.Background(), downloader, game, []ManifestDiff{
		{
			Path:   "a.dat",
			Action: "download",
			Remote: &ManifestFile{
				Path:   "a.dat",
				Size:   int64(len(newA)),
				SHA256: newASHA,
			},
		},
		{
			Path:   "c.dat",
			Action: "download",
			Remote: &ManifestFile{
				Path:   "c.dat",
				Size:   int64(len(newC)),
				SHA256: newCSHA,
			},
		},
		{
			Path:   "b.dat",
			Action: "download",
			Remote: &ManifestFile{
				Path:   "b.dat",
				Size:   7,
				SHA256: failingSHA,
			},
		},
	})
	if err == nil {
		t.Fatal("expected download failure")
	}
	if rollback != nil {
		t.Fatal("expected nil rollback handle on failure")
	}

	if content, err := os.ReadFile(filepath.Join(root, "a.dat")); err != nil || string(content) != "old-a" {
		t.Fatalf("a.dat was not restored: content=%q err=%v", string(content), err)
	}
	if content, err := os.ReadFile(filepath.Join(root, "b.dat")); err != nil || string(content) != "old-b" {
		t.Fatalf("b.dat was not restored: content=%q err=%v", string(content), err)
	}
	if _, err := os.Stat(filepath.Join(root, "c.dat")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new file c.dat was not removed on rollback: %v", err)
	}
}

func TestApplyDownloadsRollsBackDeleteOnLaterFailure(t *testing.T) {
	root := t.TempDir()
	game := Game{ID: "game-1", SavePath: root}
	if err := os.WriteFile(filepath.Join(root, "delete.dat"), []byte("keep-me"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (&Engine{}).applyDownloads(context.Background(), nil, game, []ManifestDiff{
		{Path: "delete.dat", Action: "delete_local"},
		{Path: "../escape.dat", Action: "delete_local"},
	})
	if err == nil {
		t.Fatal("expected unsafe path failure")
	}
	if content, err := os.ReadFile(filepath.Join(root, "delete.dat")); err != nil || string(content) != "keep-me" {
		t.Fatalf("deleted file was not restored: content=%q err=%v", string(content), err)
	}
}

func TestMergeManifestsEmitsRemoteDeleteForLocalDeletion(t *testing.T) {
	file := ManifestFile{Path: "save.dat", Size: 4, SHA256: "abcd"}
	base := SyncManifest{Files: []ManifestFile{file}}
	local := SyncManifest{}
	remote := SyncManifest{Files: []ManifestFile{file}}

	merged, uploads, downloads, conflicts := mergeManifests(base, local, remote, "")

	if len(merged.Files) != 0 {
		t.Fatalf("deleted file remained in merged manifest: %+v", merged.Files)
	}
	if len(downloads) != 0 || len(conflicts) != 0 {
		t.Fatalf("unexpected downloads/conflicts: downloads=%+v conflicts=%+v", downloads, conflicts)
	}
	if len(uploads) != 1 || uploads[0].Action != "delete_remote" || uploads[0].Remote == nil {
		t.Fatalf("expected delete_remote upload diff, got %+v", uploads)
	}
}

func TestForceUploadManifestFilesAddsMissingUploads(t *testing.T) {
	manifest := SyncManifest{Files: []ManifestFile{
		{Path: "a.dat", Size: 1, SHA256: "a"},
		{Path: "b.dat", Size: 1, SHA256: "b"},
	}}
	existing := []ManifestDiff{{
		Path:   "a.dat",
		Action: "upload",
		Local:  &manifest.Files[0],
	}}

	uploads := forceUploadManifestFiles(existing, manifest)

	if len(uploads) != 2 {
		t.Fatalf("expected 2 uploads, got %+v", uploads)
	}
	if uploads[1].Path != "b.dat" || uploads[1].Action != "upload" || uploads[1].Local.SHA256 != "b" {
		t.Fatalf("missing forced upload for b.dat: %+v", uploads)
	}
}

func TestMergeRemoteCatalogMergesCompletePreferencesByGroup(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	local := store.Snapshot().Preferences
	local.BackgroundSyncIntervalSeconds = 300
	local.RawgAPIKey = "local-newer"
	if err := store.SavePreferences(local); err != nil {
		t.Fatal(err)
	}
	local = store.Snapshot().Preferences

	remoteAt := local.SyncSettingsUpdatedAt.Add(time.Minute)
	if err := store.MergeRemoteCatalog(RemoteCatalog{Preferences: &RemotePreferences{
		AutoSyncOnLaunch:              false,
		StartupSyncMode:               "cloud-first",
		ConflictPolicy:                "remote",
		BackgroundSyncIntervalSeconds: 30,
		SyncSettingsUpdatedAt:         remoteAt,
		RawgAPIKey:                    "remote-older",
		RawgAPIKeyUpdatedAt:           local.RawgAPIKeyUpdatedAt.Add(-time.Minute),
		SteamGridDBAPIKey:             "remote-sgdb",
		SteamGridDBAPIKeyUpdatedAt:    remoteAt,
	}}); err != nil {
		t.Fatal(err)
	}

	got := store.Snapshot().Preferences
	if got.BackgroundSyncIntervalSeconds != 30 || got.StartupSyncMode != "cloud-first" || got.ConflictPolicy != "remote" {
		t.Fatalf("remote settings group not merged: %+v", got)
	}
	if got.RawgAPIKey != "local-newer" || got.SteamGridDBAPIKey != "remote-sgdb" {
		t.Fatalf("API key groups merged incorrectly: %+v", got)
	}
}

func TestMergeRemoteCatalogLegacyPreferencesDoNotEraseLocalValues(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	local := store.Snapshot().Preferences
	local.AutoSyncOnLaunch = true
	local.BackgroundSyncIntervalSeconds = 30
	local.RawgAPIKey = "local-rawg"
	local.SteamGridDBAPIKey = "local-sgdb"
	if err := store.SavePreferences(local); err != nil {
		t.Fatal(err)
	}

	if err := store.MergeRemoteCatalog(RemoteCatalog{Preferences: &RemotePreferences{
		StartupSyncMode: "smart",
		ConflictPolicy:  "manual",
	}}); err != nil {
		t.Fatal(err)
	}

	got := store.Snapshot().Preferences
	if !got.AutoSyncOnLaunch || got.BackgroundSyncIntervalSeconds != 30 ||
		got.RawgAPIKey != "local-rawg" || got.SteamGridDBAPIKey != "local-sgdb" {
		t.Fatalf("legacy remote preferences erased local values: %+v", got)
	}
}
