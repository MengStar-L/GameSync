package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreProtectsSecretsAtRestAndRedactsExports(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	secrets := []string{
		"cf-api-token-secret",
		"r2-access-key-secret",
		"r2-secret-access-key-secret",
		"rawg-api-key-secret",
		"steamgriddb-api-key-secret",
	}

	store.mu.Lock()
	store.state.Accounts = []CloudflareAccount{{
		ID:                "account-1",
		Name:              "Primary",
		AccountID:         "cloudflare-account",
		APIToken:          secrets[0],
		D1DatabaseID:      "d1-db",
		R2Bucket:          "bucket",
		R2AccessKeyID:     secrets[1],
		R2SecretAccessKey: secrets[2],
		IsPrimary:         true,
		Enabled:           true,
		CatalogUpdatedAt:  now,
	}}
	store.state.Preferences.RawgAPIKey = secrets[3]
	store.state.Preferences.SteamGridDBAPIKey = secrets[4]
	err = store.saveLocked()
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		if bytes.Contains(content, []byte(secret)) {
			t.Fatalf("state file contains plaintext secret %q", secret)
		}
	}
	if !bytes.Contains(content, []byte(protectedSecretPrefix)) {
		t.Fatalf("state file does not contain protected secret marker: %s", string(content))
	}

	reloaded, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	state := reloaded.Snapshot()
	if got := state.Accounts[0].APIToken; got != secrets[0] {
		t.Fatalf("api token did not round-trip: %q", got)
	}
	if got := state.Accounts[0].R2AccessKeyID; got != secrets[1] {
		t.Fatalf("r2 access key did not round-trip: %q", got)
	}
	if got := state.Accounts[0].R2SecretAccessKey; got != secrets[2] {
		t.Fatalf("r2 secret key did not round-trip: %q", got)
	}
	if got := state.Preferences.RawgAPIKey; got != secrets[3] {
		t.Fatalf("rawg key did not round-trip: %q", got)
	}
	if got := state.Preferences.SteamGridDBAPIKey; got != secrets[4] {
		t.Fatalf("steamgriddb key did not round-trip: %q", got)
	}

	exported, err := reloaded.ExportState()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		if bytes.Contains(exported, []byte(secret)) {
			t.Fatalf("export contains plaintext secret %q", secret)
		}
	}
	var exportedState AppState
	if err := json.Unmarshal(exported, &exportedState); err != nil {
		t.Fatal(err)
	}
	if exportedState.Accounts[0].APIToken != "" ||
		exportedState.Accounts[0].R2AccessKeyID != "" ||
		exportedState.Accounts[0].R2SecretAccessKey != "" ||
		exportedState.Preferences.RawgAPIKey != "" ||
		exportedState.Preferences.SteamGridDBAPIKey != "" {
		t.Fatalf("exported state was not redacted: %+v", exportedState.Accounts[0])
	}
}

// B7（M1 后端半区）：偏好列表字段按客户端快照基线做 CAS，陈旧整包不得覆盖新值
func TestSavePreferencesRejectsStaleListBaseline(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	fresh := store.Snapshot().Preferences
	fresh.FavoriteGames = []string{"game-new"}
	if err := store.SavePreferences(fresh); err != nil {
		t.Fatal(err)
	}
	current := store.Snapshot().Preferences
	if len(current.FavoriteGames) != 1 || current.FavoriteGames[0] != "game-new" {
		t.Fatalf("favorite games not saved: %v", current.FavoriteGames)
	}

	// 客户端基线早于当前 → 保留当前值不覆盖
	stale := current
	stale.FavoriteGames = []string{"game-stale"}
	stale.FavoriteGamesUpdatedAt = current.FavoriteGamesUpdatedAt.Add(-time.Minute)
	if err := store.SavePreferences(stale); err != nil {
		t.Fatal(err)
	}
	after := store.Snapshot().Preferences
	if len(after.FavoriteGames) != 1 || after.FavoriteGames[0] != "game-new" {
		t.Fatalf("stale baseline overwrote favorites: %v", after.FavoriteGames)
	}
	if !after.FavoriteGamesUpdatedAt.Equal(current.FavoriteGamesUpdatedAt) {
		t.Fatalf("stale baseline bumped favorites timestamp")
	}

	// 基线不早于当前 → 真实新改动被接受并盖新时间戳
	update := store.Snapshot().Preferences
	update.PinnedTags = []string{"rpg"}
	if err := store.SavePreferences(update); err != nil {
		t.Fatal(err)
	}
	final := store.Snapshot().Preferences
	if len(final.PinnedTags) != 1 || final.PinnedTags[0] != "rpg" {
		t.Fatalf("fresh baseline change rejected: %v", final.PinnedTags)
	}
}

