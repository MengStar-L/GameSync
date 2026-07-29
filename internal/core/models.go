package core

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type AppState struct {
	Device             DeviceInfo             `json:"device"`
	Accounts           []CloudflareAccount    `json:"accounts"`
	Games              []Game                 `json:"games"`
	Preferences        Preferences            `json:"preferences"`
	Activities         []SyncActivity         `json:"activities"`
	RecoveryStatus     RecoveryStatus         `json:"recoveryStatus"`
	Tombstones         CatalogTombstones      `json:"tombstones,omitempty"`
	CatalogSync        CatalogSyncStatus      `json:"catalogSync,omitempty"`
	StorageGeneration  int64                  `json:"storageGeneration,omitempty"`
	LastStorageHandoff *StorageHandoff        `json:"lastStorageHandoff,omitempty"`
	StorageMigration   *StorageMigrationState `json:"storageMigration,omitempty"`
}

type DashboardSnapshot struct {
	State         AppState `json:"state"`
	DataDir       string   `json:"dataDir"`
	SchemaVersion int      `json:"schemaVersion"`
}

type DeviceInfo struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Platform      string    `json:"platform"`
	LastStartedAt time.Time `json:"lastStartedAt" ts_type:"string"`
}

type Preferences struct {
	AutoSyncOnLaunch           bool      `json:"autoSyncOnLaunch"`
	StartupSyncMode            string    `json:"startupSyncMode"`
	ConflictPolicy             string    `json:"conflictPolicy"`
	DefaultInstallDir          string    `json:"defaultInstallDir"`
	DefaultSaveDir             string    `json:"defaultSaveDir"`
	DefaultSteamInstallDir     string    `json:"defaultSteamInstallDir"`
	DefaultSteamSaveDir        string    `json:"defaultSteamSaveDir"`
	DefaultThirdInstallDir     string    `json:"defaultThirdInstallDir"`
	DefaultThirdSaveDir        string    `json:"defaultThirdSaveDir"`
	RawgAPIKey                 string    `json:"rawgApiKey"`
	SteamGridDBAPIKey          string    `json:"steamGridDbApiKey"`
	FavoriteGames              []string  `json:"favoriteGames"`
	TagOrder                   []string  `json:"tagOrder"`
	PinnedTags                 []string  `json:"pinnedTags"`
	SidebarNavOrder            []string  `json:"sidebarNavOrder"`
	RawgAPIKeyUpdatedAt        time.Time `json:"rawgApiKeyUpdatedAt,omitempty" ts_type:"string"`
	SteamGridDBAPIKeyUpdatedAt time.Time `json:"steamGridDbApiKeyUpdatedAt,omitempty" ts_type:"string"`
	FavoriteGamesUpdatedAt     time.Time `json:"favoriteGamesUpdatedAt,omitempty" ts_type:"string"`
	TagOrderUpdatedAt          time.Time `json:"tagOrderUpdatedAt,omitempty" ts_type:"string"`
	PinnedTagsUpdatedAt        time.Time `json:"pinnedTagsUpdatedAt,omitempty" ts_type:"string"`
	SidebarNavOrderUpdatedAt   time.Time `json:"sidebarNavOrderUpdatedAt,omitempty" ts_type:"string"`
	GameOrderUpdatedAt         time.Time `json:"gameOrderUpdatedAt,omitempty" ts_type:"string"`
}

