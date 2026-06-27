package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Engine struct{}

type LaunchSyncInspection struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	Uploads   int    `json:"uploads"`
	Downloads int    `json:"downloads"`
	Conflicts int    `json:"conflicts"`
}

func NewEngine() *Engine {
	return &Engine{}
}

func InspectLaunchSync(ctx context.Context, game Game, gateway *CloudflareGateway) (LaunchSyncInspection, error) {
	if !game.Sync.Enabled {
		return LaunchSyncInspection{
			Status:  "ready",
			Message: "该游戏未启用同步，直接启动。",
		}, nil
	}
	if strings.TrimSpace(game.SavePath) == "" {
		return LaunchSyncInspection{
			Status:  "ready",
			Message: "未配置存档目录，直接启动。",
		}, nil
	}
	if gateway == nil || gateway.D1 == nil || gateway.R2 == nil {
		return LaunchSyncInspection{}, errors.New("cloudflare gateway is incomplete")
	}

	if err := gateway.D1.EnsureSchema(ctx); err != nil {
		return LaunchSyncInspection{}, err
	}

	localManifest, err := BuildLocalManifest(game.SavePath, game.Sync.IncludePatterns, game.Sync.ExcludePatterns)
	if err != nil {
		return LaunchSyncInspection{}, err
	}
	remoteRecord, err := gateway.D1.LoadRemoteManifest(ctx, game.ID)
	if err != nil {
		return LaunchSyncInspection{}, err
	}

	if remoteRecord.Version == 0 && len(remoteRecord.Manifest.Files) == 0 {
		if len(localManifest.Files) == 0 {
			return LaunchSyncInspection{
				Status:  "ready",
				Message: "本地与云端都没有可同步的存档。",
			}, nil
		}
		return LaunchSyncInspection{
			Status:  "local_newer",
			Message: "检测到本地存档比云端更新，启动前会先上传到云端。",
			Uploads: len(localManifest.Files),
		}, nil
	}

	_, uploads, downloads, conflicts := mergeManifests(game.Anchor.LastManifest, localManifest, remoteRecord.Manifest, "")
	inspection := LaunchSyncInspection{
		Uploads:   len(uploads),
		Downloads: len(downloads),
		Conflicts: len(conflicts),
	}

	switch {
	case len(conflicts) > 0:
		inspection.Status = "conflict"
		inspection.Message = fmt.Sprintf("检测到 %d 个文件冲突，启动前请选择保留本地还是云端版本。", len(conflicts))
	case len(uploads) > 0 && len(downloads) == 0:
		inspection.Status = "local_newer"
		inspection.Message = fmt.Sprintf("检测到本地有 %d 项较新的存档改动，启动前会先上传到云端。", len(uploads))
	case len(downloads) > 0 && len(uploads) == 0:
		inspection.Status = "cloud_newer"
		inspection.Message = fmt.Sprintf("检测到云端有 %d 项较新的存档改动，启动前会先同步到本地。", len(downloads))
	case len(downloads) > 0 || len(uploads) > 0:
		inspection.Status = "merge_needed"
		inspection.Message = fmt.Sprintf("检测到本地与云端各有改动，启动前将自动合并（上传 %d 项，下载 %d 项）。", len(uploads), len(downloads))
	default:
		inspection.Status = "ready"
		inspection.Message = "本地存档已是最新版本，正在启动游戏。"
	}

	return inspection, nil
}

func (e *Engine) SyncGame(ctx context.Context, device DeviceInfo, game Game, account CloudflareAccount, conflictChoice string, progress func(string)) (SyncSummary, SyncAnchor, error) {
	gateway, err := NewCloudflareGateway(ctx, account)
	if err != nil {
		return SyncSummary{}, game.Anchor, err
	}
	return e.SyncGameWithGateway(ctx, device, game, gateway, conflictChoice, progress)
}

