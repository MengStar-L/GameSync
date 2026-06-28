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
