# WebDAV 存储后端功能规范

目标：同步存储从"仅 Cloudflare（D1 目录 + R2 对象）"扩展为可选 **WebDAV**（Nextcloud/坚果云/
群晖等）。首次启动的欢迎流程允许用户选择存储方式。存储方式由主账号的 provider 决定，无独立开关。

## 1. 接口抽象（internal/core/storage.go，新文件，签名逐字执行）

```go
// CatalogStore 承载目录索引与游戏清单（现由 D1 实现，WebDAV 用 JSON 文件 + ETag CAS 实现）
type CatalogStore interface {
    EnsureSchema(ctx context.Context) error
    SaveRemoteCatalog(ctx context.Context, catalog RemoteCatalog, encryptedCredentials map[string]EncryptedCredentialBlob, device DeviceInfo) (int64, error)
    LoadRemoteCatalog(ctx context.Context) (RemoteCatalog, map[string]EncryptedCredentialBlob, error)
    LoadCatalogRevision(ctx context.Context) (int64, error)
    IncrementCatalogRevision(ctx context.Context) (int64, error)
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
```

现有 `CloudflareGateway{D1,R2}` 全部替换为 `StorageGateway{Catalog,Objects}`；
`gateway.D1.X` → `gateway.Catalog.X`，`gateway.R2.Y` → `gateway.Objects.Y`。
D1Client 与 R2Client 方法签名如与接口有出入（指针接收者等），以接口为准做最小适配。

## 2. 账号模型扩展（models.go CloudflareAccount，字段追加，不改名）

```go
Provider       string `json:"provider,omitempty"`        // "cloudflare"(默认/空) | "webdav"
WebdavURL      string `json:"webdavUrl,omitempty"`       // 例 https://dav.example.com/remote.php/dav/files/user
WebdavUsername string `json:"webdavUsername,omitempty"`
WebdavPassword string `json:"webdavPassword,omitempty"`  // 建议用应用专用密码
WebdavRoot     string `json:"webdavRoot,omitempty"`      // 服务器上的根目录，默认 "GameSync"
```

- provider 为空一律按 "cloudflare" 处理（历史数据兼容）。
- WebdavPassword 的加密落盘/凭据备份路径与 apiToken/r2Secret 同等对待
 （检查 state_secrets.go / credentials.go 的既有加密清单并补入）。
- 校验：webdav 账号必填 WebdavURL/Username/Password；cloudflare 账号维持原必填。

## 3. WebDAV 实现（internal/core/webdav.go，新文件）

`WebdavClient`（实现 CatalogStore + ObjectStore 双接口）+
`NewWebdavClient(account CloudflareAccount) (*WebdavClient, error)`。

服务器布局（root 相对账号 WebdavURL + WebdavRoot）：
- `catalog/catalog.json` —— {revision int64, catalog RemoteCatalog, credentials map, device}
- `manifests/<gameID>.json` —— RemoteManifestRecord 原样 JSON
- `objects/<key>` —— key 即现有对象键（`games/<id>/objects/<sha>` 等），按路径存文件

语义映射：
- EnsureSchema = 递归 MKCOL 建 root/catalog/manifests/objects（405 视为已存在）。
- CAS：GET 记录 ETag → PUT 带 If-Match（新建用 If-None-Match: *）；412 = 并发冲突。
  服务器未返回 ETag 时回退"写后读校验"（对比 revision/version 字段）。
- SaveRemoteCatalog：读 catalog.json（拿 ETag 与旧 revision）→ 写入 revision=旧+1 的新文档
  （If-Match CAS，412 时重读重试 ≤3 次）→ 返回新 revision。
  IncrementCatalogRevision 同法只改 revision；LoadCatalogRevision 只读 revision 字段；
  文件不存在返回 revision 0 / 空目录（与 D1 空表行为一致）。
- SaveRemoteManifestIfVersion：读 manifests/<id>.json 校验 Version==expected → If-Match PUT；
  版本不符返回 ErrRemoteManifestChanged（复用现有错误变量）。
