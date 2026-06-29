package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"gamesync/internal/core"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx               context.Context
	store             *core.Store
	engine            *core.Engine
	recoveryPassword  string
	baseDir           string
	savedWindowState  windowState
	windowStateLoaded bool
	tray              trayController
	windowIconMu      sync.Mutex
	windowIconCleanup func()
	catalogSyncMu     sync.Mutex
	catalogSyncTimer  *time.Timer
	catalogRetryTimer *time.Timer
	catalogSyncActive bool
	catalogSyncQueued bool
	lastSyncError     string
	lastSyncErrorAt   time.Time
	deleteQueueOnce   sync.Once
	deleteGameMu      sync.Mutex
	deleteGameQueue   chan queuedGameDelete
	deleteGamePending map[string]queuedGameDelete
	backupUploadOnce  sync.Once
	backupUploadMu    sync.Mutex
	backupUploadQueue chan queuedBackupUpload
	backupUploadSet   map[string]queuedBackupUpload
	backupDeleteOnce  sync.Once
	backupDeleteMu    sync.Mutex
	backupDeleteQueue chan queuedBackupDelete
	backupDeleteSet   map[string]queuedBackupDelete
	syncGameMu        sync.Mutex
	syncGameLocks     map[string]*sync.Mutex
}

type queuedGameDelete struct {
	GameID   string
	GameName string
}

type queuedBackupDelete struct {
	GameID   string
	Filename string
}

type queuedBackupUpload struct {
	GameID    string
	Filename  string
	AccountID string
}

func queuedBackupUploadKey(gameID string, filename string) string {
	return strings.TrimSpace(gameID) + "::" + strings.TrimSpace(filename)
}

func queuedBackupDeleteKey(gameID string, filename string) string {
	return strings.TrimSpace(gameID) + "::" + strings.TrimSpace(filename)
}

const (
	r2FreeTierStorageBytes    int64  = 10 * 1024 * 1024 * 1024
	backupRoutingReserveBytes int64  = 1 * 1024 * 1024 * 1024
	coverReferenceScheme      string = "r2cover"
	coverSourceRemoteURL             = "remote_url"
	coverSourceLocalFile             = "local_file"
)

const (
	msgRecoveryPasswordRequired         = "恢复密码不能为空"
	msgPrimaryAccountNotConfigured      = "尚未配置主 Cloudflare 账号"
	msgRecoveryPasswordDecryptFailed    = "当前恢复密码无法解密该账号凭据"
	msgGameIDRequired                   = "游戏 ID 不能为空"
	msgSyncPreparing                    = "正在准备同步存档，请稍候..."
	msgWailsRuntimeNotReady             = "Wails 运行环境尚未就绪"
	titlePickSaveFolder                 = "选择存档目录"
	titlePickLaunchFile                 = "选择启动文件"
	msgTargetPathRequired               = "目标路径不能为空"
	msgTargetPathNotFound               = "目标路径不存在: %w"
	msgCoverSourceReadFailed            = "读取封面来源失败: %s"
	msgLocalCoverReadFailed             = "读取本地封面失败: %w"
	msgCreateCoverDownloadRequestFailed = "创建封面下载请求失败: %w"
	msgDownloadCoverFailed              = "下载封面失败: %w"
	msgDownloadCoverStatusFailed        = "下载封面失败: HTTP %d"
	msgReadCoverDataFailed              = "读取封面数据失败: %w"
	msgDownloadCloudCoverFailed         = "从云端下载封面失败: %w"
	msgCreateCoverCacheDirFailed        = "创建封面缓存目录失败: %w"
	msgWriteCoverCacheFailed            = "写入封面缓存失败: %w"
	msgCoverLocalOnlyMissingCache       = "封面仅已保存到本地，因为缺少本地缓存文件"
	msgCoverLocalOnlyNoCloudAccount     = "封面已保存到本地，但尚未上传到云端，其他客户端暂时无法使用"
	msgCoverLocalOnlyPrimaryUnavailable = "封面已保存到本地，但主账号不可用，暂时无法上传云端"
	msgCoverLocalOnlyGatewayInitFailed  = "封面已保存到本地，但云端网关初始化失败: %v"
	msgCoverLocalOnlyUploadFailed       = "封面已保存到本地，但上传云端失败: %v"
	msgNoUsableCloudflareAccount        = "当前没有可用的 Cloudflare 账号"
	msgParseCoverReferenceFailed        = "解析封面引用失败: %w"
	msgUnsupportedCoverReference        = "不支持的封面引用: %s"
	msgInvalidCoverReference            = "封面引用无效: %s"
	msgResolveBaseDirFailed             = "获取程序目录失败: %w"
	msgTrayIconNotFound                 = "未找到托盘图标资源: resource/im1.png"
	msgCatalogSyncQueued                = "已加入后台同步队列"
	msgCatalogSyncQueuedNext            = "后台同步仍在进行，已排队等待下一轮"
	msgCatalogSyncRunning               = "正在同步云端目录"
	msgCatalogSyncRetrying              = "后台同步失败，30 秒后重试"
	msgCatalogSyncSucceeded             = "云端目录已同步"
	msgAutoBackupName                   = "自动游戏存档"
	msgAccountIDNotFound                = "未找到对应的 Cloudflare 账号 ID"
	msgAccountNotFound                  = "未找到对应的 Cloudflare 账号"
	msgGameNotFound                     = "未找到对应游戏"
	msgNoBackupGateways                 = "没有可用的备份存储网关（%s）"
	msgNoBackupAccounts                 = "没有可用的 Cloudflare 备份存储账号"
	msgFetchBackupUsageFailed           = "无法获取备份存储桶使用量（%s）"
	msgBackupStorageNearLimit           = "所有备份存储桶都接近 10GB 免费额度上限，至少还需要 %s 可用空间"
	msgLocalBackupRetained              = "%w；本地备份已保留在 %s"
	msgUploadBackupFailed               = "上传备份到云端失败: %w"
	msgExportAppBackupFailed            = "导出配置备份失败: %w"
	titleChooseBackupSavePath           = "选择备份保存位置"
	titleChooseBackupFile               = "选择备份文件"
	labelJSONBackupFile                 = "JSON 备份文件"
	msgWriteBackupFileFailed            = "写入备份文件失败: %w"
	msgReadBackupFileFailed             = "读取备份文件失败: %w"
)

func NewApp() *App {
	return &App{
		engine:            core.NewEngine(),
		deleteGameQueue:   make(chan queuedGameDelete, 16),
		deleteGamePending: make(map[string]queuedGameDelete),
		backupUploadQueue: make(chan queuedBackupUpload, 32),
		backupUploadSet:   make(map[string]queuedBackupUpload),
		backupDeleteQueue: make(chan queuedBackupDelete, 32),
		backupDeleteSet:   make(map[string]queuedBackupDelete),
		syncGameLocks:     make(map[string]*sync.Mutex),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startDeleteWorker()
	a.startBackupUploadWorker()
	a.startBackupDeleteWorker()
	if err := a.ensureReady(); err != nil {
		wailsruntime.LogErrorf(ctx, "startup failed: %v", err)
		return
	}
	if err := a.restoreWindowState(); err != nil {
		wailsruntime.LogErrorf(ctx, "restore window state failed: %v", err)
	}
	if err := a.startTray(); err != nil {
		wailsruntime.LogErrorf(ctx, "start tray failed: %v", err)
	}
	if a.store != nil && a.store.HasPendingCatalogSync() {
		a.queueRemoteCatalogSync("startup pending")
	}
	go a.verifyAccounts(false)
}

func (a *App) startDeleteWorker() {
	a.deleteQueueOnce.Do(func() {
		go a.runDeleteWorker()
	})
}

func (a *App) startBackupUploadWorker() {
	a.backupUploadOnce.Do(func() {
		go a.runBackupUploadWorker()
	})
}

func (a *App) startBackupDeleteWorker() {
	a.backupDeleteOnce.Do(func() {
		go a.runBackupDeleteWorker()
	})
}

func (a *App) domReady(ctx context.Context) {
	a.ctx = ctx
	a.applyWindowIcon(ctx)
	wailsruntime.WindowShow(ctx)
	go func() {
		time.Sleep(300 * time.Millisecond)
		a.applyWindowIcon(ctx)
	}()
}

func (a *App) shutdown(ctx context.Context) {
	a.ctx = ctx
	if err := a.saveCurrentWindowState(); err != nil {
		wailsruntime.LogErrorf(ctx, "save window state failed: %v", err)
	}
	if a.tray != nil {
		a.tray.Close()
		a.tray = nil
	}
	a.releaseWindowIcon()
	a.catalogSyncMu.Lock()
	if a.catalogSyncTimer != nil {
		a.catalogSyncTimer.Stop()
		a.catalogSyncTimer = nil
	}
	if a.catalogRetryTimer != nil {
		a.catalogRetryTimer.Stop()
		a.catalogRetryTimer = nil
	}
	a.catalogSyncMu.Unlock()
}

func (a *App) beforeClose(ctx context.Context) bool {
	a.ctx = ctx
	if err := a.saveCurrentWindowState(); err != nil {
		wailsruntime.LogErrorf(ctx, "pre-close save window state failed: %v", err)
	}
	return false
}

func (a *App) Bootstrap() (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}
	go a.pullRemoteCatalogInBackground("bootstrap")
	return a.snapshot()
}

func (a *App) GetAppInfo() (core.AppInfo, error) {
	if err := a.ensureReady(); err != nil {
		return core.AppInfo{}, err
	}
	return core.CurrentAppInfo(), nil
}

func (a *App) CheckForUpdates() (core.UpdateCheckResult, error) {
	if err := a.ensureReady(); err != nil {
		return core.UpdateCheckResult{}, err
	}
	updater := core.NewUpdater(core.UpdateOptions{
		DataDir: a.store.DataDir(),
	})
	return updater.Check(a.syncContext())
}

func (a *App) DownloadUpdate(request core.UpdateDownloadRequest) (core.UpdateDownloadResult, error) {
	if err := a.ensureReady(); err != nil {
		return core.UpdateDownloadResult{}, err
	}
	updater := core.NewUpdater(core.UpdateOptions{
		DataDir: a.store.DataDir(),
	})
	return updater.Download(a.syncContext(), request)
}

func (a *App) ApplyUpdateAndRestart(download core.UpdateDownloadResult) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	updater := core.NewUpdater(core.UpdateOptions{
		DataDir: a.store.DataDir(),
	})
	if err := updater.ApplyAndRestart(download, executablePath); err != nil {
		return err
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		wailsruntime.Quit(a.ctx)
	}()
	return nil
}

func (a *App) SaveAccount(account core.CloudflareAccount) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}
	if _, err := a.store.UpsertAccount(account); err != nil {
		return core.DashboardSnapshot{}, err
	}
	a.queueRemoteCatalogSync("account save")
	return a.snapshot()
}

func (a *App) SetRecoveryPassword(password string) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return core.DashboardSnapshot{}, errors.New(msgRecoveryPasswordRequired)
	}
	a.recoveryPassword = password
	if err := a.store.SetRecoveryStatus(func(status *core.RecoveryStatus) {
		status.HasRecoveryPassword = true
		status.LastRecoveryError = ""
	}); err != nil {
		return core.DashboardSnapshot{}, err
	}
	a.queueRemoteCatalogSync("recovery password update")
	return a.snapshot()
}

func (a *App) RestoreFromPrimary(password string) (core.DashboardSnapshot, error) {
	return a.restoreFromPrimary(password, true)
}

func (a *App) restoreFromPrimary(password string, verifyAfter bool) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}
	password = strings.TrimSpace(password)

	primary, ok := a.store.PrimaryAccount()
	if !ok {
		return core.DashboardSnapshot{}, errors.New(msgPrimaryAccountNotConfigured)
	}
	d1 := core.NewD1Client(primary)
	catalog, encrypted, err := d1.LoadRemoteCatalog(a.syncContext())
	if err != nil {
		_ = a.store.SetRecoveryStatus(func(status *core.RecoveryStatus) {
			status.LastRecoveryError = err.Error()
		})
		return core.DashboardSnapshot{}, err
	}

	for index := range catalog.Accounts {
		account := catalog.Accounts[index]
		if blob, ok := encrypted[account.ID]; ok && !account.IsPrimary && password != "" && (account.R2AccessKeyID == "" || account.R2SecretAccessKey == "") {
			decrypted, err := core.DecryptAccountCredentials(account, blob, password)
			if err != nil {
				account.VerificationState = "invalid"
				account.LastError = msgRecoveryPasswordDecryptFailed
			} else {
				account = decrypted
				account.CredentialsBackedUp = true
				account.VerificationState = "pending"
			}
		}
		if account.IsPrimary && account.ID == primary.ID {
			account.APIToken = primary.APIToken
			account.R2AccessKeyID = primary.R2AccessKeyID
			account.R2SecretAccessKey = primary.R2SecretAccessKey
		}
		catalog.Accounts[index] = account
	}

	if err := a.store.MergeRemoteCatalog(catalog); err != nil {
		return core.DashboardSnapshot{}, err
	}
	_ = a.store.SetRecoveryStatus(func(status *core.RecoveryStatus) {
		status.PendingCredentialBackup = false
		status.LastRecoveryError = ""
	})
	if verifyAfter {
		go a.verifyAccounts(false)
	}
	return a.snapshot()
}

