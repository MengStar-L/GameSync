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

var ErrLocalFileChanged = errors.New("local save file changed during sync")

type syncObjectDownloader interface {
	DownloadObjectToFile(ctx context.Context, key string, destinationPath string) error
}

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

func InspectLaunchSync(ctx context.Context, game Game, gateway *StorageGateway) (LaunchSyncInspection, error) {
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
	if gateway == nil || gateway.Catalog == nil || gateway.Objects == nil {
		return LaunchSyncInspection{}, errors.New("storage gateway is incomplete")
	}

	if err := gateway.Catalog.EnsureSchema(ctx); err != nil {
		return LaunchSyncInspection{}, err
	}

	localManifest, err := BuildLocalManifest(game.SavePath, game.Sync.IncludePatterns, game.Sync.ExcludePatterns)
	if err != nil {
		return LaunchSyncInspection{}, err
	}
	remoteRecord, err := gateway.Catalog.LoadRemoteManifest(ctx, game.ID)
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

func (e *Engine) SyncGameWithGateway(ctx context.Context, device DeviceInfo, game Game, gateway *StorageGateway, conflictChoice string, progress func(string)) (SyncSummary, SyncAnchor, error) {
	if !game.Sync.Enabled {
		return SyncSummary{
			Status:   "disabled",
			Message:  "该游戏的同步已禁用。",
			SyncedAt: time.Now(),
		}, game.Anchor, nil
	}
	if strings.TrimSpace(game.SavePath) == "" {
		return SyncSummary{
			Status:   "unconfigured",
			Message:  "当前设备未配置存档目录。",
			SyncedAt: time.Now(),
		}, game.Anchor, nil
	}
	if progress == nil {
		progress = func(string) {}
	}

	if gateway == nil || gateway.Catalog == nil || gateway.Objects == nil {
		return SyncSummary{}, game.Anchor, errors.New("storage gateway is incomplete")
	}

	progress("正在初始化云端元数据...")
	if err := gateway.Catalog.EnsureSchema(ctx); err != nil {
		return SyncSummary{}, game.Anchor, err
	}

	progress("正在扫描本地存档文件...")
	localManifest, err := BuildLocalManifest(game.SavePath, game.Sync.IncludePatterns, game.Sync.ExcludePatterns)
	if err != nil {
		return SyncSummary{}, game.Anchor, err
	}

	progress("正在读取云端文件索引...")
	remoteRecord, err := gateway.Catalog.LoadRemoteManifest(ctx, game.ID)
	if err != nil {
		return SyncSummary{}, game.Anchor, err
	}
	return e.SyncGameWithPreparedManifest(ctx, device, game, gateway, conflictChoice, localManifest, remoteRecord, progress)
}

// SyncGameWithPreparedManifest executes merge and transfer work without scanning the
// local save directory or loading the remote manifest. Callers own those two phases.
func (e *Engine) SyncGameWithPreparedManifest(ctx context.Context, device DeviceInfo, game Game, gateway *StorageGateway, conflictChoice string, localManifest SyncManifest, remoteRecord RemoteManifestRecord, progress func(string)) (SyncSummary, SyncAnchor, error) {
	if !game.Sync.Enabled {
		return SyncSummary{
			Status:   "disabled",
			Message:  "该游戏的同步已禁用。",
			SyncedAt: time.Now(),
		}, game.Anchor, nil
	}
	if strings.TrimSpace(game.SavePath) == "" {
		return SyncSummary{
			Status:   "unconfigured",
			Message:  "当前设备未配置存档目录。",
			SyncedAt: time.Now(),
		}, game.Anchor, nil
	}
	if gateway == nil || gateway.Catalog == nil || gateway.Objects == nil {
		return SyncSummary{}, game.Anchor, errors.New("storage gateway is incomplete")
	}
	if progress == nil {
		progress = func(string) {}
	}

	if remoteRecord.Version == 0 && len(remoteRecord.Manifest.Files) == 0 {
		if len(localManifest.Files) == 0 {
			summary := SyncSummary{
				Status:   "success",
				Message:  "本地与云端都没有可同步的存档。",
				SyncedAt: time.Now(),
			}
			anchor := SyncAnchor{LastManifest: localManifest, StorageAccountID: game.StorageAccountID}
			anchor.PendingRemoteCleanups = game.Anchor.PendingRemoteCleanups
			return summary, anchor, nil
		}

		progress("云端为空，正在初始化第一个远端版本...")
		nextVersion := 1
		localManifest.Version = nextVersion
		uploads := diffsFromManifest(localManifest, "upload")
		if err := e.applyUploads(ctx, gateway.Objects, game, uploads); err != nil {
			return SyncSummary{}, game.Anchor, err
		}

		record := RemoteManifestRecord{
			GameID:          game.ID,
			Version:         nextVersion,
			Manifest:        localManifest,
			UpdatedAt:       time.Now(),
			UpdatedByDevice: device.ID,
		}
		if err := gateway.Catalog.SaveRemoteManifestIfVersion(ctx, record, 0); err != nil {
			return SyncSummary{}, game.Anchor, err
		}

		summary := SyncSummary{
			Status:   "success",
			Message:  fmt.Sprintf("已建立初始云端版本（上传 %d 个文件）", len(uploads)),
			Uploaded: len(uploads),
			SyncedAt: time.Now(),
		}
		anchor := SyncAnchor{LastRemoteVersion: nextVersion, LastManifest: localManifest, StorageAccountID: game.StorageAccountID}
		anchor.PendingRemoteCleanups = game.Anchor.PendingRemoteCleanups
		return summary, anchor, nil
	}

	baseManifest := game.Anchor.LastManifest
	mergedManifest, uploads, downloads, conflicts := mergeManifests(baseManifest, localManifest, remoteRecord.Manifest, conflictChoice)
	storageChanged := strings.TrimSpace(game.Anchor.StorageAccountID) != "" &&
		strings.TrimSpace(game.Anchor.StorageAccountID) != strings.TrimSpace(game.StorageAccountID)
	if storageChanged {
		uploads = forceUploadManifestFiles(uploads, mergedManifest)
	}
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
			StorageAccountID:  game.StorageAccountID,
		}
		if remoteRecord.Version == 0 {
			anchor.LastManifest = localManifest
		}
		anchor.PendingRemoteCleanups = game.Anchor.PendingRemoteCleanups
		return SyncSummary{
			Status:   "success",
			Message:  "本地与云端已是最新状态。",
			SyncedAt: time.Now(),
		}, anchor, nil
	}

	// 下载先落盘但延迟提交（M8）：回滚句柄持有到 D1 CAS 成功后才 commit，
	// 中途任何失败都把本地存档还原到同步前状态，重试时不会产生虚假冲突
	var downloadRollback *localDownloadRollback
	if len(downloads) > 0 {
		progress("正在应用云端变更到本地...")
		rollback, err := e.applyDownloads(ctx, gateway.Objects, game, downloads)
		if err != nil {
			return SyncSummary{}, game.Anchor, err
		}
		downloadRollback = rollback
	}

	if len(uploads) > 0 {
		progress("正在上传本地变更到云端...")
		if err := e.applyUploads(ctx, gateway.Objects, game, uploads); err != nil {
			if downloadRollback != nil {
				_ = downloadRollback.rollback()
			}
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
		progress("正在写入新的云端版本索引...")
		if err := gateway.Catalog.SaveRemoteManifestIfVersion(ctx, record, remoteRecord.Version); err != nil {
			if downloadRollback != nil {
				_ = downloadRollback.rollback()
			}
			return SyncSummary{}, game.Anchor, err
		}
		anchor.PendingRemoteCleanups = e.cleanupRemoteObjects(ctx, gateway.Objects, game.ID, mergedManifest, uploads, game.Anchor.PendingRemoteCleanups)
		anchor.LastRemoteVersion = mergedManifest.Version
		anchor.LastManifest = mergedManifest
		anchor.StorageAccountID = game.StorageAccountID
	} else {
		anchor.LastRemoteVersion = remoteRecord.Version
		anchor.LastManifest = remoteRecord.Manifest
		anchor.StorageAccountID = game.StorageAccountID
		// 仅下载的同步同样推进延迟清理（处理已到期的登记项）
		anchor.PendingRemoteCleanups = e.cleanupRemoteObjects(ctx, gateway.Objects, game.ID, remoteRecord.Manifest, nil, game.Anchor.PendingRemoteCleanups)
	}
	if downloadRollback != nil {
		downloadRollback.commit()
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

func (e *Engine) applyUploads(ctx context.Context, r2 ObjectStore, game Game, uploads []ManifestDiff) error {
	for _, diff := range uploads {
		if diff.Action != "upload" || diff.Local == nil {
			continue
		}
		localFilePath, err := safeSaveFilePath(game.SavePath, diff.Path)
		if err != nil {
			return err
		}
		snapshotPath, err := stableUploadSnapshot(localFilePath, diff.Local)
		if err != nil {
			return err
		}
		if err := r2.PutObjectFromFile(ctx, objectKey(game.ID, diff.Local.SHA256), snapshotPath); err != nil {
			_ = os.Remove(snapshotPath)
			return err
		}
		_ = os.Remove(snapshotPath)
		if err := verifyManifestFileMetadata(localFilePath, diff.Local); err != nil {
			return err
		}
	}
	return nil
}

func stableUploadSnapshot(sourcePath string, expected *ManifestFile) (string, error) {
	if expected == nil {
		return "", errors.New("upload manifest entry is missing")
	}
	if err := verifyManifestFileMetadata(sourcePath, expected); err != nil {
		return "", err
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return "", err
	}
	defer source.Close()
	tempFile, err := os.CreateTemp("", ".gamesync-upload-*")
	if err != nil {
		return "", fmt.Errorf("create upload snapshot: %w", err)
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		_ = tempFile.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(tempFile, hasher), source)
	if err != nil {
		return "", fmt.Errorf("copy upload snapshot: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		return "", fmt.Errorf("sync upload snapshot: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("close upload snapshot: %w", err)
	}
	if written != expected.Size || !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), expected.SHA256) {
		return "", fmt.Errorf("%w: %s", ErrLocalFileChanged, expected.Path)
	}
	if err := verifyManifestFileMetadata(sourcePath, expected); err != nil {
		return "", err
	}
	removeTemp = false
	return tempPath, nil
}

func verifyManifestFileMetadata(path string, expected *ManifestFile) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrLocalFileChanged, expected.Path)
		}
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != expected.Size ||
		(!expected.ModifiedAt.IsZero() && !info.ModTime().UTC().Equal(expected.ModifiedAt.UTC())) {
		return fmt.Errorf("%w: %s", ErrLocalFileChanged, expected.Path)
	}
	return nil
}

