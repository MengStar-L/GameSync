package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- 模拟 DAV 服务器 ----

type fakeDavFile struct {
	data []byte
	etag string
}

type fakeDavServer struct {
	mu          sync.Mutex
	files       map[string]*fakeDavFile
	collections map[string]bool
	noETag      bool // 不返回 ETag 且忽略条件头，模拟不支持 CAS 的服务器
	quotaUsed   int64
	etagSeq     int
	methodCount map[string]int
	afterGet    func(path string) // GET 响应之后触发，模拟并发写
	afterPut    func(path string) // PUT 成功之后触发，模拟无 ETag 场景的并发写
	username    string
	password    string
}

func newFakeDavServer() *fakeDavServer {
	return &fakeDavServer{
		files:       map[string]*fakeDavFile{},
		collections: map[string]bool{"/": true},
		quotaUsed:   -1,
		methodCount: map[string]int{},
		username:    "alice",
		password:    "secret",
	}
}

func parentOf(path string) string {
	index := strings.LastIndex(strings.TrimRight(path, "/"), "/")
	if index <= 0 {
		return "/"
	}
	return path[:index]
}

// setFile 直接改写服务器上的文件（模拟另一台设备的并发写入）
func (s *fakeDavServer) setFile(path string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.etagSeq++
	s.files[path] = &fakeDavFile{data: data, etag: fmt.Sprintf("%q", fmt.Sprintf("v%d", s.etagSeq))}
}