func (a *App) VerifyAccount(accountID string) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}

	account, err := findAccount(a.store.Snapshot(), accountID)
	if err != nil {
		return core.DashboardSnapshot{}, err
	}

	verifiedAccount, _ := core.VerifyCloudflareAccount(a.syncContext(), account)
	if verifiedAccount.LastError == "" {
		verifiedAccount.VerificationState = "valid"
	} else {
		verifiedAccount.VerificationState = "invalid"
	}
	if _, err := a.store.UpsertAccount(verifiedAccount); err != nil {
		return core.DashboardSnapshot{}, err
	}
	a.queueRemoteCatalogSync("account verification")
	if verifiedAccount.IsPrimary && verifiedAccount.LastError == "" {
		snapshot, err := a.restoreFromPrimary("", false)
		if err != nil {
			wailsruntime.LogErrorf(a.ctx, "restore secondary accounts after primary verify failed: %v", err)
			return core.DashboardSnapshot{}, err
		}
		go a.verifyAccounts(false)
		return snapshot, nil
	}
	return a.snapshot()
}

func (a *App) DeleteAccount(accountID string) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}
	if err := a.store.DeleteAccount(accountID); err != nil {
		return core.DashboardSnapshot{}, err
	}
	a.queueRemoteCatalogSync("account delete")
	return a.snapshot()
}

func (a *App) SaveGame(game core.Game) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}
	if strings.TrimSpace(game.ID) == "" {
		game.ID = core.NewID()
	}
	state := a.store.Snapshot()
	var existing *core.Game
	for index := range state.Games {
		if state.Games[index].ID == game.ID {
			copy := state.Games[index]
			existing = &copy
			break
		}
	}
	if existing != nil {
		game = mergeEditableGameInput(*existing, game)
	}
	coverWarning, err := a.prepareAndPersistCover(&game, existing, state)
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	if _, err := a.store.UpsertGame(game); err != nil {
		return core.DashboardSnapshot{}, err
	}
	if coverWarning != "" {
		a.emitRuntimeEvent("cover:warning", map[string]string{
			"gameId":  game.ID,
			"message": coverWarning,
		})
	}
	a.queueRemoteCatalogSync("game save")
	return a.snapshot()
}

func mergeEditableGameInput(current core.Game, incoming core.Game) core.Game {
	merged := current
	merged.ID = current.ID
	merged.Name = incoming.Name
	merged.InstallPath = incoming.InstallPath
	merged.SavePath = incoming.SavePath
	merged.CoverPath = incoming.CoverPath
	merged.CoverSourceType = incoming.CoverSourceType
	merged.CoverSource = incoming.CoverSource
	merged.CoverLocalPath = incoming.CoverLocalPath
	merged.CoverCloudAccountID = incoming.CoverCloudAccountID
	merged.CoverCloudKey = incoming.CoverCloudKey
	merged.CoverMimeType = incoming.CoverMimeType
	merged.CoverUpdatedAt = incoming.CoverUpdatedAt
	merged.Description = incoming.Description
	merged.Released = incoming.Released
	merged.Rating = incoming.Rating
	merged.RatingTop = incoming.RatingTop
	merged.Metacritic = incoming.Metacritic
	merged.Genres = incoming.Genres
	merged.Platforms = incoming.Platforms
	merged.IsSteam = incoming.IsSteam
	merged.Developers = incoming.Developers
	merged.Publishers = incoming.Publishers
	merged.Website = incoming.Website
	merged.RawgID = incoming.RawgID
	merged.RawgSlug = incoming.RawgSlug
	merged.RawgURL = incoming.RawgURL
	merged.RawgTags = incoming.RawgTags
	merged.Tags = incoming.Tags
	if strings.TrimSpace(incoming.StorageAccountID) != "" {
		merged.StorageAccountID = incoming.StorageAccountID
	}
	merged.Sync = incoming.Sync
	return merged
}

func (a *App) ResolveCoverSource(identifier string) (string, error) {
	if err := a.ensureReady(); err != nil {
		return "", err
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return "", nil
	}
	state := a.store.Snapshot()
	for _, game := range state.Games {
		if game.ID == identifier {
			source, err := a.resolveGameCoverSource(game)
			if err != nil && a.ctx != nil {
				wailsruntime.LogErrorf(a.ctx, "resolve cover source failed for %s: %v", game.ID, err)
			}
			return source, err
		}
	}
	if isDirectCoverSource(identifier) {
		return identifier, nil
	}
	if isCoverReference(identifier) {
		data, err := a.loadCoverReferenceBytes(identifier)
		if err != nil {
			return "", err
		}
		return dataURLForBytes(identifier, data), nil
	}
	localPath := normalizeLocalCoverPath(identifier)
	data, err := os.ReadFile(localPath)
	if err != nil {
		return "", err
	}
	return dataURLForBytes(localPath, data), nil
}

func (a *App) prepareAndPersistCover(game *core.Game, existing *core.Game, state core.AppState) (string, error) {
	if game == nil {
		return "", errors.New("game is nil")
	}

	coverPath := strings.TrimSpace(game.CoverPath)
	if coverPath == "" {
		game.CoverSourceType = ""
		game.CoverSource = ""
		game.CoverLocalPath = ""
		game.CoverCloudAccountID = ""
		game.CoverCloudKey = ""
		game.CoverMimeType = ""
		game.CoverUpdatedAt = time.Time{}
		return "", nil
	}

	if existing != nil && strings.TrimSpace(existing.CoverPath) == coverPath {
		if strings.TrimSpace(game.CoverSourceType) == "" {
			game.CoverSourceType = existing.CoverSourceType
		}
		if strings.TrimSpace(game.CoverSource) == "" {
			game.CoverSource = existing.CoverSource
		}
		if strings.TrimSpace(game.CoverLocalPath) == "" {
			game.CoverLocalPath = existing.CoverLocalPath
		}
		if strings.TrimSpace(game.CoverCloudAccountID) == "" {
			game.CoverCloudAccountID = existing.CoverCloudAccountID
		}
		if strings.TrimSpace(game.CoverCloudKey) == "" {
			game.CoverCloudKey = existing.CoverCloudKey
		}
		if strings.TrimSpace(game.CoverMimeType) == "" {
			game.CoverMimeType = existing.CoverMimeType
		}
	}

	sourceType, source := inferCoverSource(game.CoverPath, game.CoverSourceType, game.CoverSource, existing)
	game.CoverPath = source
	game.CoverSourceType = sourceType
	game.CoverSource = source

	localPath, mimeType, err := a.ensureCoverCached(*game, existing)
	if err != nil {
		return "", err
	}
	game.CoverLocalPath = localPath
	if mimeType != "" {
		game.CoverMimeType = mimeType
	}
	game.CoverUpdatedAt = time.Now()

	if existing != nil &&
		strings.TrimSpace(existing.CoverSourceType) == strings.TrimSpace(game.CoverSourceType) &&
		strings.TrimSpace(existing.CoverSource) == strings.TrimSpace(game.CoverSource) &&
		strings.TrimSpace(existing.CoverCloudAccountID) != "" &&
		strings.TrimSpace(existing.CoverCloudKey) != "" {
		game.CoverCloudAccountID = existing.CoverCloudAccountID
		game.CoverCloudKey = existing.CoverCloudKey
		return "", nil
	}

	accountID, objectKey, warning := a.tryUploadCoverToCloud(state, *game, localPath)
	if accountID != "" && objectKey != "" {
		game.CoverCloudAccountID = accountID
		game.CoverCloudKey = objectKey
		return warning, nil
	}

	if existing != nil &&
		strings.TrimSpace(existing.CoverSourceType) == strings.TrimSpace(game.CoverSourceType) &&
		strings.TrimSpace(existing.CoverSource) == strings.TrimSpace(game.CoverSource) &&
		strings.TrimSpace(existing.CoverCloudAccountID) != "" &&
		strings.TrimSpace(existing.CoverCloudKey) != "" {
		game.CoverCloudAccountID = existing.CoverCloudAccountID
		game.CoverCloudKey = existing.CoverCloudKey
	}

	return warning, nil
}

func (a *App) resolveGameCoverSource(game core.Game) (string, error) {
	if strings.TrimSpace(game.CoverPath) == "" &&
		strings.TrimSpace(game.CoverLocalPath) == "" &&
		strings.TrimSpace(game.CoverCloudKey) == "" {
		return "", nil
	}

	var lastErr error
	if cachedPath := a.locateCoverCache(game); cachedPath != "" {
		if isCoverCacheFreshForGame(game, cachedPath) {
			a.persistResolvedCoverCache(game.ID, cachedPath, game.CoverMimeType)
			data, err := os.ReadFile(cachedPath)
			if err == nil {
				return dataURLForBytes(cachedPath, data), nil
			}
		} else {
			lastErr = errors.New("cover cache is stale")
		}
	}

	if strings.EqualFold(strings.TrimSpace(game.CoverSourceType), coverSourceRemoteURL) {
		localPath, mimeType, err := a.downloadRemoteCoverToCache(game.ID, firstNonEmpty(game.CoverSource, game.CoverPath))
		if err == nil {
			a.persistResolvedCoverCache(game.ID, localPath, mimeType)
			if mimeType != "" {
				game.CoverMimeType = mimeType
			}
			data, readErr := os.ReadFile(localPath)
			if readErr == nil {
				return dataURLForBytes(localPath, data), nil
			}
			lastErr = readErr
		} else {
			lastErr = err
		}
	}

	if localSource := normalizeLocalCoverPath(firstNonEmpty(game.CoverSource, game.CoverPath)); localSource != "" && !isDirectCoverSource(localSource) && !isCoverReference(localSource) {
		if localPath, mimeType, err := a.copyLocalCoverToCache(game.ID, localSource); err == nil {
			a.persistResolvedCoverCache(game.ID, localPath, mimeType)
			data, readErr := os.ReadFile(localPath)
			if readErr == nil {
				return dataURLForBytes(localPath, data), nil
			}
			lastErr = readErr
		} else if data, err := os.ReadFile(localSource); err == nil {
			return dataURLForBytes(localSource, data), nil
		} else if lastErr == nil {
			lastErr = err
		}
	}

	accountID, objectKey := coverCloudLocation(game)
	if accountID != "" && objectKey != "" {
		localPath, mimeType, err := a.downloadCloudCoverToCache(game.ID, accountID, objectKey)
		if err == nil {
			a.persistResolvedCoverCache(game.ID, localPath, mimeType)
			data, readErr := os.ReadFile(localPath)
			if readErr == nil {
				return dataURLForBytes(localPath, data), nil
			}
			lastErr = readErr
		} else if lastErr == nil {
			lastErr = err
		}
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", nil
}

func isCoverCacheFreshForGame(game core.Game, cachedPath string) bool {
	if strings.TrimSpace(cachedPath) == "" {
		return false
	}
	if strings.TrimSpace(game.CoverSourceType) != coverSourceRemoteURL {
		return true
	}
	if game.CoverUpdatedAt.IsZero() {
		return true
	}
	info, err := os.Stat(cachedPath)
	if err != nil {
		return false
	}
	return !info.ModTime().Before(game.CoverUpdatedAt.Add(-5 * time.Second))
}

func (a *App) DeleteGame(gameID string) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}

	state := a.store.Snapshot()
	if err := a.store.DeleteGame(gameID); err != nil {
		return core.DashboardSnapshot{}, err
	}
	a.queueRemoteCatalogSync("game delete")
	if err := a.cleanupDeletedGameRemote(state, gameID); err != nil {
		wailsruntime.LogErrorf(a.ctx, "cleanup deleted game %s failed: %v", gameID, err)
	}
	return a.snapshot()
}

func (a *App) RequestDeleteGame(gameID string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}

	a.startDeleteWorker()
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return errors.New(msgGameIDRequired)
	}

	a.deleteGameMu.Lock()
	if _, exists := a.deleteGamePending[gameID]; exists {
		request := a.deleteGamePending[gameID]
		a.deleteGameMu.Unlock()
		a.emitRuntimeEvent("game:delete_queued", map[string]string{
			"id":   request.GameID,
			"name": request.GameName,
		})
		return nil
	}
	a.deleteGameMu.Unlock()

	game, err := findGame(a.store.Snapshot(), gameID)
	if err != nil {
		return err
	}

	request := queuedGameDelete{
		GameID:   gameID,
		GameName: game.Name,
	}

	a.deleteGameMu.Lock()
	if _, exists := a.deleteGamePending[gameID]; !exists {
		a.deleteGamePending[gameID] = request
	}
	a.deleteGameMu.Unlock()

	a.deleteGameQueue <- request
	a.emitRuntimeEvent("game:delete_queued", map[string]string{
		"id":   request.GameID,
		"name": request.GameName,
	})
	return nil
}

func (a *App) runDeleteWorker() {
	for request := range a.deleteGameQueue {
		a.processQueuedGameDelete(request)
	}
}

func (a *App) processQueuedGameDelete(request queuedGameDelete) {
	if err := a.ensureReady(); err != nil {
		a.finishQueuedGameDelete(request.GameID)
		a.emitRuntimeEvent("game:delete_failed", map[string]string{
			"id":    request.GameID,
			"error": err.Error(),
			"stage": "local_delete",
		})
		return
	}

	stateBeforeDelete := a.store.Snapshot()
	if err := a.store.DeleteGame(request.GameID); err != nil {
		a.finishQueuedGameDelete(request.GameID)
		a.emitRuntimeEvent("game:delete_failed", map[string]string{
			"id":    request.GameID,
			"error": err.Error(),
			"stage": "local_delete",
		})
		return
	}

	a.queueRemoteCatalogSync("game delete")
	a.emitRuntimeEvent("game:delete_succeeded", map[string]string{
		"id": request.GameID,
	})

	if err := a.cleanupDeletedGameRemote(stateBeforeDelete, request.GameID); err != nil {
		wailsruntime.LogErrorf(a.ctx, "cleanup deleted game %s failed: %v", request.GameID, err)
		a.emitRuntimeEvent("game:delete_failed", map[string]string{
			"id":    request.GameID,
			"error": err.Error(),
			"stage": "remote_cleanup",
		})
	}

	a.finishQueuedGameDelete(request.GameID)
}