type CloudflareAccount struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	AccountID           string     `json:"accountId"`
	APIToken            string     `json:"apiToken"`
	D1DatabaseID        string     `json:"d1DatabaseId"`
	R2Bucket            string     `json:"r2Bucket"`
	R2AccessKeyID       string     `json:"r2AccessKeyId"`
	R2SecretAccessKey   string     `json:"r2SecretAccessKey"`
	Provider            string     `json:"provider,omitempty"`  // "cloudflare"(默认/空) | "webdav"
	WebdavURL           string     `json:"webdavUrl,omitempty"` // 例 https://dav.example.com/remote.php/dav/files/user
	WebdavUsername      string     `json:"webdavUsername,omitempty"`
	WebdavPassword      string     `json:"webdavPassword,omitempty"` // 建议用应用专用密码
	WebdavRoot          string     `json:"webdavRoot,omitempty"`     // 服务器上的根目录，默认 "GameSync"
	IsPrimary           bool       `json:"isPrimary"`
	Enabled             bool       `json:"enabled"`
	UsedBytes           int64      `json:"usedBytes"`
	LastVerifiedAt      *time.Time `json:"lastVerifiedAt,omitempty" ts_type:"string | null"`
	TokenExpiresAt      *time.Time `json:"tokenExpiresAt,omitempty" ts_type:"string | null"`
	LastError           string     `json:"lastError,omitempty"`
	UsageWarning        string     `json:"usageWarning,omitempty"`
	VerificationState   string     `json:"verificationState,omitempty"`
	CredentialsBackedUp bool       `json:"credentialsBackedUp,omitempty"`
	CatalogUpdatedAt    time.Time  `json:"catalogUpdatedAt,omitempty" ts_type:"string"`
}

type RecoveryStatus struct {
	HasRecoveryPassword     bool       `json:"hasRecoveryPassword"`
	RemoteCatalogAvailable  bool       `json:"remoteCatalogAvailable"`
	PendingCredentialBackup bool       `json:"pendingCredentialBackup"`
	LastCatalogSyncAt       *time.Time `json:"lastCatalogSyncAt,omitempty" ts_type:"string | null"`
	LastCredentialBackupAt  *time.Time `json:"lastCredentialBackupAt,omitempty" ts_type:"string | null"`
	LastRecoveryError       string     `json:"lastRecoveryError,omitempty"`
}

type CatalogSyncStatus struct {
	Dirty                bool       `json:"dirty"`
	InitialPullCompleted bool       `json:"initialPullCompleted"`
	LastKnownRevision    int64      `json:"lastKnownRevision"`
	LastQueuedAt         *time.Time `json:"lastQueuedAt,omitempty" ts_type:"string | null"`
	LastAttemptAt        *time.Time `json:"lastAttemptAt,omitempty" ts_type:"string | null"`
	LastSuccessAt        *time.Time `json:"lastSuccessAt,omitempty" ts_type:"string | null"`
	LastError            string     `json:"lastError,omitempty"`
}

type RemoteCatalog struct {
	Accounts          []CloudflareAccount `json:"accounts"`
	Games             []Game              `json:"games"`
	Preferences       *RemotePreferences  `json:"preferences,omitempty"`
	Tombstones        CatalogTombstones   `json:"tombstones,omitempty"`
	Revision          int64               `json:"revision,omitempty"`
	StorageGeneration int64               `json:"storageGeneration,omitempty"`
	Handoff           *StorageHandoff     `json:"handoff,omitempty"`
}

const (
	StorageHandoffPrepared  = "prepared"
	StorageHandoffCommitted = "committed"

	MigrationPhaseCopying         = "copying"
	MigrationPhaseTargetReady     = "target_ready"
	MigrationPhaseSourceCommitted = "source_committed"
	MigrationPhaseLocalCommitted  = "local_committed"

	MigrationItemPending  = "pending"
	MigrationItemCopied   = "copied"
	MigrationItemVerified = "verified"
)

type StorageHandoff struct {
	TransactionID      string    `json:"transactionId"`
	SourceAccountID    string    `json:"sourceAccountId"`
	TargetAccountID    string    `json:"targetAccountId"`
	InitiatingDeviceID string    `json:"initiatingDeviceId"`
	State              string    `json:"state"`
	Generation         int64     `json:"generation"`
	CommittedAt        time.Time `json:"committedAt,omitempty" ts_type:"string"`
}

