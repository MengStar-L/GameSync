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

	manager := NewBackupManager(NewEngine())
	err := manager.RestoreBackup(context.Background(), Game{
		ID:       "game-1",
		SavePath: saveDir,
	}, filepath.Base(backupPath), dir, nil)
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
	err := manager.RestoreBackup(context.Background(), Game{
		ID:       "game-1",
		SavePath: saveDir,
		BackupRegistry: []BackupRecord{{
			Filename: "backup_manual_hash.zip",
			SHA256:   strings.Repeat("0", 64),
		}},
	}, filepath.Base(backupPath), dir, nil)
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
	err = manager.RestoreBackup(context.Background(), Game{
		ID:       "game-1",
		SavePath: saveDir,
		BackupRegistry: []BackupRecord{{
			Filename: "backup_manual_good.zip",
			SHA256:   backupHash,
		}},
	}, filepath.Base(backupPath), dir, nil)
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