func (a *App) finishQueuedGameDelete(gameID string) {
	a.deleteGameMu.Lock()
	delete(a.deleteGamePending, gameID)
	a.deleteGameMu.Unlock()
}

func (a *App) enqueueBackupUpload(request queuedBackupUpload) {
	request.GameID = strings.TrimSpace(request.GameID)
	request.Filename = strings.TrimSpace(request.Filename)
	request.AccountID = strings.TrimSpace(request.AccountID)
	if request.GameID == "" || request.Filename == "" || request.AccountID == "" {
		return
	}
	key := queuedBackupUploadKey(request.GameID, request.Filename)
	a.backupUploadMu.Lock()
	if _, exists := a.backupUploadSet[key]; exists {
		a.backupUploadMu.Unlock()
		return
	}
	a.backupUploadSet[key] = request
	a.backupUploadMu.Unlock()
	a.backupUploadQueue <- request
}

func (a *App) finishQueuedBackupUpload(gameID string, filename string) {
	a.backupUploadMu.Lock()
	delete(a.backupUploadSet, queuedBackupUploadKey(gameID, filename))
	a.backupUploadMu.Unlock()
}

func (a *App) runBackupUploadWorker() {
	for request := range a.backupUploadQueue {
		a.processQueuedBackupUpload(request)
	}
}

func (a *App) processQueuedBackupUpload(request queuedBackupUpload) {
	defer a.finishQueuedBackupUpload(request.GameID, request.Filename)

	if err := a.ensureReady(); err != nil {
		return
	}
	game, err := findGame(a.store.Snapshot(), request.GameID)
	if err != nil {
		return
	}
	record, _, ok := findBackupRecord(game, request.Filename)
	if !ok || record.DeletedAt != nil {
		return
	}
	record = normalizeBackupRecord(record)
	if record.Status != core.BackupStatusPendingUpload {
		return
	}

	backupPath := filepath.Join(a.store.DataDir(), "backups", game.ID, request.Filename)
	info, statErr := os.Stat(backupPath)
	if statErr != nil {
		a.markBackupUploadFailed(game, request.Filename, statErr.Error())
		return
	}

	account, err := findAccount(a.store.Snapshot(), request.AccountID)
	if err != nil {
		a.markBackupUploadFailed(game, request.Filename, err.Error())
		return
	}
	state := a.store.Snapshot()
	primaryAccount, err := findPrimaryAccount(state)
	if err != nil {
		a.markBackupUploadFailed(game, request.Filename, err.Error())
		return
	}
	gateway, err := core.NewSplitCloudflareGateway(a.syncContext(), primaryAccount, account)
	if err != nil {
		a.markBackupUploadFailed(game, request.Filename, err.Error())
		return
	}

	remoteKey := fmt.Sprintf("backups/%s/%s", game.ID, request.Filename)
	if err := gateway.R2.PutObjectFromFile(a.syncContext(), remoteKey, backupPath); err != nil {
		a.markBackupUploadFailed(game, request.Filename, err.Error())
		return
	}
	if record.Type == "auto" {
		core.NewBackupManager(a.engine).CleanupCloudAutoBackups(a.syncContext(), gateway, game.ID, request.Filename)
	}

	record.AccountID = request.AccountID
	if record.CreatedAt.IsZero() {
		record.CreatedAt = info.ModTime()
	}
	record.Status = core.BackupStatusReady
	record.PendingDelete = false
	record.LastError = ""
	record.LastDeleteError = ""
	record.DeleteRetryAt = nil
	upsertBackupRecord(&game, record)
	if record.Type != "auto" {
		game.BackupStorageAccountID = request.AccountID
	}
	if _, err := a.store.UpsertGame(game); err != nil {
		wailsruntime.LogErrorf(a.ctx, "persist backup upload success %s/%s failed: %v", request.GameID, request.Filename, err)
		return
	}
	a.queueRemoteCatalogSync("backup upload success")
	a.emitStateUpdated()
	a.emitRuntimeEvent("game:backup_upload_succeeded", map[string]string{
		"id":       request.GameID,
		"filename": request.Filename,
	})
	if record.Type == "auto" {
		a.emitRuntimeEvent("game:backup_success", request.GameID)
	}
}

func (a *App) enqueueBackupDelete(request queuedBackupDelete) {
	request.GameID = strings.TrimSpace(request.GameID)
	request.Filename = strings.TrimSpace(request.Filename)
	if request.GameID == "" || request.Filename == "" {
		return
	}
	key := queuedBackupDeleteKey(request.GameID, request.Filename)
	a.backupDeleteMu.Lock()
	if _, exists := a.backupDeleteSet[key]; exists {
		a.backupDeleteMu.Unlock()
		return
	}
	a.backupDeleteSet[key] = request
	a.backupDeleteMu.Unlock()
	a.backupDeleteQueue <- request
}

func (a *App) finishQueuedBackupDelete(gameID string, filename string) {
	a.backupDeleteMu.Lock()
	delete(a.backupDeleteSet, queuedBackupDeleteKey(gameID, filename))
	a.backupDeleteMu.Unlock()
}

func (a *App) runBackupDeleteWorker() {
	for request := range a.backupDeleteQueue {
		a.processQueuedBackupDelete(request)
	}
}

func (a *App) processQueuedBackupDelete(request queuedBackupDelete) {
	defer a.finishQueuedBackupDelete(request.GameID, request.Filename)

	if err := a.ensureReady(); err != nil {
		return
	}
	game, err := findGame(a.store.Snapshot(), request.GameID)
	if err != nil {
		return
	}
	record, _, ok := findBackupRecord(game, request.Filename)
	if !ok || record.DeletedAt != nil {
		return
	}
	record = normalizeBackupRecord(record)
	if record.Status != core.BackupStatusPendingDelete {
		return
	}

	gateways, gatewayErr := a.getBackupGateways(a.ctx, game)
	if gatewayErr != nil {
		a.markBackupDeleteFailed(game, request.Filename, gatewayErr.Error())
		return
	}
	bm := core.NewBackupManager(a.engine)
	if err := bm.DeleteBackup(a.ctx, game, request.Filename, a.store.DataDir(), gateways); err != nil {
		a.markBackupDeleteFailed(game, request.Filename, err.Error())
		return
	}
	if err := a.finalizeBackupDeletion(game, request.Filename); err != nil {
		wailsruntime.LogErrorf(a.ctx, "finalize backup deletion %s/%s failed: %v", request.GameID, request.Filename, err)
		return
	}
	a.emitRuntimeEvent("game:backup_delete_succeeded", map[string]string{
		"id":       request.GameID,
		"filename": request.Filename,
	})
}

func (a *App) markBackupUploadFailed(game core.Game, filename string, uploadErr string) {
	record, _, ok := findBackupRecord(game, filename)
	if !ok {
		return
	}
	record = normalizeBackupRecord(record)
	record.Status = core.BackupStatusUploadFailed
	record.PendingDelete = false
	record.LastError = strings.TrimSpace(uploadErr)
	record.LastDeleteError = ""
	record.DeleteRetryAt = nil
	upsertBackupRecord(&game, record)
	if _, err := a.store.UpsertGame(game); err == nil {
		a.queueRemoteCatalogSync("backup upload failed")
		a.emitStateUpdated()
		a.emitRuntimeEvent("game:backup_upload_failed", map[string]string{
			"id":       game.ID,
			"filename": filename,
			"error":    record.LastError,
		})
		if record.Type == "auto" {
			a.emitRuntimeEvent("game:backup_error", map[string]string{"id": game.ID, "error": record.LastError})
		}
	}
}

func (a *App) markBackupDeleteFailed(game core.Game, filename string, deleteErr string) {
	record, _, ok := findBackupRecord(game, filename)
	if !ok {
		return
	}
	now := time.Now()
	record = normalizeBackupRecord(record)
	record.Status = core.BackupStatusDeleteFailed
	record.PendingDelete = false
	record.LastError = strings.TrimSpace(deleteErr)
	record.LastDeleteError = record.LastError
	record.DeleteRetryAt = &now
	upsertBackupRecord(&game, record)
	if _, err := a.store.UpsertGame(game); err == nil {
		a.queueRemoteCatalogSync("backup delete failed")
		a.emitStateUpdated()
		a.emitRuntimeEvent("game:backup_delete_failed", map[string]string{
			"id":       game.ID,
			"filename": filename,
			"error":    record.LastError,
		})
	}
}

func (a *App) finalizeBackupDeletion(game core.Game, filename string) error {
	removeBackupRecord(&game, filename)
	if _, err := a.store.UpsertGame(game); err != nil {
		return err
	}
	a.queueRemoteCatalogSync("backup route delete")
	a.emitStateUpdated()
	return nil
}

func (a *App) cleanupDeletedGameRemote(state core.AppState, gameID string) error {
	var errs []string

	if game, storageAccount, err := findGameAndAccount(state, gameID); err == nil {
		if clearErr := a.cleanupDeletedGameCover(state, game); clearErr != nil {
			errs = append(errs, clearErr.Error())
		}
		if primary, primaryErr := findPrimaryAccount(state); primaryErr == nil {
			if d1 := core.NewD1Client(primary); d1 != nil {
				if clearErr := d1.ClearGameRecords(a.syncContext(), gameID); clearErr != nil {
					errs = append(errs, clearErr.Error())
				}
			}
		}
		if r2, err := core.NewR2Client(a.syncContext(), storageAccount); err == nil {
			if clearErr := r2.ClearGameFiles(a.syncContext(), game.ID); clearErr != nil {
				errs = append(errs, clearErr.Error())
			}
		}
		if gateways, gatewayErr := a.getBackupGateways(a.syncContext(), game); gatewayErr == nil {
			for _, gateway := range gateways {
				if gateway == nil || gateway.R2 == nil {
					continue
				}
				if clearErr := gateway.R2.ClearPrefix(a.syncContext(), fmt.Sprintf("backups/%s/", game.ID)); clearErr != nil {
					errs = append(errs, clearErr.Error())
				}
			}
		}
	} else if primary, primaryErr := findPrimaryAccount(state); primaryErr == nil {
		if d1 := core.NewD1Client(primary); d1 != nil {
			if clearErr := d1.ClearGameRecords(a.syncContext(), gameID); clearErr != nil {
				errs = append(errs, clearErr.Error())
			}
		}
	}

	if clearErr := a.cleanupDeletedGameLocalArtifacts(gameID); clearErr != nil {
		errs = append(errs, clearErr.Error())
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (a *App) cleanupDeletedGameCover(state core.AppState, game core.Game) error {
	coverAccountID, _ := coverCloudLocation(game)
	if coverAccountID == "" {
		return nil
	}

	primaryAccount, err := findPrimaryAccount(state)
	if err != nil {
		return err
	}
	coverAccount, err := findAccount(state, coverAccountID)
	if err != nil {
		return err
	}
	gateway, err := core.NewSplitCloudflareGateway(a.syncContext(), primaryAccount, coverAccount)
	if err != nil {
		return err
	}
	return gateway.R2.ClearPrefix(a.syncContext(), fmt.Sprintf("covers/%s/", strings.TrimSpace(game.ID)))
}

func (a *App) cleanupDeletedGameLocalArtifacts(gameID string) error {
	if a.store == nil {
		return nil
	}
	var errs []string
	for _, target := range []string{
		filepath.Join(a.store.DataDir(), "covers", strings.TrimSpace(gameID)),
		filepath.Join(a.store.DataDir(), "backups", strings.TrimSpace(gameID)),
	} {
		if err := os.RemoveAll(target); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (a *App) ReorderGames(gameIDs []string) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}
	if err := a.store.ReorderGames(gameIDs); err != nil {
		return core.DashboardSnapshot{}, err
	}
	a.queueRemoteCatalogSync("game reorder")
	return a.snapshot()
}

func (a *App) UpdateTagOrder(tags []string) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}

	prefs := a.store.Snapshot().Preferences
	prefs.TagOrder = tags
	if err := a.store.SavePreferences(prefs); err != nil {
		return core.DashboardSnapshot{}, err
	}
	a.queueRemoteCatalogSync("tag order update")

	return a.snapshot()
}

func (a *App) SavePreferences(preferences core.Preferences) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}
	if err := a.store.SavePreferences(preferences); err != nil {
		return core.DashboardSnapshot{}, err
	}
	a.queueRemoteCatalogSync("preferences save")
	return a.snapshot()
}

func (a *App) SearchRAWGGames(query string) ([]core.RAWGSearchResult, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}

	client, err := core.NewRAWGClient(a.store.Snapshot().Preferences.RawgAPIKey)
	if err != nil {
		return nil, err
	}
	return client.SearchGames(a.syncContext(), query)
}

func (a *App) GetRAWGGame(rawgID int) (core.RAWGGameDetails, error) {
	if err := a.ensureReady(); err != nil {
		return core.RAWGGameDetails{}, err
	}

	client, err := core.NewRAWGClient(a.store.Snapshot().Preferences.RawgAPIKey)
	if err != nil {
		return core.RAWGGameDetails{}, err
	}
	return client.GetGameDetails(a.syncContext(), rawgID)
}

