package core

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  int
	}{
		{"1.0.10", "1.0.2", 1},
		{"1.0.0", "1.0.0", 0},
		{"1.0.0-beta.1", "1.0.0", -1},
		{"v2.0.0", "1.9.9", 1},
	}
	for _, tt := range tests {
		got := compareVersions(tt.left, tt.right)
		if got != tt.want {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", tt.left, tt.right, got, tt.want)
		}
	}
}

func TestUpdateManifestValidate(t *testing.T) {
	manifest := UpdateManifest{
		Version:     "1.2.3",
		Channel:     "stable",
		PublishedAt: time.Now(),
		Platforms: map[string]UpdatePlatformAsset{
			"windows-amd64": {
				URL:    "https://github.com/example/GameSync-Wails/releases/download/v1.2.3/GameSync-v1.2.3-windows-amd64.zip",
				SHA256: "abc123",
				Size:   12,
			},
		},
	}
	if _, err := manifest.Validate("windows-amd64"); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	delete(manifest.Platforms, "windows-amd64")
	if _, err := manifest.Validate("windows-amd64"); err == nil {
		t.Fatal("manifest without platform was accepted")
	}
}

func TestValidateGitHubReleaseURL(t *testing.T) {
	valid := "https://github.com/example/GameSync-Wails/releases/download/v1.0.0/latest.json"
	if err := validateGitHubReleaseURL(valid, "example/GameSync-Wails"); err != nil {
		t.Fatalf("valid url rejected: %v", err)
	}
	invalidHost := "https://example.com/example/GameSync-Wails/releases/download/v1.0.0/latest.json"
	if err := validateGitHubReleaseURL(invalidHost, "example/GameSync-Wails"); err == nil {
		t.Fatal("invalid host accepted")
	}
	invalidRepo := "https://github.com/other/GameSync-Wails/releases/download/v1.0.0/latest.json"
	if err := validateGitHubReleaseURL(invalidRepo, "example/GameSync-Wails"); err == nil {
		t.Fatal("invalid repo accepted")
	}
}

func TestValidateZipEntryPathRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if _, err := validateZipEntryPath(root, "GameSync.exe"); err != nil {
		t.Fatalf("safe zip path rejected: %v", err)
	}
	unsafePaths := []string{"../GameSync.exe", "/GameSync.exe", "dir/../../GameSync.exe"}
	for _, path := range unsafePaths {
		if _, err := validateZipEntryPath(root, path); err == nil {
			t.Fatalf("unsafe zip path %q accepted", path)
		}
	}
}

func TestHasZipTraversal(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "bad.zip")
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
	if err := hasZipTraversal(archivePath); err == nil {
		t.Fatal("zip traversal archive accepted")
	}
}
