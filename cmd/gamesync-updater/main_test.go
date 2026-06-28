package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeJoinRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if _, err := safeJoin(root, "GameSync.exe"); err != nil {
		t.Fatalf("safe path rejected: %v", err)
	}
	for _, path := range []string{"../GameSync.exe", "/GameSync.exe", "a/../../GameSync.exe"} {
		if _, err := safeJoin(root, path); err == nil {
			t.Fatalf("unsafe path %q accepted", path)
		}
	}
}

func TestVerifyFileSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.zip")
	content := []byte("content")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(content)
	if err := verifyFileSHA256(path, hex.EncodeToString(hash[:])); err != nil {
		t.Fatalf("valid hash rejected: %v", err)
	}
	if err := verifyFileSHA256(path, "bad"); err == nil {
		t.Fatal("invalid hash accepted")
	}
}

func TestExtractZipSafeRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "bad.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("bad")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractZipSafe(archivePath, filepath.Join(dir, "out")); err == nil {
		t.Fatal("zip traversal archive accepted")
	}
}
