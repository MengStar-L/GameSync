package core

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRemoteCatalogGameStripsLocalSyncState(t *testing.T) {
	now := time.Now()
	game := Game{
		ID:          "game-1",
		Name:        "Game",
		InstallPath: `C:\Games\Game.exe`,
		SavePath:    `C:\Users\Player\Saves`,
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