func (e *Engine) SyncGameWithGateway(ctx context.Context, device DeviceInfo, game Game, gateway *CloudflareGateway, conflictChoice string, progress func(string)) (SyncSummary, SyncAnchor, error) {
	if !game.Sync.Enabled {
		return SyncSummary{
			Status:   "disabled",
			Message:  "该游戏的同步已禁用。",
			SyncedAt: time.Now(),
		}, game.Anchor, nil
	}
	if strings.TrimSpace(game.SavePath) == "" {
		return SyncSummary{}, game.Anchor, errors.New("游戏未配置存档目录")
	}
	if progress == nil {
		progress = func(string) {}
	}

	if gateway == nil || gateway.D1 == nil || gateway.R2 == nil {
		return SyncSummary{}, game.Anchor, errors.New("cloudflare gateway is incomplete")
	}

	progress("正在初始化 D1 元数据表...")
	if err := gateway.D1.EnsureSchema(ctx); err != nil {
		return SyncSummary{}, game.Anchor, err
	}

	progress("正在扫描本地存档文件...")
	localManifest, err := BuildLocalManifest(game.SavePath, game.Sync.IncludePatterns, game.Sync.ExcludePatterns)
	if err != nil {
		return SyncSummary{}, game.Anchor, err
	}

	progress("正在读取云端文件索引...")
	remoteRecord, err := gateway.D1.LoadRemoteManifest(ctx, game.ID)
	if err != nil {
		return SyncSummary{}, game.Anchor, err
	}

	if remoteRecord.Version == 0 && len(remoteRecord.Manifest.Files) == 0 {
		if len(localManifest.Files) == 0 {
			summary := SyncSummary{
				Status:   "success",
				Message:  "本地与云端都没有可同步的存档。",
				SyncedAt: time.Now(),
			}
			return summary, SyncAnchor{LastManifest: localManifest}, nil
		}

		progress("云端为空，正在初始化第一个远端版本...")
		nextVersion := 1
		localManifest.Version = nextVersion
		uploads := diffsFromManifest(localManifest, "upload")
		if err := e.applyUploads(ctx, gateway.R2, game, uploads); err != nil {
			return SyncSummary{}, game.Anchor, err
		}

		record := RemoteManifestRecord{
			GameID:          game.ID,
			Version:         nextVersion,
			Manifest:        localManifest,
			UpdatedAt:       time.Now(),
			UpdatedByDevice: device.ID,
		}
		if err := gateway.D1.SaveRemoteManifest(ctx, record); err != nil {
			return SyncSummary{}, game.Anchor, err
		}

		summary := SyncSummary{
			Status:   "success",
			Message:  fmt.Sprintf("已建立初始云端版本（上传 %d 个文件）", len(uploads)),
			Uploaded: len(uploads),
			SyncedAt: time.Now(),
		}
		return summary, SyncAnchor{LastRemoteVersion: nextVersion, LastManifest: localManifest}, nil
	}

	baseManifest := game.Anchor.LastManifest
	mergedManifest, uploads, downloads, conflicts := mergeManifests(baseManifest, localManifest, remoteRecord.Manifest, conflictChoice)
	if len(conflicts) > 0 && strings.TrimSpace(conflictChoice) == "" {
		return SyncSummary{
			Status:    "conflict",
			Message:   fmt.Sprintf("检测到 %d 个文件冲突，请选择保留本地或云端版本", len(conflicts)),
			Conflicts: len(conflicts),
			SyncedAt:  time.Now(),
		}, game.Anchor, nil
	}

	if len(uploads) == 0 && len(downloads) == 0 {
		anchor := SyncAnchor{
			LastRemoteVersion: remoteRecord.Version,
			LastManifest:      remoteRecord.Manifest,
		}
		if remoteRecord.Version == 0 {
			anchor.LastManifest = localManifest
		}
		return SyncSummary{
			Status:   "success",
			Message:  "本地与云端已是最新状态。",
			SyncedAt: time.Now(),
		}, anchor, nil
	}

	if len(downloads) > 0 {
		progress("正在应用云端变更到本地...")
		if err := e.applyDownloads(ctx, gateway.R2, game, downloads); err != nil {
			return SyncSummary{}, game.Anchor, err
		}
	}

	if len(uploads) > 0 {
		progress("正在上传本地变更到 R2...")
		if err := e.applyUploads(ctx, gateway.R2, game, uploads); err != nil {
			return SyncSummary{}, game.Anchor, err
		}
	}

	anchor := SyncAnchor{}
	if len(uploads) > 0 {
		mergedManifest.Version = remoteRecord.Version + 1
		record := RemoteManifestRecord{
			GameID:          game.ID,
			Version:         mergedManifest.Version,
			Manifest:        mergedManifest,
			UpdatedAt:       time.Now(),
			UpdatedByDevice: device.ID,
		}
		progress("正在写入新的 D1 版本索引...")
		if err := gateway.D1.SaveRemoteManifest(ctx, record); err != nil {
			return SyncSummary{}, game.Anchor, err
		}
		anchor.LastRemoteVersion = mergedManifest.Version
		anchor.LastManifest = mergedManifest
	} else {
		anchor.LastRemoteVersion = remoteRecord.Version
		anchor.LastManifest = remoteRecord.Manifest
	}

	summary := SyncSummary{
		Status:     "success",
		Message:    fmt.Sprintf("同步完成（上传 %d 个，下载 %d 个）", len(uploads), len(downloads)),
		Uploaded:   len(uploads),
		Downloaded: len(downloads),
		SyncedAt:   time.Now(),
	}
	return summary, anchor, nil
}