func (a *App) SearchSteamGridDBGames(query string) ([]core.SteamGridDBSearchResult, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}

	client, err := core.NewSteamGridDBClient(a.store.Snapshot().Preferences.SteamGridDBAPIKey)
	if err != nil {
		return nil, err
	}
	return client.SearchGames(a.syncContext(), query)
}

func (a *App) RunSync(request core.SyncRunRequest) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}

	request.GameID = strings.TrimSpace(request.GameID)
	if request.GameID == "" {
		return core.DashboardSnapshot{}, errors.New(msgGameIDRequired)
	}
	unlock := a.lockGameSync(request.GameID)
	defer unlock()

	state := a.store.Snapshot()
	game, storageAccount, err := findGameAndAccount(state, request.GameID)
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	primaryAccount, err := findPrimaryAccount(state)
	if err != nil {
		return core.DashboardSnapshot{}, err
	}

	startedAt := time.Now()
	a.emitSyncProgress(game.ID, msgSyncPreparing)

	gateway, err := core.NewSplitCloudflareGateway(a.syncContext(), primaryAccount, storageAccount)
	var summary core.SyncSummary
	var anchor core.SyncAnchor
	if err == nil {
		conflictChoice := resolveSyncConflictChoice(game, state.Preferences, request.ConflictChoice)
		summary, anchor, err = a.engine.SyncGameWithGateway(a.syncContext(), state.Device, game, gateway, conflictChoice, func(message string) {
			a.emitSyncProgress(game.ID, message)
		})
	}
	if err != nil {
		anchor = game.Anchor
		summary = core.SyncSummary{
			Status:   "failed",
			Message:  err.Error(),
			SyncedAt: time.Now(),
		}
	}

	if summary.SyncedAt.IsZero() {
		summary.SyncedAt = time.Now()
	}
	if updateErr := a.store.UpdateGameSync(game.ID, anchor, summary); updateErr != nil {
		return core.DashboardSnapshot{}, updateErr
	}

	endedAt := time.Now()
	_ = a.store.RecordActivity(core.SyncActivity{
		ID:         core.NewID(),
		GameID:     game.ID,
		GameName:   game.Name,
		AccountID:  storageAccount.ID,
		Status:     summary.Status,
		Message:    summary.Message,
		Uploaded:   summary.Uploaded,
		Downloaded: summary.Downloaded,
		Conflicts:  summary.Conflicts,
		StartedAt:  startedAt,
		EndedAt:    &endedAt,
	})

	a.emitSyncProgress(game.ID, summary.Message)
	a.queueRemoteCatalogSync("manual sync")
	return a.snapshot()
}

func (a *App) lockGameSync(gameID string) func() {
	a.syncGameMu.Lock()
	if a.syncGameLocks == nil {
		a.syncGameLocks = make(map[string]*sync.Mutex)
	}
	lock := a.syncGameLocks[gameID]
	if lock == nil {
		lock = &sync.Mutex{}
		a.syncGameLocks[gameID] = lock
	}
	a.syncGameMu.Unlock()

	lock.Lock()
	return lock.Unlock
}

func resolveSyncConflictChoice(game core.Game, preferences core.Preferences, explicitChoice string) string {
	choice := strings.ToLower(strings.TrimSpace(explicitChoice))
	switch choice {
	case "local", "remote", "cloud":
		return choice
	}
	policy := strings.ToLower(strings.TrimSpace(game.Sync.ConflictStrategy))
	if policy == "" || policy == "manual" {
		policy = strings.ToLower(strings.TrimSpace(preferences.ConflictPolicy))
	}
	switch policy {
	case "local", "remote", "cloud":
		return policy
	default:
		return ""
	}
}

func (a *App) PrepareGameLaunch(gameID string, conflictChoice string) (map[string]any, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}

	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return nil, errors.New(msgGameIDRequired)
	}

	state := a.store.Snapshot()
	game, storageAccount, err := findGameAndAccount(state, gameID)
	if err != nil {
		return nil, err
	}
	if hasActiveLaunchRestoreOverride(game, state.Device.ID) {
		snapshot, snapErr := a.snapshot()
		if snapErr != nil {
			return nil, snapErr
		}
		return map[string]any{
			"status":   "ready",
			"reason":   "launch_restore_override",
			"message":  "当前将以手动恢复后的本地存档启动，本次不会用云端最新自动存档覆盖。",
			"snapshot": snapshot,
		}, nil
	}
	autoRestoreMessage, autoRestoreSnapshot, autoRestoreErr := a.prepareLatestAutoBackupForLaunch(game)
	if autoRestoreErr != nil {
		return nil, autoRestoreErr
	}
	if autoRestoreSnapshot == nil {
		freshState := a.store.Snapshot()
		if refreshedGame, refreshedAccount, refreshedErr := findGameAndAccount(freshState, gameID); refreshedErr == nil {
			game = refreshedGame
			storageAccount = refreshedAccount
			state = freshState
		}
	}
	primaryAccount, err := findPrimaryAccount(state)
	if err != nil {
		return nil, err
	}
	gateway, err := core.NewSplitCloudflareGateway(a.syncContext(), primaryAccount, storageAccount)
	if err != nil {
		return nil, err
	}

	inspection, err := core.InspectLaunchSync(a.syncContext(), game, gateway)
	if err != nil {
		return nil, err
	}
	resolvedConflictChoice := resolveSyncConflictChoice(game, state.Preferences, conflictChoice)

	if inspection.Status == "conflict" {
		if strings.TrimSpace(resolvedConflictChoice) == "" {
			snapshot, snapErr := a.snapshot()
			if snapErr != nil {
				return nil, snapErr
			}
			return map[string]any{
				"status":   "needs_choice",
				"reason":   inspection.Status,
				"message":  inspection.Message,
				"snapshot": snapshot,
			}, nil
		}
	}

	if inspection.Status == "cloud_newer" ||
		inspection.Status == "local_newer" ||
		inspection.Status == "merge_needed" ||
		inspection.Status == "conflict" ||
		strings.TrimSpace(conflictChoice) != "" {
		snapshot, syncErr := a.RunSync(core.SyncRunRequest{
			GameID:         gameID,
			ConflictChoice: resolvedConflictChoice,
		})
		if syncErr != nil {
			return nil, syncErr
		}
		if updatedGame, findErr := findGame(snapshot.State, gameID); findErr == nil && updatedGame.LastSync != nil && updatedGame.LastSync.Status == "conflict" {
			return map[string]any{
				"status":   "needs_choice",
				"reason":   "conflict",
				"message":  updatedGame.LastSync.Message,
				"snapshot": snapshot,
			}, nil
		}
		if updatedGame, findErr := findGame(snapshot.State, gameID); findErr == nil && updatedGame.LastSync != nil && updatedGame.LastSync.Status == "failed" {
			return map[string]any{
				"status":   "failed",
				"reason":   inspection.Status,
				"message":  updatedGame.LastSync.Message,
				"snapshot": snapshot,
			}, nil
		}
		message := inspection.Message
		if updatedGame, findErr := findGame(snapshot.State, gameID); findErr == nil && updatedGame.LastSync != nil && strings.TrimSpace(updatedGame.LastSync.Message) != "" {
			message = updatedGame.LastSync.Message
		}
		if strings.TrimSpace(autoRestoreMessage) != "" {
			message = autoRestoreMessage
		}
		return map[string]any{
			"status":   "ready",
			"reason":   inspection.Status,
			"message":  message,
			"snapshot": snapshot,
		}, nil
	}

	if autoRestoreSnapshot != nil {
		return map[string]any{
			"status":   "ready",
			"reason":   inspection.Status,
			"message":  autoRestoreMessage,
			"snapshot": *autoRestoreSnapshot,
		}, nil
	}

	snapshot, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	message := inspection.Message
	if strings.TrimSpace(autoRestoreMessage) != "" {
		message = autoRestoreMessage
	}
	return map[string]any{
		"status":   "ready",
		"reason":   inspection.Status,
		"message":  message,
		"snapshot": snapshot,
	}, nil
}

func (a *App) PickFolder(defaultDirectory string) (string, error) {
	if a.ctx == nil {
		return "", errors.New(msgWailsRuntimeNotReady)
	}

	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            titlePickSaveFolder,
		DefaultDirectory: strings.TrimSpace(defaultDirectory),
	})
}

func (a *App) PickFile(defaultDirectory string) (string, error) {
	if a.ctx == nil {
		return "", errors.New(msgWailsRuntimeNotReady)
	}

	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            titlePickLaunchFile,
		DefaultDirectory: strings.TrimSpace(defaultDirectory),
	})
}

func (a *App) OpenPath(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return errors.New(msgTargetPathRequired)
	}
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf(msgTargetPathNotFound, err)
	}

	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("explorer", target)
	case "darwin":
		command = exec.Command("open", target)
	default:
		command = exec.Command("xdg-open", target)
	}

	return command.Start()
}

func inferCoverSource(coverPath string, coverSourceType string, coverSource string, existing *core.Game) (string, string) {
	coverPath = strings.TrimSpace(coverPath)
	coverSourceType = strings.TrimSpace(coverSourceType)
	coverSource = strings.TrimSpace(coverSource)
	coverChanged := existing != nil && coverPath != "" && !strings.EqualFold(coverPath, strings.TrimSpace(existing.CoverPath))

	if coverChanged {
		if coverSource == "" || strings.EqualFold(coverSource, strings.TrimSpace(existing.CoverSource)) {
			coverSource = coverPath
		}
		if coverSourceType == "" || strings.EqualFold(coverSourceType, strings.TrimSpace(existing.CoverSourceType)) {
			switch {
			case isDirectCoverSource(coverPath):
				coverSourceType = coverSourceRemoteURL
			case isCoverReference(coverPath):
				if strings.TrimSpace(existing.CoverSourceType) != "" {
					coverSourceType = strings.TrimSpace(existing.CoverSourceType)
				}
			default:
				coverSourceType = coverSourceLocalFile
			}
		}
	}

	if coverSourceType == "" {
		switch {
		case isDirectCoverSource(coverPath):
			coverSourceType = coverSourceRemoteURL
		case coverPath != "":
			coverSourceType = coverSourceLocalFile
		}
	}
	if coverSource == "" {
		coverSource = coverPath
	}
	if isCoverReference(coverPath) && existing != nil {
		if coverSourceType == "" {
			coverSourceType = strings.TrimSpace(existing.CoverSourceType)
		}
		if coverSource == coverPath && strings.TrimSpace(existing.CoverSource) != "" {
			coverSource = strings.TrimSpace(existing.CoverSource)
		}
	}
	if strings.EqualFold(coverSourceType, coverSourceLocalFile) && !isCoverReference(coverSource) {
		coverSource = normalizeLocalCoverPath(coverSource)
	}
	return coverSourceType, coverSource
}

func (a *App) ensureCoverCached(game core.Game, existing *core.Game) (string, string, error) {
	if !coverSourceChanged(game, existing) {
		if cachedPath := a.locateCoverCache(game); cachedPath != "" {
			mimeType, err := detectFileMimeType(cachedPath)
			if err == nil {
				return cachedPath, mimeType, nil
			}
		}
	}

	sourceType := strings.TrimSpace(game.CoverSourceType)
	source := strings.TrimSpace(game.CoverSource)
	if source == "" {
		source = strings.TrimSpace(game.CoverPath)
	}

	switch {
	case strings.EqualFold(sourceType, coverSourceRemoteURL) && isDirectCoverSource(source):
		return a.downloadRemoteCoverToCache(game.ID, source)
	case isCoverReference(source):
		accountID, objectKey, err := parseCoverReference(source)
		if err != nil {
			return "", "", err
		}
		return a.downloadCloudCoverToCache(game.ID, accountID, objectKey)
	default:
		source = normalizeLocalCoverPath(source)
		if source == "" && existing != nil {
			source = normalizeLocalCoverPath(firstNonEmpty(existing.CoverSource, existing.CoverPath))
		}
		if source == "" {
			return "", "", errors.New("cover source is empty")
		}
		if _, err := os.Stat(source); err == nil {
			return a.copyLocalCoverToCache(game.ID, source)
		}
		if accountID, objectKey := coverCloudLocation(game); accountID != "" && objectKey != "" {
			return a.downloadCloudCoverToCache(game.ID, accountID, objectKey)
		}
		if existing != nil {
			if accountID, objectKey := coverCloudLocation(*existing); accountID != "" && objectKey != "" {
				return a.downloadCloudCoverToCache(game.ID, accountID, objectKey)
			}
		}
		return "", "", fmt.Errorf(msgCoverSourceReadFailed, source)
	}
}

func hasActiveLaunchRestoreOverride(game core.Game, deviceID string) bool {
	override := game.LaunchRestoreOverride
	if override == nil || !override.Active {
		return false
	}
	if strings.TrimSpace(override.Filename) == "" {
		return false
	}
	return strings.TrimSpace(override.SourceDeviceID) == strings.TrimSpace(deviceID)
}

func latestReadyAutoBackupRecord(game core.Game) (core.BackupRecord, bool) {
	var latest core.BackupRecord
	found := false
	for _, rawRecord := range game.BackupRegistry {
		record := normalizeBackupRecord(rawRecord)
		if !strings.EqualFold(record.Type, "auto") {
			continue
		}
		if record.DeletedAt != nil || record.PendingDelete || record.Status != core.BackupStatusReady {
			continue
		}
		if !found || record.CreatedAt.After(latest.CreatedAt) {
			latest = record
			found = true
		}
	}
	return latest, found
}

