package main

import (
	"testing"

	"gamesync/internal/core"
)

func TestCatalogCredentialsIncludePrimaryCloudflareAndWebdav(t *testing.T) {
	accounts := []core.CloudflareAccount{
		{ID: "cf", AccountID: "account", APIToken: "token", D1DatabaseID: "d1", R2Bucket: "bucket", R2AccessKeyID: "key", R2SecretAccessKey: "secret", IsPrimary: true},
		{ID: "dav", Provider: core.ProviderWebdav, WebdavURL: "https://dav.example.test", WebdavUsername: "u", WebdavPassword: "password"},
	}
	encrypted, err := encryptCatalogCredentials(accounts, "recovery")
	if err != nil {
		t.Fatal(err)
	}
	if len(encrypted) != 2 {
		t.Fatalf("encrypted credentials omitted an account: %#v", encrypted)
	}
	public := core.RemoteCatalog{Accounts: []core.CloudflareAccount{
		{ID: "cf", AccountID: "account", D1DatabaseID: "d1", R2Bucket: "bucket", IsPrimary: true},
		{ID: "dav", Provider: core.ProviderWebdav, WebdavURL: "https://dav.example.test", WebdavUsername: "u"},
	}}
	restored, failures := decryptCatalogCredentials(public, encrypted, "recovery")
	if len(failures) != 0 || restored.Accounts[0].APIToken != "token" || restored.Accounts[1].WebdavPassword != "password" {
		t.Fatalf("credential restore = %+v, failures = %+v", restored.Accounts, failures)
	}
}

func TestPrepareCatalogForOrdinaryMergePreservesLocalPrimary(t *testing.T) {
	local := core.AppState{Accounts: []core.CloudflareAccount{{
		ID: "old", AccountID: "account", APIToken: "token", D1DatabaseID: "d1", R2Bucket: "bucket",
		R2AccessKeyID: "key", R2SecretAccessKey: "secret", IsPrimary: true, Enabled: true,
	}}}
	remote := core.RemoteCatalog{Accounts: []core.CloudflareAccount{
		{ID: "old", AccountID: "account", D1DatabaseID: "d1", R2Bucket: "bucket", IsPrimary: false, Enabled: false},
		{ID: "new", Provider: core.ProviderWebdav, WebdavURL: "https://dav.example.test", WebdavUsername: "u", IsPrimary: true, Enabled: true},
	}}
	prepared, _ := prepareCatalogForOrdinaryMerge(local, remote, nil, "")
	if !prepared.Accounts[0].IsPrimary || !prepared.Accounts[0].Enabled {
		t.Fatalf("local primary was demoted: %+v", prepared.Accounts[0])
	}
	if prepared.Accounts[1].IsPrimary || prepared.Accounts[1].Enabled {
		t.Fatalf("unusable remote primary was accepted: %+v", prepared.Accounts[1])
	}
}
