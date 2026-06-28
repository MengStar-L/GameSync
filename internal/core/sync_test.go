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
	if !publicGame.RuntimeUpdatedAt.IsZero() {
		t.Fatalf("remote catalog leaked runtime sync timestamp: %s", publicGame.RuntimeUpdatedAt)
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

func TestMergeRemoteCatalogKeepsLocalAPIKeys(t *testing.T) {
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

	if store.state.Preferences.RawgAPIKey != "local-rawg" ||
		store.state.Preferences.SteamGridDBAPIKey != "local-sgdb" {
		t.Fatalf("remote preferences overwrote local api keys: %+v", store.state.Preferences)
	}
	if !store.state.Preferences.RawgAPIKeyUpdatedAt.Equal(localTime) ||
		!store.state.Preferences.SteamGridDBAPIKeyUpdatedAt.Equal(localTime) {
		t.Fatalf("remote preferences overwrote local api key timestamps: %+v", store.state.Preferences)
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

	err := (&Engine{}).applyDownloads(context.Background(), downloader, game, []ManifestDiff{
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

	err := (&Engine{}).applyDownloads(context.Background(), nil, game, []ManifestDiff{
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
