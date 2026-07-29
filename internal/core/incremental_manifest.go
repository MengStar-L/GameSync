package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrManifestRebuildRequired = errors.New("incremental manifest requires a rebuild")
var ErrManifestSourceChanged = errors.New("save file changed while building manifest")

type IncrementalManifestStats struct {
	EnumeratedFiles int `json:"enumeratedFiles"`
	StattedFiles    int `json:"stattedFiles"`
	HashedFiles     int `json:"hashedFiles"`
	ReusedFiles     int `json:"reusedFiles"`
}

type IncrementalManifestResult struct {
	Manifest SyncManifest               `json:"manifest"`
	Files    map[string]DeviceIndexFile `json:"files"`
	Stats    IncrementalManifestStats   `json:"stats"`
}

type IncrementalManifestBuilder struct {
	walkDir  func(string, fs.WalkDirFunc) error
	stat     func(string) (fs.FileInfo, error)
	hashFile func(string) (string, error)
	now      func() time.Time
}

func NewIncrementalManifestBuilder() *IncrementalManifestBuilder {
	return &IncrementalManifestBuilder{
		walkDir:  filepath.WalkDir,
		stat:     os.Stat,
		hashFile: sha256File,
		now:      time.Now,
	}
}

func BuildIncrementalManifest(
	root string,
	includePatterns []string,
	excludePatterns []string,
	previousFiles map[string]DeviceIndexFile,
	scanState string,
	dirtyPaths []string,
) (IncrementalManifestResult, error) {
	return NewIncrementalManifestBuilder().Build(root, includePatterns, excludePatterns, previousFiles, scanState, dirtyPaths)
}

func (b *IncrementalManifestBuilder) Build(
	root string,
	includePatterns []string,
	excludePatterns []string,
	previousFiles map[string]DeviceIndexFile,
	scanState string,
	dirtyPaths []string,
) (IncrementalManifestResult, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return IncrementalManifestResult{}, errors.New("incremental manifest root is empty")
	}
	b = b.withDefaults()
	scanState = strings.ToLower(strings.TrimSpace(scanState))
	if scanState == "" {
		scanState = ScanStateRebuild
	}

	switch scanState {
	case ScanStateClean:
		files, err := trackedPreviousFiles(previousFiles, includePatterns, excludePatterns)
		if err != nil {
			return IncrementalManifestResult{}, err
		}
		stats := IncrementalManifestStats{ReusedFiles: len(files)}
		return makeIncrementalManifestResult(files, stats, b.now()), nil
	case ScanStateDirty:
		files, err := trackedPreviousFiles(previousFiles, includePatterns, excludePatterns)
		if err != nil {
			return IncrementalManifestResult{}, err
		}
		return b.buildDirty(root, includePatterns, excludePatterns, files, dirtyPaths)
	case ScanStateRebuild:
		files, err := normalizedPreviousFiles(previousFiles)
		if err != nil {
			return IncrementalManifestResult{}, err
		}
		return b.buildRebuild(root, includePatterns, excludePatterns, files)
	default:
		return IncrementalManifestResult{}, fmt.Errorf("unknown incremental manifest scan state: %s", scanState)
	}
}

