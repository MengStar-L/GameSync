package main

import (
	"errors"
	"testing"

	"gamesync/internal/core"
)

func TestSaveAccountRepairsUnavailablePrimaryWithoutRemoteRead(t *testing.T) {
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
	snapshot, err := app.SaveAccount(account)
	if err != nil {
		t.Fatal(err)
	}
	if remoteCalls != 0 {
		t.Fatalf("old primary was contacted %d times", remoteCalls)
	}
	saved, err := findAccount(snapshot.State, account.ID)
	if err != nil || saved.WebdavURL != account.WebdavURL {
		t.Fatalf("saved account=%+v err=%v", saved, err)
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