func (a *App) currentManifestMatchesLatestAuto(game core.Game, record core.BackupRecord) bool {
	sourceHash := strings.TrimSpace(record.SourceManifestHash)
	if sourceHash == "" || strings.TrimSpace(game.SavePath) == "" {
		return false
	}
	manifest, err := core.BuildLocalManifest(game.SavePath, game.Sync.IncludePatterns, game.Sync.ExcludePatterns)
	if err != nil {
		return false
	}
	return strings.TrimSpace(manifest.Hash) != "" && manifest.Hash == sourceHash
}

func (a *App) prepareLatestAutoBackupForLaunch(game core.Game) (string, *core.DashboardSnapshot, error) {
	if !game.Sync.Enabled || strings.TrimSpace(game.SavePath) == "" {
		return "", nil, nil
	}
	record, ok := latestReadyAutoBackupRecord(game)
	if !ok {
		return "", nil, nil
	}
	if a.currentManifestMatchesLatestAuto(game, record) {
		return "当前本地存档已是最新自动存档，正在启动游戏。", nil, nil
	}
	gateways, gatewayErr := a.getBackupGateways(a.ctx, game)
	if gatewayErr != nil {
		if localPath := filepath.Join(a.store.DataDir(), "backups", game.ID, record.Filename); strings.TrimSpace(localPath) != "" {
			if _, statErr := os.Stat(localPath); statErr != nil {
				return "", nil, gatewayErr
			}
		}
	}
	bm := core.NewBackupManager(a.engine)
	if err := bm.RestoreBackup(a.ctx, game, record.Filename, a.store.DataDir(), gateways); err != nil {
		return "", nil, fmt.Errorf("restore latest auto backup before launch: %w", err)
	}
	snapshot, err := a.snapshot()
	if err != nil {
		return "", nil, err
	}
	return "已先恢复云端最新自动存档到本地，正在继续启动游戏。", &snapshot, nil
}

func coverSourceChanged(game core.Game, existing *core.Game) bool {
	if existing == nil {
		return false
	}
	if strings.TrimSpace(game.CoverPath) == "" || strings.TrimSpace(existing.CoverPath) == "" {
		return false
	}
	return strings.TrimSpace(game.CoverSourceType) != strings.TrimSpace(existing.CoverSourceType) ||
		strings.TrimSpace(game.CoverSource) != strings.TrimSpace(existing.CoverSource) ||
		strings.TrimSpace(game.CoverPath) != strings.TrimSpace(existing.CoverPath)
}

func (a *App) locateCoverCache(game core.Game) string {
	candidates := make([]string, 0, 2)
	if localPath := strings.TrimSpace(game.CoverLocalPath); localPath != "" && a.isManagedCoverCachePath(localPath, strings.TrimSpace(game.ID)) {
		candidates = append(candidates, localPath)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	cacheDir := filepath.Join(a.store.DataDir(), "covers", strings.TrimSpace(game.ID))
	matches, err := filepath.Glob(filepath.Join(cacheDir, "cover.*"))
	if err != nil {
		return ""
	}
	for _, match := range matches {
		if info, statErr := os.Stat(match); statErr == nil && !info.IsDir() {
			return match
		}
	}
	return ""
}

func (a *App) isManagedCoverCachePath(path string, gameID string) bool {
	path = normalizeLocalCoverPath(path)
	gameID = strings.TrimSpace(gameID)
	if path == "" || gameID == "" || a.store == nil {
		return false
	}
	baseDir := filepath.Clean(filepath.Join(a.store.DataDir(), "covers", gameID))
	candidate := filepath.Clean(path)
	relative, err := filepath.Rel(baseDir, candidate)
	if err != nil {
		return false
	}
	return relative == "." || (relative != "" && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func (a *App) persistResolvedCoverCache(gameID string, localPath string, mimeType string) {
	if a.store == nil {
		return
	}
	if !a.isManagedCoverCachePath(localPath, gameID) {
		return
	}
	if err := a.store.UpdateGameCoverCache(gameID, localPath, mimeType); err != nil && a.ctx != nil {
		wailsruntime.LogErrorf(a.ctx, "update local cover cache failed for %s: %v", gameID, err)
	}
}

func (a *App) copyLocalCoverToCache(gameID string, sourcePath string) (string, string, error) {
	sourcePath = normalizeLocalCoverPath(sourcePath)
	if _, err := os.Stat(sourcePath); err != nil {
		return "", "", fmt.Errorf(msgLocalCoverReadFailed, err)
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", "", fmt.Errorf(msgLocalCoverReadFailed, err)
	}
	mimeType := detectMimeType(sourcePath, data)
	ext := chooseCoverExtension(sourcePath, mimeType)
	targetPath, err := a.writeCoverCache(gameID, ext, data)
	if err != nil {
		return "", "", err
	}
	return targetPath, mimeType, nil
}

func (a *App) downloadRemoteCoverToCache(gameID string, sourceURL string) (string, string, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return "", "", errors.New("cover url is empty")
	}
	req, err := http.NewRequestWithContext(a.syncContext(), http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", "", fmt.Errorf(msgCreateCoverDownloadRequestFailed, err)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf(msgDownloadCoverFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", "", fmt.Errorf(msgDownloadCoverStatusFailed, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return "", "", fmt.Errorf(msgReadCoverDataFailed, err)
	}
	mimeType := firstNonEmpty(strings.TrimSpace(resp.Header.Get("Content-Type")), http.DetectContentType(data))
	ext := chooseCoverExtension(sourceURL, mimeType)
	targetPath, err := a.writeCoverCache(gameID, ext, data)
	if err != nil {
		return "", "", err
	}
	return targetPath, mimeType, nil
}

func (a *App) downloadCloudCoverToCache(gameID string, accountID string, objectKey string) (string, string, error) {
	state := a.store.Snapshot()
	storageAccount, err := findAccount(state, accountID)
	if err != nil {
		return "", "", err
	}
	primaryAccount, err := findPrimaryAccount(state)
	if err != nil {
		return "", "", err
	}
	gateway, err := core.NewSplitCloudflareGateway(a.syncContext(), primaryAccount, storageAccount)
	if err != nil {
		return "", "", err
	}
	ext := chooseCoverExtension(objectKey, "")
	targetPath := a.coverCachePath(gameID, ext)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", "", fmt.Errorf(msgCreateCoverCacheDirFailed, err)
	}
	if err := gateway.R2.DownloadObjectToFile(a.syncContext(), objectKey, targetPath); err != nil {
		return "", "", fmt.Errorf(msgDownloadCloudCoverFailed, err)
	}
	mimeType, err := detectFileMimeType(targetPath)
	if err != nil {
		return targetPath, "", nil
	}
	return targetPath, mimeType, nil
}

func (a *App) writeCoverCache(gameID string, ext string, data []byte) (string, error) {
	targetPath := a.coverCachePath(gameID, ext)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", fmt.Errorf(msgCreateCoverCacheDirFailed, err)
	}
	if err := os.WriteFile(targetPath, data, 0o644); err != nil {
		return "", fmt.Errorf(msgWriteCoverCacheFailed, err)
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(targetPath), "cover.*")); err == nil {
		for _, match := range matches {
			if filepath.Clean(match) == filepath.Clean(targetPath) {
				continue
			}
			_ = os.Remove(match)
		}
	}
	return targetPath, nil
}

func (a *App) coverCachePath(gameID string, ext string) string {
	return filepath.Join(a.store.DataDir(), "covers", strings.TrimSpace(gameID), "cover"+sanitizeCoverExtension(ext))
}

func (a *App) tryUploadCoverToCloud(state core.AppState, game core.Game, localPath string) (string, string, string) {
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return "", "", msgCoverLocalOnlyMissingCache
	}

	storageAccount, ok := selectCoverStorageAccount(state, game)
	if !ok {
		return "", "", msgCoverLocalOnlyNoCloudAccount
	}
	primaryAccount, err := findPrimaryAccount(state)
	if err != nil {
		return "", "", msgCoverLocalOnlyPrimaryUnavailable
	}
	gateway, err := core.NewSplitCloudflareGateway(a.syncContext(), primaryAccount, storageAccount)
	if err != nil {
		return "", "", fmt.Sprintf(msgCoverLocalOnlyGatewayInitFailed, err)
	}
	objectKey := buildCoverObjectKey(game.ID, localPath)
	if err := gateway.R2.PutObjectFromFile(a.syncContext(), objectKey, localPath); err != nil {
		return "", "", fmt.Sprintf(msgCoverLocalOnlyUploadFailed, err)
	}
	return storageAccount.ID, objectKey, ""
}

func (a *App) prepareGameCover(game core.Game) (string, string, error) {
	coverPath := strings.TrimSpace(game.CoverPath)
	if coverPath == "" || isDirectCoverSource(coverPath) || isCoverReference(coverPath) {
		return coverPath, strings.TrimSpace(game.StorageAccountID), nil
	}

	coverPath = normalizeLocalCoverPath(coverPath)
	if _, err := os.Stat(coverPath); err != nil {
		return "", "", fmt.Errorf(msgLocalCoverReadFailed, err)
	}

	state := a.store.Snapshot()
	storageAccount, err := a.resolveStorageAccountForGame(state, game)
	if err != nil {
		return coverPath, "", nil
	}
	primaryAccount, err := findPrimaryAccount(state)
	if err != nil {
		return coverPath, "", nil
	}

	remoteKey := buildCoverObjectKey(game.ID, coverPath)
	gateway, err := core.NewSplitCloudflareGateway(a.syncContext(), primaryAccount, storageAccount)
	if err != nil {
		return "", "", err
	}
	if err := gateway.R2.PutObjectFromFile(a.syncContext(), remoteKey, coverPath); err != nil {
		return "", "", fmt.Errorf(msgUploadBackupFailed, err)
	}

	return makeCoverReference(storageAccount.ID, remoteKey), storageAccount.ID, nil
}

func (a *App) resolveStorageAccountForGame(state core.AppState, game core.Game) (core.CloudflareAccount, error) {
	if accountID := strings.TrimSpace(game.StorageAccountID); accountID != "" {
		return findAccount(state, accountID)
	}
	for _, account := range state.Accounts {
		if account.Enabled {
			return account, nil
		}
	}
	if len(state.Accounts) > 0 {
		return state.Accounts[0], nil
	}
	return core.CloudflareAccount{}, errors.New(msgNoUsableCloudflareAccount)
}

func (a *App) loadCoverReferenceBytes(reference string) ([]byte, error) {
	accountID, objectKey, err := parseCoverReference(reference)
	if err != nil {
		return nil, err
	}

	state := a.store.Snapshot()
	storageAccount, err := findAccount(state, accountID)
	if err != nil {
		return nil, err
	}
	primaryAccount, err := findPrimaryAccount(state)
	if err != nil {
		return nil, err
	}

	gateway, err := core.NewSplitCloudflareGateway(a.syncContext(), primaryAccount, storageAccount)
	if err != nil {
		return nil, err
	}
	return gateway.R2.GetObjectBytes(a.syncContext(), objectKey)
}

func selectCoverStorageAccount(state core.AppState, game core.Game) (core.CloudflareAccount, bool) {
	if accountID := strings.TrimSpace(game.StorageAccountID); accountID != "" {
		for _, account := range state.Accounts {
			if account.ID == accountID && hasUsableR2Account(account) {
				return account, true
			}
		}
	}
	for _, account := range state.Accounts {
		if hasUsableR2Account(account) {
			return account, true
		}
	}
	return core.CloudflareAccount{}, false
}

func coverCloudLocation(game core.Game) (string, string) {
	accountID := strings.TrimSpace(game.CoverCloudAccountID)
	objectKey := strings.TrimSpace(game.CoverCloudKey)
	if accountID != "" && objectKey != "" {
		return accountID, objectKey
	}
	for _, candidate := range []string{game.CoverSource, game.CoverPath} {
		if !isCoverReference(candidate) {
			continue
		}
		accountID, objectKey, err := parseCoverReference(candidate)
		if err == nil {
			return accountID, objectKey
		}
	}
	return "", ""
}

func isDirectCoverSource(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(strings.ToLower(path), "http://") ||
		strings.HasPrefix(strings.ToLower(path), "https://") ||
		strings.HasPrefix(strings.ToLower(path), "data:")
}

func isCoverReference(path string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(path)), coverReferenceScheme+"://")
}

func normalizeLocalCoverPath(path string) string {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(strings.ToLower(path), "file://") {
		parsed, err := url.Parse(path)
		if err == nil {
			path = parsed.Path
			if runtime.GOOS == "windows" {
				path = strings.TrimPrefix(path, "/")
			}
			path = filepath.FromSlash(path)
		}
	}
	return path
}

func buildCoverObjectKey(gameID string, localPath string) string {
	ext := strings.ToLower(filepath.Ext(localPath))
	if ext == "" {
		ext = ".img"
	}
	return fmt.Sprintf("covers/%s/cover%s", strings.TrimSpace(gameID), ext)
}

func makeCoverReference(accountID string, objectKey string) string {
	return fmt.Sprintf("%s://%s/%s", coverReferenceScheme, strings.TrimSpace(accountID), strings.TrimLeft(objectKey, "/"))
}

func parseCoverReference(reference string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(reference))
	if err != nil {
		return "", "", fmt.Errorf(msgParseCoverReferenceFailed, err)
	}
	if !strings.EqualFold(parsed.Scheme, coverReferenceScheme) {
		return "", "", fmt.Errorf(msgUnsupportedCoverReference, reference)
	}
	accountID := strings.TrimSpace(parsed.Host)
	objectKey := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if accountID == "" || objectKey == "" {
		return "", "", fmt.Errorf(msgInvalidCoverReference, reference)
	}
	return accountID, objectKey, nil
}

