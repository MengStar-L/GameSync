package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDialogDefaultDirectory(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "game.exe")
	if err := os.WriteFile(executable, []byte("exe"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name      string
		candidate string
		want      string
	}{
		{name: "empty", candidate: "", want: ""},
		{name: "existing directory", candidate: root, want: root},
		{name: "existing file", candidate: executable, want: root},
		{name: "missing child", candidate: filepath.Join(root, "missing.exe"), want: root},
		{name: "dialog title is not a directory", candidate: "选择启动文件", want: ""},
		{name: "missing tree", candidate: filepath.Join(root, "missing", "nested", "game.exe"), want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := dialogDefaultDirectory(test.candidate); got != test.want {
				t.Fatalf("dialogDefaultDirectory(%q) = %q, want %q", test.candidate, got, test.want)
			}
		})
	}
}