func (s *fakeDavServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, pass, ok := r.BasicAuth()
	if !ok || user != s.username || pass != s.password {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	s.methodCount[r.Method]++
	s.mu.Unlock()

	path := r.URL.Path
	switch r.Method {
	case "MKCOL":
		s.handleMkcol(w, path)
	case http.MethodGet, http.MethodHead:
		s.handleGet(w, r, path)
	case http.MethodPut:
		s.handlePut(w, r, path)
	case http.MethodDelete:
		s.handleDelete(w, path)
	case "PROPFIND":
		s.handlePropfind(w, r, path)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *fakeDavServer) handleMkcol(w http.ResponseWriter, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.collections[path] || s.files[path] != nil {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.collections[parentOf(path)] {
		w.WriteHeader(http.StatusConflict)
		return
	}
	s.collections[path] = true
	w.WriteHeader(http.StatusCreated)
}

func (s *fakeDavServer) handleGet(w http.ResponseWriter, r *http.Request, path string) {
	s.mu.Lock()
	file := s.files[path]
	hook := s.afterGet
	noETag := s.noETag
	s.mu.Unlock()
	if file == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if !noETag {
		w.Header().Set("ETag", file.etag)
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(file.data)
	}
	if hook != nil && r.Method == http.MethodGet {
		hook(path)
	}
}

func (s *fakeDavServer) handlePut(w http.ResponseWriter, r *http.Request, path string) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	if !s.collections[parentOf(path)] {
		s.mu.Unlock()
		w.WriteHeader(http.StatusConflict)
		return
	}
	existing := s.files[path]
	if !s.noETag {
		if r.Header.Get("If-None-Match") == "*" && existing != nil {
			s.mu.Unlock()
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if match := r.Header.Get("If-Match"); match != "" && (existing == nil || existing.etag != match) {
			s.mu.Unlock()
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
	}
	s.etagSeq++
	s.files[path] = &fakeDavFile{data: body, etag: fmt.Sprintf("%q", fmt.Sprintf("v%d", s.etagSeq))}
	hook := s.afterPut
	s.mu.Unlock()
	w.WriteHeader(http.StatusCreated)
	if hook != nil {
		hook(path)
	}
}

func (s *fakeDavServer) handleDelete(w http.ResponseWriter, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files[path] != nil {
		delete(s.files, path)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if s.collections[path] {
		// WebDAV 语义：删除集合即递归删除全部子项
		delete(s.collections, path)
		prefix := path + "/"
		for key := range s.files {
			if strings.HasPrefix(key, prefix) {
				delete(s.files, key)
			}
		}
		for key := range s.collections {
			if strings.HasPrefix(key, prefix) {
				delete(s.collections, key)
			}
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (s *fakeDavServer) handlePropfind(w http.ResponseWriter, r *http.Request, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	isCollection := s.collections[path]
	file := s.files[path]
	if !isCollection && file == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">`)
	if isCollection {
		builder.WriteString(`<d:response><d:href>` + path + `</d:href><d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop><d:resourcetype><d:collection/></d:resourcetype>`)
		if s.quotaUsed >= 0 {
			fmt.Fprintf(&builder, `<d:quota-used-bytes>%d</d:quota-used-bytes>`, s.quotaUsed)
		}
		builder.WriteString(`</d:prop></d:propstat></d:response>`)
	} else {
		writeFakeDavFileProp(&builder, path, file, !s.noETag)
	}
	if (r.Header.Get("Depth") == "1" || r.Header.Get("Depth") == "infinity") && isCollection {
		prefix := path + "/"
		for key, entry := range s.files {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			if r.Header.Get("Depth") == "1" && parentOf(key) != path {
				continue
			}
			writeFakeDavFileProp(&builder, key, entry, !s.noETag)
		}
	}
	builder.WriteString(`</d:multistatus>`)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = io.WriteString(w, builder.String())
}

func writeFakeDavFileProp(builder *strings.Builder, path string, file *fakeDavFile, includeETag bool) {
	fmt.Fprintf(builder, `<d:response><d:href>%s</d:href><d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop><d:resourcetype/><d:getcontentlength>%d</d:getcontentlength>`, path, len(file.data))
	if includeETag {
		fmt.Fprintf(builder, `<d:getetag>%s</d:getetag>`, file.etag)
	}
	builder.WriteString(`</d:prop></d:propstat></d:response>`)
}

// ---- 测试辅助 ----

func newTestWebdavClient(t *testing.T, serverURL string) *WebdavClient {
	t.Helper()
	client, err := NewWebdavClient(CloudflareAccount{
		Provider:       ProviderWebdav,
		WebdavURL:      serverURL,
		WebdavUsername: "alice",
		WebdavPassword: "secret",
		WebdavRoot:     "GameSync",
	})
	if err != nil {
		t.Fatalf("NewWebdavClient failed: %v", err)
	}
	return client
}

func startFakeDav(t *testing.T) (*fakeDavServer, *httptest.Server) {
	t.Helper()
	fake := newFakeDavServer()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, server
}

const fakeCatalogPath = "/GameSync/catalog/catalog.json"

// ---- 测试用例 ----

func TestWebdavEnsureSchemaIdempotent(t *testing.T) {
	fake, server := startFakeDav(t)
	ctx := context.Background()

	client := newTestWebdavClient(t, server.URL)
	if err := client.EnsureSchema(ctx); err != nil {
		t.Fatalf("first EnsureSchema failed: %v", err)
	}
	for _, path := range []string{"/GameSync", "/GameSync/catalog", "/GameSync/manifests", "/GameSync/objects"} {
		if !fake.collections[path] {
			t.Fatalf("collection %s not created", path)
		}
	}

	// 目录已存在时服务器对 MKCOL 返回 405，新客户端重跑必须幂等成功
	second := newTestWebdavClient(t, server.URL)
	if err := second.EnsureSchema(ctx); err != nil {
		t.Fatalf("second EnsureSchema failed (405 should be tolerated): %v", err)
	}
}

func TestWebdavSaveRemoteCatalogRevisionAndConflictRetry(t *testing.T) {
	fake, server := startFakeDav(t)
	ctx := context.Background()
	client := newTestWebdavClient(t, server.URL)

	// 文件不存在时读空目录 / revision 0
	if revision, err := client.LoadCatalogRevision(ctx); err != nil || revision != 0 {
		t.Fatalf("empty revision = %d, err = %v; want 0, nil", revision, err)
	}
	emptyCatalog, emptyCreds, err := client.LoadRemoteCatalog(ctx)
	if err != nil || len(emptyCatalog.Games) != 0 || len(emptyCreds) != 0 {
		t.Fatalf("empty catalog load = %+v creds %v err %v", emptyCatalog, emptyCreds, err)
	}

	catalog := RemoteCatalog{
		Games: []Game{{ID: "g1", Name: "游戏一", CatalogUpdatedAt: time.Now()}},
		Accounts: []CloudflareAccount{{
			ID: "acc1", Name: "主账号", IsPrimary: true,
			APIToken: "plain-secret-token", CatalogUpdatedAt: time.Now(),
		}},
		Preferences: &RemotePreferences{
			AutoSyncOnLaunch:              true,
			StartupSyncMode:               "cloud-first",
			ConflictPolicy:                "manual",
			BackgroundSyncIntervalSeconds: 30,
			SyncSettingsUpdatedAt:         time.Now(),
			GameCardMode:                  GameCardModeOverlayHover,
			GameCardModeUpdatedAt:         time.Now(),
			RawgAPIKey:                    "rawg-preference-secret",
			RawgAPIKeyUpdatedAt:           time.Now(),
			SteamGridDBAPIKey:             "sgdb-preference-secret",
			SteamGridDBAPIKeyUpdatedAt:    time.Now(),
		},
	}
	credentials := map[string]EncryptedCredentialBlob{
		"acc1":           {Version: 1, KDF: "argon2id", Ciphertext: "cipher-a"},
		"legacy-account": {Version: 1, KDF: "argon2id", Ciphertext: "stale-cipher"},
	}
	device := DeviceInfo{ID: "dev1", Name: "机器A"}

	revision, err := client.SaveRemoteCatalog(ctx, catalog, credentials, device)
	if err != nil || revision != 1 {
		t.Fatalf("first save revision = %d, err = %v; want 1", revision, err)
	}
	revision, err = client.SaveRemoteCatalog(ctx, catalog, credentials, device)
	if err != nil || revision != 2 {
		t.Fatalf("second save revision = %d, err = %v; want 2", revision, err)
	}

	// 明文密钥不得进入远端目录文件
	stored := string(fake.files[fakeCatalogPath].data)
	if strings.Contains(stored, "plain-secret-token") {
		t.Fatalf("stored catalog leaks plaintext api token: %s", stored)
	}
	if !strings.Contains(stored, "rawg-preference-secret") || !strings.Contains(stored, "sgdb-preference-secret") {
		t.Fatalf("stored catalog omitted synchronized preference keys: %s", stored)
	}
	if !strings.Contains(stored, "cipher-a") {
		t.Fatalf("stored catalog missing encrypted credentials: %s", stored)
	}
	if strings.Contains(stored, "stale-cipher") {
		t.Fatalf("stored catalog retained credentials for a removed account: %s", stored)
	}

	// 本次未提供的密文保留旧值
	revision, err = client.SaveRemoteCatalog(ctx, catalog, map[string]EncryptedCredentialBlob{}, device)
	if err != nil || revision != 3 {
		t.Fatalf("third save revision = %d, err = %v; want 3", revision, err)
	}
	_, loadedCreds, err := client.LoadRemoteCatalog(ctx)
	if err != nil || loadedCreds["acc1"].Ciphertext != "cipher-a" {
		t.Fatalf("credentials not retained across saves: %v, err %v", loadedCreds, err)
	}

	// 412 冲突重试：客户端 GET 之后另一端把 revision 抬到 9，If-Match 必 412，重读后写出 10
	var once sync.Once
	fake.afterGet = func(path string) {
		if path != fakeCatalogPath {
			return
		}
		once.Do(func() {
			fake.setFile(fakeCatalogPath, []byte(`{"revision":9,"catalog":{"accounts":[],"games":[]}}`))
		})
	}
	revision, err = client.SaveRemoteCatalog(ctx, catalog, credentials, device)
	if err != nil || revision != 10 {
		t.Fatalf("conflicted save revision = %d, err = %v; want 10", revision, err)
	}
	fake.afterGet = nil

	if revision, err := client.IncrementCatalogRevision(ctx); err != nil || revision != 11 {
		t.Fatalf("IncrementCatalogRevision = %d, err = %v; want 11", revision, err)
	}
	if revision, err := client.LoadCatalogRevision(ctx); err != nil || revision != 11 {
		t.Fatalf("LoadCatalogRevision = %d, err = %v; want 11", revision, err)
	}

	loaded, _, err := client.LoadRemoteCatalog(ctx)
	if err != nil || len(loaded.Games) != 1 || loaded.Games[0].ID != "g1" || loaded.Revision != 11 {
		t.Fatalf("LoadRemoteCatalog = %+v, err = %v", loaded, err)
	}
	if loaded.Preferences == nil || loaded.Preferences.BackgroundSyncIntervalSeconds != 30 ||
		loaded.Preferences.GameCardMode != GameCardModeOverlayHover ||
		loaded.Preferences.RawgAPIKey != "rawg-preference-secret" || loaded.Preferences.SteamGridDBAPIKey != "sgdb-preference-secret" {
		t.Fatalf("LoadRemoteCatalog preferences = %+v", loaded.Preferences)
	}
}

func TestWebdavStorageHandoffCompareAndSwap(t *testing.T) {
	fake := newFakeDavServer()
	server := httptest.NewServer(fake)
	defer server.Close()
	client := newTestWebdavClient(t, server.URL)
	ctx := context.Background()

	prepared := StorageHandoff{
		TransactionID: "tx-1", SourceAccountID: "source", TargetAccountID: "target",
		State: StorageHandoffPrepared, Generation: 1,
	}
	if err := client.SaveStorageHandoffIfGeneration(ctx, prepared, 0); err != nil {
		t.Fatal(err)
	}
	loaded, err := client.LoadStorageHandoff(ctx)
	if err != nil || loaded.TransactionID != prepared.TransactionID || loaded.State != StorageHandoffPrepared {
		t.Fatalf("loaded handoff = %+v, err = %v", loaded, err)
	}

	competing := StorageHandoff{
		TransactionID: "tx-2", SourceAccountID: "source", TargetAccountID: "other",
		State: StorageHandoffCommitted, Generation: 1,
	}
	if err := client.SaveStorageHandoffIfGeneration(ctx, competing, 1); !errors.Is(err, ErrStorageHandoffChanged) {
		t.Fatalf("competing handoff error = %v", err)
	}

	committed := prepared
	committed.State = StorageHandoffCommitted
	committed.CommittedAt = time.Now()
	if err := client.SaveStorageHandoffIfGeneration(ctx, committed, 1); err != nil {
		t.Fatal(err)
	}
	loaded, err = client.LoadStorageHandoff(ctx)
	if err != nil || loaded.State != StorageHandoffCommitted || loaded.Generation != 1 {
		t.Fatalf("committed handoff = %+v, err = %v", loaded, err)
	}

	stale := committed
	stale.TransactionID = "tx-stale"
	stale.Generation = 2
	if err := client.SaveStorageHandoffIfGeneration(ctx, stale, 0); !errors.Is(err, ErrStorageHandoffChanged) {
		t.Fatalf("stale generation error = %v", err)
	}
}

func TestWebdavSaveRemoteManifestIfVersion(t *testing.T) {
	_, server := startFakeDav(t)
	ctx := context.Background()
	client := newTestWebdavClient(t, server.URL)

	record := func(version int, hash string) RemoteManifestRecord {
		return RemoteManifestRecord{
			GameID:          "g1",
			Version:         version,
			Manifest:        SyncManifest{Version: version, Hash: hash, TotalBytes: 10},
			UpdatedAt:       time.Now(),
			UpdatedByDevice: "dev1",
		}
	}

	// 文件不存在且 expected != 0 → 冲突
	if err := client.SaveRemoteManifestIfVersion(ctx, record(4, "h4"), 3); !errors.Is(err, ErrRemoteManifestChanged) {
		t.Fatalf("missing manifest with expected 3: err = %v; want ErrRemoteManifestChanged", err)
	}
	// expected 0 新建
	if err := client.SaveRemoteManifestIfVersion(ctx, record(1, "h1"), 0); err != nil {
		t.Fatalf("create manifest failed: %v", err)
	}
	// expected 与当前一致 → 覆盖
	if err := client.SaveRemoteManifestIfVersion(ctx, record(2, "h2"), 1); err != nil {
		t.Fatalf("update manifest failed: %v", err)
	}
	// 版本不符 → ErrRemoteManifestChanged
	if err := client.SaveRemoteManifestIfVersion(ctx, record(3, "h3"), 1); !errors.Is(err, ErrRemoteManifestChanged) {
		t.Fatalf("stale expected version: err = %v; want ErrRemoteManifestChanged", err)
	}
	// expected 0 但文件已存在 → If-None-Match 412 → 冲突
	if err := client.SaveRemoteManifestIfVersion(ctx, record(1, "h1"), 0); !errors.Is(err, ErrRemoteManifestChanged) {
		t.Fatalf("recreate over existing: err = %v; want ErrRemoteManifestChanged", err)
	}

	loaded, err := client.LoadRemoteManifest(ctx, "g1")
	if err != nil || loaded.Version != 2 || loaded.Manifest.Hash != "h2" || loaded.Manifest.Version != 2 {
		t.Fatalf("LoadRemoteManifest = %+v, err = %v; want version 2 hash h2", loaded, err)
	}

	// ClearGameRecords 删除清单；重复删除（404）也成功
	if err := client.ClearGameRecords(ctx, "g1"); err != nil {
		t.Fatalf("ClearGameRecords failed: %v", err)
	}
	if err := client.ClearGameRecords(ctx, "g1"); err != nil {
		t.Fatalf("ClearGameRecords on missing manifest failed: %v", err)
	}
	if loaded, err := client.LoadRemoteManifest(ctx, "g1"); err != nil || loaded.Version != 0 {
		t.Fatalf("manifest not cleared: %+v, err %v", loaded, err)
	}
}

func TestWebdavNoETagWriteReadFallback(t *testing.T) {
	fake, server := startFakeDav(t)
	fake.noETag = true
	ctx := context.Background()
	client := newTestWebdavClient(t, server.URL)

	catalog := RemoteCatalog{Games: []Game{{ID: "g1", CatalogUpdatedAt: time.Now()}}}
	device := DeviceInfo{ID: "dev1"}

	revision, err := client.SaveRemoteCatalog(ctx, catalog, nil, device)
	if err != nil || revision != 1 {
		t.Fatalf("no-etag first save = %d, err = %v; want 1", revision, err)
	}
	revision, err = client.SaveRemoteCatalog(ctx, catalog, nil, device)
	if err != nil || revision != 2 {
		t.Fatalf("no-etag second save = %d, err = %v; want 2", revision, err)
	}

	// 写后读校验发现并发覆盖（读回 revision 与本次写入不符）→ 重读重试
	var once sync.Once
	fake.afterPut = func(path string) {
		if path != fakeCatalogPath {
			return
		}
		once.Do(func() {
			fake.setFile(fakeCatalogPath, []byte(`{"revision":99,"catalog":{"accounts":[],"games":[]}}`))
		})
	}
	revision, err = client.SaveRemoteCatalog(ctx, catalog, nil, device)
	if err != nil || revision != 100 {
		t.Fatalf("no-etag conflicted save = %d, err = %v; want 100", revision, err)
	}
	fake.afterPut = nil

	// 无 ETag 服务器上的清单版本控制同样走写后读回退
	record := RemoteManifestRecord{
		GameID:   "g1",
		Version:  1,
		Manifest: SyncManifest{Version: 1, Hash: "h1"},
	}
	if err := client.SaveRemoteManifestIfVersion(ctx, record, 0); err != nil {
		t.Fatalf("no-etag manifest create failed: %v", err)
	}
	if loaded, err := client.LoadRemoteManifest(ctx, "g1"); err != nil || loaded.Version != 1 {
		t.Fatalf("no-etag manifest load = %+v, err = %v", loaded, err)
	}
}

func TestWebdavListRemoteManifestHeadsUsesETags(t *testing.T) {
	fake, server := startFakeDav(t)
	client := newTestWebdavClient(t, server.URL)
	ctx := context.Background()
	for _, record := range []RemoteManifestRecord{
		{GameID: "game-2", Version: 4, Manifest: SyncManifest{Version: 4, Hash: "h4"}},
		{GameID: "game-1", Version: 2, Manifest: SyncManifest{Version: 2, Hash: "h2"}},
	} {
		if err := client.SaveRemoteManifest(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	fake.mu.Lock()
	fake.methodCount = map[string]int{}
	fake.mu.Unlock()

	heads, err := client.ListRemoteManifestHeads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	propfinds := fake.methodCount["PROPFIND"]
	gets := fake.methodCount[http.MethodGet]
	fake.mu.Unlock()
	if len(heads) != 2 || heads[0].GameID != "game-1" || !strings.HasPrefix(heads[0].Token, "etag:") || heads[1].GameID != "game-2" {
		t.Fatalf("heads = %+v", heads)
	}
	if propfinds != 1 || gets != 0 {
		t.Fatalf("requests: PROPFIND=%d GET=%d", propfinds, gets)
	}
}

func TestWebdavListRemoteManifestHeadsFallsBackToContentToken(t *testing.T) {
	fake, server := startFakeDav(t)
	fake.noETag = true
	client := newTestWebdavClient(t, server.URL)
	record := RemoteManifestRecord{GameID: "game-1", Version: 7, Manifest: SyncManifest{Version: 7, Hash: "h7"}, UpdatedByDevice: "device-a"}
	if err := client.SaveRemoteManifest(context.Background(), record); err != nil {
		t.Fatal(err)
	}

	heads, err := client.ListRemoteManifestHeads(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(heads) != 1 || heads[0].Version != 7 || !strings.HasPrefix(heads[0].Token, "body:") || heads[0].UpdatedByDevice != "device-a" {
		t.Fatalf("heads = %+v", heads)
	}
}

func TestWebdavObjectRoundtrip(t *testing.T) {
	fake, server := startFakeDav(t)
	ctx := context.Background()
	client := newTestWebdavClient(t, server.URL)

	dir := t.TempDir()
	source := filepath.Join(dir, "save.dat")
	if err := os.WriteFile(source, []byte("hello save"), 0o644); err != nil {
		t.Fatal(err)
	}

	key := "games/g1/objects/sha-abc"
	if err := client.PutObjectFromFile(ctx, key, source); err != nil {
		t.Fatalf("PutObjectFromFile failed: %v", err)
	}
	stored := fake.files["/GameSync/objects/games/g1/objects/sha-abc"]
	if stored == nil || string(stored.data) != "hello save" {
		t.Fatalf("object not stored at expected path: %+v", fake.files)
	}

	// 旧格式使用固定键，因此同 key 必须允许覆盖；新格式由调用方使用内容寻址键。
	other := filepath.Join(dir, "other.dat")
	if err := os.WriteFile(other, []byte("different"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := client.PutObjectFromFile(ctx, key, other); err != nil {
		t.Fatalf("re-put failed: %v", err)
	}
	if string(fake.files["/GameSync/objects/games/g1/objects/sha-abc"].data) != "different" {
		t.Fatal("existing object was not replaced")
	}

	data, err := client.GetObjectBytes(ctx, key)
	if err != nil || string(data) != "different" {
		t.Fatalf("GetObjectBytes = %q, err = %v", data, err)
	}

	destination := filepath.Join(dir, "nested", "restored.dat")
	if err := client.DownloadObjectToFile(ctx, key, destination); err != nil {
		t.Fatalf("DownloadObjectToFile failed: %v", err)
	}
	restored, err := os.ReadFile(destination)
	if err != nil || string(restored) != "different" {
		t.Fatalf("restored content = %q, err = %v", restored, err)
	}

	if err := client.DeleteObject(ctx, key); err != nil {
		t.Fatalf("DeleteObject failed: %v", err)
	}
	// 重复删除（404）成功
	if err := client.DeleteObject(ctx, key); err != nil {
		t.Fatalf("DeleteObject on missing failed: %v", err)
	}
	if _, err := client.GetObjectBytes(ctx, key); err == nil {
		t.Fatal("GetObjectBytes after delete should fail")
	}

	// ClearPrefix / ClearGameFiles 递归删除目录，404 视为成功
	for _, extra := range []string{"games/g2/objects/a", "games/g2/objects/b", "games/g3/objects/c"} {
		if err := client.PutObjectFromFile(ctx, extra, source); err != nil {
			t.Fatalf("put %s failed: %v", extra, err)
		}
	}
	if err := client.ClearPrefix(ctx, "games/g2/"); err != nil {
		t.Fatalf("ClearPrefix failed: %v", err)
	}
	if _, err := client.GetObjectBytes(ctx, "games/g2/objects/a"); err == nil {
		t.Fatal("objects under cleared prefix should be gone")
	}
	if _, err := client.GetObjectBytes(ctx, "games/g3/objects/c"); err != nil {
		t.Fatalf("sibling prefix should survive: %v", err)
	}
	if err := client.ClearGameFiles(ctx, "g3"); err != nil {
		t.Fatalf("ClearGameFiles failed: %v", err)
	}
	if err := client.ClearPrefix(ctx, "games/none/"); err != nil {
		t.Fatalf("ClearPrefix on missing dir should succeed: %v", err)
	}
}

func TestWebdavValidateAccessAndUsage(t *testing.T) {
	fake, server := startFakeDav(t)
	ctx := context.Background()
	client := newTestWebdavClient(t, server.URL)

	if err := client.ValidateAccess(ctx); err != nil {
		t.Fatalf("ValidateAccess failed: %v", err)
	}
	if err := client.ValidateBucketAccess(ctx); err != nil {
		t.Fatalf("ValidateBucketAccess failed: %v", err)
	}
	// 探针文件写后即删，不残留
	for path := range fake.files {
		if strings.Contains(path, ".gamesync-probe") {
			t.Fatalf("probe file left behind: %s", path)
		}
	}
	if fake.methodCount[http.MethodPut] == 0 || fake.methodCount[http.MethodDelete] == 0 {
		t.Fatalf("validate should write and delete a probe, methods = %v", fake.methodCount)
	}

	// 服务器提供 quota-used-bytes 时直接使用
	fake.quotaUsed = 12345
	if used, err := client.FetchAccountUsageBytes(ctx); err != nil || used != 12345 {
		t.Fatalf("quota usage = %d, err = %v; want 12345", used, err)
	}

	// 不支持配额时回退 Depth:infinity 求和
	fake.quotaUsed = -1
	fake.setFile("/GameSync/objects/a", []byte("12345"))
	fake.setFile("/GameSync/objects/b", []byte("1234567"))
	if used, err := client.FetchAccountUsageBytes(ctx); err != nil || used != 12 {
		t.Fatalf("summed usage = %d, err = %v; want 12", used, err)
	}

	// 认证失败给出中文提示
	badClient, err := NewWebdavClient(CloudflareAccount{
		Provider:       ProviderWebdav,
		WebdavURL:      server.URL,
		WebdavUsername: "alice",
		WebdavPassword: "wrong",
	})
	if err != nil {
		t.Fatalf("NewWebdavClient failed: %v", err)
	}
	if err := badClient.ValidateAccess(ctx); err == nil || !strings.Contains(err.Error(), "认证失败") {
		t.Fatalf("auth failure error = %v; want 认证失败 message", err)
	}

	// VerifyWebdavAccount 成功回填 usedBytes，失败回填 lastError
	fake.quotaUsed = 4096
	verified, err := VerifyWebdavAccount(ctx, CloudflareAccount{
		Provider:       ProviderWebdav,
		WebdavURL:      server.URL,
		WebdavUsername: "alice",
		WebdavPassword: "secret",
	})
	if err != nil || verified.LastError != "" || verified.UsedBytes != 4096 || verified.LastVerifiedAt == nil {
		t.Fatalf("VerifyWebdavAccount = %+v, err = %v", verified, err)
	}
	failed, err := VerifyWebdavAccount(ctx, CloudflareAccount{
		Provider:       ProviderWebdav,
		WebdavURL:      server.URL,
		WebdavUsername: "alice",
		WebdavPassword: "wrong",
	})
	if err == nil || failed.LastError == "" {
		t.Fatalf("VerifyWebdavAccount with bad password = %+v, err = %v; want error", failed, err)
	}
}

func TestNewWebdavClientValidation(t *testing.T) {
	if _, err := NewWebdavClient(CloudflareAccount{Provider: ProviderWebdav}); err == nil || err.Error() != msgWebdavConfigIncomplete {
		t.Fatalf("missing fields err = %v; want %s", err, msgWebdavConfigIncomplete)
	}
	if _, err := NewWebdavClient(CloudflareAccount{
		Provider:       ProviderWebdav,
		WebdavURL:      "ftp://example.com/dav",
		WebdavUsername: "u",
		WebdavPassword: "p",
	}); err == nil || err.Error() != msgWebdavURLInvalid {
		t.Fatalf("invalid url err = %v; want %s", err, msgWebdavURLInvalid)
	}
	// 默认根目录 GameSync；自定义多级根目录逐段生效
	client, err := NewWebdavClient(CloudflareAccount{
		Provider:       ProviderWebdav,
		WebdavURL:      "https://dav.example.com/remote.php/dav/files/user/",
		WebdavUsername: "u",
		WebdavPassword: "p",
	})
	if err != nil {
		t.Fatalf("NewWebdavClient failed: %v", err)
	}
	if got := client.resourceURL("catalog", "catalog.json"); got != "https://dav.example.com/remote.php/dav/files/user/GameSync/catalog/catalog.json" {
		t.Fatalf("resourceURL = %s", got)
	}
	nested, err := NewWebdavClient(CloudflareAccount{
		Provider:       ProviderWebdav,
		WebdavURL:      "https://dav.example.com",
		WebdavUsername: "u",
		WebdavPassword: "p",
		WebdavRoot:     "apps/game sync",
	})
	if err != nil {
		t.Fatalf("NewWebdavClient failed: %v", err)
	}
	if got := nested.resourceURL(); got != "https://dav.example.com/apps/game%20sync" {
		t.Fatalf("nested root resourceURL = %s", got)
	}
}