func chooseCoverExtension(source string, mimeType string) string {
	if ext := sanitizeCoverExtension(filepath.Ext(strings.TrimSpace(source))); ext != ".img" {
		return ext
	}
	mimeType = strings.TrimSpace(strings.Split(mimeType, ";")[0])
	if mimeType != "" {
		if exts, err := mime.ExtensionsByType(mimeType); err == nil {
			for _, ext := range exts {
				if sanitized := sanitizeCoverExtension(ext); sanitized != ".img" {
					return sanitized
				}
			}
		}
	}
	return ".img"
}

func sanitizeCoverExtension(ext string) string {
	ext = strings.TrimSpace(strings.ToLower(ext))
	if ext == "" {
		return ".img"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	if len(ext) > 10 || strings.ContainsAny(ext, `/\:*?"<>|`) {
		return ".img"
	}
	return ext
}

func detectMimeType(path string, data []byte) string {
	if mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); mimeType != "" {
		return mimeType
	}
	if len(data) > 0 {
		return http.DetectContentType(data)
	}
	return ""
}

func detectFileMimeType(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return detectMimeType(path, data), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func dataURLForBytes(path string, data []byte) string {
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func (a *App) snapshot() (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}

	return core.DashboardSnapshot{
		State:         a.snapshotState(),
		DataDir:       a.store.DataDir(),
		SchemaVersion: 1,
	}, nil
}

func (a *App) snapshotState() core.AppState {
	if a.store == nil {
		return core.AppState{}
	}
	return a.enrichStateWithBackupCounts(a.store.Snapshot())
}

func (a *App) enrichStateWithBackupCounts(state core.AppState) core.AppState {
	enriched := state
	if len(state.Games) == 0 {
		return enriched
	}
	enriched.Games = make([]core.Game, len(state.Games))
	for index, game := range state.Games {
		game.BackupCount = a.countKnownBackups(game)
		enriched.Games[index] = game
	}
	return enriched
}

func (a *App) countKnownBackups(game core.Game) int {
	count := 0
	for _, record := range game.BackupRegistry {
		record = normalizeBackupRecord(record)
		if strings.TrimSpace(record.Filename) == "" || record.DeletedAt != nil || record.Status == core.BackupStatusPendingDelete {
			continue
		}
		count++
	}
	return count
}

func activeBackupRegistry(game *core.Game) []core.BackupRecord {
	if game == nil {
		return nil
	}
	next := make([]core.BackupRecord, 0, len(game.BackupRegistry))
	for _, record := range game.BackupRegistry {
		if record.DeletedAt != nil {
			continue
		}
		next = append(next, record)
	}
	return next
}

func findBackupRecord(game core.Game, filename string) (core.BackupRecord, int, bool) {
	filename = strings.TrimSpace(filename)
	for index, record := range game.BackupRegistry {
		if strings.TrimSpace(record.Filename) == filename {
			return record, index, true
		}
	}
	return core.BackupRecord{}, -1, false
}

func upsertBackupRecord(game *core.Game, record core.BackupRecord) {
	if game == nil {
		return
	}
	record = normalizeBackupRecord(record)
	if existing, index, ok := findBackupRecord(*game, record.Filename); ok {
		if record.CreatedAt.IsZero() {
			record.CreatedAt = existing.CreatedAt
		}
		game.BackupRegistry[index] = record
	} else {
		game.BackupRegistry = append(game.BackupRegistry, record)
	}
	rebuildBackupCompatFields(game)
}

func removeBackupRecord(game *core.Game, filename string) {
	if game == nil {
		return
	}
	filename = strings.TrimSpace(filename)
	next := make([]core.BackupRecord, 0, len(game.BackupRegistry))
	for _, record := range game.BackupRegistry {
		if strings.TrimSpace(record.Filename) == filename {
			continue
		}
		next = append(next, record)
	}
	game.BackupRegistry = next
	rebuildBackupCompatFields(game)
}

func rebuildBackupCompatFields(game *core.Game) {
	if game == nil {
		return
	}
	locations := make(map[string]string)
	for _, record := range game.BackupRegistry {
		if record.DeletedAt != nil || strings.TrimSpace(record.Filename) == "" || strings.TrimSpace(record.AccountID) == "" {
			continue
		}
		locations[record.Filename] = record.AccountID
	}
	game.BackupLocations = locations
	if strings.TrimSpace(game.BackupStorageAccountID) == "" {
		if strings.TrimSpace(game.AutoBackupAccountID) != "" {
			game.BackupStorageAccountID = strings.TrimSpace(game.AutoBackupAccountID)
		} else if strings.TrimSpace(game.StorageAccountID) != "" {
			game.BackupStorageAccountID = strings.TrimSpace(game.StorageAccountID)
		}
	}
}

func normalizeBackupRecord(record core.BackupRecord) core.BackupRecord {
	record.Filename = strings.TrimSpace(record.Filename)
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
		record.Type = "manual"
	}
	if record.Status == "" {
		if record.PendingDelete {
			record.Status = core.BackupStatusPendingDelete
		} else {
			record.Status = core.BackupStatusReady
		}
	}
	record.PendingDelete = record.Status == core.BackupStatusPendingDelete
	if record.Status != core.BackupStatusUploadFailed && record.Status != core.BackupStatusDeleteFailed {
		record.LastError = ""
	}
	if record.Status != core.BackupStatusDeleteFailed {
		record.LastDeleteError = ""
	}
	return record
}

func (a *App) ensureReady() error {
	if a.store != nil {
		return nil
	}

	baseDir, err := a.resolveBaseDir()
	if err != nil {
		return err
	}
	store, err := core.NewStore(baseDir)
	if err != nil {
		return err
	}

	a.store = store
	return nil
}

func (a *App) resolveBaseDir() (string, error) {
	if a.baseDir != "" {
		return a.baseDir, nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf(msgResolveBaseDirFailed, err)
	}

	a.baseDir = filepath.Dir(exePath)
	return a.baseDir, nil
}

func (a *App) startupWindowState() windowState {
	if a.windowStateLoaded {
		return a.savedWindowState
	}

	state := defaultWindowState()
	baseDir, err := a.resolveBaseDir()
	if err == nil {
		if loaded, loadErr := loadWindowState(baseDir); loadErr == nil {
			state = loaded
		}
	}

	a.savedWindowState = state
	a.windowStateLoaded = true
	return state
}

func (a *App) restoreWindowState() error {
	if a.ctx == nil {
		return nil
	}

	state := a.startupWindowState()
	if state.HasPosition() {
		wailsruntime.WindowSetPosition(a.ctx, state.X, state.Y)
	}
	return nil
}

func (a *App) saveCurrentWindowState() error {
	if a.ctx == nil {
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			wailsruntime.LogErrorf(a.ctx, "save window state panic recovered: %v", recovered)
		}
	}()

	baseDir, err := a.resolveBaseDir()
	if err != nil {
		return err
	}

	state := a.startupWindowState()
	if windowMetrics, ok := a.readWindowMetrics(); ok {
		if !windowMetrics.IsMinimised {
			if windowMetrics.Width > 0 && windowMetrics.Height > 0 {
				state.Width = windowMetrics.Width
				state.Height = windowMetrics.Height
			}
			state.X = windowMetrics.X
			state.Y = windowMetrics.Y
		}

		switch {
		case windowMetrics.IsFullscreen:
			state.StartState = windowStateFullscreen
		case windowMetrics.IsMaximised:
			state.StartState = windowStateMaximised
		default:
			state.StartState = windowStateNormal
		}
	}

	a.savedWindowState = state
	a.windowStateLoaded = true
	return saveWindowState(baseDir, state)
}

type windowMetrics struct {
	Width        int
	Height       int
	X            int
	Y            int
	IsMinimised  bool
	IsMaximised  bool
	IsFullscreen bool
}

func (a *App) readWindowMetrics() (_ windowMetrics, ok bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			ok = false
		}
	}()

	metrics := windowMetrics{
		IsMinimised:  wailsruntime.WindowIsMinimised(a.ctx),
		IsMaximised:  wailsruntime.WindowIsMaximised(a.ctx),
		IsFullscreen: wailsruntime.WindowIsFullscreen(a.ctx),
	}

	if !metrics.IsMinimised {
		metrics.Width, metrics.Height = wailsruntime.WindowGetSize(a.ctx)
		metrics.X, metrics.Y = wailsruntime.WindowGetPosition(a.ctx)
	}

	return metrics, true
}

func (a *App) startTray() error {
	if runtime.GOOS != "windows" || a.ctx == nil || a.tray != nil {
		return nil
	}

	iconPath := ""
	if len(trayIconPNG) == 0 {
		var err error
		iconPath, err = a.resolveIconPath()
		if err != nil {
			return err
		}
	}

	tray, err := newWindowsTray(a.ctx, iconPath, trayIconPNG)
	if err != nil {
		return err
	}
	a.tray = tray
	return nil
}

func (a *App) resolveIconPath() (string, error) {
	baseDir, err := a.resolveBaseDir()
	if err != nil {
		return "", err
	}

	candidates := []string{
		filepath.Join(baseDir, "resource", "im1.png"),
		filepath.Join(baseDir, "..", "resource", "im1.png"),
		filepath.Join(baseDir, "..", "..", "resource", "im1.png"),
		filepath.Join(".", "resource", "im1.png"),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			absPath, absErr := filepath.Abs(candidate)
			if absErr == nil {
				return absPath, nil
			}
			return candidate, nil
		}
	}

	return "", fmt.Errorf(msgTrayIconNotFound)
}

func (a *App) ExportWindowState() (string, error) {
	state := a.startupWindowState()
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func (a *App) syncRemoteCatalog() error {
	state := a.store.Snapshot()
	primary, err := findPrimaryAccount(state)
	if err != nil {
		return err
	}
	d1 := core.NewD1Client(primary)

	encryptedCredentials := map[string]core.EncryptedCredentialBlob{}
	if strings.TrimSpace(a.recoveryPassword) != "" {
		for _, account := range state.Accounts {
			if account.IsPrimary {
				continue
			}
			blob, err := core.EncryptAccountCredentials(account, a.recoveryPassword)
			if err != nil {
				return err
			}
			encryptedCredentials[account.ID] = blob
		}
	} else {
		_ = a.store.SetRecoveryStatus(func(status *core.RecoveryStatus) {
			status.PendingCredentialBackup = false
		})
	}

	revision, err := d1.SaveRemoteCatalog(a.syncContext(), core.RemoteCatalog{
		Accounts: state.Accounts,
		Games:    state.Games,
		Preferences: &core.RemotePreferences{
			TagOrder:               state.Preferences.TagOrder,
			TagOrderUpdatedAt:      state.Preferences.TagOrderUpdatedAt,
			FavoriteGames:          state.Preferences.FavoriteGames,
			FavoriteGamesUpdatedAt: state.Preferences.FavoriteGamesUpdatedAt,
			GameOrderUpdatedAt:     state.Preferences.GameOrderUpdatedAt,
		},
		Tombstones: activeCatalogTombstones(state),
	}, encryptedCredentials, state.Device)
	if err != nil {
		_ = a.store.SetRecoveryStatus(func(status *core.RecoveryStatus) {
			status.LastRecoveryError = err.Error()
		})
		return err
	}

	now := time.Now()
	if err := a.store.SetRecoveryStatus(func(status *core.RecoveryStatus) {
		status.RemoteCatalogAvailable = true
		status.LastCatalogSyncAt = &now
		status.LastRecoveryError = ""
		status.PendingCredentialBackup = false
		if len(state.Accounts) > 1 {
			status.LastCredentialBackupAt = &now
		}
		if strings.TrimSpace(a.recoveryPassword) != "" {
			status.HasRecoveryPassword = true
			status.LastCredentialBackupAt = &now
		}
	}); err != nil {
		return err
	}
	return a.store.MarkCatalogSynced(revision)
}

func activeCatalogTombstones(state core.AppState) core.CatalogTombstones {
	gameUpdatedAt := make(map[string]time.Time, len(state.Games))
	for _, game := range state.Games {
		gameUpdatedAt[game.ID] = game.CatalogUpdatedAt
	}
	accountUpdatedAt := make(map[string]time.Time, len(state.Accounts))
	for _, account := range state.Accounts {
		accountUpdatedAt[account.ID] = account.CatalogUpdatedAt
	}
	return core.CatalogTombstones{
		Games:    activeTombstoneMap(state.Tombstones.Games, gameUpdatedAt),
		Accounts: activeTombstoneMap(state.Tombstones.Accounts, accountUpdatedAt),
	}
}

func activeTombstoneMap(tombstones map[string]time.Time, updatedAtByID map[string]time.Time) map[string]time.Time {
	active := make(map[string]time.Time, len(tombstones))
	for id, deletedAt := range tombstones {
		if updatedAt, ok := updatedAtByID[id]; ok && updatedAt.After(deletedAt) {
			continue
		}
		active[id] = deletedAt
	}
	return active
}

func (a *App) queueRemoteCatalogSync(reason string) {
	if a.ctx == nil {
		return
	}
	if a.store != nil {
		if err := a.store.MarkCatalogDirty(); err != nil {
			wailsruntime.LogErrorf(a.ctx, "mark catalog dirty failed: %v", err)
		}
	}
	a.catalogSyncMu.Lock()
	defer a.catalogSyncMu.Unlock()
	if a.catalogSyncTimer != nil {
		a.catalogSyncTimer.Stop()
	}
	if a.catalogRetryTimer != nil {
		a.catalogRetryTimer.Stop()
		a.catalogRetryTimer = nil
	}
	a.catalogSyncQueued = true
	a.emitCatalogSyncStatus("queued", reason, msgCatalogSyncQueued, nil)
	a.catalogSyncTimer = time.AfterFunc(700*time.Millisecond, func() {
		a.runRemoteCatalogSync(reason)
	})
}

func (a *App) runRemoteCatalogSync(reason string) {
	a.catalogSyncMu.Lock()
	if a.catalogSyncActive {
		a.catalogSyncQueued = true
		a.catalogSyncMu.Unlock()
		a.emitCatalogSyncStatus("queued", reason, msgCatalogSyncQueuedNext, nil)
		return
	}
	a.catalogSyncActive = true
	a.catalogSyncQueued = false
	if a.catalogSyncTimer != nil {
		a.catalogSyncTimer.Stop()
		a.catalogSyncTimer = nil
	}
	a.catalogSyncMu.Unlock()
	a.emitCatalogSyncStatus("syncing", reason, msgCatalogSyncRunning, nil)

	err := a.syncLatestRemoteCatalog()

	a.catalogSyncMu.Lock()
	queued := a.catalogSyncQueued
	a.catalogSyncActive = false
	a.catalogSyncMu.Unlock()

	if err != nil {
		wailsruntime.LogErrorf(a.ctx, "background catalog sync failed after %s: %v", reason, err)
		if a.store != nil {
			_ = a.store.MarkCatalogSyncFailed(err.Error())
		}
		a.emitCatalogSyncStatus("retrying", reason, msgCatalogSyncRetrying, map[string]any{
			"retryAfter": 30,
			"error":      err.Error(),
		})
		a.scheduleCatalogRetry(30*time.Second, reason)
		return
	}

	a.catalogSyncMu.Lock()
	if a.catalogRetryTimer != nil {
		a.catalogRetryTimer.Stop()
		a.catalogRetryTimer = nil
	}
	a.lastSyncError = ""
	a.lastSyncErrorAt = time.Time{}
	a.catalogSyncMu.Unlock()
	a.emitCatalogSyncStatus("succeeded", reason, msgCatalogSyncSucceeded, nil)
	if queued {
		a.queueRemoteCatalogSync(reason)
	}
}

func (a *App) scheduleCatalogRetry(delay time.Duration, reason string) {
	a.catalogSyncMu.Lock()
	defer a.catalogSyncMu.Unlock()
	if a.catalogRetryTimer != nil {
		a.catalogRetryTimer.Stop()
	}
	a.catalogRetryTimer = time.AfterFunc(delay, func() {
		a.catalogSyncMu.Lock()
		a.catalogRetryTimer = nil
		a.catalogSyncMu.Unlock()
		a.queueRemoteCatalogSync(reason)
	})
}

func (a *App) syncLatestRemoteCatalog() error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	_ = a.store.MarkCatalogSyncAttempt()
	if err := a.pullRemoteCatalog(); err != nil {
		return err
	}
	return a.syncRemoteCatalog()
}