- ClearGameRecords = 删 manifests/<id>.json；ClearGameFiles/ClearPrefix = DELETE 对应目录/
  前缀（WebDAV DELETE 目录即递归；对不存在的 404 视为成功）。
- FetchAccountUsageBytes：PROPFIND root 取 RFC4331 quota-used-bytes；服务器不支持时对
  root 做 Depth:infinity PROPFIND 求和（>2000 项则放弃返回 0，避免慢）。
- ValidateAccess/ValidateBucketAccess：PROPFIND root（验证连通+认证）+ 写删一个探针文件
 （验证可写）。
- HTTP：Basic Auth；PROPFIND/MKCOL 用自定义 method 的 http.Request；超时 30s；
  所有路径段 url.PathEscape；4xx/5xx 错误包含中文说明（401→"认证失败，请检查用户名与应用密码"）。
- `VerifyWebdavAccount(ctx, account) (CloudflareAccount, error)`：连通+可写+配额探测，
  回填 verificationState/usedBytes/lastError，语义对齐 VerifyCloudflareAccount。

## 4. 接线（app.go）

- 新增 `newCatalogStore(account) (CatalogStore, error)` 与 `newObjectStore(ctx, account)
  (ObjectStore, error)`：按 account.Provider 分支（cloudflare→D1/R2，webdav→WebdavClient）。
- 所有 `core.NewD1Client(primary)` 调用点改走 newCatalogStore；`core.NewR2Client(ctx, acc)`
  改走 newObjectStore；`CloudflareGateway` 构造点改 StorageGateway。
- VerifyAccount / verifyAccounts 按 provider 分支调用对应 Verify*。
- 主账号说明：webdav 主账号 = 目录索引存于该 DAV 服务器；副账号仍可为任意 provider
 （对象存储按每游戏 storageAccountId 的账号 provider 构建，现有逐账号构建逻辑天然支持）。
- 涉及 D1 专属逻辑（如 credentialsBackedUp / recovery）在 webdav 主账号下的降级：
  凭据加密备份照常写入 catalog.json 的 credentials 字段（结构不变，无需特判）。

## 5. 前端

**welcome.js**：三选卡之前插入"选择存储方式"步骤（点"配置云端存储"后进入）：
两张大卡 —— Cloudflare R2（"免费额度大 · 需要一点配置"）与 WebDAV（"Nextcloud/坚果云/NAS ·
填地址账号密码即用"）→ `router.navigate('accounts', { openNew: true, provider: 'webdav'|'cloudflare' })`。
恢复备份/先逛逛两卡保留。

**accounts.js**：
- 抽屉表单顶部：新建首个账号时显示 provider 分段选择（.seg，默认取 params.provider 或
  cloudflare）；编辑时只读展示。
- webdav 字段组：服务器地址(URL,必填)、用户名(必填)、密码(secret,必填)、根目录(默认 GameSync)；
  cloudflare 维持原字段组。脏字段提交机制保持。
- 票券卡：provider 徽章（badge info "WebDAV" / badge mute "Cloudflare"）；webdav 卡主行显示
  服务器 host（new URL(...).host，解析失败显示原串），隐藏 Account ID/D1 行。
- params.openNew + params.provider 联动自动开抽屉。

**mock.js**：账号样例补 provider 字段；新增一个 webdav 示例账号；SaveAccount/VerifyAccount
不需真实校验。

**settings.js**：架构说明区补一段 WebDAV 模式说明（目录索引与对象都存 DAV 目录）。

## 6. 纪律

- go build ./... + go test ./internal/core/... 必须过；新增 webdav_test.go 用 httptest 模拟
  DAV 服务器覆盖：CAS 冲突(412)、无 ETag 回退、EnsureSchema 幂等、Validate 探针。
- 不回退近期修复（脏字段提交/FLIP/增量渲染/同步修复）。用户可见文案中文。
- JS 改动 node --check；契约：provider 值仅 'cloudflare'|'webdav'。