type StorageMigrationItem struct {
	Kind            string `json:"kind"`
	GameID          string `json:"gameId"`
	SourceAccountID string `json:"sourceAccountId,omitempty"`
	SourceKey       string `json:"sourceKey,omitempty"`
	LocalPath       string `json:"localPath,omitempty"`
	TargetKey       string `json:"targetKey"`
	SHA256          string `json:"sha256"`
	Size            int64  `json:"size"`
	Status          string `json:"status"`
	LastError       string `json:"lastError,omitempty"`
}

type StorageMigrationState struct {
	TransactionID         string                 `json:"transactionId"`
	SourceAccountID       string                 `json:"sourceAccountId"`
	TargetAccountID       string                 `json:"targetAccountId"`
	Phase                 string                 `json:"phase"`
	SourceRevision        int64                  `json:"sourceRevision"`
	SourceManifestVersion map[string]int         `json:"sourceManifestVersion,omitempty"`
	LocalManifestHash     map[string]string      `json:"localManifestHash,omitempty"`
	Generation            int64                  `json:"generation"`
	Items                 []StorageMigrationItem `json:"items"`
	TargetGames           []Game                 `json:"targetGames,omitempty"`
	ConflictGameID        string                 `json:"conflictGameId,omitempty"`
	LastError             string                 `json:"lastError,omitempty"`
	UpdatedAt             time.Time              `json:"updatedAt" ts_type:"string"`
}

type RemotePreferences struct {
	RawgAPIKey                 string    `json:"rawgApiKey"`
	SteamGridDBAPIKey          string    `json:"steamGridDbApiKey"`
	RawgAPIKeyUpdatedAt        time.Time `json:"rawgApiKeyUpdatedAt,omitempty" ts_type:"string"`
	SteamGridDBAPIKeyUpdatedAt time.Time `json:"steamGridDbApiKeyUpdatedAt,omitempty" ts_type:"string"`
	FavoriteGames              []string  `json:"favoriteGames"`
	FavoriteGamesUpdatedAt     time.Time `json:"favoriteGamesUpdatedAt,omitempty" ts_type:"string"`
	TagOrder                   []string  `json:"tagOrder"`
	TagOrderUpdatedAt          time.Time `json:"tagOrderUpdatedAt,omitempty" ts_type:"string"`
	PinnedTags                 []string  `json:"pinnedTags"`
	PinnedTagsUpdatedAt        time.Time `json:"pinnedTagsUpdatedAt,omitempty" ts_type:"string"`
	SidebarNavOrder            []string  `json:"sidebarNavOrder"`
	SidebarNavOrderUpdatedAt   time.Time `json:"sidebarNavOrderUpdatedAt,omitempty" ts_type:"string"`
	GameOrderUpdatedAt         time.Time `json:"gameOrderUpdatedAt,omitempty" ts_type:"string"`
}

type CatalogTombstones struct {
	Games    map[string]time.Time `json:"games,omitempty" ts_type:"Record<string, string>"`
	Accounts map[string]time.Time `json:"accounts,omitempty" ts_type:"Record<string, string>"`
}

