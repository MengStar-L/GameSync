package core

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreBackupRejectsZipSlipAndKeepsSave(t *testing.T) {
	dir := t.TempDir()
	saveDir := filepath.Join(dir, "save")
	backupDir := filepath.Join(dir, "backups", "game-1")
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	originalPath := filepath.Join(saveDir, "profile.dat")
	if err := os.WriteFile(originalPath, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(backupDir, "backup_manual_bad.zip")
	writeTestZip(t, backupPath, map[string]string{
		"../outside.txt": "bad",
	})
	record := BackupRecord{Filename: filepath.Base(backupPath)}

	manager := NewBackupManager(NewEngine())
	err := manager.RestoreBackup(context.Background(), Game{
		ID:             "game-1",
		SavePath:       saveDir,
		BackupRegistry: []BackupRecord{record},
	}, BackupRecordID(record), dir, nil)
	if err == nil || !strings.Contains(err.Error(), "unsafe backup entry") {
		t.Fatalf("expected unsafe backup entry error, got %v", err)
	}
	content, readErr := os.ReadFile(originalPath)
	if readErr != nil {
		t.Fatalf("original save was removed: %v", readErr)
	}
	if string(content) != "original" {
		t.Fatalf("original save changed: %q", content)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "outside.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("zip slip wrote outside save dir: %v", statErr)
	}
}

func TestRestoreBackupRejectsHashMismatchAndKeepsSave(t *testing.T) {
	dir := t.TempDir()
	saveDir := filepath.Join(dir, "save")
	backupDir := filepath.Join(dir, "backups", "game-1")
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	originalPath := filepath.Join(saveDir, "profile.dat")
	if err := os.WriteFile(originalPath, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(backupDir, "backup_manual_hash.zip")
	writeTestZip(t, backupPath, map[string]string{
		"profile.dat": "new",
	})

	manager := NewBackupManager(NewEngine())
	record := BackupRecord{
		Filename: "backup_manual_hash.zip",
		SHA256:   strings.Repeat("0", 64),
	}
	err := manager.RestoreBackup(context.Background(), Game{
		ID:             "game-1",
		SavePath:       saveDir,
		BackupRegistry: []BackupRecord{record},
	}, BackupRecordID(record), dir, nil)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected sha256 mismatch, got %v", err)
	}
	content, readErr := os.ReadFile(originalPath)
	if readErr != nil {
		t.Fatalf("original save was removed: %v", readErr)
	}
	if string(content) != "original" {
		t.Fatalf("original save changed: %q", content)
	}
}

func TestRestoreBackupReplacesSaveAfterValidation(t *testing.T) {
	dir := t.TempDir()
	saveDir := filepath.Join(dir, "save")
	backupDir := filepath.Join(dir, "backups", "game-1")
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(saveDir, "old.dat"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(backupDir, "backup_manual_good.zip")
	writeTestZip(t, backupPath, map[string]string{
		"profile/main.dat": "new",
	})
	backupHash, err := sha256File(backupPath)
	if err != nil {
		t.Fatal(err)
	}

	manager := NewBackupManager(NewEngine())
	record := BackupRecord{
		Filename: "backup_manual_good.zip",
		SHA256:   backupHash,
	}
	err = manager.RestoreBackup(context.Background(), Game{
		ID:             "game-1",
		SavePath:       saveDir,
		BackupRegistry: []BackupRecord{record},
	}, BackupRecordID(record), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(saveDir, "old.dat")); !os.IsNotExist(statErr) {
		t.Fatalf("old save file still exists: %v", statErr)
	}
	content, readErr := os.ReadFile(filepath.Join(saveDir, "profile", "main.dat"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "new" {
		t.Fatalf("restored content = %q", content)
	}
}

func TestBackupOperationsDistinguishSameFilenameFromDifferentDevices(t *testing.T) {
	dir := t.TempDir()
	filename := "backup_manual_same.zip"
	sourceA := filepath.Join(dir, "device-a.zip")
	sourceB := filepath.Join(dir, "device-b.zip")
	writeTestZip(t, sourceA, map[string]string{"profile.dat": "device-a"})
	writeTestZip(t, sourceB, map[string]string{"profile.dat": "device-b"})

	store := newMemoryObjectStore()
	recordA := BackupRecord{Filename: filename, SourceDeviceID: "device-a"}
	recordB := BackupRecord{Filename: filename, SourceDeviceID: "device-b"}
	if err := store.PutObjectFromFile(context.Background(), BackupObjectKeyForRecord("game-1", recordA), sourceA); err != nil {
		t.Fatal(err)
	}
	if err := store.PutObjectFromFile(context.Background(), BackupObjectKeyForRecord("game-1", recordB), sourceB); err != nil {
		t.Fatal(err)
	}

	game := Game{
		ID:             "game-1",
		SavePath:       filepath.Join(dir, "save"),
		BackupRegistry: []BackupRecord{recordA, recordB},
	}
	gateway := &StorageGateway{Objects: store}
	manager := NewBackupManager(NewEngine())
	for _, testCase := range []struct {
		record  BackupRecord
		content string
	}{
		{record: recordA, content: "device-a"},
		{record: recordB, content: "device-b"},
	} {
		_ = os.Remove(filepath.Join(dir, "backups", game.ID, filename))
		if err := manager.RestoreBackup(context.Background(), game, BackupRecordID(testCase.record), dir, map[string]*StorageGateway{"storage": gateway}); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(filepath.Join(game.SavePath, "profile.dat"))
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != testCase.content {
			t.Fatalf("restored %s content = %q", BackupRecordID(testCase.record), content)
		}
	}

	if err := manager.DeleteBackup(context.Background(), game, BackupRecordID(recordA), dir, map[string]*StorageGateway{"storage": gateway}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.objects[BackupObjectKeyForRecord(game.ID, recordA)]; ok {
		t.Fatal("device-a object was not deleted")
	}
	if _, ok := store.objects[BackupObjectKeyForRecord(game.ID, recordB)]; !ok {
		t.Fatal("deleting device-a backup also deleted device-b object")
	}
}

func TestBackupObjectKeyForLegacyRecordUsesLegacyPath(t *testing.T) {
	record := BackupRecord{Filename: "backup_manual_legacy.zip"}
	if got := BackupObjectKeyForRecord("game-1", record); got != "backups/game-1/backup_manual_legacy.zip" {
		t.Fatalf("legacy object key = %q", got)
	}
}

func writeTestZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
