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

func TestNormalizeRemoteWebdavIdentityBeforeCredentialMerge(t *testing.T) {
	localAccount, err := core.NormalizeWebdavAccount(core.CloudflareAccount{
		Provider: core.ProviderWebdav, WebdavURL: "https://dav.example.test/root", WebdavRoot: "GameSync",
		WebdavUsername: "current-user", WebdavPassword: "current-password", IsPrimary: true, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	remote := core.RemoteCatalog{Accounts: []core.CloudflareAccount{{
		ID: "legacy-random-id", Provider: core.ProviderWebdav, WebdavURL: "HTTPS://DAV.EXAMPLE.TEST:443/root/",
		WebdavRoot: "/GameSync/", WebdavUsername: "legacy-user", IsPrimary: true, Enabled: true,
	}}}
	encrypted := map[string]core.EncryptedCredentialBlob{
		"legacy-random-id": {Version: 1, Ciphertext: "legacy-cipher"},
	}

	normalized, normalizedCredentials := normalizeRemoteCatalogForMerge(remote, encrypted)
	if len(normalized.Accounts) != 1 || normalized.Accounts[0].ID != localAccount.ID {
		t.Fatalf("normalized accounts = %+v", normalized.Accounts)
	}
	if normalizedCredentials[localAccount.ID].Ciphertext != "legacy-cipher" {
		t.Fatalf("normalized credentials = %+v", normalizedCredentials)
	}
	prepared, failures := prepareCatalogForOrdinaryMerge(
		core.AppState{Accounts: []core.CloudflareAccount{localAccount}}, normalized, nil, "",
	)
	if len(failures) != 0 || prepared.Accounts[0].WebdavPassword != "current-password" || !prepared.Accounts[0].Enabled {
		t.Fatalf("prepared account = %+v, failures = %+v", prepared.Accounts[0], failures)
	}
}