// applyDownloads 把云端变更落盘但不提交：成功时返回未提交的回滚句柄，由调用方在
// D1 CAS 成功后 commit（M8）；自身失败时先回滚再返回错误，句柄为 nil。
func (e *Engine) applyDownloads(ctx context.Context, r2 syncObjectDownloader, game Game, downloads []ManifestDiff) (*localDownloadRollback, error) {
	rollback := newLocalDownloadRollback()

	for _, diff := range downloads {
		targetPath, err := safeSaveFilePath(game.SavePath, diff.Path)
		if err != nil {
			_ = rollback.rollback()
			return nil, err
		}
		switch diff.Action {
		case "download":
			if diff.Remote == nil {
				continue
			}
			if err := rollback.stage(targetPath); err != nil {
				_ = rollback.rollback()
				return nil, err
			}
			if err := downloadObjectToVerifiedFile(ctx, r2, objectKey(game.ID, diff.Remote.SHA256), targetPath, diff.Remote); err != nil {
				_ = rollback.rollback()
				return nil, err
			}
		case "delete_local":
			if err := rollback.stage(targetPath); err != nil {
				_ = rollback.rollback()
				return nil, err
			}
		}
	}
	return rollback, nil
}

// remoteObjectCleanupGrace 被替换对象的删除宽限期：给其他设备进行中的下载留时间
const remoteObjectCleanupGrace = 10 * time.Minute