// B6（M6）：本地墓碑存在且列表无此游戏时，UpsertGame 拒绝保存复活幽灵
func TestUpsertGameRejectsResurrectingDeletedGame(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	game := Game{ID: "game-1", Name: "Game", SavePath: `C:\Saves`}
	if _, err := store.UpsertGame(game); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteGame("game-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertGame(game); err == nil {
		t.Fatalf("expected resurrection of deleted game to be rejected")
	}
	// 未删除的游戏正常保存不受影响
	if _, err := store.UpsertGame(Game{ID: "game-2", Name: "Other", SavePath: `C:\Saves2`}); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertGameAllowsEmptyPaths(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	game, err := store.UpsertGame(Game{Name: "Pathless"})
	if err != nil {
		t.Fatalf("upsert pathless game: %v", err)
	}
	if game.InstallPath != "" || game.SavePath != "" {
		t.Fatalf("pathless game gained local paths: install=%q save=%q", game.InstallPath, game.SavePath)
	}
	if !game.Sync.Enabled || game.Sync.ConflictStrategy != "manual" {
		t.Fatalf("pathless game did not receive default sync config: %+v", game.Sync)
	}
}

// B3（M4）：拉取合并时远端 RuntimeUpdatedAt 为零不得回填获胜，本地游玩记录保留
func TestMergeGameFieldsRemoteZeroRuntimeNeverWins(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	newer := time.Now()
	played := newer
	local := Game{
		ID: "game-1", Name: "Game",
		PlayTime: 120, LastPlayed: &played,
		MetadataUpdatedAt: base, TagsUpdatedAt: base, SyncConfigUpdatedAt: base,
		StorageUpdatedAt: base, RuntimeUpdatedAt: newer, CatalogUpdatedAt: newer,
	}
	remote := Game{
		ID: "game-1", Name: "Game",
		PlayTime:          30,
		MetadataUpdatedAt: base, TagsUpdatedAt: base, SyncConfigUpdatedAt: base,
		StorageUpdatedAt: base,
		// 远端旧格式：RuntimeUpdatedAt 清零但整条 CatalogUpdatedAt 晚于本地
		CatalogUpdatedAt: newer.Add(time.Minute),
	}
	merged := mergeGameFields(local, remote)
	if merged.PlayTime != 120 {
		t.Fatalf("remote zero-runtime overwrote local playtime: %v", merged.PlayTime)
	}
	if !merged.RuntimeUpdatedAt.Equal(newer) {
		t.Fatalf("runtime timestamp regressed: %s", merged.RuntimeUpdatedAt)
	}
}

// B6（M6）：GameEditTimestamp 不含 Runtime，纯 playtime 写入不抬高编辑时间
func TestGameEditTimestampExcludesRuntime(t *testing.T) {
	edit := time.Now().Add(-time.Hour)
	runtime := time.Now()
	game := Game{
		MetadataUpdatedAt: edit, TagsUpdatedAt: edit.Add(-time.Minute),
		SyncConfigUpdatedAt: edit.Add(-2 * time.Minute), StorageUpdatedAt: edit.Add(-3 * time.Minute),
		RuntimeUpdatedAt: runtime, CatalogUpdatedAt: runtime,
	}
	if got := GameEditTimestamp(game); !got.Equal(edit) {
		t.Fatalf("edit timestamp should exclude runtime: got %s want %s", got, edit)
	}
	// 四组全零（旧数据）回退 CatalogUpdatedAt
	legacy := Game{CatalogUpdatedAt: runtime}
	if got := GameEditTimestamp(legacy); !got.Equal(runtime) {
		t.Fatalf("legacy fallback failed: got %s", got)
	}
}

// 存储切换：SwitchPrimaryStorage 一次锁内原子完成旧账号停用、新账号入主、
// 游戏重挂与锚点清零（含 PendingRemoteCleanups）
func TestSwitchPrimaryStorageAtomicRepoint(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	oldAccount, err := store.UpsertAccount(CloudflareAccount{
		AccountID: "cf-account", APIToken: "token", D1DatabaseID: "d1",
		R2Bucket: "bucket", R2AccessKeyID: "key", R2SecretAccessKey: "secret",
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	lastSync := SyncSummary{Status: "success", SyncedAt: time.Now()}
	game, err := store.UpsertGame(Game{
		Name: "Game", SavePath: `C:\Saves`,
		StorageAccountID: oldAccount.ID, BackupStorageAccountID: oldAccount.ID,
		AutoBackupAccountID: oldAccount.ID,
		Anchor: SyncAnchor{
			LastRemoteVersion: 3,
			LastManifest:      SyncManifest{Version: 3, Hash: "manifest-hash"},
			StorageAccountID:  oldAccount.ID,
			PendingRemoteCleanups: []PendingRemoteCleanup{
				{SHA256: "stale-object", ReplacedAt: time.Now()},
			},
		},
		LastSync: &lastSync,
		LaunchRestoreOverride: &LaunchRestoreOverride{
			Filename: "backup_manual_1.zip", Active: true, RestoredAt: time.Now(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	beforeStorageAt := game.StorageUpdatedAt

	newAccount, err := store.SwitchPrimaryStorage(CloudflareAccount{
		Provider: ProviderWebdav, WebdavURL: "https://dav.example.com/dav",
		WebdavUsername: "user", WebdavPassword: "pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	if newAccount.ID == "" || newAccount.ID == oldAccount.ID {
		t.Fatalf("new account id invalid: %q", newAccount.ID)
	}
	if !newAccount.IsPrimary || !newAccount.Enabled {
		t.Fatalf("new account not primary/enabled: %+v", newAccount)
	}

	state := store.Snapshot()
	var oldSaved *CloudflareAccount
	for index := range state.Accounts {
		if state.Accounts[index].ID == oldAccount.ID {
			oldSaved = &state.Accounts[index]
		}
	}
	if oldSaved == nil {
		t.Fatalf("old account record was not retained")
	}
	if oldSaved.Enabled || oldSaved.IsPrimary {
		t.Fatalf("old account not deactivated: %+v", oldSaved)
	}
	if !oldSaved.CatalogUpdatedAt.After(oldAccount.CatalogUpdatedAt) {
		t.Fatalf("old account CatalogUpdatedAt did not advance")
	}

	saved := state.Games[0]
	if saved.StorageAccountID != newAccount.ID ||
		saved.BackupStorageAccountID != newAccount.ID ||
		saved.AutoBackupAccountID != newAccount.ID {
		t.Fatalf("game not repointed to new account: %+v", saved)
	}
	if saved.Anchor.LastManifest.Hash != "" || saved.Anchor.LastRemoteVersion != 0 ||
		saved.Anchor.StorageAccountID != "" || len(saved.Anchor.PendingRemoteCleanups) != 0 {
		t.Fatalf("anchor not fully reset: %+v", saved.Anchor)
	}
	if saved.LastSync != nil {
		t.Fatalf("last sync not cleared: %+v", saved.LastSync)
	}
	if saved.LaunchRestoreOverride != nil {
		t.Fatalf("launch restore override not cleared: %+v", saved.LaunchRestoreOverride)
	}
	if !saved.StorageUpdatedAt.After(beforeStorageAt) {
		t.Fatalf("game StorageUpdatedAt did not advance")
	}
	if !state.CatalogSync.Dirty {
		t.Fatalf("catalog not marked dirty after switch")
	}
}

// 存储切换：新账号配置不完整时校验失败，本地状态不得有任何改动
func TestSwitchPrimaryStorageRejectsIncompleteAccountWithoutSideEffects(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldAccount, err := store.UpsertAccount(CloudflareAccount{
		AccountID: "cf-account", APIToken: "token", D1DatabaseID: "d1",
		R2Bucket: "bucket", R2AccessKeyID: "key", R2SecretAccessKey: "secret",
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.SwitchPrimaryStorage(CloudflareAccount{
		Provider: ProviderWebdav, WebdavURL: "https://dav.example.com/dav",
	}); err == nil {
		t.Fatalf("expected incomplete webdav account to be rejected")
	}

	state := store.Snapshot()
	if len(state.Accounts) != 1 {
		t.Fatalf("account list changed after rejected switch: %d", len(state.Accounts))
	}
	if !state.Accounts[0].IsPrimary || !state.Accounts[0].Enabled || state.Accounts[0].ID != oldAccount.ID {
		t.Fatalf("old primary account mutated after rejected switch: %+v", state.Accounts[0])
	}
}

func TestSwitchPrimaryStorageReusesExistingAccount(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldAccount, err := store.UpsertAccount(CloudflareAccount{
		AccountID: "cf-account", APIToken: "token", D1DatabaseID: "d1",
		R2Bucket: "bucket", R2AccessKeyID: "key", R2SecretAccessKey: "secret",
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	targetAccount, err := store.UpsertAccount(CloudflareAccount{
		Provider: ProviderWebdav, WebdavURL: "https://dav.example.com/dav",
		WebdavUsername: "user", WebdavPassword: "pass", Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertGame(Game{
		Name: "Game", SavePath: `C:\Saves`, StorageAccountID: oldAccount.ID,
		BackupStorageAccountID: oldAccount.ID,
	}); err != nil {
		t.Fatal(err)
	}

	switched, err := store.SwitchPrimaryStorage(targetAccount)
	if err != nil {
		t.Fatal(err)
	}
	if switched.ID != targetAccount.ID {
		t.Fatalf("existing account id was not reused: got %q want %q", switched.ID, targetAccount.ID)
	}

	state := store.Snapshot()
	if len(state.Accounts) != 2 {
		t.Fatalf("switch created a duplicate account: %+v", state.Accounts)
	}
	if state.Games[0].StorageAccountID != targetAccount.ID || state.Games[0].BackupStorageAccountID != targetAccount.ID {
		t.Fatalf("game was not repointed to reused account: %+v", state.Games[0])
	}
	for _, account := range state.Accounts {
		switch account.ID {
		case oldAccount.ID:
			if account.Enabled || account.IsPrimary {
				t.Fatalf("old account remained active: %+v", account)
			}
		case targetAccount.ID:
			if !account.Enabled || !account.IsPrimary {
				t.Fatalf("target account was not activated: %+v", account)
			}
		}
	}
}

func TestMergeGameFieldsMergesCoverIndependently(t *testing.T) {
	local := Game{
		ID:                "game-1",
		Name:              "Local title",
		CoverCloudKey:     "covers/game-1/old.jpg",
		CoverUpdatedAt:    time.Unix(10, 0),
		MetadataUpdatedAt: time.Unix(30, 0),
		CatalogUpdatedAt:  time.Unix(30, 0),
	}
	remote := Game{
		ID:                  "game-1",
		Name:                "Stale title",
		CoverCloudAccountID: "webdav",
		CoverCloudKey:       "covers/game-1/new.jpg",
		CoverUpdatedAt:      time.Unix(40, 0),
		MetadataUpdatedAt:   time.Unix(20, 0),
		CatalogUpdatedAt:    time.Unix(40, 0),
	}

	merged := mergeGameFields(local, remote)
	if merged.Name != local.Name {
		t.Fatalf("newer local metadata was overwritten: %+v", merged)
	}
	if merged.CoverCloudKey != remote.CoverCloudKey || merged.CoverCloudAccountID != remote.CoverCloudAccountID {
		t.Fatalf("newer remote cover was not merged: %+v", merged)
	}
	if !merged.CatalogUpdatedAt.Equal(remote.CoverUpdatedAt) {
		t.Fatalf("cover clock was omitted from catalog clock: %v", merged.CatalogUpdatedAt)
	}
}

func TestApplyGameChangeTimestampsKeepsMetadataClockForCoverOnlyChange(t *testing.T) {
	metadataAt := time.Unix(20, 0)
	current := Game{
		ID:                "game-1",
		Name:              "Game",
		CoverCloudKey:     "covers/game-1/old.jpg",
		CoverUpdatedAt:    time.Unix(10, 0),
		MetadataUpdatedAt: metadataAt,
		CatalogUpdatedAt:  metadataAt,
	}
	next := current
	next.CoverCloudKey = "covers/game-1/new.jpg"
	now := time.Unix(40, 0)

	applyGameChangeTimestamps(&next, current, now)
	if !next.MetadataUpdatedAt.Equal(metadataAt) {
		t.Fatalf("cover-only edit advanced metadata clock: %v", next.MetadataUpdatedAt)
	}
	if !next.CoverUpdatedAt.Equal(now) {
		t.Fatalf("cover-only edit did not advance cover clock: %v", next.CoverUpdatedAt)
	}
}

func TestMergeBackupRegistriesUsesDeviceAndFilename(t *testing.T) {
	created := time.Unix(20, 0)
	merged := mergeBackupRegistries(
		[]BackupRecord{{Filename: "backup.zip", SourceDeviceID: "device-a", CreatedAt: created}},
		[]BackupRecord{{Filename: "backup.zip", SourceDeviceID: "device-b", CreatedAt: created}},
	)
	if len(merged) != 2 {
		t.Fatalf("different-device backups were collapsed: %+v", merged)
	}
	if BackupRecordID(merged[0]) == BackupRecordID(merged[1]) {
		t.Fatalf("different-device backups have the same identity: %+v", merged)
	}
	legacy := BackupRecord{Filename: "legacy.zip"}
	if got := BackupRecordID(legacy); got != "legacy:legacy.zip" {
		t.Fatalf("legacy backup identity = %q", got)
	}
}