type Game struct {
	ID                     string                 `json:"id"`
	Name                   string                 `json:"name"`
	InstallPath            string                 `json:"installPath"`
	SavePath               string                 `json:"savePath"`
	CoverPath              string                 `json:"coverPath"`
	CoverSourceType        string                 `json:"coverSourceType,omitempty"`
	CoverSource            string                 `json:"coverSource,omitempty"`
	CoverLocalPath         string                 `json:"coverLocalPath,omitempty"`
	CoverCloudAccountID    string                 `json:"coverCloudAccountId,omitempty"`
	CoverCloudKey          string                 `json:"coverCloudKey,omitempty"`
	CoverMimeType          string                 `json:"coverMimeType,omitempty"`
	CoverUpdatedAt         time.Time              `json:"coverUpdatedAt,omitempty" ts_type:"string"`
	Description            string                 `json:"description"`
	Released               string                 `json:"released"`
	Rating                 float64                `json:"rating"`
	RatingTop              int                    `json:"ratingTop"`
	Metacritic             int                    `json:"metacritic"`
	Genres                 []string               `json:"genres"`
	Platforms              []string               `json:"platforms"`
	IsSteam                bool                   `json:"isSteam"`
	Developers             []string               `json:"developers"`
	Publishers             []string               `json:"publishers"`
	Website                string                 `json:"website"`
	RawgID                 int                    `json:"rawgId"`
	RawgSlug               string                 `json:"rawgSlug"`
	RawgURL                string                 `json:"rawgUrl"`
	RawgTags               []string               `json:"rawgTags"`
	Tags                   []string               `json:"tags"`
	StorageAccountID       string                 `json:"storageAccountId"`
	AutoBackupAccountID    string                 `json:"autoBackupAccountId,omitempty"`
	BackupStorageAccountID string                 `json:"backupStorageAccountId"`
	BackupLocations        map[string]string      `json:"backupLocations,omitempty" ts_type:"Record<string, string>"`
	BackupRegistry         []BackupRecord         `json:"backupRegistry,omitempty"`
	LaunchRestoreOverride  *LaunchRestoreOverride `json:"launchRestoreOverride,omitempty"`
	BackupCount            int                    `json:"backupCount,omitempty"`
	Sync                   SyncConfig             `json:"sync"`
	Anchor                 SyncAnchor             `json:"anchor"`
	LastSync               *SyncSummary           `json:"lastSync,omitempty"`
	PlayTime               float64                `json:"playTime"`
	LastPlayed             *time.Time             `json:"lastPlayed,omitempty" ts_type:"string | null"`
	CatalogUpdatedAt       time.Time              `json:"catalogUpdatedAt,omitempty" ts_type:"string"`
	MetadataUpdatedAt      time.Time              `json:"metadataUpdatedAt,omitempty" ts_type:"string"`
	TagsUpdatedAt          time.Time              `json:"tagsUpdatedAt,omitempty" ts_type:"string"`
	SyncConfigUpdatedAt    time.Time              `json:"syncConfigUpdatedAt,omitempty" ts_type:"string"`
	StorageUpdatedAt       time.Time              `json:"storageUpdatedAt,omitempty" ts_type:"string"`
	RuntimeUpdatedAt       time.Time              `json:"runtimeUpdatedAt,omitempty" ts_type:"string"`
}

type BackupRecord struct {
	Filename                  string     `json:"filename"`
	ObjectKey                 string     `json:"objectKey,omitempty"`
	AccountID                 string     `json:"accountId,omitempty"`
	Type                      string     `json:"type"`
	Name                      string     `json:"name,omitempty"`
	SHA256                    string     `json:"sha256,omitempty"`
	CreatedAt                 time.Time  `json:"createdAt" ts_type:"string"`
	SourceDeviceID            string     `json:"sourceDeviceId,omitempty"`
	SourceManifestHash        string     `json:"sourceManifestHash,omitempty"`
	SourceManifestGeneratedAt time.Time  `json:"sourceManifestGeneratedAt,omitempty" ts_type:"string"`
	Status                    string     `json:"status,omitempty"`
	PendingDelete             bool       `json:"pendingDelete,omitempty"`
	DeletedAt                 *time.Time `json:"deletedAt,omitempty" ts_type:"string | null"`
	DeleteRetryAt             *time.Time `json:"deleteRetryAt,omitempty" ts_type:"string | null"`
	LastError                 string     `json:"lastError,omitempty"`
	LastDeleteError           string     `json:"lastDeleteError,omitempty"`
}