func (b *IncrementalManifestBuilder) buildDirty(
	root string,
	includePatterns []string,
	excludePatterns []string,
	files map[string]DeviceIndexFile,
	dirtyPaths []string,
) (IncrementalManifestResult, error) {
	paths, err := normalizeDirtyPaths(dirtyPaths)
	if err != nil {
		return IncrementalManifestResult{}, err
	}
	if len(paths) == 0 {
		return IncrementalManifestResult{}, ErrManifestRebuildRequired
	}
	stats := IncrementalManifestStats{ReusedFiles: len(files)}
	if err := b.ensureRoot(root); err != nil {
		return makeIncrementalManifestResult(files, stats, b.now()), err
	}

	for _, relPath := range paths {
		previous, existed := files[relPath]
		if !shouldTrackFile(relPath, includePatterns, excludePatterns) {
			if existed {
				delete(files, relPath)
				stats.ReusedFiles--
			}
			continue
		}
		stats.StattedFiles++
		filePath := filepath.Join(root, filepath.FromSlash(relPath))
		info, statErr := b.stat(filePath)
		if errors.Is(statErr, fs.ErrNotExist) {
			if existed {
				delete(files, relPath)
				stats.ReusedFiles--
			}
			continue
		}
		if statErr != nil {
			return makeIncrementalManifestResult(files, stats, b.now()), fmt.Errorf("stat dirty save file %s: %w", relPath, statErr)
		}
		if info.IsDir() {
			return makeIncrementalManifestResult(files, stats, b.now()), fmt.Errorf("%w: %s", ErrManifestRebuildRequired, relPath)
		}
		if !info.Mode().IsRegular() {
			if existed {
				delete(files, relPath)
				stats.ReusedFiles--
			}
			continue
		}
		if existed && indexedMetadataMatches(previous, info) && strings.TrimSpace(previous.SHA256) != "" {
			continue
		}
		if existed {
			stats.ReusedFiles--
		}
		stats.HashedFiles++
		hash, hashErr := b.hashFile(filePath)
		if hashErr != nil {
			return makeIncrementalManifestResult(files, stats, b.now()), fmt.Errorf("hash dirty save file %s: %w", relPath, hashErr)
		}
		if strings.TrimSpace(hash) == "" {
			return makeIncrementalManifestResult(files, stats, b.now()), fmt.Errorf("hash dirty save file %s: empty hash", relPath)
		}
		if err := b.verifyUnchanged(filePath, relPath, info); err != nil {
			return makeIncrementalManifestResult(files, stats, b.now()), err
		}
		files[relPath] = indexedFileFromInfo(info, hash)
	}

	return makeIncrementalManifestResult(files, stats, b.now()), nil
}

func (b *IncrementalManifestBuilder) buildRebuild(
	root string,
	includePatterns []string,
	excludePatterns []string,
	previousFiles map[string]DeviceIndexFile,
) (IncrementalManifestResult, error) {
	stats := IncrementalManifestStats{}
	if err := b.ensureRoot(root); err != nil {
		return makeIncrementalManifestResult(nil, stats, b.now()), err
	}
	files := make(map[string]DeviceIndexFile)
	walkErr := b.walkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		stats.EnumeratedFiles++
		relPath, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		relPath, err = normalizeIndexedRelativePath(filepath.ToSlash(relPath))
		if err != nil {
			return err
		}
		if !shouldTrackFile(relPath, includePatterns, excludePatterns) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stats.StattedFiles++
		if !info.Mode().IsRegular() {
			return nil
		}
		previous, existed := previousFiles[relPath]
		if existed && indexedMetadataMatches(previous, info) && strings.TrimSpace(previous.SHA256) != "" {
			files[relPath] = previous
			stats.ReusedFiles++
			return nil
		}
		stats.HashedFiles++
		hash, err := b.hashFile(filePath)
		if err != nil {
			return fmt.Errorf("hash save file %s: %w", relPath, err)
		}
		if strings.TrimSpace(hash) == "" {
			return fmt.Errorf("hash save file %s: empty hash", relPath)
		}
		if err := b.verifyUnchanged(filePath, relPath, info); err != nil {
			return err
		}
		files[relPath] = indexedFileFromInfo(info, hash)
		return nil
	})
	if walkErr != nil {
		return makeIncrementalManifestResult(files, stats, b.now()), fmt.Errorf("enumerate save directory: %w", walkErr)
	}
	return makeIncrementalManifestResult(files, stats, b.now()), nil
}

func (b *IncrementalManifestBuilder) ensureRoot(root string) error {
	info, err := b.stat(root)
	if err != nil {
		return fmt.Errorf("read save directory: %w", err)
	}
	if !info.IsDir() {
		return errors.New("save path is not a directory")
	}
	return nil
}

func (b *IncrementalManifestBuilder) verifyUnchanged(filePath string, relPath string, before fs.FileInfo) error {
	after, err := b.stat(filePath)
	if err != nil || !after.Mode().IsRegular() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrManifestSourceChanged, relPath, err)
		}
		return fmt.Errorf("%w: %s", ErrManifestSourceChanged, relPath)
	}
	return nil
}

