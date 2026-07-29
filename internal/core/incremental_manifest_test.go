package core

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestBuildIncrementalManifestCleanUsesOnlyIndex(t *testing.T) {
	previous := map[string]DeviceIndexFile{
		"z.sav": {Size: 2, ModifiedAt: time.Unix(2, 0).UTC(), SHA256: "zz"},
		"a.sav": {Size: 1, ModifiedAt: time.Unix(1, 0).UTC(), SHA256: "aa"},
	}
	builder := NewIncrementalManifestBuilder()
	builder.walkDir = func(string, fs.WalkDirFunc) error { return errors.New("walk must not be called") }
	builder.stat = func(string) (fs.FileInfo, error) { return nil, errors.New("stat must not be called") }
	builder.hashFile = func(string) (string, error) { return "", errors.New("hash must not be called") }
	builder.now = func() time.Time { return time.Unix(10, 0).UTC() }

	result, err := builder.Build("missing-root-is-not-accessed", []string{"*"}, nil, previous, ScanStateClean, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats != (IncrementalManifestStats{ReusedFiles: 2}) {
		t.Fatalf("stats = %+v", result.Stats)
	}
	if len(result.Manifest.Files) != 2 || result.Manifest.Files[0].Path != "a.sav" || result.Manifest.Files[1].Path != "z.sav" {
		t.Fatalf("manifest files = %+v", result.Manifest.Files)
	}
	if result.Manifest.TotalBytes != 3 || result.Manifest.Hash != hashManifestFiles(result.Manifest.Files) {
		t.Fatalf("manifest = %+v", result.Manifest)
	}
	result.Files["a.sav"] = DeviceIndexFile{}
	if previous["a.sav"].SHA256 != "aa" {
		t.Fatal("result aliases previous files")
	}
}

func TestBuildIncrementalManifestDirtyHashesOnlyChangedPath(t *testing.T) {
	root := t.TempDir()
	unchangedPath := filepath.Join(root, "unchanged.sav")
	changedPath := filepath.Join(root, "changed.sav")
	if err := os.WriteFile(unchangedPath, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changedPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	unchangedInfo, err := os.Stat(unchangedPath)
	if err != nil {
		t.Fatal(err)
	}
	changedInfo, err := os.Stat(changedPath)
	if err != nil {
		t.Fatal(err)
	}
	previous := map[string]DeviceIndexFile{
		"unchanged.sav": indexedFileForTest(unchangedInfo, testSHA256([]byte("same"))),
		"changed.sav":   indexedFileForTest(changedInfo, testSHA256([]byte("old"))),
	}
	if err := os.WriteFile(changedPath, []byte("new-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	builder := NewIncrementalManifestBuilder()
	walkCalls := 0
	hashCalls := 0
	builder.walkDir = func(string, fs.WalkDirFunc) error {
		walkCalls++
		return errors.New("dirty build must not walk")
	}
	builder.hashFile = func(path string) (string, error) {
		hashCalls++
		return sha256File(path)
	}
	result, err := builder.Build(root, []string{"*.sav"}, nil, previous, ScanStateDirty, []string{"changed.sav"})
	if err != nil {
		t.Fatal(err)
	}
	if walkCalls != 0 || hashCalls != 1 {
		t.Fatalf("walks=%d hashes=%d", walkCalls, hashCalls)
	}
	wantStats := IncrementalManifestStats{StattedFiles: 1, HashedFiles: 1, ReusedFiles: 1}
	if result.Stats != wantStats {
		t.Fatalf("stats = %+v, want %+v", result.Stats, wantStats)
	}
	if result.Files["unchanged.sav"].SHA256 != previous["unchanged.sav"].SHA256 ||
		result.Files["changed.sav"].SHA256 != testSHA256([]byte("new-content")) {
		t.Fatalf("files = %+v", result.Files)
	}
}

func TestBuildIncrementalManifestDirtyHandlesDeleteAndRename(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "old.sav")
	newPath := filepath.Join(root, "new.sav")
	if err := os.WriteFile(oldPath, []byte("save"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	previous := map[string]DeviceIndexFile{"old.sav": indexedFileForTest(info, testSHA256([]byte("save")))}
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}

	result, err := BuildIncrementalManifest(root, []string{"*.sav"}, nil, previous, ScanStateDirty, []string{"old.sav", "new.sav"})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := result.Files["old.sav"]; exists || result.Files["new.sav"].SHA256 != testSHA256([]byte("save")) {
		t.Fatalf("renamed files = %+v", result.Files)
	}
	wantStats := IncrementalManifestStats{StattedFiles: 2, HashedFiles: 1}
	if result.Stats != wantStats {
		t.Fatalf("stats = %+v, want %+v", result.Stats, wantStats)
	}
}

func TestBuildIncrementalManifestDirtyDirectoryRequiresRebuild(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "new-directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := BuildIncrementalManifest(root, []string{"*"}, nil, nil, ScanStateDirty, []string{"new-directory"})
	if !errors.Is(err, ErrManifestRebuildRequired) {
		t.Fatalf("err = %v; want ErrManifestRebuildRequired", err)
	}
	if result.Stats.StattedFiles != 1 {
		t.Fatalf("stats = %+v", result.Stats)
	}
}

func TestBuildIncrementalManifestRebuildReusesMetadataMatches(t *testing.T) {
	root := t.TempDir()
	unchangedPath := filepath.Join(root, "unchanged.sav")
	changedPath := filepath.Join(root, "changed.sav")
	excludedPath := filepath.Join(root, "notes.tmp")
	for path, content := range map[string]string{
		unchangedPath: "same",
		changedPath:   "new-value",
		excludedPath:  "ignore",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	unchangedInfo, err := os.Stat(unchangedPath)
	if err != nil {
		t.Fatal(err)
	}
	changedInfo, err := os.Stat(changedPath)
	if err != nil {
		t.Fatal(err)
	}
	previous := map[string]DeviceIndexFile{
		"unchanged.sav": indexedFileForTest(unchangedInfo, testSHA256([]byte("same"))),
		"changed.sav": {
			Size:       changedInfo.Size(),
			ModifiedAt: changedInfo.ModTime().Add(-time.Hour).UTC(),
			SHA256:     testSHA256([]byte("old-value")),
		},
		"removed.sav": {Size: 1, ModifiedAt: time.Unix(1, 0), SHA256: "removed"},
	}

	hashCalls := 0
	builder := NewIncrementalManifestBuilder()
	builder.hashFile = func(path string) (string, error) {
		hashCalls++
		return sha256File(path)
	}
	result, err := builder.Build(root, []string{"*.sav"}, []string{"notes.*"}, previous, ScanStateRebuild, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hashCalls != 1 {
		t.Fatalf("hash calls = %d", hashCalls)
	}
	wantStats := IncrementalManifestStats{EnumeratedFiles: 3, StattedFiles: 2, HashedFiles: 1, ReusedFiles: 1}
	if result.Stats != wantStats {
		t.Fatalf("stats = %+v, want %+v", result.Stats, wantStats)
	}
	if len(result.Files) != 2 || result.Files["unchanged.sav"].SHA256 != previous["unchanged.sav"].SHA256 ||
		result.Files["changed.sav"].SHA256 != testSHA256([]byte("new-value")) {
		t.Fatalf("files = %+v", result.Files)
	}
	if _, exists := result.Files["removed.sav"]; exists {
		t.Fatal("removed file survived rebuild")
	}
}

func TestBuildIncrementalManifestRebuildRepairsMissingHash(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "save.dat")
	if err := os.WriteFile(filePath, []byte("save"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	previous := map[string]DeviceIndexFile{"save.dat": indexedFileForTest(info, "")}

	result, err := BuildIncrementalManifest(root, []string{"*"}, nil, previous, ScanStateRebuild, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Files["save.dat"].SHA256 != testSHA256([]byte("save")) || result.Stats.HashedFiles != 1 || result.Stats.ReusedFiles != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestBuildIncrementalManifestDetectsMutationWhileHashing(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "save.dat")
	if err := os.WriteFile(filePath, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	builder := NewIncrementalManifestBuilder()
	builder.hashFile = func(path string) (string, error) {
		hash, err := sha256File(path)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte("changed-after-hash"), 0o644); err != nil {
			return "", err
		}
		return hash, nil
	}
	result, err := builder.Build(root, []string{"*"}, nil, nil, ScanStateDirty, []string{"save.dat"})
	if !errors.Is(err, ErrManifestSourceChanged) {
		t.Fatalf("err = %v; want ErrManifestSourceChanged", err)
	}
	if result.Stats != (IncrementalManifestStats{StattedFiles: 1, HashedFiles: 1}) {
		t.Fatalf("stats = %+v", result.Stats)
	}
}

func TestBuildIncrementalManifestRejectsUnknownModeAndEmptyDirtySet(t *testing.T) {
	root := t.TempDir()
	if _, err := BuildIncrementalManifest(root, nil, nil, nil, "unknown", nil); err == nil {
		t.Fatal("expected unknown scan state error")
	}
	if _, err := BuildIncrementalManifest(root, nil, nil, nil, ScanStateDirty, nil); !errors.Is(err, ErrManifestRebuildRequired) {
		t.Fatalf("empty dirty set err = %v", err)
	}
}

func indexedFileForTest(info fs.FileInfo, hash string) DeviceIndexFile {
	return DeviceIndexFile{
		Size:       info.Size(),
		ModifiedAt: info.ModTime().UTC(),
		SHA256:     hash,
	}
}

func TestIncrementalManifestResultFileSnapshotMatchesManifest(t *testing.T) {
	files := map[string]DeviceIndexFile{
		"b": {Size: 2, ModifiedAt: time.Unix(2, 0).UTC(), SHA256: "b"},
		"a": {Size: 1, ModifiedAt: time.Unix(1, 0).UTC(), SHA256: "a"},
	}
	result, err := BuildIncrementalManifest("unused", []string{"*"}, nil, files, ScanStateClean, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []ManifestFile{
		{Path: "a", Size: 1, ModifiedAt: time.Unix(1, 0).UTC(), SHA256: "a"},
		{Path: "b", Size: 2, ModifiedAt: time.Unix(2, 0).UTC(), SHA256: "b"},
	}
	if !reflect.DeepEqual(result.Manifest.Files, want) {
		t.Fatalf("manifest files = %+v, want %+v", result.Manifest.Files, want)
	}
}