func (a *App) emitCatalogSyncFailure(err error) {
	if a.ctx == nil || err == nil {
		return
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "unknown error"
	}
	a.catalogSyncMu.Lock()
	defer a.catalogSyncMu.Unlock()
	now := time.Now()
	if message == a.lastSyncError && now.Sub(a.lastSyncErrorAt) < time.Minute {
		return
	}
	a.lastSyncError = message
	a.lastSyncErrorAt = now
	wailsruntime.EventsEmit(a.ctx, "catalog:sync_failed", map[string]string{
		"message": message,
	})
}

func (a *App) emitCatalogSyncEvent(status string, payload map[string]any) {
	if a.ctx == nil {
		return
	}
	eventPayload := map[string]any{
		"status": status,
	}
	for key, value := range payload {
		eventPayload[key] = value
	}
	wailsruntime.EventsEmit(a.ctx, "catalog:sync_state", eventPayload)
}

func (a *App) emitCatalogSyncStatus(status string, reason string, message string, extras map[string]any) {
	payload := map[string]any{
		"reason":  reason,
		"message": message,
	}
	for key, value := range extras {
		payload[key] = value
	}
	a.emitCatalogSyncEvent(status, payload)
}

func (a *App) pullRemoteCatalog() error {
	state := a.store.Snapshot()
	if len(state.Accounts) == 0 {
		return nil
	}
	primary, err := findPrimaryAccount(state)
	if err != nil {
		return nil
	}
	d1 := core.NewD1Client(primary)
	remoteRevision, revisionErr := d1.LoadCatalogRevision(a.syncContext())
	if revisionErr == nil && remoteRevision > 0 && remoteRevision == a.store.LastKnownCatalogRevision() && !a.store.HasPendingCatalogSync() {
		return nil
	}
	catalog, _, err := d1.LoadRemoteCatalog(a.syncContext())
	if err != nil {
		_ = a.store.SetRecoveryStatus(func(status *core.RecoveryStatus) {
			status.LastRecoveryError = err.Error()
		})
		return err
	}
	if err := a.store.MergeRemoteCatalog(catalog); err != nil {
		return err
	}
	if catalog.Revision > 0 {
		return a.store.MarkCatalogRevision(catalog.Revision)
	}
	return nil
}

func (a *App) pullRemoteCatalogInBackground(reason string) {
	if err := a.ensureReady(); err != nil {
		return
	}
	if err := a.pullRemoteCatalog(); err != nil {
		wailsruntime.LogErrorf(a.ctx, "pull remote catalog in background during %s failed: %v", reason, err)
		return
	}
	a.emitStateUpdated()
}

func (a *App) verifyAccounts(pullCatalogAfter bool) {
	if err := a.ensureReady(); err != nil {
		return
	}
	state := a.store.Snapshot()
	for _, account := range state.Accounts {
		account.VerificationState = "pending"
		verifiedAccount, _ := core.VerifyCloudflareAccount(a.syncContext(), account)
		if verifiedAccount.LastError == "" {
			verifiedAccount.VerificationState = "valid"
		} else {
			verifiedAccount.VerificationState = "invalid"
		}
		if _, err := a.store.UpsertAccount(verifiedAccount); err != nil {
			wailsruntime.LogErrorf(a.ctx, "store verified account failed: %v", err)
		}
	}

	if pullCatalogAfter {
		if err := a.pullRemoteCatalog(); err != nil {
			wailsruntime.LogErrorf(a.ctx, "pull remote catalog after account verification failed: %v", err)
		}
	}

	a.emitStateUpdated()
}

func (a *App) syncContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) emitSyncProgress(gameID string, message string) {
	if a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, "sync:progress", map[string]string{
		"gameId":  gameID,
		"message": message,
	})
}

func (a *App) emitRuntimeEvent(name string, payload any) {
	if a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, name, payload)
}

func (a *App) emitStateUpdated() {
	if a.store == nil {
		return
	}
	a.emitRuntimeEvent("state:updated", a.snapshotState())
}

func (a *App) emitGameEnded(gameID string, duration time.Duration) {
	a.emitRuntimeEvent("game:ended", map[string]interface{}{
		"id":       gameID,
		"duration": duration.Minutes(),
	})
}

func (a *App) handleGameSessionEnded(gameID string, duration time.Duration) {
	gameToUpdate, err := findGame(a.store.Snapshot(), gameID)
	if err != nil {
		a.emitGameEnded(gameID, duration)
		return
	}

	gameToUpdate.PlayTime += duration.Minutes()
	now := time.Now()
	gameToUpdate.LastPlayed = &now
	gameToUpdate.LaunchRestoreOverride = nil
	if _, err := a.store.UpsertGame(gameToUpdate); err == nil {
		a.queueRemoteCatalogSync("playtime update")
		a.emitStateUpdated()
	} else {
		wailsruntime.LogErrorf(a.ctx, "store playtime update failed after %s ended: %v", gameID, err)
	}

	a.emitGameEnded(gameID, duration)
	a.emitRuntimeEvent("game:backup_starting", gameID)

	bm := core.NewBackupManager(a.engine)
	backup, err := bm.CreateBackup(context.Background(), gameToUpdate, "auto", msgAutoBackupName, a.store.DataDir(), nil)
	if err == nil && backup != nil {
		backup, err = a.queueBackupUploadForGame(gameToUpdate, *backup)
	}
	if err != nil {
		a.emitRuntimeEvent("game:backup_error", map[string]string{"id": gameID, "error": err.Error()})
		return
	}
	if backup != nil && backup.Status == core.BackupStatusUploadFailed {
		errorMessage := strings.TrimSpace(backup.LastError)
		if errorMessage == "" {
			errorMessage = "自动备份已保留在本地，云端上传失败"
		}
		a.emitRuntimeEvent("game:backup_error", map[string]string{"id": gameID, "error": errorMessage})
	}
}

func findAccount(state core.AppState, accountID string) (core.CloudflareAccount, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return core.CloudflareAccount{}, errors.New(msgAccountIDNotFound)
	}

	for _, account := range state.Accounts {
		if account.ID == accountID {
			return account, nil
		}
	}

	return core.CloudflareAccount{}, errors.New(msgAccountNotFound)
}

func findGameAndAccount(state core.AppState, gameID string) (core.Game, core.CloudflareAccount, error) {
	for _, game := range state.Games {
		if game.ID != gameID {
			continue
		}

		if strings.TrimSpace(game.StorageAccountID) != "" {
			for _, account := range state.Accounts {
				if account.ID == game.StorageAccountID {
					return game, account, nil
				}
			}
		}

		for _, account := range state.Accounts {
			if account.Enabled {
				return game, account, nil
			}
		}
		if len(state.Accounts) > 0 {
			return game, state.Accounts[0], nil
		}
		return core.Game{}, core.CloudflareAccount{}, errors.New(msgNoUsableCloudflareAccount)
	}

	return core.Game{}, core.CloudflareAccount{}, errors.New(msgGameNotFound)
}

func hasUsableR2Account(account core.CloudflareAccount) bool {
	return account.Enabled &&
		strings.TrimSpace(account.AccountID) != "" &&
		strings.TrimSpace(account.R2Bucket) != "" &&
		strings.TrimSpace(account.R2AccessKeyID) != "" &&
		strings.TrimSpace(account.R2SecretAccessKey) != ""
}

func appendUniqueAccount(accounts []core.CloudflareAccount, seen map[string]bool, account core.CloudflareAccount) []core.CloudflareAccount {
	if strings.TrimSpace(account.ID) == "" || seen[account.ID] || !hasUsableR2Account(account) {
		return accounts
	}
	seen[account.ID] = true
	return append(accounts, account)
}

func canonicalBackupAccounts(state core.AppState) []core.CloudflareAccount {
	ordered := make([]core.CloudflareAccount, 0, len(state.Accounts))
	for _, account := range state.Accounts {
		if !hasUsableR2Account(account) {
			continue
		}
		ordered = append(ordered, account)
	}
	return ordered
}

func orderedBackupAccounts(state core.AppState, game core.Game, backupType string) []core.CloudflareAccount {
	lookup := make(map[string]core.CloudflareAccount, len(state.Accounts))
	for _, account := range state.Accounts {
		lookup[account.ID] = account
	}
	canonical := canonicalBackupAccounts(state)
	if strings.EqualFold(strings.TrimSpace(backupType), "auto") {
		if accountID := strings.TrimSpace(game.AutoBackupAccountID); accountID != "" {
			if account, ok := lookup[accountID]; ok && hasUsableR2Account(account) {
				return []core.CloudflareAccount{account}
			}
		}
		if accountID := strings.TrimSpace(game.StorageAccountID); accountID != "" {
			if account, ok := lookup[accountID]; ok && hasUsableR2Account(account) {
				ordered := []core.CloudflareAccount{account}
				for _, candidate := range canonical {
					if candidate.ID == account.ID {
						continue
					}
					ordered = append(ordered, candidate)
				}
				return ordered
			}
		}
		return canonical
	}
	return canonical
}

func (a *App) getBackupGateways(ctx context.Context, game core.Game) (map[string]*core.CloudflareGateway, error) {
	state := a.store.Snapshot()
	primaryAccount, err := findPrimaryAccount(state)
	if err != nil {
		return nil, err
	}
	gateways := make(map[string]*core.CloudflareGateway)
	var failures []string
	for _, account := range canonicalBackupAccounts(state) {
		gateway, gatewayErr := core.NewSplitCloudflareGateway(ctx, primaryAccount, account)
		if gatewayErr != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", account.Name, gatewayErr))
			continue
		}
		gateways[account.ID] = gateway
	}
	if len(gateways) == 0 && len(failures) > 0 {
		return nil, fmt.Errorf(msgNoBackupGateways, strings.Join(failures, "; "))
	}
	return gateways, nil
}

func (a *App) selectBackupStorageAccount(ctx context.Context, game core.Game, backupType string, backupSize int64) (core.CloudflareAccount, error) {
	state := a.store.Snapshot()
	candidates := orderedBackupAccounts(state, game, backupType)
	if len(candidates) == 0 {
		return core.CloudflareAccount{}, errors.New(msgNoBackupAccounts)
	}
	maxUsage := r2FreeTierStorageBytes - backupRoutingReserveBytes
	if maxUsage < 0 {
		maxUsage = r2FreeTierStorageBytes
	}
	var failures []string
	for _, account := range candidates {
		r2, err := core.NewR2Client(ctx, account)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", account.Name, err))
			continue
		}
		usedBytes, err := r2.FetchAccountUsageBytes(ctx)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", account.Name, err))
			continue
		}
		if usedBytes+backupSize <= maxUsage {
			account.UsedBytes = usedBytes
			return account, nil
		}
	}
	if len(failures) == len(candidates) && len(failures) > 0 {
		return core.CloudflareAccount{}, fmt.Errorf(msgFetchBackupUsageFailed, strings.Join(failures, "; "))
	}
	return core.CloudflareAccount{}, fmt.Errorf(msgBackupStorageNearLimit, formatBytes(backupSize))
}

