package core

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSyncConfigFingerprintCanonicalizesEquivalentRules(t *testing.T) {
	left := SyncConfigFingerprint([]string{" *.sav ", "profiles/*", "*.sav"}, []string{"cache/*", " tmp/* "})
	right := SyncConfigFingerprint([]string{"profiles\\*", "*.sav"}, []string{"tmp\\*", "cache/*"})
	if left == "" || left != right {
		t.Fatalf("equivalent fingerprints differ: %q != %q", left, right)
	}
	if SyncConfigFingerprint(nil, nil) != SyncConfigFingerprint([]string{"*"}, []string{}) {
		t.Fatal("implicit and explicit include-all rules should have the same fingerprint")
	}
}

func TestDeviceIndexStoreGameLifecycle(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewDeviceIndexStore(dataDir, "device-1")
	if err != nil {
		t.Fatal(err)
	}

	configured, err := store.ConfigureGame(" game-1 ", filepath.Join(dataDir, "saves"), []string{"*.sav"}, []string{"cache/*"})
	if err != nil {
		t.Fatal(err)
	}
	if configured.ScanState != ScanStateRebuild || configured.SyncConfigFingerprint == "" {
		t.Fatalf("configured index = %+v", configured)
	}

	generatedAt := time.Unix(100, 0).UTC()
	manifest := SyncManifest{
		GeneratedAt: generatedAt,
		Files: []ManifestFile{
			{Path: "slot/a.sav", Size: 4, ModifiedAt: time.Unix(90, 0).UTC(), SHA256: "aaaa"},
			{Path: "slot/b.sav", Size: 8, ModifiedAt: time.Unix(91, 0).UTC(), SHA256: "bbbb"},
		},
	}
	manifest.Hash = hashManifestFiles(manifest.Files)
	committed, err := store.CommitGame("game-1", 7, "remote-manifest-hash", manifest)
	if err != nil {
		t.Fatal(err)
	}
	if committed.ScanState != ScanStateClean || len(committed.DirtyPaths) != 0 || committed.RemoteVersion != 7 ||
		committed.RemoteManifestHash != "remote-manifest-hash" || !committed.GeneratedAt.Equal(generatedAt) || len(committed.Files) != 2 {
		t.Fatalf("committed index = %+v", committed)
	}

	if err := store.MarkDirty("game-1", "slot\\b.sav", "slot/a.sav", "slot/a.sav"); err != nil {
		t.Fatal(err)
	}
	dirty, ok := store.Game("game-1")
	if !ok || dirty.ScanState != ScanStateDirty || !reflect.DeepEqual(dirty.DirtyPaths, []string{"slot/a.sav", "slot/b.sav"}) {
		t.Fatalf("dirty index = %+v", dirty)
	}
	if err := store.MarkRebuild("game-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDirty("game-1", "slot/c.sav"); err != nil {
		t.Fatal(err)
	}
	rebuild, _ := store.Game("game-1")
	if rebuild.ScanState != ScanStateRebuild || len(rebuild.DirtyPaths) != 0 {
		t.Fatalf("rebuild was downgraded: %+v", rebuild)
	}

	cover := DeviceCoverIndex{
		SourceFingerprint: "cover-hash",
		AccountID:         "account-1",
		ObjectKey:         "covers/game-1/cover-hash.jpg",
	}
	updated, err := store.UpdateCover("game-1", cover)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Cover != cover {
		t.Fatalf("cover = %+v", updated.Cover)
	}

	clone := store.Clone()
	clone.Games["game-1"] = DeviceGameIndex{}
	actual, _ := store.Game("game-1")
	if actual.Cover != cover || actual.ScanState != ScanStateRebuild {
		t.Fatalf("mutating clone changed store: %+v", actual)
	}

	reloaded, err := NewDeviceIndexStore(dataDir, "device-1")
	if err != nil {
		t.Fatal(err)
	}
	persisted, ok := reloaded.Game("game-1")
	if !ok || persisted.Cover != cover || persisted.ScanState != ScanStateRebuild {
		t.Fatalf("persisted index = %+v", persisted)
	}

	if err := reloaded.RemoveGame("game-1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Game("game-1"); ok {
		t.Fatal("removed game is still indexed")
	}
}

