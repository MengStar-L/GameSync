package core

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrStorageHandoffChanged = errors.New("remote storage handoff changed")

// 存储 provider 取值契约：仅 "cloudflare" | "webdav"，空值按 cloudflare 处理
const (
	ProviderCloudflare = "cloudflare"
	ProviderWebdav     = "webdav"
)

// RemoteObjectInfo 按前缀列举云端对象时返回的元信息
type RemoteObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// ObjectLister 是 ObjectStore 的可选扩展能力：按前缀列举对象。
// 云端备份列表与旧自动备份清理依赖它；R2 与 WebDAV 后端均实现。
type ObjectLister interface {
	ListObjects(ctx context.Context, prefix string) ([]RemoteObjectInfo, error)
}

// RemoteManifestHeadLister is an optional lightweight capability used by the
// background coordinator to discover changed manifests without loading them.
type RemoteManifestHeadLister interface {
	ListRemoteManifestHeads(ctx context.Context) ([]RemoteManifestHead, error)
}

// CatalogStore 承载目录索引与游戏清单（现由 D1 实现，WebDAV 用 JSON 文件 + ETag CAS 实现）
type CatalogStore interface {
	EnsureSchema(ctx context.Context) error
	SaveRemoteCatalog(ctx context.Context, catalog RemoteCatalog, encryptedCredentials map[string]EncryptedCredentialBlob, device DeviceInfo) (int64, error)
	LoadRemoteCatalog(ctx context.Context) (RemoteCatalog, map[string]EncryptedCredentialBlob, error)
	LoadCatalogRevision(ctx context.Context) (int64, error)
	IncrementCatalogRevision(ctx context.Context) (int64, error)
	LoadStorageHandoff(ctx context.Context) (StorageHandoff, error)
	SaveStorageHandoffIfGeneration(ctx context.Context, handoff StorageHandoff, expectedGeneration int64) error
	LoadRemoteManifest(ctx context.Context, gameID string) (RemoteManifestRecord, error)
	SaveRemoteManifest(ctx context.Context, record RemoteManifestRecord) error
	SaveRemoteManifestIfVersion(ctx context.Context, record RemoteManifestRecord, expectedVersion int) error
	ClearGameRecords(ctx context.Context, gameID string) error
	ValidateAccess(ctx context.Context) error
}

// ObjectStore 承载存档对象/备份 zip/封面（现由 R2 实现）
type ObjectStore interface {
	PutObjectFromFile(ctx context.Context, key string, path string) error
	DownloadObjectToFile(ctx context.Context, key string, path string) error
	GetObjectBytes(ctx context.Context, key string) ([]byte, error)
	DeleteObject(ctx context.Context, key string) error
	ClearPrefix(ctx context.Context, prefix string) error
	ClearGameFiles(ctx context.Context, gameID string) error
	FetchAccountUsageBytes(ctx context.Context) (int64, error)
	ValidateBucketAccess(ctx context.Context) error
}

type StorageGateway struct {
	Catalog CatalogStore
	Objects ObjectStore
}

// 现有实现必须持续满足接口（编译期断言）
var (
	_ CatalogStore             = (*D1Client)(nil)
	_ RemoteManifestHeadLister = (*D1Client)(nil)
	_ ObjectStore              = (*R2Client)(nil)
)

// AccountProvider 归一化账号的存储 provider：空值一律按 cloudflare 处理（历史数据兼容）
func AccountProvider(account CloudflareAccount) string {
	if strings.EqualFold(strings.TrimSpace(account.Provider), ProviderWebdav) {
		return ProviderWebdav
	}
	return ProviderCloudflare
}
