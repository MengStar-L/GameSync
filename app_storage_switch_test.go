package main

import (
	"testing"

	"gamesync/internal/core"
)

func TestResolveStorageSwitchTargetReusesStoredAccount(t *testing.T) {
	state := core.AppState{Accounts: []core.CloudflareAccount{
		{ID: "cf", Provider: core.ProviderCloudflare, IsPrimary: true, Enabled: true},
		{
			ID: "dav", Provider: core.ProviderWebdav,
			WebdavURL: "https://dav.example.test", WebdavUsername: "user", WebdavPassword: "stored-secret",
		},
	}}

	target, err := resolveStorageSwitchTarget(state, core.StorageSwitchRequest{ExistingAccountID: "dav"})
	if err != nil {
		t.Fatal(err)
	}
	if target.ID != "dav" || target.WebdavPassword != "stored-secret" {
		t.Fatalf("stored account was not reused: %+v", target)
	}
}

func TestResolveStorageSwitchTargetRejectsCurrentProvider(t *testing.T) {
	state := core.AppState{Accounts: []core.CloudflareAccount{
		{ID: "cf-primary", Provider: core.ProviderCloudflare, IsPrimary: true, Enabled: true},
		{ID: "cf-secondary", Provider: core.ProviderCloudflare},
	}}

	_, err := resolveStorageSwitchTarget(state, core.StorageSwitchRequest{ExistingAccountID: "cf-secondary"})
	if err == nil || err.Error() != msgStorageSwitchSameProvider {
		t.Fatalf("expected same-provider rejection, got %v", err)
	}
}

func TestResolveStorageSwitchTargetRequiresExactlyOneTarget(t *testing.T) {
	state := core.AppState{Accounts: []core.CloudflareAccount{{ID: "cf", IsPrimary: true, Enabled: true}}}
	newAccount := core.CloudflareAccount{Provider: core.ProviderWebdav}

	for _, request := range []core.StorageSwitchRequest{
		{},
		{ExistingAccountID: "missing", NewAccount: &newAccount},
	} {
		if _, err := resolveStorageSwitchTarget(state, request); err == nil {
			t.Fatalf("expected invalid target request to fail: %+v", request)
		}
	}
}

func TestResolveStorageSwitchTargetClearsNewAccountID(t *testing.T) {
	state := core.AppState{Accounts: []core.CloudflareAccount{{ID: "cf", IsPrimary: true, Enabled: true}}}
	newAccount := core.CloudflareAccount{ID: "spoofed-existing-id", Provider: core.ProviderWebdav}

	target, err := resolveStorageSwitchTarget(state, core.StorageSwitchRequest{NewAccount: &newAccount})
	if err != nil {
		t.Fatal(err)
	}
	if target.ID != "" {
		t.Fatalf("new account id was not cleared: %q", target.ID)
	}
}
