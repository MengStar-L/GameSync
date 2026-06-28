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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type BackupManager struct {
	Engine *Engine
}

func NewBackupManager(engine *Engine) *BackupManager {
	return &BackupManager{Engine: engine}
}

func (bm *BackupManager) EnsureLocalBackupDir(dataDir string, gameID string) string {
	dir := filepath.Join(dataDir, "backups", gameID)
	_ = os.MkdirAll(dir, 0o755)
	return dir
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

func (bm *BackupManager) CreateBackup(ctx context.Context, game Game, backupType string, name string, dataDir string, gateway *CloudflareGateway) (*Backup, error) {
	if strings.TrimSpace(game.SavePath) == "" {
		return nil, fmt.Errorf("save path is empty")
	}

	backupDir := bm.EnsureLocalBackupDir(dataDir, game.ID)
	timestamp := time.Now().Format("20060102_150405_000000000")
	filename := fmt.Sprintf("backup_manual_%s.zip", timestamp)
	if strings.EqualFold(strings.TrimSpace(backupType), "auto") {
		filename = fmt.Sprintf("backup_auto_%s.zip", timestamp)
		backupType = "auto"
	} else {
		backupType = "manual"
	}

	targetLocalPath := filepath.Join(backupDir, filename)
	if err := zipDirectory(game.SavePath, targetLocalPath); err != nil {
		return nil, fmt.Errorf("create backup archive: %w", err)
	}

	info, err := os.Stat(targetLocalPath)
	if err != nil {
		return nil, err
	}

	if backupType == "auto" {
		files, _ := os.ReadDir(backupDir)
		for _, file := range files {
			if strings.HasPrefix(file.Name(), "backup_auto_") && file.Name() != filename {
				_ = os.Remove(filepath.Join(backupDir, file.Name()))
			}
		}
	}

	backup := &Backup{
		ID:          NewID(),
		GameID:      game.ID,
		Type:        backupType,
		Name:        name,
		Filename:    filename,
		Size:        info.Size(),
		CreatedAt:   time.Now(),
		LocalExists: true,
		Status:      BackupStatusReady,
	}
	if backupHash, err := sha256File(targetLocalPath); err == nil {
		backup.SHA256 = backupHash
	}
	manifest, manifestErr := BuildLocalManifest(game.SavePath, game.Sync.IncludePatterns, game.Sync.ExcludePatterns)
	if manifestErr == nil {
		backup.SourceManifestHash = manifest.Hash
		backup.SourceManifestGeneratedAt = manifest.GeneratedAt
	}

	if gateway != nil && gateway.R2 != nil {
		remoteKey := fmt.Sprintf("backups/%s/%s", game.ID, filename)
		if err := gateway.R2.PutObjectFromFile(ctx, remoteKey, targetLocalPath); err != nil {
			return nil, fmt.Errorf("upload backup to cloud: %w", err)
		}
		backup.CloudExists = true
		if backupType == "auto" {
			bm.CleanupCloudAutoBackups(ctx, gateway, game.ID, filename)
		}
	}

	return backup, nil
}

func (bm *BackupManager) CleanupCloudAutoBackups(ctx context.Context, gateway *CloudflareGateway, gameID string, keepFilename string) {
	if gateway == nil || gateway.R2 == nil {
		return
	}
	prefix := fmt.Sprintf("backups/%s/", gameID)
	paginator := s3.NewListObjectsV2Paginator(gateway.R2.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(gateway.R2.bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			filename := filepath.Base(key)
			if strings.HasPrefix(filename, "backup_auto_") && filename != keepFilename {
				_, _ = gateway.R2.client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(gateway.R2.bucket),
					Key:    aws.String(key),
				})
			}
		}
	}
}

func backupRecordForFilename(filename string, registry []BackupRecord) (BackupRecord, bool) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return BackupRecord{}, false
	}
	for _, record := range registry {
		if strings.TrimSpace(record.Filename) == filename {
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

func (bm *BackupManager) listLocalBackupFiles(gameID string, backupDir string) map[string]Backup {
	files, _ := os.ReadDir(backupDir)
	backups := make(map[string]Backup)
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".zip") {
			continue
		}
		info, err := file.Info()
		if err != nil {
			continue
		}
		filename := strings.TrimSpace(file.Name())
		backups[filename] = Backup{
			ID:          NewID(),
			GameID:      gameID,
			Filename:    filename,
			Size:        info.Size(),
			CreatedAt:   info.ModTime(),
			LocalExists: true,
			Status:      BackupStatusReady,
		}
		if backupHash, err := sha256File(filepath.Join(backupDir, filename)); err == nil {
			backup := backups[filename]
			backup.SHA256 = backupHash
			backups[filename] = backup
		}
	}
	return backups
}

