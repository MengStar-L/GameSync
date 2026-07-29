package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	ctx                context.Context
	store              *core.Store
	engine             *core.Engine
	recoveryPassword   string
	baseDir            string
	savedWindowState   windowState
	windowStateLoaded  bool
	tray               trayController
	windowIconMu       sync.Mutex
	windowIconCleanup  func()
	catalogSyncMu      sync.Mutex
	catalogSyncTimer   *time.Timer
	catalogRetryTimer  *time.Timer
	catalogSyncActive  bool
	catalogSyncQueued  bool
	lastSyncError      string
	lastSyncErrorAt    time.Time
	coverRetryMu       sync.Mutex
	coverRetryTimers   map[string]*time.Timer
	coverRetryAttempts map[string]int
	coverRetryDelayFn  func(int) time.Duration
	coverRetryStopped  bool
	runtimeEventFn     func(string, any)
	deleteQueueOnce    sync.Once
	deleteGameMu       sync.Mutex
	deleteGameQueue    chan queuedGameDelete
	deleteGamePending  map[string]queuedGameDelete
	backupUploadOnce   sync.Once
	backupUploadMu     sync.Mutex
	backupUploadQueue  chan queuedBackupUpload
	backupUploadSet    map[string]queuedBackupUpload
	backupDeleteOnce   sync.Once
	backupDeleteMu     sync.Mutex
	backupDeleteQueue  chan queuedBackupDelete
	backupDeleteSet    map[string]queuedBackupDelete
	syncCoordinatorMu  sync.Mutex
	syncInfraMu        sync.Mutex
	deviceIndex        *core.DeviceIndexStore
	saveChangeTracker  *core.SaveChangeTracker
	syncGameMu         sync.Mutex
	syncGameLocks      map[string]*sync.Mutex
	switchStorageMu    sync.Mutex
	switchStorageBusy  bool
	remoteOpsMu        sync.RWMutex
	handoffMu          sync.Mutex
	catalogStoreFn     func(core.CloudflareAccount) (core.CatalogStore, error)
	objectStoreFn      func(context.Context, core.CloudflareAccount) (core.ObjectStore, error)
	verifyStorageFn    func(context.Context, core.CloudflareAccount) (core.CloudflareAccount, error)
}

type queuedGameDelete struct {
	GameID   string
	GameName string
}

type queuedBackupDelete struct {
	GameID   string
	BackupID string
}

type queuedBackupUpload struct {
	GameID    string
	BackupID  string
	AccountID string
}

func queuedBackupUploadKey(gameID string, backupID string) string {
	return strings.TrimSpace(gameID) + "::" + strings.TrimSpace(backupID)
}

func queuedBackupDeleteKey(gameID string, backupID string) string {
	return strings.TrimSpace(gameID) + "::" + strings.TrimSpace(backupID)
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
	msgCoverSyncFailed                  = "存档已同步，但封面同步失败: %v"
	msgCoverSyncing                     = "正在同步游戏封面..."
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
	msgStorageSwitchBusy                = "存储切换正在进行中，请稍候"
	msgStorageSwitchTargetRequired      = "请选择一个已有连接或填写一个新连接"
	msgStorageSwitchSameProvider        = "目标连接必须使用另一种存储方式"
	msgStorageSwitchVerifying           = "正在验证新存储账号的连通性与可写性..."
	msgStorageSwitchVerifyFailed        = "新存储账号验证失败: %s"
	msgStorageSwitchAccount             = "正在切换主账号并重置游戏同步锚点..."
	msgStorageSwitchCatalog             = "正在初始化新云端并上传目录..."
	msgStorageSwitchCatalogFailed       = "本地已切换为新存储账号，但目录上传失败: %v；可稍后在游戏库手动同步补传"
	msgStorageSwitchUploading           = "正在同步「%s」的存档 (%d/%d)..."
	msgStorageSwitchDone                = "存储切换完成"
	msgStorageSwitchDoneWithFailures    = "存储切换完成，但以下游戏未完成首次同步：%s；可稍后在游戏库继续同步"
	msgWebdavInitialPullRequired        = "WebDAV 云端目录尚未完成首次拉取；本地内容已保留，将在连接恢复后重试"
	msgWebdavDifferentNamespace         = "已接入一个 WebDAV 同步空间；如需更改服务器地址或根目录，请使用“切换存储方式”"
)