// cleanupRemoteObjects 延迟清理被替换的 R2 对象：新替换的先登记不删；
// 已登记且超过宽限期的才真正删除；期间又被清单重新引用的直接放弃清理。
// 返回更新后的登记列表，由调用方存入 anchor 持久化。
func (e *Engine) cleanupRemoteObjects(ctx context.Context, r2 ObjectStore, gameID string, manifest SyncManifest, diffs []ManifestDiff, pending []PendingRemoteCleanup) []PendingRemoteCleanup {
	referenced := make(map[string]bool, len(manifest.Files))
	for _, file := range manifest.Files {
		if strings.TrimSpace(file.SHA256) != "" {
			referenced[file.SHA256] = true
		}
	}
	now := time.Now()
	seen := make(map[string]bool)
	next := make([]PendingRemoteCleanup, 0, len(pending)+len(diffs))
	for _, entry := range pending {
		sha := strings.TrimSpace(entry.SHA256)
		if sha == "" || referenced[sha] || seen[sha] {
			continue
		}
		if r2 != nil && now.Sub(entry.ReplacedAt) > remoteObjectCleanupGrace {
			_ = r2.DeleteObject(ctx, objectKey(gameID, sha))
			continue
		}
		seen[sha] = true
		next = append(next, entry)
	}
	for _, diff := range diffs {
		if diff.Remote == nil {
			continue
		}
		sha := strings.TrimSpace(diff.Remote.SHA256)
		if sha == "" || referenced[sha] || seen[sha] {
			continue
		}
		seen[sha] = true
		next = append(next, PendingRemoteCleanup{SHA256: sha, ReplacedAt: now})
	}
	if len(next) == 0 {
		return nil
	}
	return next
}