func (bm *BackupManager) listRemoteBackupFiles(ctx context.Context, gameID string, accountID string, gateway *CloudflareGateway) (map[string]Backup, error) {
	if gateway == nil || gateway.R2 == nil {
		return map[string]Backup{}, nil
	}
	prefix := fmt.Sprintf("backups/%s/", gameID)
	paginator := s3.NewListObjectsV2Paginator(gateway.R2.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(gateway.R2.bucket),
		Prefix: aws.String(prefix),
	})
	backups := make(map[string]Backup)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			filename := strings.TrimSpace(filepath.Base(aws.ToString(obj.Key)))
			backups[filename] = Backup{
				ID:               NewID(),
				GameID:           gameID,
				Filename:         filename,
				Size:             aws.ToInt64(obj.Size),
				StorageAccountID: accountID,
				CreatedAt:        aws.ToTime(obj.LastModified),
				CloudExists:      true,
				Status:           BackupStatusReady,
			}
		}
	}
	return backups, nil
}

func backupRouteForFilename(game Game, filename string) string {
	if record, ok := backupRecordForFilename(filename, game.BackupRegistry); ok {
		return strings.TrimSpace(record.AccountID)
	}
	return ""
}

func (bm *BackupManager) GetBackups(ctx context.Context, game Game, dataDir string, gateways map[string]*CloudflareGateway) (BackupListResult, error) {
	backupDir := bm.EnsureLocalBackupDir(dataDir, game.ID)
	localBackups := bm.listLocalBackupFiles(game.ID, backupDir)

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
			ID:                        NewID(),
			GameID:                    game.ID,
			Type:                      record.Type,
			Name:                      normalizedBackupDisplayName(record),
			Filename:                  record.Filename,
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
		if localBackup, ok := localBackups[backup.Filename]; ok {
			backup.LocalExists = true
			backup.Size = localBackup.Size
			if backup.CreatedAt.IsZero() {
				backup.CreatedAt = localBackup.CreatedAt
			}
		}
		if accountID := strings.TrimSpace(backup.StorageAccountID); accountID != "" {
			if accountBackups, ok := remoteBackups[accountID]; ok {
				if remoteBackup, ok := accountBackups[backup.Filename]; ok {
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

func (bm *BackupManager) RestoreBackup(ctx context.Context, game Game, filename string, dataDir string, gateways map[string]*CloudflareGateway) error {
	backupDir := bm.EnsureLocalBackupDir(dataDir, game.ID)
	filename, err := safeBackupFilename(filename)
	if err != nil {
		return err
	}
	localPath := filepath.Join(backupDir, filename)
	record, _ := backupRecordForFilename(filename, game.BackupRegistry)
	expectedSHA256 := strings.TrimSpace(record.SHA256)

	if _, err := os.Stat(localPath); errors.Is(err, os.ErrNotExist) {
		remoteKey := fmt.Sprintf("backups/%s/%s", game.ID, filename)
		if len(gateways) == 0 {
			return fmt.Errorf("backup not found locally and no cloud storage is available")
		}

		var lastErr error
		if routedAccountID := backupRouteForFilename(game, filename); routedAccountID != "" {
			gateway := gateways[routedAccountID]
			if gateway == nil || gateway.R2 == nil {
				return fmt.Errorf("backup %s is routed to account %s, but that storage is not available", filename, routedAccountID)
			}
			lastErr = gateway.R2.DownloadObjectToFile(ctx, remoteKey, localPath)
		} else {
			for _, gateway := range gateways {
				if gateway == nil || gateway.R2 == nil {
					continue
				}
				lastErr = gateway.R2.DownloadObjectToFile(ctx, remoteKey, localPath)
				if lastErr == nil {
					break
				}
			}
		}
		if lastErr != nil {
			return fmt.Errorf("download backup from cloud: %w", lastErr)
		}
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
	}

	return nil
}

func (bm *BackupManager) DeleteBackup(ctx context.Context, game Game, filename string, dataDir string, gateways map[string]*CloudflareGateway) error {
	localPath := filepath.Join(dataDir, "backups", game.ID, filename)
	_ = os.Remove(localPath)

	remoteKey := fmt.Sprintf("backups/%s/%s", game.ID, filename)
	routedAccountID := backupRouteForFilename(game, filename)
	if routedAccountID != "" {
		gateway := gateways[routedAccountID]
		if gateway == nil || gateway.R2 == nil {
			return fmt.Errorf("backup %s is routed to account %s, but that storage is not available", filename, routedAccountID)
		}
		_, err := gateway.R2.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(gateway.R2.bucket),
			Key:    aws.String(remoteKey),
		})
		return err
	}

	var deleteErr error
	for _, gateway := range gateways {
		if gateway == nil || gateway.R2 == nil {
			continue
		}
		_, err := gateway.R2.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(gateway.R2.bucket),
			Key:    aws.String(remoteKey),
		})
		if err != nil && deleteErr == nil {
			deleteErr = err
		}
	}
	return deleteErr
}