type Backup struct {
	ID                        string    `json:"id"`
	GameID                    string    `json:"gameId"`
	Type                      string    `json:"type"`
	Name                      string    `json:"name"`
	Filename                  string    `json:"filename"`
	ObjectKey                 string    `json:"objectKey,omitempty"`
	Size                      int64     `json:"size"`
	SHA256                    string    `json:"sha256,omitempty"`
	StorageAccountID          string    `json:"storageAccountId,omitempty"`
	CreatedAt                 time.Time `json:"createdAt" ts_type:"string"`
	SourceDeviceID            string    `json:"sourceDeviceId,omitempty"`
	SourceManifestHash        string    `json:"sourceManifestHash,omitempty"`
	SourceManifestGeneratedAt time.Time `json:"sourceManifestGeneratedAt,omitempty" ts_type:"string"`
	Status                    string    `json:"status,omitempty"`
	LocalExists               bool      `json:"localExists,omitempty"`
	CloudExists               bool      `json:"cloudExists,omitempty"`
	PendingDelete             bool      `json:"pendingDelete,omitempty"`
	LastError                 string    `json:"lastError,omitempty"`
	LastDeleteError           string    `json:"lastDeleteError,omitempty"`
}

type BackupListResult struct {
	Backups        []Backup `json:"backups"`
	Partial        bool     `json:"partial"`
	Message        string   `json:"message,omitempty"`
	FailedAccounts []string `json:"failedAccounts,omitempty"`
}

type LaunchRestoreOverride struct {
	Filename       string    `json:"filename"`
	BackupType     string    `json:"backupType"`
	SourceDeviceID string    `json:"sourceDeviceId,omitempty"`
	RestoredAt     time.Time `json:"restoredAt" ts_type:"string"`
	Active         bool      `json:"active"`
}

const (
	BackupStatusReady         = "ready"
	BackupStatusPendingUpload = "pending_upload"
	BackupStatusPendingDelete = "pending_delete"
	BackupStatusUploadFailed  = "upload_failed"
	BackupStatusDeleteFailed  = "delete_failed"
)

type SyncConfig struct {
	Enabled          bool     `json:"enabled"`
	IncludePatterns  []string `json:"includePatterns"`
	ExcludePatterns  []string `json:"excludePatterns"`
	ConflictStrategy string   `json:"conflictStrategy"`
}

type SyncAnchor struct {
	LastRemoteVersion int          `json:"lastRemoteVersion"`
	LastManifest      SyncManifest `json:"lastManifest"`
	StorageAccountID  string       `json:"storageAccountId,omitempty"`
	// PendingRemoteCleanups 记录已被新版本替换、等待延迟删除的 R2 对象，
	// 给其他设备进行中的下载留宽限期（本地状态，不上传云端目录）
	PendingRemoteCleanups []PendingRemoteCleanup `json:"pendingRemoteCleanups,omitempty"`
}

type PendingRemoteCleanup struct {
	SHA256     string    `json:"sha256"`
	ReplacedAt time.Time `json:"replacedAt" ts_type:"string"`
}

type SyncManifest struct {
	Version     int            `json:"version"`
	GeneratedAt time.Time      `json:"generatedAt" ts_type:"string"`
	Files       []ManifestFile `json:"files"`
	TotalBytes  int64          `json:"totalBytes"`
	Hash        string         `json:"hash"`
}

type ManifestFile struct {
	Path       string    `json:"path"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt" ts_type:"string"`
	SHA256     string    `json:"sha256"`
}

type ManifestDiff struct {
	Path   string        `json:"path"`
	Action string        `json:"action"`
	Local  *ManifestFile `json:"local,omitempty"`
	Remote *ManifestFile `json:"remote,omitempty"`
}

type SyncSummary struct {
	Status     string    `json:"status"`
	Message    string    `json:"message"`
	Uploaded   int       `json:"uploaded"`
	Downloaded int       `json:"downloaded"`
	Conflicts  int       `json:"conflicts"`
	SyncedAt   time.Time `json:"syncedAt" ts_type:"string"`
}