func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}

func canonicalBackupRecordName(backupType string, requestedName string) string {
	if strings.EqualFold(strings.TrimSpace(backupType), "auto") {
		return "自动游戏存档"
	}
	if strings.TrimSpace(requestedName) != "" {
		return strings.TrimSpace(requestedName)
	}
	return "手动存档"
}

func defaultBackupRecordName(backupType string, requestedName string) string {
	if strings.EqualFold(strings.TrimSpace(backupType), "auto") {
		return "自动游戏存档"
	}
	if strings.TrimSpace(requestedName) != "" {
		return strings.TrimSpace(requestedName)
	}
	return "手动存档"
}

func (a *App) persistBackupRoute(game core.Game, backup core.Backup, accountID string) error {
	if strings.TrimSpace(accountID) == "" {
		return nil
	}
	backup.Name = defaultBackupRecordName(backup.Type, backup.Name)
	record := core.BackupRecord{
		Filename:                  backup.Filename,
		AccountID:                 accountID,
		Type:                      backup.Type,
		Name:                      backup.Name,
		SHA256:                    strings.TrimSpace(backup.SHA256),
		CreatedAt:                 backup.CreatedAt,
		SourceDeviceID:            strings.TrimSpace(backup.SourceDeviceID),
		SourceManifestHash:        strings.TrimSpace(backup.SourceManifestHash),
		SourceManifestGeneratedAt: backup.SourceManifestGeneratedAt,
	}
	if backup.Type == "auto" {
		game.AutoBackupAccountID = accountID
		nextRegistry := make([]core.BackupRecord, 0, len(game.BackupRegistry))
		for _, existing := range game.BackupRegistry {
			if strings.EqualFold(existing.Type, "auto") {
				continue
			}
			nextRegistry = append(nextRegistry, existing)
		}
		game.BackupRegistry = nextRegistry
	}
	upsertBackupRecord(&game, record)
	if backup.Type != "auto" {
		game.BackupStorageAccountID = accountID
	}
	if _, err := a.store.UpsertGame(game); err != nil {
		return err
	}
	a.queueRemoteCatalogSync("backup route update")
	a.emitStateUpdated()
	return nil
}

func (a *App) uploadBackupToCloud(ctx context.Context, game core.Game, backup *core.Backup) error {
	if backup == nil {
		return nil
	}
	account, err := a.selectBackupStorageAccount(ctx, game, backup.Type, backup.Size)
	if err != nil {
		return fmt.Errorf(msgLocalBackupRetained, err, filepath.Join(a.store.DataDir(), "backups", game.ID, backup.Filename))
	}
	state := a.store.Snapshot()
	primaryAccount, err := findPrimaryAccount(state)
	if err != nil {
		return err
	}
	gateway, err := core.NewSplitCloudflareGateway(ctx, primaryAccount, account)
	if err != nil {
		return err
	}
	localPath := filepath.Join(a.store.DataDir(), "backups", game.ID, backup.Filename)
	remoteKey := fmt.Sprintf("backups/%s/%s", game.ID, backup.Filename)
	if err := gateway.R2.PutObjectFromFile(ctx, remoteKey, localPath); err != nil {
		return fmt.Errorf(msgUploadBackupFailed, err)
	}
	if backup.Type == "auto" {
		bm := core.NewBackupManager(a.engine)
		bm.CleanupCloudAutoBackups(ctx, gateway, game.ID, backup.Filename)
	}
	backup.StorageAccountID = account.ID
	return a.persistBackupRoute(game, *backup, account.ID)
}

func (a *App) persistBackupRecord(game core.Game, backup core.Backup, accountID string, status string, lastError string) (*core.Backup, error) {
	backup.Name = canonicalBackupRecordName(backup.Type, backup.Name)
	record := core.BackupRecord{
		Filename:                  backup.Filename,
		AccountID:                 strings.TrimSpace(accountID),
		Type:                      backup.Type,
		Name:                      backup.Name,
		SHA256:                    strings.TrimSpace(backup.SHA256),
		CreatedAt:                 backup.CreatedAt,
		SourceDeviceID:            strings.TrimSpace(backup.SourceDeviceID),
		SourceManifestHash:        strings.TrimSpace(backup.SourceManifestHash),
		SourceManifestGeneratedAt: backup.SourceManifestGeneratedAt,
		Status:                    strings.TrimSpace(status),
		LastError:                 strings.TrimSpace(lastError),
	}
	if backup.Type == "auto" {
		game.AutoBackupAccountID = strings.TrimSpace(accountID)
		nextRegistry := make([]core.BackupRecord, 0, len(game.BackupRegistry))
		for _, existing := range game.BackupRegistry {
			if strings.EqualFold(existing.Type, "auto") {
				continue
			}
			nextRegistry = append(nextRegistry, existing)
		}
		game.BackupRegistry = nextRegistry
	}
	upsertBackupRecord(&game, record)
	if backup.Type != "auto" && strings.TrimSpace(accountID) != "" {
		game.BackupStorageAccountID = strings.TrimSpace(accountID)
	}
	if _, err := a.store.UpsertGame(game); err != nil {
		return nil, err
	}
	a.queueRemoteCatalogSync("backup record update")
	a.emitStateUpdated()
	backup.StorageAccountID = record.AccountID
	backup.Name = record.Name
	backup.Status = record.Status
	backup.LastError = record.LastError
	backup.PendingDelete = record.Status == core.BackupStatusPendingDelete
	backup.LocalExists = true
	backup.CloudExists = record.Status == core.BackupStatusReady
	return &backup, nil
}

func (a *App) queueBackupUploadForGame(game core.Game, backup core.Backup) (*core.Backup, error) {
	if strings.TrimSpace(backup.SourceDeviceID) == "" {
		backup.SourceDeviceID = strings.TrimSpace(a.store.Snapshot().Device.ID)
	}
	account, err := a.selectBackupStorageAccount(a.ctx, game, backup.Type, backup.Size)
	if err != nil {
		return a.persistBackupRecord(game, backup, "", core.BackupStatusUploadFailed, err.Error())
	}
	savedBackup, persistErr := a.persistBackupRecord(game, backup, account.ID, core.BackupStatusPendingUpload, "")
	if persistErr != nil {
		return nil, persistErr
	}
	a.startBackupUploadWorker()
	a.enqueueBackupUpload(queuedBackupUpload{
		GameID:    game.ID,
		Filename:  backup.Filename,
		AccountID: account.ID,
	})
	return savedBackup, nil
}

func findPrimaryAccount(state core.AppState) (core.CloudflareAccount, error) {
	for _, account := range state.Accounts {
		if account.IsPrimary {
			return account, nil
		}
	}
	for _, account := range state.Accounts {
		if account.Enabled {
			return account, nil
		}
	}
	if len(state.Accounts) > 0 {
		return state.Accounts[0], nil
	}
	return core.CloudflareAccount{}, errors.New(msgPrimaryAccountNotConfigured)
}

func findGame(state core.AppState, gameID string) (core.Game, error) {
	for _, game := range state.Games {
		if game.ID == gameID {
			return game, nil
		}
	}
	return core.Game{}, errors.New(msgGameNotFound)
}

func (a *App) getGatewayForGame(ctx context.Context, gameID string) (*core.CloudflareGateway, error) {
	state := a.store.Snapshot()
	_, storageAccount, err := findGameAndAccount(state, gameID)
	if err != nil {
		return nil, err
	}
	primaryAccount, err := findPrimaryAccount(state)
	if err != nil {
		return nil, err
	}
	return core.NewSplitCloudflareGateway(ctx, primaryAccount, storageAccount)
}

func (a *App) LaunchAndMonitorGame(gameID string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	state := a.store.Snapshot()
	game, err := findGame(state, gameID)
	if err != nil {
		return err
	}

	pm := core.NewProcessMonitor()

	onStart := func(pid int32) {
		a.emitRuntimeEvent("game:started", gameID)
	}

	onEnd := func(duration time.Duration) {
		a.handleGameSessionEnded(gameID, duration)
	}

	return pm.LaunchAndMonitor(a.ctx, game.InstallPath, onStart, onEnd)
}

func (a *App) GetGameBackups(gameID string) (core.BackupListResult, error) {
	if err := a.ensureReady(); err != nil {
		return core.BackupListResult{}, err
	}
	game, err := findGame(a.store.Snapshot(), gameID)
	if err != nil {
		return core.BackupListResult{}, err
	}
	gateways, gatewayErr := a.getBackupGateways(a.ctx, game)
	bm := core.NewBackupManager(a.engine)
	result, err := bm.GetBackups(a.ctx, game, a.store.DataDir(), gateways)
	if err != nil {
		return core.BackupListResult{}, err
	}
	if gatewayErr != nil {
		result.Partial = true
		if result.Message == "" {
			result.Message = "部分备份桶读取失败，列表可能不完整"
		}
		if len(result.FailedAccounts) == 0 {
			for _, account := range canonicalBackupAccounts(a.store.Snapshot()) {
				result.FailedAccounts = append(result.FailedAccounts, account.ID)
			}
		}
		if len(result.Backups) == 0 {
			return result, gatewayErr
		}
	}
	if result.Partial && result.Message == "" {
		result.Message = "部分备份桶读取失败，列表可能不完整"
	}
	return result, nil
}

func (a *App) CreateGameBackup(gameID string, backupType string, name string) (*core.Backup, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	state := a.store.Snapshot()
	game, err := findGame(state, gameID)
	if err != nil {
		return nil, err
	}
	bm := core.NewBackupManager(a.engine)
	backup, err := bm.CreateBackup(a.ctx, game, backupType, name, a.store.DataDir(), nil)
	if err != nil {
		return nil, err
	}
	return a.queueBackupUploadForGame(game, *backup)
}

func (a *App) RestoreGameBackup(gameID string, filename string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	state := a.store.Snapshot()
	game, err := findGame(state, gameID)
	if err != nil {
		return err
	}
	record, _, ok := findBackupRecord(game, filename)
	if !ok {
		return fmt.Errorf("backup %s not found", filename)
	}
	record = normalizeBackupRecord(record)
	gateways, gatewayErr := a.getBackupGateways(a.ctx, game)
	if gatewayErr != nil {
		if localPath := filepath.Join(a.store.DataDir(), "backups", game.ID, filename); strings.TrimSpace(localPath) != "" {
			if _, statErr := os.Stat(localPath); statErr != nil {
				return gatewayErr
			}
		}
	}
	bm := core.NewBackupManager(a.engine)
	if err := bm.RestoreBackup(a.ctx, game, filename, a.store.DataDir(), gateways); err != nil {
		return err
	}
	game.LaunchRestoreOverride = &core.LaunchRestoreOverride{
		Filename:       record.Filename,
		BackupType:     record.Type,
		SourceDeviceID: state.Device.ID,
		RestoredAt:     time.Now(),
		Active:         true,
	}
	if _, err := a.store.UpsertGame(game); err != nil {
		return err
	}
	a.queueRemoteCatalogSync("backup restore override")
	a.emitStateUpdated()
	return nil
}

func (a *App) DeleteGameBackup(gameID string, filename string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	game, err := findGame(a.store.Snapshot(), gameID)
	if err != nil {
		return err
	}
	record, _, ok := findBackupRecord(game, filename)
	if !ok {
		return nil
	}
	record = normalizeBackupRecord(record)
	if record.Status == core.BackupStatusPendingDelete {
		return nil
	}
	record.Status = core.BackupStatusPendingDelete
	record.PendingDelete = true
	record.LastError = ""
	record.LastDeleteError = ""
	record.DeleteRetryAt = nil
	upsertBackupRecord(&game, record)
	if _, err := a.store.UpsertGame(game); err != nil {
		return err
	}
	a.queueRemoteCatalogSync("backup delete queued")
	a.emitStateUpdated()
	a.startBackupDeleteWorker()
	a.enqueueBackupDelete(queuedBackupDelete{GameID: gameID, Filename: filename})
	return nil
}

// ExportAppBackup exports the local app state as a JSON backup file.
func (a *App) ExportAppBackup() error {
	if err := a.ensureReady(); err != nil {
		return err
	}

	data, err := a.store.ExportState()
	if err != nil {
		return fmt.Errorf(msgExportAppBackupFailed, err)
	}

	savePath, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           titleChooseBackupSavePath,
		DefaultFilename: "gamesync_backup.json",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: labelJSONBackupFile, Pattern: "*.json"},
		},
	})
	if err != nil || savePath == "" {
		return nil
	}

	if err := os.WriteFile(savePath, data, 0o644); err != nil {
		return fmt.Errorf(msgWriteBackupFileFailed, err)
	}

	return nil
}

func (a *App) ImportAppBackup() error {
	if err := a.ensureReady(); err != nil {
		return err
	}

	filePath, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: titleChooseBackupFile,
		Filters: []wailsruntime.FileFilter{
			{DisplayName: labelJSONBackupFile, Pattern: "*.json"},
		},
	})
	if err != nil || filePath == "" {
		return nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf(msgReadBackupFileFailed, err)
	}

	if err := a.store.ImportState(data); err != nil {
		return err
	}
	a.queueRemoteCatalogSync("import backup")

	return nil
}

// IsFirstLaunch reports whether the app has no games and no accounts yet.
func (a *App) IsFirstLaunch() bool {
	if a.store == nil {
		return true
	}
	return a.store.IsFirstLaunch()
}