func NewApp() *App {
	return &App{
		engine:             core.NewEngine(),
		deleteGameQueue:    make(chan queuedGameDelete, 16),
		deleteGamePending:  make(map[string]queuedGameDelete),
		backupUploadQueue:  make(chan queuedBackupUpload, 32),
		backupUploadSet:    make(map[string]queuedBackupUpload),
		backupDeleteQueue:  make(chan queuedBackupDelete, 32),
		backupDeleteSet:    make(map[string]queuedBackupDelete),
		syncGameLocks:      make(map[string]*sync.Mutex),
		coverRetryTimers:   make(map[string]*time.Timer),
		coverRetryAttempts: make(map[string]int),
		coverRetryDelayFn:  coverRetryDelay,
		catalogStoreFn:     newCatalogStore,
		objectStoreFn:      newObjectStore,
		verifyStorageFn:    verifyStorageAccount,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := a.ensureReady(); err != nil {
		wailsruntime.LogErrorf(ctx, "startup failed: %v", err)
		return
	}
	if err := a.startSyncTracking(); err != nil {
		wailsruntime.LogErrorf(ctx, "start save change tracking failed: %v", err)
	}
	a.startDeleteWorker()
	a.startBackupUploadWorker()
	a.startBackupDeleteWorker()
	if err := a.restoreWindowState(); err != nil {
		wailsruntime.LogErrorf(ctx, "restore window state failed: %v", err)
	}
	if err := a.startTray(); err != nil {
		wailsruntime.LogErrorf(ctx, "start tray failed: %v", err)
	}
	if a.store != nil {
		state := a.store.Snapshot()
		needsInitialWebdavPull := false
		if primary, err := findPrimaryAccount(state); err == nil {
			needsInitialWebdavPull = core.AccountProvider(primary) == core.ProviderWebdav && !state.CatalogSync.InitialPullCompleted
		}
		if a.store.HasPendingCatalogSync() || needsInitialWebdavPull {
			a.queueRemoteCatalogSync("startup pending")
		}
	}
	// 上传/删除队列为纯内存队列，重启后从注册表重入未完成任务（M9）
	go a.requeuePendingBackupOperations()
	go a.verifyAccounts(false)
}

// requeuePendingBackupOperations 启动时扫描全部游戏的 BackupRegistry：
// pending_upload 重新入上传队列；pending_delete 与 DeleteRetryAt 已到期的
// delete_failed 重新入删除队列。
func (a *App) requeuePendingBackupOperations() {
	if a.store == nil {
		return
	}
	now := time.Now()
	type uploadTask struct{ gameID, backupID, accountID string }
	type deleteTask struct{ gameID, backupID string }
	uploads := make([]uploadTask, 0)
	deletes := make([]deleteTask, 0)
	for _, game := range a.store.Snapshot().Games {
		gameCopy := game
		changed := false
		for _, rawRecord := range gameCopy.BackupRegistry {
			record := normalizeBackupRecord(rawRecord)
			if record.DeletedAt != nil || record.Filename == "" {
				continue
			}
			switch record.Status {
			case core.BackupStatusPendingUpload:
				if record.AccountID != "" {
					uploads = append(uploads, uploadTask{gameCopy.ID, core.BackupRecordID(record), record.AccountID})
				}
			case core.BackupStatusPendingDelete:
				deletes = append(deletes, deleteTask{gameCopy.ID, core.BackupRecordID(record)})
			case core.BackupStatusDeleteFailed:
				if record.DeleteRetryAt != nil && !record.DeleteRetryAt.After(now) {
					record.Status = core.BackupStatusPendingDelete
					record.PendingDelete = true
					record.DeleteRetryAt = nil
					upsertBackupRecord(&gameCopy, record)
					changed = true
					deletes = append(deletes, deleteTask{gameCopy.ID, core.BackupRecordID(record)})
				}
			}
		}
		if changed {
			// 状态先落盘再入队，避免 worker 读到旧状态直接跳过
			if _, err := a.store.UpsertGame(gameCopy); err != nil {
				wailsruntime.LogErrorf(a.ctx, "requeue backup delete state persist failed for %s: %v", gameCopy.ID, err)
				continue
			}
		}
	}
	for _, task := range uploads {
		a.enqueueBackupUpload(queuedBackupUpload{GameID: task.gameID, BackupID: task.backupID, AccountID: task.accountID})
	}
	for _, task := range deletes {
		a.enqueueBackupDelete(queuedBackupDelete{GameID: task.gameID, BackupID: task.backupID})
	}
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
	a.closeSyncTracking()
	a.stopCoverRetries()
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
	finish, err := a.beginLocalAccountMutation()
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	state := a.store.Snapshot()
	if core.AccountProvider(account) == core.ProviderWebdav {
		normalized, normalizeErr := core.NormalizeWebdavAccount(account)
		if normalizeErr != nil {
			finish()
			return core.DashboardSnapshot{}, normalizeErr
		}
		account = normalized
		if len(state.Accounts) > 0 {
			matchingNamespace := false
			for _, existing := range state.Accounts {
				if core.AccountProvider(existing) == core.ProviderWebdav && existing.ID == account.ID {
					matchingNamespace = true
					break
				}
			}
			if !matchingNamespace {
				finish()
				return core.DashboardSnapshot{}, errors.New(msgWebdavDifferentNamespace)
			}
		}
	} else if primary, ok := a.store.PrimaryAccount(); ok && core.AccountProvider(primary) == core.ProviderWebdav {
		matchingAccount := false
		for _, existing := range state.Accounts {
			if existing.ID == strings.TrimSpace(account.ID) {
				matchingAccount = true
				break
			}
		}
		if !matchingAccount {
			finish()
			return core.DashboardSnapshot{}, errors.New(msgWebdavDifferentNamespace)
		}
	}
	firstWebdavAccount := len(state.Accounts) == 0 && core.AccountProvider(account) == core.ProviderWebdav
	if _, err := a.store.UpsertAccount(account); err != nil {
		finish()
		return core.DashboardSnapshot{}, err
	}
	if firstWebdavAccount {
		if err := a.store.ResetCatalogInitialPull(); err != nil {
			finish()
			return core.DashboardSnapshot{}, err
		}
	}
	finish()
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
	if migration := a.store.Snapshot().StorageMigration; migration != nil && migration.ConflictGameID == "" {
		go func(transactionID string) {
			if _, err := a.ResumeStorageMigration(core.StorageMigrationResumeRequest{TransactionID: transactionID}); err != nil {
				wailsruntime.LogErrorf(a.ctx, "resume storage migration after recovery password update failed: %v", err)
			}
		}(migration.TransactionID)
	}
	return a.snapshot()
}

func (a *App) RestoreFromPrimary(password string) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	defer finish()
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
	catalogStore, err := a.catalogStoreFor(primary)
	if err != nil {
		_ = a.store.SetRecoveryStatus(func(status *core.RecoveryStatus) {
			status.LastRecoveryError = err.Error()
		})
		return core.DashboardSnapshot{}, err
	}
	catalog, encrypted, err := catalogStore.LoadRemoteCatalog(a.syncContext())
	if err != nil {
		_ = a.store.SetRecoveryStatus(func(status *core.RecoveryStatus) {
			status.LastRecoveryError = err.Error()
		})
		return core.DashboardSnapshot{}, err
	}
	catalog, encrypted = normalizeRemoteCatalogForMerge(catalog, encrypted)

	catalog, failures := prepareCatalogForOrdinaryMerge(a.store.Snapshot(), catalog, encrypted, password)
	if len(failures) > 0 {
		_ = a.store.SetRecoveryStatus(func(status *core.RecoveryStatus) {
			status.LastRecoveryError = msgRecoveryPasswordDecryptFailed
		})
	}

	if err := a.store.MergeRemoteCatalog(catalog); err != nil {
		return core.DashboardSnapshot{}, err
	}
	if err := a.store.MarkCatalogInitialPullCompleted(); err != nil {
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
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	defer finish()

	account, err := findAccount(a.store.Snapshot(), accountID)
	if err != nil {
		return core.DashboardSnapshot{}, err
	}

	verifiedAccount, _ := verifyStorageAccount(a.syncContext(), account)
	verificationState := "valid"
	if verifiedAccount.LastError != "" {
		verificationState = "invalid"
	}
	// 只回写验证结果字段（M2）；验证结果不改动目录配置，也无需触发目录推送
	if err := a.store.UpdateAccountVerification(
		account.ID,
		verificationState,
		verifiedAccount.LastVerifiedAt,
		verifiedAccount.LastError,
		verifiedAccount.UsageWarning,
		verifiedAccount.UsedBytes,
		verifiedAccount.TokenExpiresAt,
		verifiedAccount.CredentialsBackedUp,
	); err != nil {
		return core.DashboardSnapshot{}, err
	}
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
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	defer finish()
	if err := a.store.DeleteAccount(accountID); err != nil {
		return core.DashboardSnapshot{}, err
	}
	a.queueRemoteCatalogSync("account delete")
	return a.snapshot()
}

// emitStorageSwitchProgress 广播存储切换进度事件（契约：storage:switch_progress）。
// upload 阶段 current/total 为游戏序号/总数，其余阶段为 0/0。
func (a *App) emitStorageSwitchProgress(stage string, message string, current int, total int) {
	a.emitRuntimeEvent("storage:switch_progress", map[string]any{
		"stage":   stage,
		"message": message,
		"current": current,
		"total":   total,
	})
}

// SwitchStoragePrimary 运行时切换存储方式：验证目标账号 → 旧账号全部停用（保留记录）→
// 目标账号入主并重挂全部游戏、清零同步锚点 → 同步目录与每游戏存档到目标云端 →
// 返回新快照。前置的"先同步旧云端"由前端负责，这里不做旧云端同步。
// 失败语义：验证阶段失败无任何改动；账号切换落盘后的目录/存档上传失败不回滚，
// 由 dirty 标记与用户手动同步自愈。
func (a *App) legacySwitchStoragePrimary(request core.StorageSwitchRequest) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}

	// 并发重入闸：切换期间拒绝再次进入
	a.switchStorageMu.Lock()
	if a.switchStorageBusy {
		a.switchStorageMu.Unlock()
		return core.DashboardSnapshot{}, errors.New(msgStorageSwitchBusy)
	}
	a.switchStorageBusy = true
	a.switchStorageMu.Unlock()
	defer func() {
		a.switchStorageMu.Lock()
		a.switchStorageBusy = false
		a.switchStorageMu.Unlock()
	}()

	account, err := resolveStorageSwitchTarget(a.store.Snapshot(), request)
	if err != nil {
		return core.DashboardSnapshot{}, err
	}

	// 第 1 步 verify：校验新账号连通/可写，失败即返回，全程无副作用
	a.emitStorageSwitchProgress("verify", msgStorageSwitchVerifying, 0, 0)
	verifiedAccount, verifyErr := verifyStorageAccount(a.syncContext(), account)
	if verifiedAccount.LastError != "" {
		return core.DashboardSnapshot{}, fmt.Errorf(msgStorageSwitchVerifyFailed, verifiedAccount.LastError)
	}
	if verifyErr != nil {
		return core.DashboardSnapshot{}, fmt.Errorf(msgStorageSwitchVerifyFailed, verifyErr)
	}
	verifiedAccount.ID = account.ID
	verifiedAccount.VerificationState = "valid"

	// 第 2 步 account：store 层一次锁内原子完成旧账号停用、新账号入主、游戏重挂与锚点清零
	a.emitStorageSwitchProgress("account", msgStorageSwitchAccount, 0, 0)
	newAccount, err := a.store.SwitchPrimaryStorage(verifiedAccount)
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	a.emitStateUpdated()

	// 第 3 步 catalog：新账号建 schema 后走既有收敛环把目录推上新云端
	// （findPrimaryAccount 此时已解析到新主账号）
	a.emitStorageSwitchProgress("catalog", msgStorageSwitchCatalog, 0, 0)
	catalogStore, err := newCatalogStore(newAccount)
	if err == nil {
		err = catalogStore.EnsureSchema(a.syncContext())
	}
	if err == nil {
		if markErr := a.store.MarkCatalogDirty(); markErr != nil {
			wailsruntime.LogErrorf(a.ctx, "mark catalog dirty during storage switch failed: %v", markErr)
		}
		err = a.syncRemoteCatalog()
	}
	if err != nil {
		// 不回滚：本地已是新配置，目录保持 dirty，由后台重试/手动同步自愈
		_ = a.store.MarkCatalogSyncFailed(err.Error())
		return core.DashboardSnapshot{}, fmt.Errorf(msgStorageSwitchCatalogFailed, err)
	}

	// 第 4 步 upload：逐个游戏走普通同步（目标端为空时上传，已有数据时沿用冲突规则）；
	// 单游戏失败记录继续，最后失败清单入 done 消息
	failures := a.syncGamesOnNewStorage()
	// 本地 zip 仍存在的备份记录改挂新账号并重新入上传队列（复用启动重入机制）
	a.repointLocalBackupsToAccount(newAccount.ID)

	// 第 5 步 done
	doneMessage := msgStorageSwitchDone
	if len(failures) > 0 {
		doneMessage = fmt.Sprintf(msgStorageSwitchDoneWithFailures, strings.Join(failures, "、"))
	}
	a.emitStorageSwitchProgress("done", doneMessage, 0, 0)
	return a.snapshot()
}

// syncGamesOnNewStorage 对每个 savePath 非空且启用同步的游戏复用 RunSync，
// 内部含 lockGameSync 与 SyncGameWithGateway；返回失败或冲突游戏名清单。
func (a *App) syncGamesOnNewStorage() []string {
	state := a.store.Snapshot()
	targets := make([]core.Game, 0, len(state.Games))
	for _, game := range state.Games {
		if strings.TrimSpace(game.SavePath) == "" || !game.Sync.Enabled {
			continue
		}
		targets = append(targets, game)
	}
	total := len(targets)
	var failures []string
	for index, game := range targets {
		a.emitStorageSwitchProgress("upload", fmt.Sprintf(msgStorageSwitchUploading, game.Name, index+1, total), index+1, total)
		snapshot, err := a.RunSync(core.SyncRunRequest{GameID: game.ID})
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s（%v）", game.Name, err))
			continue
		}
		if synced, findErr := findGame(snapshot.State, game.ID); findErr == nil && synced.LastSync != nil {
			switch synced.LastSync.Status {
			case "success":
				// 已完成。
			case "conflict":
				failures = append(failures, fmt.Sprintf("%s（存在同步冲突）", game.Name))
			default:
				message := strings.TrimSpace(synced.LastSync.Message)
				if message == "" {
					message = synced.LastSync.Status
				}
				failures = append(failures, fmt.Sprintf("%s（%s）", game.Name, message))
			}
		}
	}
	return failures
}

// repointLocalBackupsToAccount 把 BackupRegistry 中本地 zip 仍存在的备份记录改挂
// 新账号并置为 pending_upload，随后复用 requeuePendingBackupOperations 重新入上传队列。
// 带删除意图或本地 zip 已缺失的记录保持原状（仅保留历史）。
func (a *App) repointLocalBackupsToAccount(accountID string) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return
	}
	for _, game := range a.store.Snapshot().Games {
		gameCopy := game
		changed := false
		for _, rawRecord := range gameCopy.BackupRegistry {
			record := normalizeBackupRecord(rawRecord)
			if record.Filename == "" || record.DeletedAt != nil {
				continue
			}
			if record.Status == core.BackupStatusPendingDelete || record.Status == core.BackupStatusDeleteFailed {
				continue
			}
			localPath := filepath.Join(a.store.DataDir(), "backups", gameCopy.ID, record.Filename)
			if _, statErr := os.Stat(localPath); statErr != nil {
				continue
			}
			record.AccountID = accountID
			record.Status = core.BackupStatusPendingUpload
			record.LastError = ""
			upsertBackupRecord(&gameCopy, record)
			changed = true
		}
		if changed {
			// 状态先落盘再入队，避免 worker 读到旧状态直接跳过（与启动重入同一纪律）
			if _, err := a.store.UpsertGame(gameCopy); err != nil {
				wailsruntime.LogErrorf(a.ctx, "repoint backups to new storage failed for %s: %v", gameCopy.ID, err)
			}
		}
	}
	a.requeuePendingBackupOperations()
}

