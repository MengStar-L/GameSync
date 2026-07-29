package core

import (
	"path/filepath"
	"testing"
)

func TestExecutableInDirectoryUsesPathBoundary(t *testing.T) {
	directory := filepath.Join("C:\\Games", "Example")
	if !executableInDirectory(filepath.Join(directory, "bin", "game.exe"), directory) {
		t.Fatal("executable inside game directory was not detected")
	}
	if executableInDirectory(filepath.Join("C:\\Games", "Example Other", "game.exe"), directory) {
		t.Fatal("similar directory prefix was treated as the same game")
	}
}