func BuildLocalManifest(root string, includePatterns []string, excludePatterns []string) (SyncManifest, error) {
	if root == "" {
		return SyncManifest{}, errors.New("存档目录为空")
	}

	info, err := os.Stat(root)
	if err != nil {
		return SyncManifest{}, fmt.Errorf("读取存档目录失败: %w", err)
	}
	if !info.IsDir() {
		return SyncManifest{}, errors.New("存档路径不是目录")
	}

	manifest := SyncManifest{
		GeneratedAt: time.Now(),
		Files:       make([]ManifestFile, 0),
	}

	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		if !shouldTrackFile(relPath, includePatterns, excludePatterns) {
			return nil
		}

		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		fileHash, err := sha256File(path)
		if err != nil {
			return err
		}

		manifest.Files = append(manifest.Files, ManifestFile{
			Path:       relPath,
			Size:       fileInfo.Size(),
			ModifiedAt: fileInfo.ModTime().UTC(),
			SHA256:     fileHash,
		})
		manifest.TotalBytes += fileInfo.Size()
		return nil
	})
	if err != nil {
		return SyncManifest{}, err
	}

	sort.Slice(manifest.Files, func(left, right int) bool {
		return manifest.Files[left].Path < manifest.Files[right].Path
	})
	manifest.Hash = hashManifestFiles(manifest.Files)
	return manifest, nil
}

