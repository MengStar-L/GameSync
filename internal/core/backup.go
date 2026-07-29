package core

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type BackupManager struct {
	Engine *Engine
}

func BackupObjectKey(gameID, deviceID, filename string) string {
	gameID = strings.TrimSpace(gameID)
	deviceID = strings.TrimSpace(deviceID)
	filename = strings.TrimSpace(filename)
	if deviceID == "" {
		return fmt.Sprintf("backups/%s/%s", gameID, filename)
	}
	return fmt.Sprintf("backups/%s/%s/%s", gameID, deviceID, filename)
}

func BackupObjectKeyForRecord(gameID string, record BackupRecord) string {
	if key := strings.TrimSpace(record.ObjectKey); key != "" {
		return key
	}
	return BackupObjectKey(gameID, record.SourceDeviceID, record.Filename)
}

func NewBackupManager(engine *Engine) *BackupManager {
	return &BackupManager{Engine: engine}
}

func (bm *BackupManager) EnsureLocalBackupDir(dataDir string, gameID string) string {
	dir := filepath.Join(dataDir, "backups", gameID)
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func BackupLocalPathForRecord(dataDir string, gameID string, record BackupRecord) string {
	root := filepath.Join(dataDir, "backups", strings.TrimSpace(gameID))
	filename := strings.TrimSpace(record.Filename)
	deviceID := strings.TrimSpace(record.SourceDeviceID)
	if deviceID == "" || deviceID != filepath.Base(deviceID) || strings.ContainsAny(deviceID, `/\`) {
		return filepath.Join(root, filename)
	}
	return filepath.Join(root, deviceID, filename)
}

func existingBackupLocalPath(dataDir string, gameID string, record BackupRecord, allowLegacyFallback bool) string {
	preferred := BackupLocalPathForRecord(dataDir, gameID, record)
	if _, err := os.Stat(preferred); err == nil {
		return preferred
	}
	legacy := filepath.Join(dataDir, "backups", strings.TrimSpace(gameID), strings.TrimSpace(record.Filename))
	if allowLegacyFallback && preferred != legacy {
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	return preferred
}

func ExistingBackupLocalPathForRecord(dataDir string, gameID string, record BackupRecord) string {
	return existingBackupLocalPath(dataDir, gameID, record, true)
}

func zipDirectory(source string, target string) error {
	zipFile, err := os.Create(target)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	archive := zip.NewWriter(zipFile)
	defer archive.Close()

	info, err := os.Stat(source)
	if err != nil {
		return err
	}

	var baseDir string
	if info.IsDir() {
		baseDir = filepath.Base(source)
	}

	return filepath.Walk(source, func(path string, fileInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source && fileInfo.IsDir() {
			return nil
		}

		header, err := zip.FileInfoHeader(fileInfo)
		if err != nil {
			return err
		}
		if baseDir != "" {
			rel, _ := filepath.Rel(source, path)
			header.Name = filepath.ToSlash(rel)
		}
		if fileInfo.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}

		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		if fileInfo.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(writer, file)
		return err
	})
}

func (bm *BackupManager) CreateBackup(ctx context.Context, game Game, backupType string, name string, sourceDeviceID string, dataDir string, gateway *StorageGateway) (*Backup, error) {
	if strings.TrimSpace(game.SavePath) == "" {
		return nil, fmt.Errorf("save path is empty")
	}

	timestamp := time.Now().Format("20060102_150405_000000000")
	filename := fmt.Sprintf("backup_manual_%s.zip", timestamp)
	if strings.EqualFold(strings.TrimSpace(backupType), "auto") {
		filename = fmt.Sprintf("backup_auto_%s.zip", timestamp)
		backupType = "auto"
	} else {
		backupType = "manual"
	}

	record := BackupRecord{Filename: filename, SourceDeviceID: strings.TrimSpace(sourceDeviceID)}
	targetLocalPath := BackupLocalPathForRecord(dataDir, game.ID, record)
	if err := os.MkdirAll(filepath.Dir(targetLocalPath), 0o755); err != nil {
		return nil, fmt.Errorf("create local backup directory: %w", err)
	}
	if err := zipDirectory(game.SavePath, targetLocalPath); err != nil {
		return nil, fmt.Errorf("create backup archive: %w", err)
	}

	info, err := os.Stat(targetLocalPath)
	if err != nil {
		return nil, err
	}

	// 旧的 backup_auto_*.zip 不在此处清理：上传成功前删除旧档会在上传失败时
	// 造成本地/云端都没有可用自动备份（M9），清理移到上传成功回调中执行。

	backup := &Backup{
		GameID:         game.ID,
		Type:           backupType,
		Name:           name,
		Filename:       filename,
		Size:           info.Size(),
		CreatedAt:      time.Now(),
		SourceDeviceID: strings.TrimSpace(sourceDeviceID),
		LocalExists:    true,
		Status:         BackupStatusReady,
	}
	backupHash, err := sha256File(targetLocalPath)
	if err != nil {
		return nil, fmt.Errorf("hash backup archive: %w", err)
	}
	backup.SHA256 = backupHash
	backupRecord := BackupRecord{Filename: filename, SourceDeviceID: backup.SourceDeviceID}
	backup.ID = BackupRecordID(backupRecord)
	backup.ObjectKey = BackupObjectKeyForRecord(game.ID, backupRecord)
	manifest, manifestErr := BuildLocalManifest(game.SavePath, game.Sync.IncludePatterns, game.Sync.ExcludePatterns)
	if manifestErr == nil {
		backup.SourceManifestHash = manifest.Hash
		backup.SourceManifestGeneratedAt = manifest.GeneratedAt
	}

	if gateway != nil && gateway.Objects != nil {
		remoteKey := backup.ObjectKey
		if err := gateway.Objects.PutObjectFromFile(ctx, remoteKey, targetLocalPath); err != nil {
			return nil, fmt.Errorf("upload backup to cloud: %w", err)
		}
		backup.CloudExists = true
		if backupType == "auto" {
			bm.CleanupCloudAutoBackups(ctx, gateway, game.ID, backup.SourceDeviceID, filename)
		}
	}

	return backup, nil
}

func (bm *BackupManager) CleanupCloudAutoBackups(ctx context.Context, gateway *StorageGateway, gameID string, sourceDeviceID string, keepFilename string) {
	if gateway == nil || gateway.Objects == nil {
		return
	}
	lister, ok := gateway.Objects.(ObjectLister)
	if !ok {
		return
	}
	prefix := fmt.Sprintf("backups/%s/", gameID)
	if strings.TrimSpace(sourceDeviceID) != "" {
		prefix = BackupObjectKey(gameID, sourceDeviceID, "")
	}
	objects, err := lister.ListObjects(ctx, prefix)
	if err != nil {
		return
	}
	for _, object := range objects {
		filename := filepath.Base(object.Key)
		if strings.HasPrefix(filename, "backup_auto_") && filename != keepFilename {
			_ = gateway.Objects.DeleteObject(ctx, object.Key)
		}
	}
}

func backupRecordForID(backupID string, registry []BackupRecord) (BackupRecord, bool) {
	backupID = strings.TrimSpace(backupID)
	if backupID == "" {
		return BackupRecord{}, false
	}
	for _, record := range registry {
		if BackupRecordID(record) == backupID {
			return record, true
		}
	}
	return BackupRecord{}, false
}

func applyBackupRecord(backup *Backup, record BackupRecord) {
	if backup == nil {
		return
	}
	if strings.TrimSpace(record.Type) != "" {
		backup.Type = record.Type
	}
	if strings.TrimSpace(record.Name) != "" {
		backup.Name = record.Name
	}
	if strings.TrimSpace(record.AccountID) != "" {
		backup.StorageAccountID = record.AccountID
	}
	backup.ObjectKey = strings.TrimSpace(record.ObjectKey)
	if !record.CreatedAt.IsZero() {
		backup.CreatedAt = record.CreatedAt
	}
	backup.SourceDeviceID = strings.TrimSpace(record.SourceDeviceID)
	backup.SHA256 = strings.TrimSpace(record.SHA256)
	backup.SourceManifestHash = strings.TrimSpace(record.SourceManifestHash)
	if !record.SourceManifestGeneratedAt.IsZero() {
		backup.SourceManifestGeneratedAt = record.SourceManifestGeneratedAt
	}
	backup.Status = strings.TrimSpace(record.Status)
	if backup.Status == "" {
		if record.PendingDelete {
			backup.Status = BackupStatusPendingDelete
		} else {
			backup.Status = BackupStatusReady
		}
	}
	backup.PendingDelete = record.PendingDelete
	backup.LastError = strings.TrimSpace(record.LastError)
	backup.LastDeleteError = strings.TrimSpace(record.LastDeleteError)
}

func normalizedBackupDisplayName(record BackupRecord) string {
	if strings.TrimSpace(record.Name) != "" {
		return strings.TrimSpace(record.Name)
	}
	backupType := strings.TrimSpace(record.Type)
	if backupType == "" {
		backupType = backupTypeFromFilename(record.Filename)
	}
	if strings.EqualFold(backupType, "auto") {
		return "自动游戏存档"
	}
	return "手动存档"
}

func normalizeBackupRecordStatus(record BackupRecord) BackupRecord {
	record.AccountID = strings.TrimSpace(record.AccountID)
	record.ObjectKey = strings.TrimSpace(record.ObjectKey)
	record.Type = strings.TrimSpace(record.Type)
	record.Name = strings.TrimSpace(record.Name)
	record.SHA256 = strings.TrimSpace(record.SHA256)
	record.SourceDeviceID = strings.TrimSpace(record.SourceDeviceID)
	record.SourceManifestHash = strings.TrimSpace(record.SourceManifestHash)
	record.Status = strings.TrimSpace(record.Status)
	record.LastError = strings.TrimSpace(record.LastError)
	record.LastDeleteError = strings.TrimSpace(record.LastDeleteError)
	if record.Type == "" {
		record.Type = backupTypeFromFilename(record.Filename)
	}
	if record.Status == "" {
		if record.PendingDelete {
			record.Status = BackupStatusPendingDelete
		} else {
			record.Status = BackupStatusReady
		}
	}
	record.PendingDelete = record.Status == BackupStatusPendingDelete
	if record.Status != BackupStatusDeleteFailed {
		record.LastDeleteError = ""
	}
	if record.Status != BackupStatusUploadFailed && record.Status != BackupStatusDeleteFailed {
		record.LastError = ""
	}
	return record
}

func (bm *BackupManager) listRemoteBackupFiles(ctx context.Context, gameID string, accountID string, gateway *StorageGateway) (map[string]Backup, error) {
	if gateway == nil || gateway.Objects == nil {
		return map[string]Backup{}, nil
	}
	lister, ok := gateway.Objects.(ObjectLister)
	if !ok {
		return map[string]Backup{}, nil
	}
	prefix := fmt.Sprintf("backups/%s/", gameID)
	objects, err := lister.ListObjects(ctx, prefix)
	if err != nil {
		return nil, err
	}
	backups := make(map[string]Backup)
	{
		for _, obj := range objects {
			filename := strings.TrimSpace(filepath.Base(obj.Key))
			backups[obj.Key] = Backup{
				ID:               NewID(),
				GameID:           gameID,
				Filename:         filename,
				Size:             obj.Size,
				StorageAccountID: accountID,
				ObjectKey:        obj.Key,
				CreatedAt:        obj.LastModified,
				CloudExists:      true,
				Status:           BackupStatusReady,
			}
		}
	}
	return backups, nil
}

func (bm *BackupManager) GetBackups(ctx context.Context, game Game, dataDir string, gateways map[string]*StorageGateway) (BackupListResult, error) {
	bm.EnsureLocalBackupDir(dataDir, game.ID)
	filenameCounts := make(map[string]int)
	for _, record := range game.BackupRegistry {
		if record.DeletedAt == nil {
			filenameCounts[strings.TrimSpace(record.Filename)]++
		}
	}

	failedAccounts := make([]string, 0)
	remoteBackups := make(map[string]map[string]Backup)
	for accountID, gateway := range gateways {
		accountBackups, err := bm.listRemoteBackupFiles(ctx, game.ID, accountID, gateway)
		if err != nil {
			failedAccounts = append(failedAccounts, accountID)
			continue
		}
		remoteBackups[accountID] = accountBackups
	}

	backups := make([]Backup, 0, len(game.BackupRegistry))
	for _, rawRecord := range game.BackupRegistry {
		record := normalizeBackupRecordStatus(rawRecord)
		if strings.TrimSpace(record.Filename) == "" || record.DeletedAt != nil {
			continue
		}
		backup := Backup{
			ID:                        BackupRecordID(record),
			GameID:                    game.ID,
			Type:                      record.Type,
			Name:                      normalizedBackupDisplayName(record),
			Filename:                  record.Filename,
			ObjectKey:                 BackupObjectKeyForRecord(game.ID, record),
			StorageAccountID:          record.AccountID,
			CreatedAt:                 record.CreatedAt,
			SourceDeviceID:            record.SourceDeviceID,
			SourceManifestHash:        record.SourceManifestHash,
			SourceManifestGeneratedAt: record.SourceManifestGeneratedAt,
			SHA256:                    record.SHA256,
			Status:                    record.Status,
			PendingDelete:             record.PendingDelete,
			LastError:                 record.LastError,
			LastDeleteError:           record.LastDeleteError,
		}
		localPath := existingBackupLocalPath(dataDir, game.ID, record, filenameCounts[record.Filename] == 1)
		if info, err := os.Stat(localPath); err == nil && !info.IsDir() {
			backup.LocalExists = true
			backup.Size = info.Size()
			if backup.CreatedAt.IsZero() {
				backup.CreatedAt = info.ModTime()
			}
		}
		if accountID := strings.TrimSpace(backup.StorageAccountID); accountID != "" {
			if accountBackups, ok := remoteBackups[accountID]; ok {
				if remoteBackup, ok := accountBackups[backup.ObjectKey]; ok {
					backup.CloudExists = true
					if backup.Size == 0 {
						backup.Size = remoteBackup.Size
					}
					if backup.CreatedAt.IsZero() {
						backup.CreatedAt = remoteBackup.CreatedAt
					}
				}
			}
		}
		backups = append(backups, backup)
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return BackupListResult{
		Backups:        backups,
		Partial:        len(failedAccounts) > 0,
		FailedAccounts: failedAccounts,
	}, nil
}

func (bm *BackupManager) RestoreBackup(ctx context.Context, game Game, backupID string, dataDir string, gateways map[string]*StorageGateway) error {
	record, ok := backupRecordForID(backupID, game.BackupRegistry)
	if !ok {
		return fmt.Errorf("backup %s not found", backupID)
	}
	filename := record.Filename
	filename, err := safeBackupFilename(filename)
	if err != nil {
		return err
	}
	matchingFilenames := 0
	for _, candidate := range game.BackupRegistry {
		if candidate.DeletedAt == nil && strings.TrimSpace(candidate.Filename) == filename {
			matchingFilenames++
		}
	}
	localPath := existingBackupLocalPath(dataDir, game.ID, record, matchingFilenames == 1)
	expectedSHA256 := strings.TrimSpace(record.SHA256)

	needsDownload := false
	localExists := false
	if _, err := os.Stat(localPath); errors.Is(err, os.ErrNotExist) {
		needsDownload = true
	} else if err != nil {
		return fmt.Errorf("stat local backup: %w", err)
	} else if expectedSHA256 != "" {
		localExists = true
		actualSHA256, hashErr := sha256File(localPath)
		needsDownload = hashErr != nil || !strings.EqualFold(actualSHA256, expectedSHA256)
	} else {
		localExists = true
	}

	if needsDownload {
		remoteKey := BackupObjectKeyForRecord(game.ID, record)
		if len(gateways) == 0 {
			if localExists {
				return verifyBackupArchive(localPath, expectedSHA256)
			}
			return fmt.Errorf("backup not found locally and no cloud storage is available")
		}

		targetPath := BackupLocalPathForRecord(dataDir, game.ID, record)
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("create local backup directory: %w", err)
		}
		tempFile, err := os.CreateTemp(filepath.Dir(targetPath), ".backup-download-*.zip")
		if err != nil {
			return fmt.Errorf("create backup download file: %w", err)
		}
		tempPath := tempFile.Name()
		_ = tempFile.Close()
		defer os.Remove(tempPath)

		var lastErr error
		if routedAccountID := strings.TrimSpace(record.AccountID); routedAccountID != "" {
			gateway := gateways[routedAccountID]
			if gateway == nil || gateway.Objects == nil {
				return fmt.Errorf("backup %s is routed to account %s, but that storage is not available", filename, routedAccountID)
			}
			lastErr = gateway.Objects.DownloadObjectToFile(ctx, remoteKey, tempPath)
		} else {
			for _, gateway := range gateways {
				if gateway == nil || gateway.Objects == nil {
					continue
				}
				lastErr = gateway.Objects.DownloadObjectToFile(ctx, remoteKey, tempPath)
				if lastErr == nil {
					break
				}
			}
		}
		if lastErr != nil {
			return fmt.Errorf("download backup from cloud: %w", lastErr)
		}
		if err := verifyBackupArchive(tempPath, expectedSHA256); err != nil {
			return err
		}
		if err := os.Rename(tempPath, targetPath); err != nil {
			return fmt.Errorf("cache downloaded backup: %w", err)
		}
		localPath = targetPath
	}

	if err := verifyBackupArchive(localPath, expectedSHA256); err != nil {
		return err
	}
	return restoreBackupArchiveAtomic(localPath, game.SavePath)
}

func safeBackupFilename(filename string) (string, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return "", errors.New("backup filename is empty")
	}
	if filename != filepath.Base(filename) || strings.Contains(filename, "/") || strings.Contains(filename, `\`) || !strings.HasSuffix(strings.ToLower(filename), ".zip") {
		return "", fmt.Errorf("unsafe backup filename: %s", filename)
	}
	return filename, nil
}

func verifyBackupArchive(archivePath string, expectedSHA256 string) error {
	if strings.TrimSpace(expectedSHA256) != "" {
		actualSHA256, err := sha256File(archivePath)
		if err != nil {
			return fmt.Errorf("hash backup archive: %w", err)
		}
		if !strings.EqualFold(actualSHA256, strings.TrimSpace(expectedSHA256)) {
			return fmt.Errorf("backup archive sha256 mismatch")
		}
	}

	zipFile, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open backup zip: %w", err)
	}
	defer zipFile.Close()

	for _, file := range zipFile.File {
		if _, err := safeSaveFilePath(os.TempDir(), file.Name); err != nil {
			return fmt.Errorf("unsafe backup entry %q: %w", file.Name, err)
		}
	}
	return nil
}

func restoreBackupArchiveAtomic(archivePath string, savePath string) error {
	savePath = strings.TrimSpace(savePath)
	if savePath == "" {
		return errors.New("save path is empty")
	}
	saveAbs, err := filepath.Abs(savePath)
	if err != nil {
		return err
	}
	parent := filepath.Dir(saveAbs)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create save parent dir: %w", err)
	}
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(saveAbs)+".restore-*")
	if err != nil {
		return fmt.Errorf("create restore staging dir: %w", err)
	}
	stagingActive := true
	defer func() {
		if stagingActive {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := extractBackupArchive(archivePath, staging); err != nil {
		return err
	}

	var rollback string
	currentExists := false
	if _, err := os.Stat(saveAbs); err == nil {
		currentExists = true
		rollback, err = os.MkdirTemp(parent, "."+filepath.Base(saveAbs)+".rollback-*")
		if err != nil {
			return fmt.Errorf("create restore rollback dir: %w", err)
		}
		_ = os.RemoveAll(rollback)
		if err := os.Rename(saveAbs, rollback); err != nil {
			return fmt.Errorf("prepare restore rollback: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat save path: %w", err)
	}

	if err := os.Rename(staging, saveAbs); err != nil {
		if currentExists {
			_ = os.RemoveAll(saveAbs)
			_ = os.Rename(rollback, saveAbs)
		}
		return fmt.Errorf("replace save directory: %w", err)
	}
	stagingActive = false
	if currentExists {
		_ = os.RemoveAll(rollback)
	}
	return nil
}

func extractBackupArchive(archivePath string, destinationRoot string) error {
	zipFile, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open backup zip: %w", err)
	}
	defer zipFile.Close()

	for _, file := range zipFile.File {
		targetPath, err := safeSaveFilePath(destinationRoot, file.Name)
		if err != nil {
			return fmt.Errorf("unsafe backup entry %q: %w", file.Name, err)
		}
		if file.FileInfo().IsDir() {
			_ = os.MkdirAll(targetPath, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.Create(targetPath)
		if err != nil {
			_ = src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		_ = src.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		// 还原 zip 内记录的修改时间：清单哈希包含 ModifiedAt，
		// 不还原会导致恢复后哈希守卫跨设备恒不匹配、每次启动重复恢复
		if !file.Modified.IsZero() {
			_ = os.Chtimes(targetPath, file.Modified, file.Modified)
		}
	}

	return nil
}

func (bm *BackupManager) DeleteBackup(ctx context.Context, game Game, backupID string, dataDir string, gateways map[string]*StorageGateway) error {
	record, ok := backupRecordForID(backupID, game.BackupRegistry)
	if !ok {
		return nil
	}
	filename := record.Filename
	matchingFilenames := 0
	for _, candidate := range game.BackupRegistry {
		if candidate.DeletedAt == nil && strings.TrimSpace(candidate.Filename) == filename {
			matchingFilenames++
		}
	}
	localPath := existingBackupLocalPath(dataDir, game.ID, record, matchingFilenames == 1)
	_ = os.Remove(localPath)

	remoteKey := BackupObjectKeyForRecord(game.ID, record)
	routedAccountID := strings.TrimSpace(record.AccountID)
	if routedAccountID != "" {
		gateway := gateways[routedAccountID]
		if gateway == nil || gateway.Objects == nil {
			return fmt.Errorf("backup %s is routed to account %s, but that storage is not available", filename, routedAccountID)
		}
		return gateway.Objects.DeleteObject(ctx, remoteKey)
	}

	var deleteErr error
	for _, gateway := range gateways {
		if gateway == nil || gateway.Objects == nil {
			continue
		}
		if err := gateway.Objects.DeleteObject(ctx, remoteKey); err != nil && deleteErr == nil {
			deleteErr = err
		}
	}
	return deleteErr
}