func (a *App) SaveGame(game core.Game) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	defer finish()
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
	storedGame, err := a.store.UpsertGame(game)
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	if err := a.refreshGameSyncTracking(storedGame, false); err != nil {
		if a.ctx != nil {
			wailsruntime.LogWarningf(a.ctx, "refresh save tracking after game save failed: %v", err)
		}
	}
	a.updateCoverIndexForGame(storedGame)
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
		finish, err := a.beginRemoteOperation()
		if err != nil {
			return "", err
		}
		defer finish()
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
		game.CoverUpdatedAt = existing.CoverUpdatedAt
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
	if existing != nil && shouldReuseExistingCoverReference(state, *game, *existing) {
		game.CoverCloudAccountID = existing.CoverCloudAccountID
		game.CoverCloudKey = existing.CoverCloudKey
		return "", nil
	}

	accountID, objectKey, warning := a.tryUploadCoverToCloud(state, *game, localPath)
	if accountID != "" && objectKey != "" {
		game.CoverCloudAccountID = accountID
		game.CoverCloudKey = objectKey
		if existing == nil || existing.CoverCloudAccountID != accountID || existing.CoverCloudKey != objectKey {
			game.CoverUpdatedAt = time.Now()
		}
		_ = a.writeCoverCacheMetadata(*game, localPath)
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

func shouldReuseExistingCoverReference(state core.AppState, game, existing core.Game) bool {
	if strings.TrimSpace(existing.CoverSourceType) != strings.TrimSpace(game.CoverSourceType) ||
		strings.TrimSpace(existing.CoverSource) != strings.TrimSpace(game.CoverSource) ||
		strings.TrimSpace(existing.CoverCloudAccountID) == "" ||
		strings.TrimSpace(existing.CoverCloudKey) == "" {
		return false
	}
	target, ok := selectCoverStorageAccount(state, game)
	if ok && target.ID != strings.TrimSpace(existing.CoverCloudAccountID) {
		return false
	}
	expectedHash := coverFingerprintFromObjectKey(existing.CoverCloudKey)
	if expectedHash == "" || strings.TrimSpace(game.CoverLocalPath) == "" {
		return false
	}
	actualHash, err := sha256FileHex(game.CoverLocalPath)
	return err == nil && strings.EqualFold(actualHash, expectedHash)
}

func (a *App) syncGameCover(state core.AppState, game core.Game) error {
	if strings.TrimSpace(game.CoverPath) == "" &&
		strings.TrimSpace(game.CoverSource) == "" &&
		strings.TrimSpace(game.CoverLocalPath) == "" {
		return nil
	}
	updated := game
	if strings.TrimSpace(updated.CoverPath) == "" {
		updated.CoverPath = firstNonEmpty(updated.CoverSource, updated.CoverLocalPath)
	}
	warning, err := a.prepareAndPersistCover(&updated, &game, state)
	if err != nil {
		return err
	}
	if strings.TrimSpace(warning) != "" {
		return errors.New(warning)
	}
	_, err = a.store.UpsertGame(updated)
	return err
}

func (a *App) resolveGameCoverSource(game core.Game) (string, error) {
	if strings.TrimSpace(game.CoverPath) == "" &&
		strings.TrimSpace(game.CoverLocalPath) == "" &&
		strings.TrimSpace(game.CoverCloudKey) == "" {
		return "", nil
	}

	var lastErr error
	if source, found, err := a.resolveCachedGameCover(game); found {
		return source, nil
	} else if err != nil {
		lastErr = err
	}

	if localSource := normalizeLocalCoverPath(firstNonEmpty(game.CoverSource, game.CoverPath)); localSource != "" && !isDirectCoverSource(localSource) && !isCoverReference(localSource) {
		if localPath, mimeType, err := a.copyLocalCoverToCache(game.ID, localSource); err == nil {
			_ = a.writeCoverCacheMetadata(game, localPath)
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

	if strings.EqualFold(strings.TrimSpace(game.CoverSourceType), coverSourceRemoteURL) {
		localPath, mimeType, err := a.downloadRemoteCoverToCache(game.ID, firstNonEmpty(game.CoverSource, game.CoverPath))
		if err == nil {
			_ = a.writeCoverCacheMetadata(game, localPath)
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

	accountID, objectKey := coverCloudLocation(game)
	if accountID != "" && objectKey != "" {
		finish, err := a.beginRemoteOperation()
		if err != nil {
			return "", err
		}
		defer finish()

		if current, findErr := findGame(a.store.Snapshot(), game.ID); findErr == nil {
			game = current
		}
		if source, found, cacheErr := a.resolveCachedGameCover(game); found {
			return source, nil
		} else if cacheErr != nil {
			lastErr = cacheErr
		}
		accountID, objectKey = coverCloudLocation(game)
		if accountID == "" || objectKey == "" {
			if lastErr != nil {
				return "", lastErr
			}
			return "", nil
		}
		localPath, mimeType, err := a.downloadCloudCoverToCache(game.ID, accountID, objectKey)
		if err == nil {
			_ = a.writeCoverCacheMetadata(game, localPath)
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

func (a *App) resolveCachedGameCover(game core.Game) (string, bool, error) {
	cachedPath := a.locateCoverCache(game)
	if cachedPath == "" {
		return "", false, nil
	}
	fresh, repairMetadata := coverCacheFreshnessForGame(game, cachedPath)
	if !fresh {
		return "", false, errors.New("cover cache is stale")
	}
	if repairMetadata {
		if err := a.writeCoverCacheMetadata(game, cachedPath); err != nil && a.ctx != nil {
			wailsruntime.LogWarningf(a.ctx, "repair cover cache metadata failed for %s: %v", game.ID, err)
		}
	}
	data, err := os.ReadFile(cachedPath)
	if err != nil {
		return "", false, err
	}
	a.persistResolvedCoverCache(game.ID, cachedPath, game.CoverMimeType)
	return dataURLForBytes(cachedPath, data), true, nil
}

func coverCacheFreshnessForGame(game core.Game, cachedPath string) (fresh bool, repairMetadata bool) {
	if strings.TrimSpace(cachedPath) == "" {
		return false, false
	}
	if accountID, objectKey := coverCloudLocation(game); accountID != "" && objectKey != "" {
		expectedHash := coverFingerprintFromObjectKey(objectKey)
		content, err := os.ReadFile(cachedPath + ".json")
		if err == nil {
			var metadata coverCacheMetadata
			if json.Unmarshal(content, &metadata) == nil &&
				metadata.AccountID == accountID && metadata.ObjectKey == objectKey &&
				metadata.CoverUpdatedAt.Equal(game.CoverUpdatedAt) {
				if metadata.SHA256 == "" {
					if expectedHash == "" {
						return true, false
					}
				} else {
					hash, hashErr := sha256FileHex(cachedPath)
					if hashErr == nil && strings.EqualFold(hash, metadata.SHA256) &&
						(expectedHash == "" || strings.EqualFold(hash, expectedHash)) {
						return true, false
					}
				}
			}
		}
		if expectedHash != "" {
			hash, hashErr := sha256FileHex(cachedPath)
			matched := hashErr == nil && strings.EqualFold(hash, expectedHash)
			return matched, matched
		}
		if !strings.Contains(filepath.Base(objectKey), "cover.") {
			return false, false
		}
	}
	if strings.TrimSpace(game.CoverSourceType) != coverSourceRemoteURL && game.CoverUpdatedAt.IsZero() {
		return true, false
	}
	if game.CoverUpdatedAt.IsZero() {
		return true, false
	}
	info, err := os.Stat(cachedPath)
	if err != nil {
		return false, false
	}
	return !info.ModTime().Before(game.CoverUpdatedAt.Add(-5 * time.Second)), false
}

func (a *App) DeleteGame(gameID string) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	defer finish()

	state := a.store.Snapshot()
	if err := a.store.DeleteGame(gameID); err != nil {
		return core.DashboardSnapshot{}, err
	}
	a.removeGameSyncTracking(gameID)
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

func (a *App) enqueueGameDelete(request queuedGameDelete) {
	request.GameID = strings.TrimSpace(request.GameID)
	if request.GameID == "" {
		return
	}
	a.deleteGameMu.Lock()
	if _, exists := a.deleteGamePending[request.GameID]; exists {
		a.deleteGameMu.Unlock()
		return
	}
	a.deleteGamePending[request.GameID] = request
	a.deleteGameMu.Unlock()
	a.deleteGameQueue <- request
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
	finishRemote, remoteErr := a.beginRemoteOperation()
	if remoteErr != nil {
		a.finishQueuedGameDelete(request.GameID)
		a.emitRuntimeEvent("game:delete_failed", map[string]string{"id": request.GameID, "error": remoteErr.Error(), "stage": "handoff"})
		time.AfterFunc(30*time.Second, func() { a.enqueueGameDelete(request) })
		return
	}
	defer finishRemote()

	stateBeforeDelete := a.store.Snapshot()
	if err := a.store.DeleteGame(request.GameID); err != nil {
		a.finishQueuedGameDelete(request.GameID)
		a.emitRuntimeEvent("game:delete_failed", map[string]string{
			"id":    request.GameID,
			"error": err.Error(),
			"stage": "local_delete",
		})
		// 删除失败恢复：推送最新快照让前端把乐观移除的条目复原（B1）
		a.emitStateUpdated()
		return
	}
	a.removeGameSyncTracking(request.GameID)

	a.queueRemoteCatalogSync("game delete")
	a.emitRuntimeEvent("game:delete_succeeded", map[string]string{
		"id": request.GameID,
	})
	// 本地删除成功必须刷新前端快照，否则已删游戏在下次重渲染时以幽灵条目复现（M6）
	a.emitStateUpdated()

	if err := a.cleanupDeletedGameRemote(stateBeforeDelete, request.GameID); err != nil {
		wailsruntime.LogErrorf(a.ctx, "cleanup deleted game %s failed: %v", request.GameID, err)
		a.emitRuntimeEvent("game:delete_failed", map[string]string{
			"id":    request.GameID,
			"error": err.Error(),
			"stage": "remote_cleanup",
		})
		a.emitStateUpdated()
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
	request.BackupID = strings.TrimSpace(request.BackupID)
	request.AccountID = strings.TrimSpace(request.AccountID)
	if request.GameID == "" || request.BackupID == "" || request.AccountID == "" {
		return
	}
	key := queuedBackupUploadKey(request.GameID, request.BackupID)
	a.backupUploadMu.Lock()
	if _, exists := a.backupUploadSet[key]; exists {
		a.backupUploadMu.Unlock()
		return
	}
	a.backupUploadSet[key] = request
	a.backupUploadMu.Unlock()
	a.backupUploadQueue <- request
}

func (a *App) finishQueuedBackupUpload(gameID string, backupID string) {
	a.backupUploadMu.Lock()
	delete(a.backupUploadSet, queuedBackupUploadKey(gameID, backupID))
	a.backupUploadMu.Unlock()
}

func (a *App) runBackupUploadWorker() {
	for request := range a.backupUploadQueue {
		a.processQueuedBackupUpload(request)
	}
}

func (a *App) processQueuedBackupUpload(request queuedBackupUpload) {
	defer a.finishQueuedBackupUpload(request.GameID, request.BackupID)

	if err := a.ensureReady(); err != nil {
		return
	}
	finishRemote, remoteErr := a.beginRemoteOperation()
	if remoteErr != nil {
		time.AfterFunc(30*time.Second, func() { a.enqueueBackupUpload(request) })
		return
	}
	defer finishRemote()
	game, err := findGame(a.store.Snapshot(), request.GameID)
	if err != nil {
		return
	}
	record, _, ok := findBackupRecord(game, request.BackupID)
	if !ok || record.DeletedAt != nil {
		return
	}
	record = normalizeBackupRecord(record)
	if record.Status != core.BackupStatusPendingUpload {
		return
	}

	backupPath := core.ExistingBackupLocalPathForRecord(a.store.DataDir(), game.ID, record)
	info, statErr := os.Stat(backupPath)
	if statErr != nil {
		a.markBackupUploadFailed(game, request.BackupID, statErr.Error())
		return
	}

	account, err := findAccount(a.store.Snapshot(), request.AccountID)
	if err != nil {
		a.markBackupUploadFailed(game, request.BackupID, err.Error())
		return
	}
	state := a.store.Snapshot()
	primaryAccount, err := findPrimaryAccount(state)
	if err != nil {
		a.markBackupUploadFailed(game, request.BackupID, err.Error())
		return
	}
	gateway, err := newSplitStorageGateway(a.syncContext(), primaryAccount, account)
	if err != nil {
		a.markBackupUploadFailed(game, request.BackupID, err.Error())
		return
	}

	remoteKey := core.BackupObjectKeyForRecord(game.ID, record)
	if err := gateway.Objects.PutObjectFromFile(a.syncContext(), remoteKey, backupPath); err != nil {
		a.markBackupUploadFailed(game, request.BackupID, err.Error())
		return
	}
	if record.Type == "auto" {
		core.NewBackupManager(a.engine).CleanupCloudAutoBackups(a.syncContext(), gateway, game.ID, record.SourceDeviceID, record.Filename)
	}

	record.AccountID = request.AccountID
	record.ObjectKey = remoteKey
	if record.CreatedAt.IsZero() {
		record.CreatedAt = info.ModTime()
	}
	record.Status = core.BackupStatusReady
	record.PendingDelete = false
	record.LastError = ""
	record.LastDeleteError = ""
	record.DeleteRetryAt = nil
	upsertBackupRecord(&game, record)
	if record.Type == "auto" {
		// 新 auto 备份此刻才真正 ready：清理更旧的 auto 记录与本地旧 zip（M9）
		cleanupOlderAutoBackupArtifacts(&game, record, a.store.DataDir())
	}
	if record.Type != "auto" {
		game.BackupStorageAccountID = request.AccountID
	}
	if _, err := a.store.UpsertGame(game); err != nil {
		wailsruntime.LogErrorf(a.ctx, "persist backup upload success %s/%s failed: %v", request.GameID, request.BackupID, err)
		return
	}
	a.queueRemoteCatalogSync("backup upload success")
	a.emitStateUpdated()
	a.emitRuntimeEvent("game:backup_upload_succeeded", map[string]string{
		"id":       request.GameID,
		"filename": record.Filename,
		"backupId": request.BackupID,
	})
	if record.Type == "auto" {
		a.emitRuntimeEvent("game:backup_success", request.GameID)
	}
}

func (a *App) enqueueBackupDelete(request queuedBackupDelete) {
	request.GameID = strings.TrimSpace(request.GameID)
	request.BackupID = strings.TrimSpace(request.BackupID)
	if request.GameID == "" || request.BackupID == "" {
		return
	}
	key := queuedBackupDeleteKey(request.GameID, request.BackupID)
	a.backupDeleteMu.Lock()
	if _, exists := a.backupDeleteSet[key]; exists {
		a.backupDeleteMu.Unlock()
		return
	}
	a.backupDeleteSet[key] = request
	a.backupDeleteMu.Unlock()
	a.backupDeleteQueue <- request
}

func (a *App) finishQueuedBackupDelete(gameID string, backupID string) {
	a.backupDeleteMu.Lock()
	delete(a.backupDeleteSet, queuedBackupDeleteKey(gameID, backupID))
	a.backupDeleteMu.Unlock()
}

func (a *App) runBackupDeleteWorker() {
	for request := range a.backupDeleteQueue {
		a.processQueuedBackupDelete(request)
	}
}

func (a *App) processQueuedBackupDelete(request queuedBackupDelete) {
	defer a.finishQueuedBackupDelete(request.GameID, request.BackupID)

	if err := a.ensureReady(); err != nil {
		return
	}
	finishRemote, remoteErr := a.beginRemoteOperation()
	if remoteErr != nil {
		time.AfterFunc(30*time.Second, func() { a.enqueueBackupDelete(request) })
		return
	}
	defer finishRemote()
	game, err := findGame(a.store.Snapshot(), request.GameID)
	if err != nil {
		return
	}
	record, _, ok := findBackupRecord(game, request.BackupID)
	if !ok || record.DeletedAt != nil {
		return
	}
	record = normalizeBackupRecord(record)
	if record.Status != core.BackupStatusPendingDelete {
		return
	}

	gateways, gatewayErr := a.getBackupGateways(a.ctx, game)
	if gatewayErr != nil {
		a.markBackupDeleteFailed(game, request.BackupID, gatewayErr.Error())
		return
	}
	bm := core.NewBackupManager(a.engine)
	if err := bm.DeleteBackup(a.ctx, game, request.BackupID, a.store.DataDir(), gateways); err != nil {
		a.markBackupDeleteFailed(game, request.BackupID, err.Error())
		return
	}
	if err := a.finalizeBackupDeletion(game, request.BackupID); err != nil {
		wailsruntime.LogErrorf(a.ctx, "finalize backup deletion %s/%s failed: %v", request.GameID, request.BackupID, err)
		return
	}
	a.emitRuntimeEvent("game:backup_delete_succeeded", map[string]string{
		"id":       request.GameID,
		"filename": record.Filename,
		"backupId": request.BackupID,
	})
}

func (a *App) markBackupUploadFailed(game core.Game, backupID string, uploadErr string) {
	record, _, ok := findBackupRecord(game, backupID)
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
			"filename": record.Filename,
			"backupId": backupID,
			"error":    record.LastError,
		})
		if record.Type == "auto" {
			a.emitRuntimeEvent("game:backup_error", map[string]string{"id": game.ID, "error": record.LastError})
		}
	}
}

func (a *App) markBackupDeleteFailed(game core.Game, backupID string, deleteErr string) {
	record, _, ok := findBackupRecord(game, backupID)
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
			"filename": record.Filename,
			"backupId": backupID,
			"error":    record.LastError,
		})
	}
}

func (a *App) finalizeBackupDeletion(game core.Game, backupID string) error {
	removeBackupRecord(&game, backupID)
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
			if catalogStore, storeErr := newCatalogStore(primary); storeErr == nil {
				if clearErr := catalogStore.ClearGameRecords(a.syncContext(), gameID); clearErr != nil {
					errs = append(errs, clearErr.Error())
				}
			} else {
				errs = append(errs, storeErr.Error())
			}
		}
		if objects, err := newObjectStore(a.syncContext(), storageAccount); err == nil {
			if clearErr := objects.ClearGameFiles(a.syncContext(), game.ID); clearErr != nil {
				errs = append(errs, clearErr.Error())
			}
		}
		if gateways, gatewayErr := a.getBackupGateways(a.syncContext(), game); gatewayErr == nil {
			for _, gateway := range gateways {
				if gateway == nil || gateway.Objects == nil {
					continue
				}
				if clearErr := gateway.Objects.ClearPrefix(a.syncContext(), fmt.Sprintf("backups/%s/", game.ID)); clearErr != nil {
					errs = append(errs, clearErr.Error())
				}
			}
		}
	} else if primary, primaryErr := findPrimaryAccount(state); primaryErr == nil {
		if catalogStore, storeErr := newCatalogStore(primary); storeErr == nil {
			if clearErr := catalogStore.ClearGameRecords(a.syncContext(), gameID); clearErr != nil {
				errs = append(errs, clearErr.Error())
			}
		} else {
			errs = append(errs, storeErr.Error())
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
	gateway, err := newSplitStorageGateway(a.syncContext(), primaryAccount, coverAccount)
	if err != nil {
		return err
	}
	return gateway.Objects.ClearPrefix(a.syncContext(), fmt.Sprintf("covers/%s/", strings.TrimSpace(game.ID)))
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
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	defer finish()
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
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	defer finish()

	prefs := a.store.Snapshot().Preferences
	prefs.TagOrder = tags
	if err := a.store.SavePreferences(prefs); err != nil {
		return core.DashboardSnapshot{}, err
	}
	a.queueRemoteCatalogSync("tag order update")

	return a.snapshot()
}

func (a *App) UpdateSidebarNavOrder(items []string) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	defer finish()

	prefs := a.store.Snapshot().Preferences
	prefs.SidebarNavOrder = items
	if err := a.store.SavePreferences(prefs); err != nil {
		return core.DashboardSnapshot{}, err
	}
	a.queueRemoteCatalogSync("sidebar nav order update")

	return a.snapshot()
}

func (a *App) SavePreferences(preferences core.Preferences) (core.DashboardSnapshot, error) {
	if err := a.ensureReady(); err != nil {
		return core.DashboardSnapshot{}, err
	}
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	defer finish()
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
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	defer finish()
	return a.runSyncUnlocked(request)
}

func (a *App) runSyncUnlocked(request core.SyncRunRequest) (core.DashboardSnapshot, error) {
	result, err := a.runSyncBatch([]core.SyncRunRequest{request})
	if err != nil {
		return core.DashboardSnapshot{}, err
	}
	return result.Snapshot, nil
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
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return nil, err
	}
	defer finish()

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
	gateway, err := newSplitStorageGateway(a.syncContext(), primaryAccount, storageAccount)
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
		snapshot, syncErr := a.runSyncUnlocked(core.SyncRunRequest{
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
	if !coverSourceChanged(game, existing) && !strings.EqualFold(strings.TrimSpace(game.CoverSourceType), coverSourceLocalFile) {
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
		if cachedPath := a.locateCoverCache(game); cachedPath != "" {
			mimeType, err := detectFileMimeType(cachedPath)
			if err == nil {
				return cachedPath, mimeType, nil
			}
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

// backupRecordIsNewer 判断 candidate 是否比 current 更新：
// 优先比较存档清单世代 SourceManifestGeneratedAt（不受跨设备墙钟偏差影响），仅在双方都缺失时退回 CreatedAt。
func backupRecordIsNewer(candidate core.BackupRecord, current core.BackupRecord) bool {
	switch {
	case !candidate.SourceManifestGeneratedAt.IsZero() && !current.SourceManifestGeneratedAt.IsZero():
		if !candidate.SourceManifestGeneratedAt.Equal(current.SourceManifestGeneratedAt) {
			return candidate.SourceManifestGeneratedAt.After(current.SourceManifestGeneratedAt)
		}
		return candidate.CreatedAt.After(current.CreatedAt)
	case !candidate.SourceManifestGeneratedAt.IsZero():
		return true
	case !current.SourceManifestGeneratedAt.IsZero():
		return false
	default:
		return candidate.CreatedAt.After(current.CreatedAt)
	}
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
		if !found || backupRecordIsNewer(record, latest) {
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
	// 与同步/其它存档目录写入方互斥（M7）；本函数返回后 PrepareGameLaunch 才会调 RunSync，不会自死锁
	unlock := a.lockGameSync(game.ID)
	defer unlock()
	if a.currentManifestMatchesLatestAuto(game, record) {
		return "当前本地存档已是最新自动存档，正在启动游戏。", nil, nil
	}
	// 门2：候选备份必须能证明比 anchor 更新——用清单世代而非墙钟 CreatedAt；
	// 缺失该字段的旧记录无法证明更新，不自动恢复
	if record.SourceManifestGeneratedAt.IsZero() ||
		!record.SourceManifestGeneratedAt.After(game.Anchor.LastManifest.GeneratedAt) {
		return "", nil, nil
	}
	// 门1：本地自 anchor 后无改动才允许自动快进恢复；否则跳过预恢复，
	// 交由后续 InspectLaunchSync 正常冲突流程让用户选择
	anchorHash := strings.TrimSpace(game.Anchor.LastManifest.Hash)
	if anchorHash == "" {
		return "", nil, nil
	}
	localManifest, manifestErr := core.BuildLocalManifest(game.SavePath, game.Sync.IncludePatterns, game.Sync.ExcludePatterns)
	if manifestErr != nil || strings.TrimSpace(localManifest.Hash) != anchorHash {
		return "", nil, nil
	}
	gateways, gatewayErr := a.getBackupGateways(a.ctx, game)
	if gatewayErr != nil {
		if localPath := core.ExistingBackupLocalPathForRecord(a.store.DataDir(), game.ID, record); strings.TrimSpace(localPath) != "" {
			if _, statErr := os.Stat(localPath); statErr != nil {
				return "", nil, gatewayErr
			}
		}
	}
	bm := core.NewBackupManager(a.engine)
	// 门3：恢复前先对当前存档目录做安全备份（manual 类型，不参与 auto 清理），失败则中止恢复
	safetyName := "预恢复安全备份 " + time.Now().Format("2006-01-02 15:04:05")
	safetyBackup, safetyErr := bm.CreateBackup(a.ctx, game, "manual", safetyName, a.store.Snapshot().Device.ID, a.store.DataDir(), nil)
	if safetyErr != nil {
		return "", nil, fmt.Errorf("创建预恢复安全备份失败，已中止自动恢复: %w", safetyErr)
	}
	if safetyBackup != nil {
		// 记录进注册表以便用户可见/可回滚；失败不阻断（zip 已落盘）
		if _, persistErr := a.persistBackupRecord(game, *safetyBackup, "", core.BackupStatusReady, ""); persistErr != nil {
			wailsruntime.LogErrorf(a.ctx, "persist pre-restore safety backup record failed: %v", persistErr)
		}
	}
	if err := bm.RestoreBackup(a.ctx, game, core.BackupRecordID(record), a.store.DataDir(), gateways); err != nil {
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
	candidates := make([]string, 0, 3)
	if _, objectKey := coverCloudLocation(game); objectKey != "" {
		name := filepath.Base(filepath.FromSlash(objectKey))
		if name != "." && name != "" {
			candidates = append(candidates, filepath.Join(a.store.DataDir(), "covers", strings.TrimSpace(game.ID), name))
		}
	}
	if localPath := strings.TrimSpace(game.CoverLocalPath); localPath != "" && a.isManagedCoverCachePath(localPath, strings.TrimSpace(game.ID)) {
		candidates = append(candidates, localPath)
	}
	for _, candidate := range candidates {
		if candidate == "" || strings.EqualFold(filepath.Ext(candidate), ".json") {
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
		if strings.EqualFold(filepath.Ext(match), ".json") {
			continue
		}
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
	gateway, err := newSplitStorageGateway(a.syncContext(), primaryAccount, storageAccount)
	if err != nil {
		return "", "", err
	}
	name := filepath.Base(filepath.FromSlash(strings.TrimSpace(objectKey)))
	if name == "." || name == "" {
		name = "cover" + chooseCoverExtension(objectKey, "")
	}
	targetPath := filepath.Join(a.store.DataDir(), "covers", strings.TrimSpace(gameID), name)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", "", fmt.Errorf(msgCreateCoverCacheDirFailed, err)
	}
	if err := gateway.Objects.DownloadObjectToFile(a.syncContext(), objectKey, targetPath); err != nil {
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
	targetClean := filepath.Clean(targetPath)
	targetMetadataClean := filepath.Clean(targetPath + ".json")
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(targetPath), "cover.*")); err == nil {
		for _, match := range matches {
			cleaned := filepath.Clean(match)
			if cleaned == targetClean || cleaned == targetMetadataClean {
				continue
			}
			if strings.EqualFold(filepath.Ext(match), ".json") {
				_ = os.Remove(match)
				continue
			}
			_ = os.Remove(match)
			_ = os.Remove(match + ".json")
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
	gateway, err := a.storageGatewayFor(a.syncContext(), primaryAccount, storageAccount)
	if err != nil {
		return "", "", fmt.Sprintf(msgCoverLocalOnlyGatewayInitFailed, err)
	}
	objectKey, err := buildCoverObjectKey(game.ID, localPath)
	if err != nil {
		return "", "", fmt.Sprintf(msgCoverLocalOnlyUploadFailed, err)
	}
	if err := gateway.Objects.PutObjectFromFile(a.syncContext(), objectKey, localPath); err != nil {
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

	remoteKey, err := buildCoverObjectKey(game.ID, coverPath)
	if err != nil {
		return "", "", err
	}
	gateway, err := newSplitStorageGateway(a.syncContext(), primaryAccount, storageAccount)
	if err != nil {
		return "", "", err
	}
	if err := gateway.Objects.PutObjectFromFile(a.syncContext(), remoteKey, coverPath); err != nil {
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

	gateway, err := newSplitStorageGateway(a.syncContext(), primaryAccount, storageAccount)
	if err != nil {
		return nil, err
	}
	return gateway.Objects.GetObjectBytes(a.syncContext(), objectKey)
}

func selectCoverStorageAccount(state core.AppState, game core.Game) (core.CloudflareAccount, bool) {
	if accountID := strings.TrimSpace(game.StorageAccountID); accountID != "" {
		for _, account := range state.Accounts {
			if account.ID == accountID && hasUsableObjectAccount(account) {
				return account, true
			}
		}
	}
	for _, account := range state.Accounts {
		if hasUsableObjectAccount(account) {
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

func buildCoverObjectKey(gameID string, localPath string) (string, error) {
	hash, err := sha256FileHex(localPath)
	if err != nil {
		return "", err
	}
	ext := sanitizeCoverExtension(filepath.Ext(localPath))
	return fmt.Sprintf("covers/%s/%s%s", strings.TrimSpace(gameID), strings.ToLower(hash), ext), nil
}

type coverCacheMetadata struct {
	AccountID      string    `json:"accountId"`
	ObjectKey      string    `json:"objectKey"`
	SHA256         string    `json:"sha256"`
	CoverUpdatedAt time.Time `json:"coverUpdatedAt"`
}

func (a *App) writeCoverCacheMetadata(game core.Game, cachedPath string) error {
	accountID, objectKey := coverCloudLocation(game)
	if accountID == "" || objectKey == "" || strings.TrimSpace(cachedPath) == "" {
		return nil
	}
	hash, err := sha256FileHex(cachedPath)
	if err != nil {
		return err
	}
	content, err := json.Marshal(coverCacheMetadata{AccountID: accountID, ObjectKey: objectKey, SHA256: hash, CoverUpdatedAt: game.CoverUpdatedAt})
	if err != nil {
		return err
	}
	return os.WriteFile(cachedPath+".json", content, 0o644)
}

func sha256FileHex(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
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

func findBackupRecord(game core.Game, backupID string) (core.BackupRecord, int, bool) {
	backupID = strings.TrimSpace(backupID)
	for index, record := range game.BackupRegistry {
		if core.BackupRecordID(record) == backupID {
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
	if existing, index, ok := findBackupRecord(*game, core.BackupRecordID(record)); ok {
		if record.CreatedAt.IsZero() {
			record.CreatedAt = existing.CreatedAt
		}
		game.BackupRegistry[index] = record
	} else {
		game.BackupRegistry = append(game.BackupRegistry, record)
	}
	rebuildBackupCompatFields(game)
}

func removeBackupRecord(game *core.Game, backupID string) {
	if game == nil {
		return
	}
	backupID = strings.TrimSpace(backupID)
	next := make([]core.BackupRecord, 0, len(game.BackupRegistry))
	for _, record := range game.BackupRegistry {
		if core.BackupRecordID(record) == backupID {
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
	record.ObjectKey = strings.TrimSpace(record.ObjectKey)
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
	// pull-merge-repush 收敛环（M5/M11）：D1 行级守卫可能把本次 UPDATE 静默 no-op，
	// 因此 push 后必须回拉合并确认字段没有丢失，本地仍领先则重推，最多三轮
	const maxPushRounds = 3
	for round := 0; round < maxPushRounds; round++ {
		state := a.store.Snapshot()
		primary, err := findPrimaryAccount(state)
		if err != nil {
			return err
		}
		if core.AccountProvider(primary) == core.ProviderWebdav && !state.CatalogSync.InitialPullCompleted {
			return errors.New(msgWebdavInitialPullRequired)
		}
		catalogStore, err := a.catalogStoreFor(primary)
		if err != nil {
			_ = a.store.SetRecoveryStatus(func(status *core.RecoveryStatus) {
				status.LastRecoveryError = err.Error()
			})
			return err
		}

		encryptedCredentials, err := encryptCatalogCredentials(state.Accounts, a.recoveryPassword)
		if err != nil {
			return err
		}
		if strings.TrimSpace(a.recoveryPassword) == "" {
			_ = a.store.SetRecoveryStatus(func(status *core.RecoveryStatus) {
				status.PendingCredentialBackup = len(state.Accounts) > 0
			})
		}

		baselineRevision := a.store.LastKnownCatalogRevision()
		revision, err := catalogStore.SaveRemoteCatalog(a.syncContext(), core.RemoteCatalog{
			Accounts: state.Accounts,
			Games:    state.Games,
			Preferences: &core.RemotePreferences{
				TagOrder:                 state.Preferences.TagOrder,
				TagOrderUpdatedAt:        state.Preferences.TagOrderUpdatedAt,
				PinnedTags:               state.Preferences.PinnedTags,
				PinnedTagsUpdatedAt:      state.Preferences.PinnedTagsUpdatedAt,
				SidebarNavOrder:          state.Preferences.SidebarNavOrder,
				SidebarNavOrderUpdatedAt: state.Preferences.SidebarNavOrderUpdatedAt,
				FavoriteGames:            state.Preferences.FavoriteGames,
				FavoriteGamesUpdatedAt:   state.Preferences.FavoriteGamesUpdatedAt,
				GameOrderUpdatedAt:       state.Preferences.GameOrderUpdatedAt,
			},
			Tombstones:        activeCatalogTombstones(state),
			StorageGeneration: state.StorageGeneration,
			Handoff:           state.LastStorageHandoff,
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

		// M11：读回的 revision 是两次独立请求，可能包含并发他机自增；
		// 只有恰为 pull 基线 +1 才可作为已拉取水位，否则保守记基线，
		// 保证下次 pull 不被 revision 相等短路而漏掉他机写入
		if revision == baselineRevision+1 {
			if err := a.store.MarkCatalogSynced(revision); err != nil {
				return err
			}
		} else {
			if err := a.store.MarkCatalogSynced(baselineRevision); err != nil {
				return err
			}
		}

		// push 后回拉合并（M5）：检测被行级守卫吞掉的字段
		catalog, encrypted, err := catalogStore.LoadRemoteCatalog(a.syncContext())
		if err != nil {
			return err
		}
		catalog, encrypted = normalizeRemoteCatalogForMerge(catalog, encrypted)
		catalog, _ = prepareCatalogForOrdinaryMerge(a.store.Snapshot(), catalog, encrypted, a.recoveryPassword)
		before := catalogStateFingerprint(a.store.Snapshot())
		if err := a.store.MergeRemoteCatalog(catalog); err != nil {
			return err
		}
		if catalog.Revision > 0 {
			if err := a.store.MarkCatalogRevision(catalog.Revision); err != nil {
				return err
			}
		}
		merged := a.store.Snapshot()
		if catalogStateFingerprint(merged) != before {
			a.emitStateUpdated()
		}
		if !localCatalogAheadOfRemote(merged, catalog) {
			return nil
		}
		if a.ctx != nil {
			wailsruntime.LogWarningf(a.ctx, "catalog push round %d lost fields to row-level guard, repushing", round+1)
		}
	}

	// 多轮仍未收敛：保持 dirty，稍后整轮重试
	if err := a.store.MarkCatalogDirty(); err != nil {
		return err
	}
	a.scheduleCatalogRetry(30*time.Second, "catalog convergence")
	if a.ctx != nil {
		wailsruntime.LogWarningf(a.ctx, "catalog did not converge after %d push rounds, retry scheduled", maxPushRounds)
	}
	return nil
}

// catalogStateFingerprint 序列化目录相关状态用于变更检测（仅在内存中比较，不落盘）
func catalogStateFingerprint(state core.AppState) string {
	payload := struct {
		Games       []core.Game              `json:"games"`
		Accounts    []core.CloudflareAccount `json:"accounts"`
		Preferences core.Preferences         `json:"preferences"`
		Tombstones  core.CatalogTombstones   `json:"tombstones"`
	}{state.Games, state.Accounts, state.Preferences, state.Tombstones}
	content, _ := json.Marshal(payload)
	return string(content)
}

// localCatalogAheadOfRemote 判断合并后的本地目录是否仍持有远端缺失的更新（M5）：
// 任一游戏分组时间戳/账号时间戳/偏好列表时间戳晚于远端对应值，或本地条目远端不存在，
// 都说明本次 push 有字段被 D1 行级整 blob LWW 静默丢弃，需要再推一轮
func localCatalogAheadOfRemote(state core.AppState, catalog core.RemoteCatalog) bool {
	remoteGames := make(map[string]core.Game, len(catalog.Games))
	for _, game := range catalog.Games {
		remoteGames[game.ID] = game
	}
	for _, game := range state.Games {
		remote, ok := remoteGames[game.ID]
		if !ok {
			return true
		}
		fallback := remote.CatalogUpdatedAt
		if groupTimestampNewer(game.MetadataUpdatedAt, remote.MetadataUpdatedAt, fallback) ||
			groupTimestampNewer(game.CoverUpdatedAt, remote.CoverUpdatedAt, fallback) ||
			groupTimestampNewer(game.TagsUpdatedAt, remote.TagsUpdatedAt, fallback) ||
			groupTimestampNewer(game.SyncConfigUpdatedAt, remote.SyncConfigUpdatedAt, fallback) ||
			groupTimestampNewer(game.StorageUpdatedAt, remote.StorageUpdatedAt, fallback) ||
			groupTimestampNewer(game.RuntimeUpdatedAt, remote.RuntimeUpdatedAt, fallback) {
			return true
		}
	}
	remoteAccounts := make(map[string]core.CloudflareAccount, len(catalog.Accounts))
	for _, account := range catalog.Accounts {
		remoteAccounts[account.ID] = account
	}
	for _, account := range state.Accounts {
		remote, ok := remoteAccounts[account.ID]
		if !ok {
			return true
		}
		if account.CatalogUpdatedAt.After(remote.CatalogUpdatedAt) {
			return true
		}
	}
	var remotePrefs core.RemotePreferences
	if catalog.Preferences != nil {
		remotePrefs = *catalog.Preferences
	}
	prefs := state.Preferences
	return prefs.FavoriteGamesUpdatedAt.After(remotePrefs.FavoriteGamesUpdatedAt) ||
		prefs.TagOrderUpdatedAt.After(remotePrefs.TagOrderUpdatedAt) ||
		prefs.PinnedTagsUpdatedAt.After(remotePrefs.PinnedTagsUpdatedAt) ||
		prefs.SidebarNavOrderUpdatedAt.After(remotePrefs.SidebarNavOrderUpdatedAt) ||
		prefs.GameOrderUpdatedAt.After(remotePrefs.GameOrderUpdatedAt)
}

// groupTimestampNewer 远端分组时间戳缺失（旧格式行）时回退整条 CatalogUpdatedAt 比较，
// 避免对历史数据误判"本地领先"造成无谓重推
func groupTimestampNewer(localAt time.Time, remoteAt time.Time, remoteFallback time.Time) bool {
	if remoteAt.IsZero() {
		remoteAt = remoteFallback
	}
	return localAt.After(remoteAt)
}

func activeCatalogTombstones(state core.AppState) core.CatalogTombstones {
	gameUpdatedAt := make(map[string]time.Time, len(state.Games))
	for _, game := range state.Games {
		// 墓碑跳过判定用编辑时间（不含 Runtime，M6）：纯 playtime 写入不得压制墓碑
		gameUpdatedAt[game.ID] = core.GameEditTimestamp(game)
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

	finishRemote, err := a.beginRemoteOperation()
	if err == nil {
		err = a.syncLatestRemoteCatalog()
		finishRemote()
	}

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
	a.syncCoordinatorMu.Lock()
	defer a.syncCoordinatorMu.Unlock()
	if err := a.ensureReady(); err != nil {
		return err
	}
	_ = a.store.MarkCatalogSyncAttempt()
	changed, err := a.pullRemoteCatalog()
	if err != nil {
		return err
	}
	if changed {
		// 后台 pull 合并改变了状态必须通知前端，否则快照陈旧窗口一直持续（M1 放大器）
		a.emitStateUpdated()
	}
	coverResults := a.syncPendingGameCovers()
	a.logCoverSyncFailures(coverResults)
	for _, result := range coverResults {
		if result.Status == "uploaded" {
			a.emitStateUpdated()
			break
		}
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

// pullRemoteCatalog 拉取并合并云端目录。返回值表示合并是否实际改变了本地状态，
// 供调用方决定是否 emit state:updated（B1）。
func (a *App) pullRemoteCatalog() (bool, error) {
	state := a.store.Snapshot()
	if len(state.Accounts) == 0 {
		return false, nil
	}
	primary, err := findPrimaryAccount(state)
	if err != nil {
		return false, nil
	}
	catalogStore, err := a.catalogStoreFor(primary)
	if err != nil {
		_ = a.store.SetRecoveryStatus(func(status *core.RecoveryStatus) {
			status.LastRecoveryError = err.Error()
		})
		return false, err
	}
	remoteRevision, revisionErr := catalogStore.LoadCatalogRevision(a.syncContext())
	if state.CatalogSync.InitialPullCompleted && revisionErr == nil && remoteRevision > 0 && remoteRevision == a.store.LastKnownCatalogRevision() && !a.store.HasPendingCatalogSync() {
		return false, nil
	}
	catalog, encrypted, err := catalogStore.LoadRemoteCatalog(a.syncContext())
	if err != nil {
		_ = a.store.SetRecoveryStatus(func(status *core.RecoveryStatus) {
			status.LastRecoveryError = err.Error()
		})
		return false, err
	}
	catalog, encrypted = normalizeRemoteCatalogForMerge(catalog, encrypted)
	catalog, _ = prepareCatalogForOrdinaryMerge(state, catalog, encrypted, a.recoveryPassword)
	before := catalogStateFingerprint(state)
	if err := a.store.MergeRemoteCatalog(catalog); err != nil {
		return false, err
	}
	if err := a.store.MarkCatalogInitialPullCompleted(); err != nil {
		return false, err
	}
	changed := catalogStateFingerprint(a.store.Snapshot()) != before
	if catalog.Revision > 0 {
		return changed, a.store.MarkCatalogRevision(catalog.Revision)
	}
	return changed, nil
}

func (a *App) pullRemoteCatalogInBackground(reason string) {
	if err := a.ensureReady(); err != nil {
		return
	}
	finish, err := a.beginRemoteOperation()
	if err != nil {
		wailsruntime.LogErrorf(a.ctx, "follow storage handoff during %s failed: %v", reason, err)
		return
	}
	defer finish()
	changed, err := a.pullRemoteCatalog()
	if err != nil {
		wailsruntime.LogErrorf(a.ctx, "pull remote catalog in background during %s failed: %v", reason, err)
		return
	}
	if changed {
		if err := a.refreshAllSyncTracking(false); err != nil {
			wailsruntime.LogErrorf(a.ctx, "refresh save tracking after %s failed: %v", reason, err)
		}
		a.emitStateUpdated()
	}
}

func (a *App) verifyAccounts(pullCatalogAfter bool) {
	if err := a.ensureReady(); err != nil {
		return
	}
	finish, err := a.beginRemoteOperation()
	if err != nil {
		wailsruntime.LogErrorf(a.ctx, "follow storage handoff before account verification failed: %v", err)
		return
	}
	defer finish()
	state := a.store.Snapshot()
	for _, account := range state.Accounts {
		verifiedAccount, _ := verifyStorageAccount(a.syncContext(), account)
		verificationState := "valid"
		if verifiedAccount.LastError != "" {
			verificationState = "invalid"
		}
		// 只回写验证结果字段（M2）：验证耗时数秒，期间 pull 合并进来的
		// 他机账号配置不能被启动时的整体快照冲掉
		if err := a.store.UpdateAccountVerification(
			account.ID,
			verificationState,
			verifiedAccount.LastVerifiedAt,
			verifiedAccount.LastError,
			verifiedAccount.UsageWarning,
			verifiedAccount.UsedBytes,
			verifiedAccount.TokenExpiresAt,
			verifiedAccount.CredentialsBackedUp,
		); err != nil {
			wailsruntime.LogErrorf(a.ctx, "store verified account failed: %v", err)
		}
	}

	if pullCatalogAfter {
		if _, err := a.pullRemoteCatalog(); err != nil {
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
	a.emitRuntimeEvent("sync:progress", map[string]string{
		"gameId":  gameID,
		"message": message,
	})
}

func (a *App) emitRuntimeEvent(name string, payload any) {
	if a.runtimeEventFn != nil {
		a.runtimeEventFn(name, payload)
		return
	}
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
	if err := a.ensureReady(); err != nil {
		return
	}
	finishRemote, remoteErr := a.beginRemoteOperation()
	if remoteErr != nil {
		wailsruntime.LogErrorf(a.ctx, "follow storage handoff after game session failed: %v", remoteErr)
		return
	}
	defer finishRemote()
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
	// 打包存档目录期间与同步/恢复互斥（M7），锁只覆盖读目录的短临界区
	unlock := a.lockGameSync(gameID)
	backup, err := bm.CreateBackup(context.Background(), gameToUpdate, "auto", msgAutoBackupName, a.store.Snapshot().Device.ID, a.store.DataDir(), nil)
	unlock()
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

func resolveStorageSwitchTarget(state core.AppState, request core.StorageSwitchRequest) (core.CloudflareAccount, error) {
	existingID := strings.TrimSpace(request.ExistingAccountID)
	hasExisting := existingID != ""
	hasNew := request.NewAccount != nil
	if hasExisting == hasNew {
		return core.CloudflareAccount{}, errors.New(msgStorageSwitchTargetRequired)
	}

	primary, err := findPrimaryAccount(state)
	if err != nil {
		return core.CloudflareAccount{}, err
	}

	var target core.CloudflareAccount
	if hasExisting {
		target, err = findAccount(state, existingID)
		if err != nil {
			return core.CloudflareAccount{}, err
		}
	} else {
		target = *request.NewAccount
		target.ID = ""
	}
	if core.AccountProvider(target) == core.AccountProvider(primary) {
		return core.CloudflareAccount{}, errors.New(msgStorageSwitchSameProvider)
	}
	return target, nil
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

func hasR2Credentials(account core.CloudflareAccount) bool {
	return strings.TrimSpace(account.AccountID) != "" &&
		strings.TrimSpace(account.R2Bucket) != "" &&
		strings.TrimSpace(account.R2AccessKeyID) != "" &&
		strings.TrimSpace(account.R2SecretAccessKey) != ""
}

func hasUsableR2Account(account core.CloudflareAccount) bool {
	return account.Enabled && hasR2Credentials(account)
}

func hasObjectCredentials(account core.CloudflareAccount) bool {
	if core.AccountProvider(account) == core.ProviderWebdav {
		return strings.TrimSpace(account.WebdavURL) != "" &&
			strings.TrimSpace(account.WebdavUsername) != "" &&
			strings.TrimSpace(account.WebdavPassword) != ""
	}
	return hasR2Credentials(account)
}

func hasUsableObjectAccount(account core.CloudflareAccount) bool {
	return account.Enabled && hasObjectCredentials(account)
}

func appendUniqueAccount(accounts []core.CloudflareAccount, seen map[string]bool, account core.CloudflareAccount) []core.CloudflareAccount {
	if strings.TrimSpace(account.ID) == "" || seen[account.ID] || !hasUsableObjectAccount(account) {
		return accounts
	}
	seen[account.ID] = true
	return append(accounts, account)
}

func writableObjectAccounts(state core.AppState) []core.CloudflareAccount {
	ordered := make([]core.CloudflareAccount, 0, len(state.Accounts))
	seen := make(map[string]bool, len(state.Accounts))
	for _, account := range state.Accounts {
		if account.IsPrimary {
			ordered = appendUniqueAccount(ordered, seen, account)
		}
	}
	for _, account := range state.Accounts {
		ordered = appendUniqueAccount(ordered, seen, account)
	}
	return ordered
}

func canonicalBackupAccounts(state core.AppState) []core.CloudflareAccount {
	return writableObjectAccounts(state)
}

func referencedBackupAccounts(state core.AppState, game core.Game) []core.CloudflareAccount {
	ordered := writableObjectAccounts(state)
	seen := make(map[string]bool, len(state.Accounts))
	for _, account := range ordered {
		seen[account.ID] = true
	}
	lookup := make(map[string]core.CloudflareAccount, len(state.Accounts))
	for _, account := range state.Accounts {
		lookup[account.ID] = account
	}
	for _, record := range game.BackupRegistry {
		accountID := strings.TrimSpace(record.AccountID)
		account, ok := lookup[accountID]
		if !ok || accountID == "" || seen[accountID] || !hasObjectCredentials(account) {
			continue
		}
		seen[accountID] = true
		ordered = append(ordered, account)
	}
	return ordered
}

func orderedBackupAccounts(state core.AppState, game core.Game, backupType string) []core.CloudflareAccount {
	lookup := make(map[string]core.CloudflareAccount, len(state.Accounts))
	for _, account := range state.Accounts {
		lookup[account.ID] = account
	}
	canonical := writableObjectAccounts(state)
	if strings.EqualFold(strings.TrimSpace(backupType), "auto") {
		if accountID := strings.TrimSpace(game.AutoBackupAccountID); accountID != "" {
			if account, ok := lookup[accountID]; ok && hasUsableObjectAccount(account) {
				return []core.CloudflareAccount{account}
			}
		}
		if accountID := strings.TrimSpace(game.StorageAccountID); accountID != "" {
			if account, ok := lookup[accountID]; ok && hasUsableObjectAccount(account) {
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

// newCatalogStore 按账号 provider 构造目录存储：cloudflare→D1，webdav→WebdavClient
func newCatalogStore(account core.CloudflareAccount) (core.CatalogStore, error) {
	if core.AccountProvider(account) == core.ProviderWebdav {
		client, err := core.NewWebdavClient(account)
		if err != nil {
			return nil, err
		}
		return client, nil
	}
	return core.NewD1Client(account), nil
}

// newObjectStore 按账号 provider 构造对象存储：cloudflare→R2，webdav→WebdavClient
func newObjectStore(ctx context.Context, account core.CloudflareAccount) (core.ObjectStore, error) {
	if core.AccountProvider(account) == core.ProviderWebdav {
		client, err := core.NewWebdavClient(account)
		if err != nil {
			return nil, err
		}
		return client, nil
	}
	return core.NewR2Client(ctx, account)
}

// newSplitStorageGateway 目录走主账号、对象走存档账号；纯 cloudflare 组合沿用
// 原 NewSplitCloudflareGateway 的配置完整性校验，混合 provider 时按各自分支构造
func newSplitStorageGateway(ctx context.Context, metadataAccount core.CloudflareAccount, storageAccount core.CloudflareAccount) (*core.StorageGateway, error) {
	if core.AccountProvider(metadataAccount) == core.ProviderCloudflare && core.AccountProvider(storageAccount) == core.ProviderCloudflare {
		return core.NewSplitCloudflareGateway(ctx, metadataAccount, storageAccount)
	}
	catalog, err := newCatalogStore(metadataAccount)
	if err != nil {
		return nil, err
	}
	objects, err := newObjectStore(ctx, storageAccount)
	if err != nil {
		return nil, err
	}
	return &core.StorageGateway{Catalog: catalog, Objects: objects}, nil
}

// verifyStorageAccount 按 provider 分支调用对应的账号验证
func verifyStorageAccount(ctx context.Context, account core.CloudflareAccount) (core.CloudflareAccount, error) {
	if core.AccountProvider(account) == core.ProviderWebdav {
		return core.VerifyWebdavAccount(ctx, account)
	}
	return core.VerifyCloudflareAccount(ctx, account)
}

func (a *App) getBackupGateways(ctx context.Context, game core.Game) (map[string]*core.StorageGateway, error) {
	state := a.store.Snapshot()
	primaryAccount, err := findPrimaryAccount(state)
	if err != nil {
		return nil, err
	}
	gateways := make(map[string]*core.StorageGateway)
	var failures []string
	for _, account := range referencedBackupAccounts(state, game) {
		gateway, gatewayErr := newSplitStorageGateway(ctx, primaryAccount, account)
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
		objects, err := newObjectStore(ctx, account)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", account.Name, err))
			continue
		}
		usedBytes, err := objects.FetchAccountUsageBytes(ctx)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", account.Name, err))
			continue
		}
		if core.AccountProvider(account) == core.ProviderWebdav || usedBytes+backupSize <= maxUsage {
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

// cleanupOlderAutoBackupArtifacts 在新的 auto 备份进入 ready 后，清理注册表中更旧的
// auto 记录及其本地 zip；他机产生的更新记录与带删除意图的记录保留（M9）。
func cleanupOlderAutoBackupArtifacts(game *core.Game, newRecord core.BackupRecord, dataDir string) {
	if game == nil {
		return
	}
	newBackupID := core.BackupRecordID(newRecord)
	nextRegistry := make([]core.BackupRecord, 0, len(game.BackupRegistry))
	for _, existing := range game.BackupRegistry {
		record := normalizeBackupRecord(existing)
		removable := strings.EqualFold(record.Type, "auto") &&
			core.BackupRecordID(record) != newBackupID &&
			record.SourceDeviceID == newRecord.SourceDeviceID &&
			record.DeletedAt == nil &&
			record.Status != core.BackupStatusPendingDelete &&
			record.Status != core.BackupStatusDeleteFailed &&
			!backupRecordIsNewer(record, newRecord)
		if removable {
			_ = os.Remove(core.BackupLocalPathForRecord(dataDir, game.ID, record))
			continue
		}
		nextRegistry = append(nextRegistry, existing)
	}
	game.BackupRegistry = nextRegistry
	rebuildBackupCompatFields(game)
}

func (a *App) persistBackupRoute(game core.Game, backup core.Backup, accountID string) error {
	if strings.TrimSpace(accountID) == "" {
		return nil
	}
	backup.Name = defaultBackupRecordName(backup.Type, backup.Name)
	if strings.TrimSpace(backup.SourceDeviceID) == "" {
		backup.SourceDeviceID = strings.TrimSpace(a.store.Snapshot().Device.ID)
	}
	record := core.BackupRecord{
		Filename:                  backup.Filename,
		ObjectKey:                 strings.TrimSpace(backup.ObjectKey),
		AccountID:                 accountID,
		Type:                      backup.Type,
		Name:                      backup.Name,
		SHA256:                    strings.TrimSpace(backup.SHA256),
		CreatedAt:                 backup.CreatedAt,
		SourceDeviceID:            strings.TrimSpace(backup.SourceDeviceID),
		SourceManifestHash:        strings.TrimSpace(backup.SourceManifestHash),
		SourceManifestGeneratedAt: backup.SourceManifestGeneratedAt,
	}
	if record.ObjectKey == "" {
		record.ObjectKey = core.BackupObjectKeyForRecord(game.ID, record)
	}
	if backup.Type == "auto" {
		game.AutoBackupAccountID = accountID
		// 旧 auto 记录不在此处清除（M9）：仅在新记录 ready 后清理更旧的
		cleanupOlderAutoBackupArtifacts(&game, record, a.store.DataDir())
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
	gateway, err := newSplitStorageGateway(ctx, primaryAccount, account)
	if err != nil {
		return err
	}
	record := core.BackupRecord{Filename: backup.Filename, ObjectKey: backup.ObjectKey, SourceDeviceID: backup.SourceDeviceID}
	localPath := core.ExistingBackupLocalPathForRecord(a.store.DataDir(), game.ID, record)
	remoteKey := core.BackupObjectKeyForRecord(game.ID, record)
	if err := gateway.Objects.PutObjectFromFile(ctx, remoteKey, localPath); err != nil {
		return fmt.Errorf(msgUploadBackupFailed, err)
	}
	if backup.Type == "auto" {
		bm := core.NewBackupManager(a.engine)
		bm.CleanupCloudAutoBackups(ctx, gateway, game.ID, backup.SourceDeviceID, backup.Filename)
	}
	backup.StorageAccountID = account.ID
	backup.ObjectKey = remoteKey
	return a.persistBackupRoute(game, *backup, account.ID)
}

func (a *App) persistBackupRecord(game core.Game, backup core.Backup, accountID string, status string, lastError string) (*core.Backup, error) {
	backup.Name = canonicalBackupRecordName(backup.Type, backup.Name)
	if strings.TrimSpace(backup.SourceDeviceID) == "" {
		backup.SourceDeviceID = strings.TrimSpace(a.store.Snapshot().Device.ID)
	}
	record := core.BackupRecord{
		Filename:                  backup.Filename,
		ObjectKey:                 strings.TrimSpace(backup.ObjectKey),
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
	if record.ObjectKey == "" {
		record.ObjectKey = core.BackupObjectKeyForRecord(game.ID, record)
	}
	if backup.Type == "auto" {
		game.AutoBackupAccountID = strings.TrimSpace(accountID)
		// 不清除既有 auto 记录（M9）：上传失败时旧的 ready 记录必须保留，
		// 更旧记录的清理在上传成功回调里执行
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
	backup.ID = core.BackupRecordID(record)
	backup.ObjectKey = record.ObjectKey
	backup.SourceDeviceID = record.SourceDeviceID
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
		BackupID:  savedBackup.ID,
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

func (a *App) getGatewayForGame(ctx context.Context, gameID string) (*core.StorageGateway, error) {
	state := a.store.Snapshot()
	_, storageAccount, err := findGameAndAccount(state, gameID)
	if err != nil {
		return nil, err
	}
	primaryAccount, err := findPrimaryAccount(state)
	if err != nil {
		return nil, err
	}
	return newSplitStorageGateway(ctx, primaryAccount, storageAccount)
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

	// 60 秒内未识别到游戏进程：不记时长、不做会话结束自动备份，只通知前端
	onMiss := func() {
		wailsruntime.LogWarningf(a.ctx, "game %s process not identified within monitor window; skip session-end backup and playtime", gameID)
		a.emitRuntimeEvent("game:monitor_timeout", map[string]string{"gameId": gameID})
	}

	return pm.LaunchAndMonitor(a.ctx, game.InstallPath, onStart, onEnd, onMiss)
}

func (a *App) GetGameBackups(gameID string) (core.BackupListResult, error) {
	if err := a.ensureReady(); err != nil {
		return core.BackupListResult{}, err
	}
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return core.BackupListResult{}, err
	}
	defer finish()
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
			for _, account := range referencedBackupAccounts(a.store.Snapshot(), game) {
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
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return nil, err
	}
	defer finish()
	state := a.store.Snapshot()
	game, err := findGame(state, gameID)
	if err != nil {
		return nil, err
	}
	bm := core.NewBackupManager(a.engine)
	backup, err := bm.CreateBackup(a.ctx, game, backupType, name, state.Device.ID, a.store.DataDir(), nil)
	if err != nil {
		return nil, err
	}
	return a.queueBackupUploadForGame(game, *backup)
}

func (a *App) RestoreGameBackup(gameID string, backupID string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return err
	}
	defer finish()
	state := a.store.Snapshot()
	game, err := findGame(state, gameID)
	if err != nil {
		return err
	}
	record, _, ok := findBackupRecord(game, backupID)
	if !ok {
		return fmt.Errorf("backup %s not found", backupID)
	}
	record = normalizeBackupRecord(record)
	gateways, gatewayErr := a.getBackupGateways(a.ctx, game)
	if gatewayErr != nil {
		if localPath := core.ExistingBackupLocalPathForRecord(a.store.DataDir(), game.ID, record); strings.TrimSpace(localPath) != "" {
			if _, statErr := os.Stat(localPath); statErr != nil {
				return gatewayErr
			}
		}
	}
	// 覆盖存档目录期间与同步互斥（M7）
	unlock := a.lockGameSync(gameID)
	defer unlock()
	bm := core.NewBackupManager(a.engine)
	if err := bm.RestoreBackup(a.ctx, game, backupID, a.store.DataDir(), gateways); err != nil {
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

func (a *App) DeleteGameBackup(gameID string, backupID string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return err
	}
	defer finish()
	game, err := findGame(a.store.Snapshot(), gameID)
	if err != nil {
		return err
	}
	record, _, ok := findBackupRecord(game, backupID)
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
	a.enqueueBackupDelete(queuedBackupDelete{GameID: gameID, BackupID: backupID})
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
	finish, err := a.beginRemoteOperation()
	if err != nil {
		return err
	}
	defer finish()

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