func (b *IncrementalManifestBuilder) withDefaults() *IncrementalManifestBuilder {
	if b == nil {
		return NewIncrementalManifestBuilder()
	}
	clone := *b
	defaults := NewIncrementalManifestBuilder()
	if clone.walkDir == nil {
		clone.walkDir = defaults.walkDir
	}
	if clone.stat == nil {
		clone.stat = defaults.stat
	}
	if clone.hashFile == nil {
		clone.hashFile = defaults.hashFile
	}
	if clone.now == nil {
		clone.now = defaults.now
	}
	return &clone
}

func trackedPreviousFiles(previousFiles map[string]DeviceIndexFile, includePatterns []string, excludePatterns []string) (map[string]DeviceIndexFile, error) {
	previous, err := normalizedPreviousFiles(previousFiles)
	if err != nil {
		return nil, err
	}
	files := make(map[string]DeviceIndexFile, len(previous))
	for relPath, file := range previous {
		if !shouldTrackFile(relPath, includePatterns, excludePatterns) {
			continue
		}
		if strings.TrimSpace(file.SHA256) == "" {
			return nil, fmt.Errorf("%w: invalid indexed file %s", ErrManifestRebuildRequired, relPath)
		}
		files[relPath] = file
	}
	return files, nil
}

func normalizedPreviousFiles(previousFiles map[string]DeviceIndexFile) (map[string]DeviceIndexFile, error) {
	files := make(map[string]DeviceIndexFile, len(previousFiles))
	for rawPath, file := range previousFiles {
		relPath, err := normalizeIndexedRelativePath(rawPath)
		if err != nil {
			return nil, err
		}
		if file.Size < 0 {
			return nil, fmt.Errorf("invalid indexed file size: %s", relPath)
		}
		if _, exists := files[relPath]; exists {
			return nil, fmt.Errorf("duplicate indexed file path: %s", relPath)
		}
		file.ModifiedAt = file.ModifiedAt.UTC()
		file.SHA256 = strings.ToLower(strings.TrimSpace(file.SHA256))
		files[relPath] = file
	}
	return files, nil
}

func normalizeDirtyPaths(dirtyPaths []string) ([]string, error) {
	seen := make(map[string]bool, len(dirtyPaths))
	paths := make([]string, 0, len(dirtyPaths))
	for _, rawPath := range dirtyPaths {
		relPath, err := normalizeIndexedRelativePath(rawPath)
		if err != nil {
			return nil, err
		}
		if seen[relPath] {
			continue
		}
		seen[relPath] = true
		paths = append(paths, relPath)
	}
	sort.Strings(paths)
	return paths, nil
}

func indexedMetadataMatches(indexed DeviceIndexFile, info fs.FileInfo) bool {
	return indexed.Size == info.Size() && indexed.ModifiedAt.Equal(info.ModTime().UTC())
}

func indexedFileFromInfo(info fs.FileInfo, hash string) DeviceIndexFile {
	return DeviceIndexFile{
		Size:       info.Size(),
		ModifiedAt: info.ModTime().UTC(),
		SHA256:     strings.ToLower(strings.TrimSpace(hash)),
	}
}

func makeIncrementalManifestResult(files map[string]DeviceIndexFile, stats IncrementalManifestStats, generatedAt time.Time) IncrementalManifestResult {
	snapshot := make(map[string]DeviceIndexFile, len(files))
	paths := make([]string, 0, len(files))
	for relPath, file := range files {
		snapshot[relPath] = file
		paths = append(paths, relPath)
	}
	sort.Strings(paths)
	manifest := SyncManifest{
		GeneratedAt: generatedAt.UTC(),
		Files:       make([]ManifestFile, 0, len(paths)),
	}
	for _, relPath := range paths {
		file := snapshot[relPath]
		manifest.Files = append(manifest.Files, ManifestFile{
			Path:       relPath,
			Size:       file.Size,
			ModifiedAt: file.ModifiedAt.UTC(),
			SHA256:     file.SHA256,
		})
		manifest.TotalBytes += file.Size
	}
	manifest.Hash = hashManifestFiles(manifest.Files)
	return IncrementalManifestResult{
		Manifest: manifest,
		Files:    snapshot,
		Stats:    stats,
	}
}