func safeSaveFilePath(root string, relPath string) (string, error) {
	root = strings.TrimSpace(root)
	relPath = filepath.ToSlash(strings.TrimSpace(relPath))
	if root == "" {
		return "", errors.New("save path is empty")
	}
	if relPath == "" || relPath == "." || strings.HasPrefix(relPath, "/") {
		return "", fmt.Errorf("unsafe save file path: %s", relPath)
	}
	nativeRel := filepath.FromSlash(relPath)
	if filepath.IsAbs(nativeRel) {
		return "", fmt.Errorf("unsafe save file path: %s", relPath)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(filepath.Join(rootAbs, nativeRel))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", err
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("unsafe save file path: %s", relPath)
	}
	return targetAbs, nil
}

type localDownloadRollback struct {
	entries []localDownloadRollbackEntry
	staged  map[string]string
}

type localDownloadRollbackEntry struct {
	targetPath string
	backupPath string
}

func newLocalDownloadRollback() *localDownloadRollback {
	return &localDownloadRollback{
		staged: map[string]string{},
	}
}

func (r *localDownloadRollback) stage(targetPath string) error {
	if r == nil {
		return nil
	}
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return errors.New("rollback target path is empty")
	}
	if _, exists := r.staged[targetPath]; exists {
		return nil
	}
	info, err := os.Stat(targetPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.staged[targetPath] = ""
			r.entries = append(r.entries, localDownloadRollbackEntry{
				targetPath: targetPath,
			})
			return nil
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("sync target is a directory: %s", targetPath)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(targetPath), ".gamesync-rollback-*")
	if err != nil {
		return fmt.Errorf("create rollback file: %w", err)
	}
	backupPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("close rollback file: %w", err)
	}
	_ = os.Remove(backupPath)
	if err := os.Rename(targetPath, backupPath); err != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("stage rollback file: %w", err)
	}
	r.staged[targetPath] = backupPath
	r.entries = append(r.entries, localDownloadRollbackEntry{
		targetPath: targetPath,
		backupPath: backupPath,
	})
	return nil
}

func (r *localDownloadRollback) commit() {
	if r == nil {
		return
	}
	for _, entry := range r.entries {
		if entry.backupPath != "" {
			_ = os.Remove(entry.backupPath)
		}
	}
}

func (r *localDownloadRollback) rollback() error {
	if r == nil {
		return nil
	}
	var failures []string
	for index := len(r.entries) - 1; index >= 0; index-- {
		entry := r.entries[index]
		if err := os.Remove(entry.targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, err.Error())
			continue
		}
		if entry.backupPath == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(entry.targetPath), 0o755); err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if err := os.Rename(entry.backupPath, entry.targetPath); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func downloadObjectToVerifiedFile(ctx context.Context, r2 syncObjectDownloader, key string, targetPath string, remote *ManifestFile) error {
	if remote == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("create destination dir: %w", err)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(targetPath), ".gamesync-download-*")
	if err != nil {
		return fmt.Errorf("create temporary download file: %w", err)
	}
	tempPath := tempFile.Name()
	_ = tempFile.Close()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if err := r2.DownloadObjectToFile(ctx, key, tempPath); err != nil {
		return err
	}
	downloadedHash, err := sha256File(tempPath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(downloadedHash, remote.SHA256) {
		return fmt.Errorf("downloaded object hash mismatch for %s", remote.Path)
	}
	info, err := os.Stat(tempPath)
	if err != nil {
		return err
	}
	if info.Size() != remote.Size {
		return fmt.Errorf("downloaded object size mismatch for %s", remote.Path)
	}
	if !remote.ModifiedAt.IsZero() {
		_ = os.Chtimes(tempPath, remote.ModifiedAt, remote.ModifiedAt)
	}
	if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		return fmt.Errorf("replace destination file: %w", err)
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

func forceUploadManifestFiles(existing []ManifestDiff, manifest SyncManifest) []ManifestDiff {
	seen := make(map[string]bool, len(existing))
	for _, diff := range existing {
		if diff.Action == "upload" {
			seen[diff.Path] = true
		}
	}
	result := append([]ManifestDiff{}, existing...)
	for index := range manifest.Files {
		file := manifest.Files[index]
		if seen[file.Path] {
			continue
		}
		copied := file
		result = append(result, ManifestDiff{
			Path:   file.Path,
			Action: "upload",
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