func TestDeviceIndexStoreConfigureScopesRebuildAndClearsFilesWithoutSavePath(t *testing.T) {
	store, err := NewDeviceIndexStore(t.TempDir(), "device-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, gameID := range []string{"game-1", "game-2"} {
		if _, err := store.ConfigureGame(gameID, filepath.Join(t.TempDir(), gameID), []string{"*"}, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := store.CommitGame(gameID, 1, "hash", SyncManifest{Files: []ManifestFile{{Path: "save.dat", Size: 1, SHA256: gameID}}}); err != nil {
			t.Fatal(err)
		}
	}

	unchanged, err := store.ConfigureGame("game-1", store.Clone().Games["game-1"].SavePath, []string{"*"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.ScanState != ScanStateClean {
		t.Fatalf("unchanged config dirtied game: %+v", unchanged)
	}

	withoutPath, err := store.ConfigureGame("game-1", "", []string{"*.sav"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if withoutPath.ScanState != ScanStateRebuild || withoutPath.SavePath != "" || len(withoutPath.Files) != 0 {
		t.Fatalf("path removal did not clear local files: %+v", withoutPath)
	}
	other, _ := store.Game("game-2")
	if other.ScanState != ScanStateClean || len(other.Files) != 1 {
		t.Fatalf("configuring game-1 changed game-2: %+v", other)
	}
}

func TestDeviceIndexStoreQuarantinesInvalidIndexes(t *testing.T) {
	for name, content := range map[string]string{
		"malformed":       "{not-json",
		"unknown-version": `{"version":99,"deviceId":"device-1","games":{}}`,
		"wrong-device":    `{"version":1,"deviceId":"device-2","games":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			dataDir := t.TempDir()
			indexDir := filepath.Join(dataDir, "indexes")
			if err := os.MkdirAll(indexDir, 0o755); err != nil {
				t.Fatal(err)
			}
			indexPath := filepath.Join(indexDir, "device-index.json")
			if err := os.WriteFile(indexPath, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}

			store, err := NewDeviceIndexStore(dataDir, "device-1")
			if err != nil {
				t.Fatal(err)
			}
			clone := store.Clone()
			if clone.Version != DeviceIndexVersion || clone.DeviceID != "device-1" || len(clone.Games) != 0 {
				t.Fatalf("replacement index = %+v", clone)
			}
			matches, err := filepath.Glob(indexPath + ".invalid-*")
			if err != nil || len(matches) != 1 {
				t.Fatalf("quarantined files = %v, err = %v", matches, err)
			}
			if _, err := os.Stat(indexPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid index remains active: %v", err)
			}
		})
	}
}

func TestDeviceIndexStoreFailedCommitPreservesMemoryAndDisk(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewDeviceIndexStore(dataDir, "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConfigureGame("game-1", filepath.Join(dataDir, "saves"), []string{"*"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitGame("game-1", 1, "hash", SyncManifest{}); err != nil {
		t.Fatal(err)
	}
	beforeMemory := store.Clone()
	beforeDisk, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}

	store.writeAtomic = func(string, []byte) error { return errors.New("injected write failure") }
	if err := store.MarkRebuild("game-1"); err == nil {
		t.Fatal("expected write failure")
	}
	afterDisk, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.Clone(), beforeMemory) {
		t.Fatalf("failed commit changed memory: before=%+v after=%+v", beforeMemory, store.Clone())
	}
	if !bytes.Equal(afterDisk, beforeDisk) {
		t.Fatal("failed commit changed persisted index")
	}
}

func TestDeviceIndexStoreCoverOnlyGameHasRebuildState(t *testing.T) {
	store, err := NewDeviceIndexStore(t.TempDir(), "device-1")
	if err != nil {
		t.Fatal(err)
	}
	game, err := store.UpdateCover("game-1", DeviceCoverIndex{SourceFingerprint: "HASH"})
	if err != nil {
		t.Fatal(err)
	}
	if game.ScanState != ScanStateRebuild || game.Cover.SourceFingerprint != "hash" {
		t.Fatalf("cover-only game = %+v", game)
	}
}
