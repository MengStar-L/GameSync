package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gamesync/internal/core"
)

type failingCatalogLoadStore struct {
	*migrationCatalogStore
	err error
}

func (s *failingCatalogLoadStore) LoadRemoteCatalog(context.Context) (core.RemoteCatalog, map[string]core.EncryptedCredentialBlob, error) {
	return core.RemoteCatalog{}, nil, s.err
}

func TestSaveAccountRejectsDifferentWebdavNamespaceWithoutRemoteRead(t *testing.T) {
	store, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.UpsertAccount(core.CloudflareAccount{
		ID: "primary", Provider: core.ProviderWebdav, Enabled: true, IsPrimary: true,
		WebdavURL: "https://old.example.test", WebdavUsername: "user", WebdavPassword: "password",
	})
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.store = store
	remoteCalls := 0
	app.catalogStoreFn = func(core.CloudflareAccount) (core.CatalogStore, error) {
		remoteCalls++
		return nil, errors.New("injected 502")
	}

	account.WebdavURL = "https://new.example.test"
	_, err = app.SaveAccount(account)
	if err == nil || !strings.Contains(err.Error(), "切换存储方式") {
		t.Fatalf("save error = %v", err)
	}
	if remoteCalls != 0 {
		t.Fatalf("old primary was contacted %d times", remoteCalls)
	}
	saved, err := findAccount(store.Snapshot(), account.ID)
	if err != nil || saved.WebdavURL == account.WebdavURL {
		t.Fatalf("saved account=%+v err=%v", saved, err)
	}
}

func TestWebdavCatalogCannotPushBeforeInitialPull(t *testing.T) {
	store, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertAccount(core.CloudflareAccount{
		Provider: core.ProviderWebdav, Enabled: true, IsPrimary: true,
		WebdavURL: "https://dav.example.test", WebdavUsername: "user", WebdavPassword: "password",
	}); err != nil {
		t.Fatal(err)
	}
	catalog := newMigrationCatalogStore()
	app := NewApp()
	app.store = store
	app.catalogStoreFn = func(core.CloudflareAccount) (core.CatalogStore, error) { return catalog, nil }

	if err := app.syncRemoteCatalog(); err == nil || !strings.Contains(err.Error(), "首次拉取") {
		t.Fatalf("push error = %v", err)
	}
	if catalog.revision != 0 {
		t.Fatalf("remote catalog was overwritten at revision %d", catalog.revision)
	}
	if _, err := app.pullRemoteCatalog(); err != nil {
		t.Fatal(err)
	}
	if !store.Snapshot().CatalogSync.InitialPullCompleted {
		t.Fatal("successful empty catalog pull was not recorded")
	}
}

func TestFailedInitialWebdavPullKeepsLocalStateAndDoesNotPush(t *testing.T) {
	store, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertAccount(core.CloudflareAccount{
		Provider: core.ProviderWebdav, Enabled: true, IsPrimary: true,
		WebdavURL: "https://dav.example.test", WebdavUsername: "user", WebdavPassword: "password",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertGame(core.Game{ID: "local-game", Name: "Local Game"}); err != nil {
		t.Fatal(err)
	}
	remote := &failingCatalogLoadStore{migrationCatalogStore: newMigrationCatalogStore(), err: errors.New("injected offline")}
	app := NewApp()
	app.store = store
	app.catalogStoreFn = func(core.CloudflareAccount) (core.CatalogStore, error) { return remote, nil }

	if _, err := app.pullRemoteCatalog(); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("pull error = %v", err)
	}
	state := store.Snapshot()
	if state.CatalogSync.InitialPullCompleted || remote.revision != 0 || len(state.Games) != 1 || state.Games[0].ID != "local-game" {
		t.Fatalf("failed pull changed local/remote state: local=%+v revision=%d", state, remote.revision)
	}
}

func TestSaveAccountRejectsOrdinaryWebdavAddToCloudflareCatalog(t *testing.T) {
	store, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertAccount(core.CloudflareAccount{
		Enabled: true, IsPrimary: true, AccountID: "cf", APIToken: "token", D1DatabaseID: "d1",
		R2Bucket: "bucket", R2AccessKeyID: "key", R2SecretAccessKey: "secret",
	}); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.store = store
	_, err = app.SaveAccount(core.CloudflareAccount{
		Provider: core.ProviderWebdav, WebdavURL: "https://dav.example.test",
		WebdavUsername: "user", WebdavPassword: "password",
	})
	if err == nil || !strings.Contains(err.Error(), "切换存储方式") {
		t.Fatalf("ordinary WebDAV add error = %v", err)
	}
	if len(store.Snapshot().Accounts) != 1 {
		t.Fatalf("ordinary add created an account: %+v", store.Snapshot().Accounts)
	}
}

func TestSaveAccountRejectsMutationDuringStorageMigration(t *testing.T) {
	store, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertAccount(core.CloudflareAccount{
		ID: "source", Provider: core.ProviderWebdav, Enabled: true, IsPrimary: true,
		WebdavURL: "https://source.example.test", WebdavUsername: "user", WebdavPassword: "password",
	})
	if err != nil {
		t.Fatal(err)
	}
	target, err := store.UpsertAccount(core.CloudflareAccount{
		ID: "target", Provider: core.ProviderWebdav, Enabled: false,
		WebdavURL: "https://target.example.test", WebdavUsername: "user", WebdavPassword: "password",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BeginStorageMigration(core.StorageMigrationState{
		TransactionID: "tx", SourceAccountID: source.ID, TargetAccountID: target.ID,
	}); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.store = store
	source.WebdavURL = "https://changed.example.test"
	if _, err := app.SaveAccount(source); !errors.Is(err, errStorageMigrationInProgress) {
		t.Fatalf("save error = %v", err)
	}
	saved, err := findAccount(store.Snapshot(), source.ID)
	if err != nil || saved.WebdavURL == source.WebdavURL {
		t.Fatalf("account changed during migration: %+v, %v", saved, err)
	}
}