type SyncActivity struct {
	ID         string     `json:"id"`
	GameID     string     `json:"gameId"`
	GameName   string     `json:"gameName"`
	AccountID  string     `json:"accountId"`
	Status     string     `json:"status"`
	Message    string     `json:"message"`
	Uploaded   int        `json:"uploaded"`
	Downloaded int        `json:"downloaded"`
	Conflicts  int        `json:"conflicts"`
	StartedAt  time.Time  `json:"startedAt" ts_type:"string"`
	EndedAt    *time.Time `json:"endedAt,omitempty" ts_type:"string | null"`
}

type SyncRunRequest struct {
	GameID         string `json:"gameId"`
	ConflictChoice string `json:"conflictChoice"`
}

type SyncResourceStats struct {
	EnumeratedGames   int `json:"enumeratedGames"`
	StattedFiles      int `json:"stattedFiles"`
	HashedFiles       int `json:"hashedFiles"`
	UploadedObjects   int `json:"uploadedObjects"`
	DownloadedObjects int `json:"downloadedObjects"`
}

type SyncCatalogResult struct {
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
	Revision int64  `json:"revision,omitempty"`
}

type SyncCoverResult struct {
	GameID   string `json:"gameId"`
	GameName string `json:"gameName"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
}

type SyncGameResult struct {
	GameID     string `json:"gameId"`
	GameName   string `json:"gameName"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
	Uploaded   int    `json:"uploaded"`
	Downloaded int    `json:"downloaded"`
	Conflicts  int    `json:"conflicts"`
}

type SyncBatchResult struct {
	Snapshot DashboardSnapshot `json:"snapshot"`
	Catalog  SyncCatalogResult `json:"catalog"`
	Covers   []SyncCoverResult `json:"covers"`
	Saves    []SyncGameResult  `json:"saves"`
	Stats    SyncResourceStats `json:"stats"`
}

type StorageSwitchRequest struct {
	ExistingAccountID string             `json:"existingAccountId,omitempty"`
	NewAccount        *CloudflareAccount `json:"newAccount,omitempty"`
	UseLocalData      bool               `json:"useLocalData,omitempty"`
}

type StorageSwitchResult struct {
	Snapshot       DashboardSnapshot `json:"snapshot"`
	Status         string            `json:"status"`
	TransactionID  string            `json:"transactionId,omitempty"`
	ConflictGameID string            `json:"conflictGameId,omitempty"`
	Message        string            `json:"message,omitempty"`
}

type StorageMigrationResumeRequest struct {
	TransactionID  string `json:"transactionId"`
	GameID         string `json:"gameId"`
	ConflictChoice string `json:"conflictChoice"`
}

type RemoteManifestRecord struct {
	GameID          string       `json:"gameId"`
	Version         int          `json:"version"`
	Manifest        SyncManifest `json:"manifest"`
	UpdatedAt       time.Time    `json:"updatedAt" ts_type:"string"`
	UpdatedByDevice string       `json:"updatedByDevice"`
}

func DefaultPreferences() Preferences {
	return Preferences{
		AutoSyncOnLaunch:       true,
		StartupSyncMode:        "smart",
		ConflictPolicy:         "manual",
		DefaultInstallDir:      "",
		DefaultSaveDir:         "",
		DefaultSteamInstallDir: "",
		DefaultSteamSaveDir:    "",
		DefaultThirdInstallDir: "",
		DefaultThirdSaveDir:    "",
		RawgAPIKey:             "",
		SteamGridDBAPIKey:      "",
	}
}

func DefaultSyncConfig() SyncConfig {
	return SyncConfig{
		Enabled:          true,
		IncludePatterns:  []string{"*"},
		ExcludePatterns:  []string{},
		ConflictStrategy: "manual",
	}
}

func NewID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return time.Now().Format("20060102150405")
	}

	return hex.EncodeToString(buf)
}