func (e *Engine) applyUploads(ctx context.Context, r2 *R2Client, game Game, uploads []ManifestDiff) error {
	for _, diff := range uploads {
		if diff.Action != "upload" || diff.Local == nil {
			continue
		}
		localFilePath := filepath.Join(game.SavePath, filepath.FromSlash(diff.Path))
		if err := r2.PutObjectFromFile(ctx, objectKey(game.ID, diff.Local.SHA256), localFilePath); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) applyDownloads(ctx context.Context, r2 *R2Client, game Game, downloads []ManifestDiff) error {
	for _, diff := range downloads {
		targetPath := filepath.Join(game.SavePath, filepath.FromSlash(diff.Path))
		switch diff.Action {
		case "download":
			if diff.Remote == nil {
				continue
			}
			if err := r2.DownloadObjectToFile(ctx, objectKey(game.ID, diff.Remote.SHA256), targetPath); err != nil {
				return err
			}
		case "delete_local":
			if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

func mergeManifests(base SyncManifest, local SyncManifest, remote SyncManifest, conflictChoice string) (SyncManifest, []ManifestDiff, []ManifestDiff, []ManifestDiff) {
	baseMap := manifestMap(base)
	localMap := manifestMap(local)
	remoteMap := manifestMap(remote)
	allPaths := uniquePaths(baseMap, localMap, remoteMap)

	mergedFiles := make([]ManifestFile, 0, len(allPaths))
	uploads := make([]ManifestDiff, 0)
	downloads := make([]ManifestDiff, 0)
	conflicts := make([]ManifestDiff, 0)

	for _, path := range allPaths {
		baseFile := cloneFile(baseMap[path])
		localFile := cloneFile(localMap[path])
		remoteFile := cloneFile(remoteMap[path])

		changedLocal := !sameFile(baseFile, localFile)
		changedRemote := !sameFile(baseFile, remoteFile)

		var selected *ManifestFile
		switch {
		case !changedLocal && !changedRemote:
			selected = firstNonNil(remoteFile, localFile, baseFile)
		case changedLocal && !changedRemote:
			selected = localFile
			uploads = append(uploads, uploadDiff(path, localFile, remoteFile))
		case !changedLocal && changedRemote:
			selected = remoteFile
			downloads = append(downloads, downloadDiff(path, localFile, remoteFile))
		default:
			if sameFile(localFile, remoteFile) {
				selected = firstNonNil(localFile, remoteFile)
				break
			}

			switch strings.ToLower(strings.TrimSpace(conflictChoice)) {
			case "local":
				selected = localFile
				uploads = append(uploads, uploadDiff(path, localFile, remoteFile))
			case "remote", "cloud":
				selected = remoteFile
				downloads = append(downloads, downloadDiff(path, localFile, remoteFile))
			default:
				conflicts = append(conflicts, ManifestDiff{
					Path:   path,
					Action: "conflict",
					Local:  localFile,
					Remote: remoteFile,
				})
				continue
			}
		}

		if selected != nil {
			mergedFiles = append(mergedFiles, *selected)
		}
	}

	sort.Slice(mergedFiles, func(left, right int) bool {
		return mergedFiles[left].Path < mergedFiles[right].Path
	})

	manifest := SyncManifest{
		GeneratedAt: time.Now(),
		Files:       mergedFiles,
	}
	for _, file := range mergedFiles {
		manifest.TotalBytes += file.Size
	}
	manifest.Hash = hashManifestFiles(manifest.Files)

	return manifest, uploads, downloads, conflicts
}

func manifestMap(manifest SyncManifest) map[string]*ManifestFile {
	result := make(map[string]*ManifestFile, len(manifest.Files))
	for index := range manifest.Files {
		file := manifest.Files[index]
		copied := file
		result[file.Path] = &copied
	}
	return result
}

func uniquePaths(maps ...map[string]*ManifestFile) []string {
	set := make(map[string]struct{})
	for _, fileMap := range maps {
		for path := range fileMap {
			set[path] = struct{}{}
		}
	}

	paths := make([]string, 0, len(set))
	for path := range set {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func uploadDiff(path string, local *ManifestFile, remote *ManifestFile) ManifestDiff {
	action := "upload"
	if local == nil {
		action = "delete_remote"
	}
	return ManifestDiff{Path: path, Action: action, Local: local, Remote: remote}
}

func downloadDiff(path string, local *ManifestFile, remote *ManifestFile) ManifestDiff {
	action := "download"
	if remote == nil {
		action = "delete_local"
	}
	return ManifestDiff{Path: path, Action: action, Local: local, Remote: remote}
}

func diffsFromManifest(manifest SyncManifest, action string) []ManifestDiff {
	result := make([]ManifestDiff, 0, len(manifest.Files))
	for index := range manifest.Files {
		file := manifest.Files[index]
		copied := file
		result = append(result, ManifestDiff{
			Path:   file.Path,
			Action: action,
			Local:  &copied,
		})
	}
	return result
}

func shouldTrackFile(relPath string, includePatterns []string, excludePatterns []string) bool {
	relPath = filepath.ToSlash(relPath)
	included := len(includePatterns) == 0
	for _, pattern := range includePatterns {
		if globMatch(relPath, pattern) {
			included = true
			break
		}
	}
	if !included {
		return false
	}

	for _, pattern := range excludePatterns {
		if globMatch(relPath, pattern) {
			return false
		}
	}
	return true
}

func globMatch(relPath string, pattern string) bool {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	if ok, _ := filepath.Match(pattern, relPath); ok {
		return true
	}
	if ok, _ := filepath.Match(pattern, filepath.Base(relPath)); ok {
		return true
	}
	return false
}

func sha256File(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func hashManifestFiles(files []ManifestFile) string {
	content, err := json.Marshal(files)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func objectKey(gameID string, sha string) string {
	return fmt.Sprintf("games/%s/objects/%s", gameID, sha)
}

func sameFile(left *ManifestFile, right *ManifestFile) bool {
	if left == nil && right == nil {
		return true
	}
	if left == nil || right == nil {
		return false
	}
	return left.Path == right.Path && left.Size == right.Size && left.SHA256 == right.SHA256
}

func cloneFile(file *ManifestFile) *ManifestFile {
	if file == nil {
		return nil
	}
	copied := *file
	return &copied
}

func firstNonNil(files ...*ManifestFile) *ManifestFile {
	for _, file := range files {
		if file != nil {
			return file
		}
	}
	return nil
}
