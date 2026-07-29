package core

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	webdavDefaultRoot = "GameSync"
	// 412 并发冲突时的重读重试上限
	webdavCasRetries = 3
	// Depth:infinity 求和统计的文件数上限，超过则放弃统计，避免大目录拖慢验证
	webdavUsageSumLimit = 2000
)

const (
	webdavPropfindQuotaBody = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:quota-used-bytes/></d:prop></d:propfind>`
	webdavPropfindUsageBody = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:getcontentlength/><d:resourcetype/></d:prop></d:propfind>`
	webdavPropfindListBody  = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:getcontentlength/><d:getlastmodified/><d:resourcetype/></d:prop></d:propfind>`
)

// WebdavClient 用一个 WebDAV 目录同时承载目录索引（JSON 文件 + ETag CAS）与存档对象，
// 实现 CatalogStore 与 ObjectStore 双接口。服务器布局（相对 WebdavURL + WebdavRoot）：
//
//	catalog/catalog.json    目录索引整体文档
//	manifests/<gameID>.json 每游戏清单记录
//	objects/<key>           对象按现有 key 的路径层级存放
type WebdavClient struct {
	base         string // 规范化后的服务器地址（无结尾斜杠）
	rootSegments []string
	username     string
	password     string
	httpClient   *http.Client

	mu          sync.Mutex
	schemaReady bool
	ensuredDirs map[string]bool
}

var (
	_ CatalogStore = (*WebdavClient)(nil)
	_ ObjectStore  = (*WebdavClient)(nil)
)

// webdavCatalogDocument 对应 catalog/catalog.json 的存储结构
type webdavCatalogDocument struct {
	Revision    int64                              `json:"revision"`
	Catalog     RemoteCatalog                      `json:"catalog"`
	Credentials map[string]EncryptedCredentialBlob `json:"credentials,omitempty"`
	Device      DeviceInfo                         `json:"device"`
}

func NewWebdavClient(account CloudflareAccount) (*WebdavClient, error) {
	urlText := strings.TrimSpace(account.WebdavURL)
	username := strings.TrimSpace(account.WebdavUsername)
	password := strings.TrimSpace(account.WebdavPassword)
	if urlText == "" || username == "" || password == "" {
		return nil, errors.New(msgWebdavConfigIncomplete)
	}
	parsed, err := url.Parse(urlText)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New(msgWebdavURLInvalid)
	}

	root := strings.Trim(strings.TrimSpace(account.WebdavRoot), "/")
	if root == "" {
		root = webdavDefaultRoot
	}
	segments := make([]string, 0, 2)
	for _, segment := range strings.Split(root, "/") {
		if segment = strings.TrimSpace(segment); segment != "" {
			segments = append(segments, segment)
		}
	}

	return &WebdavClient{
		base:         strings.TrimRight(urlText, "/"),
		rootSegments: segments,
		username:     username,
		password:     password,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		ensuredDirs:  map[string]bool{},
	}, nil
}

// absoluteURL 拼接 base 与路径段；所有段逐一 PathEscape
func (c *WebdavClient) absoluteURL(segments []string) string {
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		parts = append(parts, url.PathEscape(segment))
	}
	return c.base + "/" + strings.Join(parts, "/")
}

// resourceURL 返回 root 之下资源的完整 URL
func (c *WebdavClient) resourceURL(segments ...string) string {
	return c.absoluteURL(append(append([]string{}, c.rootSegments...), segments...))
}

func (c *WebdavClient) do(ctx context.Context, method string, resource string, headers map[string]string, body io.Reader, contentLength int64) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, resource, body)
	if err != nil {
		return nil, fmt.Errorf("构造 WebDAV 请求失败: %w", err)
	}
	request.SetBasicAuth(c.username, c.password)
	if contentLength > 0 {
		request.ContentLength = contentLength
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("连接 WebDAV 服务器失败: %w", err)
	}
	return response, nil
}

func webdavStatusError(operation string, statusCode int) error {
	switch {
	case statusCode == http.StatusUnauthorized:
		return fmt.Errorf("%s失败：%s", operation, msgWebdavAuthFailed)
	case statusCode == http.StatusForbidden:
		return fmt.Errorf("%s失败：服务器拒绝访问（403），请检查目录权限", operation)
	case statusCode == http.StatusNotFound:
		return fmt.Errorf("%s失败：远端资源不存在（404）", operation)
	case statusCode == http.StatusInsufficientStorage:
		return fmt.Errorf("%s失败：服务器存储空间不足（507）", operation)
	case statusCode >= 500:
		return fmt.Errorf("%s失败：服务器错误（%d）", operation, statusCode)
	default:
		return fmt.Errorf("%s失败：请求被拒绝（%d）", operation, statusCode)
	}
}

func discardBody(response *http.Response) {
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
}

// EnsureSchema 递归 MKCOL 建 root 与 catalog/manifests/objects 子集合；405 视为已存在
func (c *WebdavClient) EnsureSchema(ctx context.Context) error {
	c.mu.Lock()
	ready := c.schemaReady
	c.mu.Unlock()
	if ready {
		return nil
	}

	targets := make([][]string, 0, len(c.rootSegments)+3)
	cumulative := make([]string, 0, len(c.rootSegments))
	for _, segment := range c.rootSegments {
		cumulative = append(cumulative, segment)
		targets = append(targets, append([]string{}, cumulative...))
	}
	for _, child := range []string{"catalog", "manifests", "objects"} {
		targets = append(targets, append(append([]string{}, c.rootSegments...), child))
	}
	for _, target := range targets {
		if err := c.mkcol(ctx, target); err != nil {
			return err
		}
	}

	c.mu.Lock()
	c.schemaReady = true
	c.mu.Unlock()
	return nil
}

func (c *WebdavClient) mkcol(ctx context.Context, segments []string) error {
	key := strings.Join(segments, "/")
	c.mu.Lock()
	done := c.ensuredDirs[key]
	c.mu.Unlock()
	if done {
		return nil
	}

	response, err := c.do(ctx, "MKCOL", c.absoluteURL(segments), nil, nil, 0)
	if err != nil {
		return err
	}
	discardBody(response)
	// 201 新建成功；405 表示集合已存在，幂等成功
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusMethodNotAllowed {
		return webdavStatusError("创建 WebDAV 目录", response.StatusCode)
	}

	c.mu.Lock()
	c.ensuredDirs[key] = true
	c.mu.Unlock()
	return nil
}

// getResource 读取资源内容与 ETag；404 返回 exists=false 且无错误
func (c *WebdavClient) getResource(ctx context.Context, resource string, operation string) ([]byte, string, bool, error) {
	response, err := c.do(ctx, http.MethodGet, resource, nil, nil, 0)
	if err != nil {
		return nil, "", false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, "", false, nil
	}
	if response.StatusCode >= 300 {
		return nil, "", false, webdavStatusError(operation, response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, "", false, fmt.Errorf("读取 WebDAV 响应失败: %w", err)
	}
	return body, strings.TrimSpace(response.Header.Get("ETag")), true, nil
}

// casPut 条件写：新建用 If-None-Match: *，覆盖用 If-Match: <etag>；412 返回 conflict=true。
// 服务器未返回 ETag（etag 为空）时回退"写后读校验"，由 verify 比对读回内容是否为本次写入。
func (c *WebdavClient) casPut(ctx context.Context, resource string, payload []byte, etag string, exists bool, operation string, verify func([]byte) bool) (bool, error) {
	headers := map[string]string{"Content-Type": "application/json"}
	if !exists {
		headers["If-None-Match"] = "*"
	} else if etag != "" {
		headers["If-Match"] = etag
	}
	response, err := c.do(ctx, http.MethodPut, resource, headers, bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return false, err
	}
	discardBody(response)
	if response.StatusCode == http.StatusPreconditionFailed {
		return true, nil
	}
	if response.StatusCode >= 300 {
		return false, webdavStatusError(operation, response.StatusCode)
	}
	if etag == "" && verify != nil {
		readBack, _, readExists, err := c.getResource(ctx, resource, operation)
		if err != nil {
			return false, err
		}
		if !readExists || !verify(readBack) {
			return true, nil
		}
	}
	return false, nil
}

func (c *WebdavClient) deleteResource(ctx context.Context, resource string, operation string) error {
	response, err := c.do(ctx, http.MethodDelete, resource, nil, nil, 0)
	if err != nil {
		return err
	}
	discardBody(response)
	// 404 视为已删除
	if response.StatusCode < 300 || response.StatusCode == http.StatusNotFound {
		return nil
	}
	return webdavStatusError(operation, response.StatusCode)
}

// ---- CatalogStore ----

func (c *WebdavClient) catalogResource() string {
	return c.resourceURL("catalog", "catalog.json")
}

func (c *WebdavClient) loadCatalogDocument(ctx context.Context) (webdavCatalogDocument, string, bool, error) {
	body, etag, exists, err := c.getResource(ctx, c.catalogResource(), "读取云端目录")
	if err != nil || !exists {
		return webdavCatalogDocument{}, "", exists, err
	}
	var document webdavCatalogDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return webdavCatalogDocument{}, "", false, fmt.Errorf("解析云端目录失败: %w", err)
	}
	return document, etag, true, nil
}

// sanitizeRemoteCatalog 与 D1 路径共用同一套脱敏规则：
// 明文密钥、本地路径不进入远端目录文件
func sanitizeRemoteCatalog(catalog RemoteCatalog) RemoteCatalog {
	sanitized := catalog
	sanitized.Games = make([]Game, 0, len(catalog.Games))
	for _, game := range catalog.Games {
		sanitized.Games = append(sanitized.Games, remoteCatalogGame(game, catalogTimestamp(game.CatalogUpdatedAt)))
	}
	sanitized.Accounts = make([]CloudflareAccount, 0, len(catalog.Accounts))
	for _, account := range catalog.Accounts {
		sanitized.Accounts = append(sanitized.Accounts, remoteCatalogAccount(account, catalogTimestamp(account.CatalogUpdatedAt)))
	}
	sanitized.Preferences = remoteCatalogPreferences(catalog.Preferences)
	if sanitized.Tombstones.Games == nil {
		sanitized.Tombstones.Games = map[string]time.Time{}
	}
	if sanitized.Tombstones.Accounts == nil {
		sanitized.Tombstones.Accounts = map[string]time.Time{}
	}
	return sanitized
}

// mergeWebdavCredentials 与 D1 行为对齐：本次未提供某账号密文时保留旧值，
// 避免整文档替换把已备份的加密凭据清空
func mergeWebdavCredentials(existing map[string]EncryptedCredentialBlob, updates map[string]EncryptedCredentialBlob) map[string]EncryptedCredentialBlob {
	merged := make(map[string]EncryptedCredentialBlob, len(existing)+len(updates))
	for id, blob := range existing {
		merged[id] = blob
	}
	for id, blob := range updates {
		merged[id] = blob
	}
	return merged
}

func (c *WebdavClient) SaveRemoteCatalog(ctx context.Context, catalog RemoteCatalog, encryptedCredentials map[string]EncryptedCredentialBlob, device DeviceInfo) (int64, error) {
	if err := c.EnsureSchema(ctx); err != nil {
		return 0, err
	}
	sanitized := sanitizeRemoteCatalog(catalog)
	for attempt := 0; attempt <= webdavCasRetries; attempt++ {
		current, etag, exists, err := c.loadCatalogDocument(ctx)
		if err != nil {
			return 0, err
		}
		next := webdavCatalogDocument{
			Revision:    current.Revision + 1,
			Catalog:     sanitized,
			Credentials: mergeWebdavCredentials(current.Credentials, encryptedCredentials),
			Device:      device,
		}
		next.Catalog.Handoff = current.Catalog.Handoff
		next.Catalog.StorageGeneration = current.Catalog.StorageGeneration
		next.Catalog.Revision = next.Revision
		payload, err := json.Marshal(next)
		if err != nil {
			return 0, fmt.Errorf("编码云端目录失败: %w", err)
		}
		conflict, err := c.casPut(ctx, c.catalogResource(), payload, etag, exists, "写入云端目录", func(readBack []byte) bool {
			var document webdavCatalogDocument
			return json.Unmarshal(readBack, &document) == nil && document.Revision == next.Revision
		})
		if err != nil {
			return 0, err
		}
		if !conflict {
			return next.Revision, nil
		}
	}
	return 0, errors.New(msgWebdavCatalogConflict)
}

func (c *WebdavClient) LoadRemoteCatalog(ctx context.Context) (RemoteCatalog, map[string]EncryptedCredentialBlob, error) {
	document, _, _, err := c.loadCatalogDocument(ctx)
	if err != nil {
		return RemoteCatalog{}, nil, err
	}
	// 文件不存在时 document 为零值，等价于 D1 空表：空目录 + revision 0
	catalog := document.Catalog
	if catalog.Accounts == nil {
		catalog.Accounts = []CloudflareAccount{}
	}
	if catalog.Games == nil {
		catalog.Games = []Game{}
	}
	if catalog.Tombstones.Games == nil {
		catalog.Tombstones.Games = map[string]time.Time{}
	}
	if catalog.Tombstones.Accounts == nil {
		catalog.Tombstones.Accounts = map[string]time.Time{}
	}
	catalog.Revision = document.Revision
	encrypted := document.Credentials
	if encrypted == nil {
		encrypted = map[string]EncryptedCredentialBlob{}
	}
	return catalog, encrypted, nil
}

func (c *WebdavClient) LoadStorageHandoff(ctx context.Context) (StorageHandoff, error) {
	document, _, _, err := c.loadCatalogDocument(ctx)
	if err != nil {
		return StorageHandoff{}, err
	}
	if document.Catalog.Handoff == nil {
		return StorageHandoff{}, nil
	}
	return *document.Catalog.Handoff, nil
}

func (c *WebdavClient) SaveStorageHandoffIfGeneration(ctx context.Context, handoff StorageHandoff, expectedGeneration int64) error {
	if err := c.EnsureSchema(ctx); err != nil {
		return err
	}
	if err := validateStorageHandoffUpdate(handoff, expectedGeneration); err != nil {
		return err
	}
	for attempt := 0; attempt <= webdavCasRetries; attempt++ {
		current, etag, exists, err := c.loadCatalogDocument(ctx)
		if err != nil {
			return err
		}
		currentHandoff := StorageHandoff{}
		if current.Catalog.Handoff != nil {
			currentHandoff = *current.Catalog.Handoff
		}
		if currentHandoff.Generation != expectedGeneration {
			return ErrStorageHandoffChanged
		}
		if handoff.Generation == expectedGeneration && currentHandoff.TransactionID != handoff.TransactionID {
			return ErrStorageHandoffChanged
		}
		next := current
		next.Revision = current.Revision + 1
		next.Catalog.Revision = next.Revision
		next.Catalog.StorageGeneration = handoff.Generation
		handoffCopy := handoff
		next.Catalog.Handoff = &handoffCopy
		payload, err := json.Marshal(next)
		if err != nil {
			return fmt.Errorf("encode storage handoff: %w", err)
		}
		conflict, err := c.casPut(ctx, c.catalogResource(), payload, etag, exists, "write storage handoff", func(readBack []byte) bool {
			var document webdavCatalogDocument
			return json.Unmarshal(readBack, &document) == nil && document.Revision == next.Revision &&
				document.Catalog.Handoff != nil && document.Catalog.Handoff.TransactionID == handoff.TransactionID &&
				document.Catalog.Handoff.State == handoff.State
		})
		if err != nil {
			return err
		}
		if !conflict {
			return nil
		}
	}
	return ErrStorageHandoffChanged
}

func (c *WebdavClient) LoadCatalogRevision(ctx context.Context) (int64, error) {
	document, _, _, err := c.loadCatalogDocument(ctx)
	if err != nil {
		return 0, err
	}
	return document.Revision, nil
}

func (c *WebdavClient) IncrementCatalogRevision(ctx context.Context) (int64, error) {
	if err := c.EnsureSchema(ctx); err != nil {
		return 0, err
	}
	for attempt := 0; attempt <= webdavCasRetries; attempt++ {
		current, etag, exists, err := c.loadCatalogDocument(ctx)
		if err != nil {
			return 0, err
		}
		next := current
		next.Revision = current.Revision + 1
		next.Catalog.Revision = next.Revision
		payload, err := json.Marshal(next)
		if err != nil {
			return 0, fmt.Errorf("编码云端目录失败: %w", err)
		}
		conflict, err := c.casPut(ctx, c.catalogResource(), payload, etag, exists, "写入云端目录", func(readBack []byte) bool {
			var document webdavCatalogDocument
			return json.Unmarshal(readBack, &document) == nil && document.Revision == next.Revision
		})
		if err != nil {
			return 0, err
		}
		if !conflict {
			return next.Revision, nil
		}
	}
	return 0, errors.New(msgWebdavCatalogConflict)
}

func (c *WebdavClient) manifestResource(gameID string) string {
	return c.resourceURL("manifests", strings.TrimSpace(gameID)+".json")
}

func (c *WebdavClient) LoadRemoteManifest(ctx context.Context, gameID string) (RemoteManifestRecord, error) {
	body, _, exists, err := c.getResource(ctx, c.manifestResource(gameID), "读取云端存档索引")
	if err != nil {
		return RemoteManifestRecord{}, err
	}
	if !exists {
		return RemoteManifestRecord{}, nil
	}
	var record RemoteManifestRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return RemoteManifestRecord{}, fmt.Errorf("解析云端存档索引失败: %w", err)
	}
	record.Manifest.Version = record.Version
	return record, nil
}

func (c *WebdavClient) SaveRemoteManifest(ctx context.Context, record RemoteManifestRecord) error {
	if err := c.EnsureSchema(ctx); err != nil {
		return err
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("编码云端存档索引失败: %w", err)
	}
	// 无条件覆盖，语义与 D1 upsert 一致
	if _, err := c.casPut(ctx, c.manifestResource(record.GameID), payload, "", true, "写入云端存档索引", nil); err != nil {
		return err
	}
	return nil
}

func (c *WebdavClient) SaveRemoteManifestIfVersion(ctx context.Context, record RemoteManifestRecord, expectedVersion int) error {
	if err := c.EnsureSchema(ctx); err != nil {
		return err
	}
	resource := c.manifestResource(record.GameID)
	body, etag, exists, err := c.getResource(ctx, resource, "读取云端存档索引")
	if err != nil {
		return err
	}
	if exists {
		var current RemoteManifestRecord
		if err := json.Unmarshal(body, &current); err != nil {
			return fmt.Errorf("解析云端存档索引失败: %w", err)
		}
		if current.Version != expectedVersion {
			return ErrRemoteManifestChanged
		}
	} else if expectedVersion != 0 {
		return ErrRemoteManifestChanged
	}

	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("编码云端存档索引失败: %w", err)
	}
	conflict, err := c.casPut(ctx, resource, payload, etag, exists, "写入云端存档索引", func(readBack []byte) bool {
		var current RemoteManifestRecord
		return json.Unmarshal(readBack, &current) == nil &&
			current.Version == record.Version &&
			current.Manifest.Hash == record.Manifest.Hash
	})
	if err != nil {
		return err
	}
	if conflict {
		return ErrRemoteManifestChanged
	}
	return nil
}

func (c *WebdavClient) ClearGameRecords(ctx context.Context, gameID string) error {
	return c.deleteResource(ctx, c.manifestResource(gameID), "删除云端存档索引")
}

func (c *WebdavClient) ValidateAccess(ctx context.Context) error {
	return c.validateReadWrite(ctx)
}

// ---- ObjectStore ----

func splitObjectKey(key string) []string {
	segments := []string{}
	for _, part := range strings.Split(strings.Trim(strings.TrimSpace(key), "/"), "/") {
		if part != "" {
			segments = append(segments, part)
		}
	}
	return segments
}

// ensureObjectParents 为对象 key 逐级建目录（root 与 objects 已由 EnsureSchema 保证）
func (c *WebdavClient) ensureObjectParents(ctx context.Context, segments []string) error {
	full := append(append([]string{}, c.rootSegments...), segments...)
	for i := len(c.rootSegments) + 1; i < len(full); i++ {
		if err := c.mkcol(ctx, full[:i]); err != nil {
			return err
		}
	}
	return nil
}

func (c *WebdavClient) resourceExists(ctx context.Context, resource string) (bool, error) {
	response, err := c.do(ctx, http.MethodHead, resource, nil, nil, 0)
	if err != nil {
		return false, err
	}
	discardBody(response)
	if response.StatusCode < 300 {
		return true, nil
	}
	if response.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, webdavStatusError("查询云端对象", response.StatusCode)
}

func (c *WebdavClient) PutObjectFromFile(ctx context.Context, key string, path string) error {
	if err := c.EnsureSchema(ctx); err != nil {
		return err
	}
	segments := append([]string{"objects"}, splitObjectKey(key)...)
	if len(segments) == 1 {
		return errors.New("对象键为空")
	}
	resource := c.resourceURL(segments...)

	if err := c.ensureObjectParents(ctx, segments); err != nil {
		return err
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开待上传文件失败: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("读取待上传文件信息失败: %w", err)
	}

	headers := map[string]string{"Content-Type": "application/octet-stream"}
	response, err := c.do(ctx, http.MethodPut, resource, headers, file, info.Size())
	if err != nil {
		return err
	}
	discardBody(response)
	if response.StatusCode >= 300 {
		return webdavStatusError("上传云端对象", response.StatusCode)
	}
	return nil
}

func (c *WebdavClient) DownloadObjectToFile(ctx context.Context, key string, path string) error {
	resource := c.resourceURL(append([]string{"objects"}, splitObjectKey(key)...)...)
	response, err := c.do(ctx, http.MethodGet, resource, nil, nil, 0)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return webdavStatusError("下载云端对象", response.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建下载目录失败: %w", err)
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("创建下载文件失败: %w", err)
	}
	defer file.Close()
	if _, err := io.Copy(file, response.Body); err != nil {
		return fmt.Errorf("写入下载文件失败: %w", err)
	}
	return nil
}

func (c *WebdavClient) GetObjectBytes(ctx context.Context, key string) ([]byte, error) {
	resource := c.resourceURL(append([]string{"objects"}, splitObjectKey(key)...)...)
	body, _, exists, err := c.getResource(ctx, resource, "读取云端对象")
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, webdavStatusError("读取云端对象", http.StatusNotFound)
	}
	return body, nil
}

func (c *WebdavClient) DeleteObject(ctx context.Context, key string) error {
	resource := c.resourceURL(append([]string{"objects"}, splitObjectKey(key)...)...)
	return c.deleteResource(ctx, resource, "删除云端对象")
}

// ClearPrefix 删除对象前缀对应的集合（WebDAV DELETE 目录即递归），404 视为成功
func (c *WebdavClient) ClearPrefix(ctx context.Context, prefix string) error {
	segments := splitObjectKey(prefix)
	if len(segments) == 0 {
		return errors.New("prefix is empty")
	}
	resource := c.resourceURL(append([]string{"objects"}, segments...)...)
	return c.deleteResource(ctx, resource, "删除云端对象目录")
}

func (c *WebdavClient) ClearGameFiles(ctx context.Context, gameID string) error {
	return c.ClearPrefix(ctx, fmt.Sprintf("games/%s/", gameID))
}

// ---- 用量与校验 ----

// ListObjects 实现 ObjectLister：PROPFIND Depth:1 列举前缀目录下的对象
// （云端备份列表/自动备份清理使用）。目录不存在按空列表处理，对齐 R2 空前缀行为。
func (c *WebdavClient) ListObjects(ctx context.Context, prefix string) ([]RemoteObjectInfo, error) {
	segments := append([]string{"objects"}, splitObjectKey(prefix)...)
	if len(segments) == 1 {
		return nil, errors.New("列举前缀为空")
	}
	result, status, err := c.propfind(ctx, c.resourceURL(segments...), "1", webdavPropfindListBody)
	if err != nil {
		return nil, err
	}
	if result == nil {
		if status == http.StatusNotFound {
			return nil, nil
		}
		return nil, webdavStatusError("列举云端对象", status)
	}

	normalizedPrefix := strings.Trim(strings.TrimSpace(prefix), "/")
	var objects []RemoteObjectInfo
	for _, entry := range result.Responses {
		for _, propstat := range entry.Propstats {
			if !propstatOK(propstat.Status) || propstat.Prop.ResourceType.Collection != nil {
				continue
			}
			href := strings.TrimSuffix(strings.TrimSpace(entry.Href), "/")
			if unescaped, err := url.PathUnescape(href); err == nil {
				href = unescaped
			}
			filename := href
			if idx := strings.LastIndex(href, "/"); idx >= 0 {
				filename = href[idx+1:]
			}
			if filename == "" {
				continue
			}
			info := RemoteObjectInfo{Key: normalizedPrefix + "/" + filename}
			if value := strings.TrimSpace(propstat.Prop.GetContentLength); value != "" {
				if size, err := strconv.ParseInt(value, 10, 64); err == nil && size >= 0 {
					info.Size = size
				}
			}
			if value := strings.TrimSpace(propstat.Prop.GetLastModified); value != "" {
				if parsed, err := http.ParseTime(value); err == nil {
					info.LastModified = parsed
				}
			}
			objects = append(objects, info)
			break
		}
	}
	return objects, nil
}

type webdavMultistatus struct {
	Responses []webdavPropfindEntry `xml:"response"`
}

type webdavPropfindEntry struct {
	Href      string           `xml:"href"`
	Propstats []webdavPropstat `xml:"propstat"`
}

type webdavPropstat struct {
	Status string     `xml:"status"`
	Prop   webdavProp `xml:"prop"`
}

type webdavProp struct {
	QuotaUsedBytes   string             `xml:"quota-used-bytes"`
	GetContentLength string             `xml:"getcontentlength"`
	GetLastModified  string             `xml:"getlastmodified"`
	ResourceType     webdavResourceType `xml:"resourcetype"`
}

type webdavResourceType struct {
	Collection *struct{} `xml:"collection"`
}

// propfind 执行 PROPFIND；状态码 >=300 时返回 (nil, status, nil) 由调用方按语义处理
func (c *WebdavClient) propfind(ctx context.Context, resource string, depth string, body string) (*webdavMultistatus, int, error) {
	headers := map[string]string{"Depth": depth, "Content-Type": "application/xml"}
	response, err := c.do(ctx, "PROPFIND", resource, headers, strings.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, response.StatusCode, fmt.Errorf("读取 WebDAV 响应失败: %w", err)
	}
	if response.StatusCode >= 300 {
		return nil, response.StatusCode, nil
	}
	var status webdavMultistatus
	if err := xml.Unmarshal(raw, &status); err != nil {
		return nil, response.StatusCode, fmt.Errorf("解析 WebDAV 响应失败: %w", err)
	}
	return &status, response.StatusCode, nil
}

func propstatOK(status string) bool {
	return strings.Contains(status, "200")
}

// quotaUsedBytes 通过 RFC4331 quota-used-bytes 查询用量；ok=false 表示服务器未提供配额
func (c *WebdavClient) quotaUsedBytes(ctx context.Context) (int64, bool, error) {
	result, status, err := c.propfind(ctx, c.resourceURL(), "0", webdavPropfindQuotaBody)
	if err != nil {
		return 0, false, err
	}
	if status == http.StatusNotFound {
		// root 尚未创建，视为无占用
		return 0, true, nil
	}
	if status == http.StatusUnauthorized {
		return 0, false, webdavStatusError("查询云端存储用量", status)
	}
	if result == nil {
		return 0, false, nil
	}
	for _, entry := range result.Responses {
		for _, propstat := range entry.Propstats {
			if !propstatOK(propstat.Status) {
				continue
			}
			value := strings.TrimSpace(propstat.Prop.QuotaUsedBytes)
			if value == "" {
				continue
			}
			// 部分服务器用负数表示配额不可用
			if used, err := strconv.ParseInt(value, 10, 64); err == nil && used >= 0 {
				return used, true, nil
			}
		}
	}
	return 0, false, nil
}

// sumUsageBytes 对 root 做 Depth:infinity PROPFIND 求和；服务器拒绝或条目过多时放弃返回 0
func (c *WebdavClient) sumUsageBytes(ctx context.Context) (int64, error) {
	result, status, err := c.propfind(ctx, c.resourceURL(), "infinity", webdavPropfindUsageBody)
	if err != nil {
		return 0, err
	}
	if status == http.StatusUnauthorized {
		return 0, webdavStatusError("查询云端存储用量", status)
	}
	if result == nil {
		return 0, nil
	}
	var total int64
	count := 0
	for _, entry := range result.Responses {
		for _, propstat := range entry.Propstats {
			if !propstatOK(propstat.Status) || propstat.Prop.ResourceType.Collection != nil {
				continue
			}
			value := strings.TrimSpace(propstat.Prop.GetContentLength)
			if value == "" {
				continue
			}
			size, err := strconv.ParseInt(value, 10, 64)
			if err != nil || size < 0 {
				continue
			}
			count++
			if count > webdavUsageSumLimit {
				return 0, nil
			}
			total += size
			break
		}
	}
	return total, nil
}

func (c *WebdavClient) FetchAccountUsageBytes(ctx context.Context) (int64, error) {
	used, ok, err := c.quotaUsedBytes(ctx)
	if err != nil {
		return 0, err
	}
	if ok {
		return used, nil
	}
	return c.sumUsageBytes(ctx)
}

// validateReadWrite 验证连通与认证（PROPFIND root）+ 可写（写删探针文件）
func (c *WebdavClient) validateReadWrite(ctx context.Context) error {
	if err := c.EnsureSchema(ctx); err != nil {
		return err
	}
	_, status, err := c.propfind(ctx, c.resourceURL(), "0", webdavPropfindQuotaBody)
	if err != nil {
		return err
	}
	// 个别服务器不支持 PROPFIND（405）时不阻断，仍以探针写删为准
	if status >= 300 && status != http.StatusMethodNotAllowed {
		return webdavStatusError("访问 WebDAV 目录", status)
	}

	probe := c.resourceURL(fmt.Sprintf(".gamesync-probe-%d", time.Now().UnixNano()))
	if _, err := c.casPut(ctx, probe, []byte("gamesync probe"), "", true, "写入探针文件", nil); err != nil {
		return err
	}
	return c.deleteResource(ctx, probe, "删除探针文件")
}

func (c *WebdavClient) ValidateBucketAccess(ctx context.Context) error {
	return c.validateReadWrite(ctx)
}

// VerifyWebdavAccount 连通 + 可写 + 配额探测，回填字段语义对齐 VerifyCloudflareAccount
func VerifyWebdavAccount(ctx context.Context, account CloudflareAccount) (CloudflareAccount, error) {
	verified := account
	now := time.Now()
	verified.LastVerifiedAt = &now
	verified.TokenExpiresAt = nil
	verified.LastError = ""
	verified.UsageWarning = ""

	client, err := NewWebdavClient(account)
	if err != nil {
		verified.LastError = err.Error()
		return verified, err
	}
	if err := client.ValidateAccess(ctx); err != nil {
		verified.LastError = err.Error()
		return verified, err
	}

	usedBytes, err := client.FetchAccountUsageBytes(ctx)
	if err != nil {
		verified.UsageWarning = err.Error()
		return verified, nil
	}
	verified.UsedBytes = usedBytes
	return verified, nil
}
