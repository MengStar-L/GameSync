package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gamesync/internal/core"
)

func TestWritableObjectAccountsSupportsWebdavAndPrefersPrimary(t *testing.T) {
	state := core.AppState{Accounts: []core.CloudflareAccount{
		{
			ID: "cf", Provider: core.ProviderCloudflare, Enabled: true,
			AccountID: "account", R2Bucket: "bucket", R2AccessKeyID: "key", R2SecretAccessKey: "secret",
		},
		{
			ID: "dav", Provider: core.ProviderWebdav, Enabled: true, IsPrimary: true,
			WebdavURL: "https://dav.example.test", WebdavUsername: "user", WebdavPassword: "secret",
		},
		{ID: "incomplete", Provider: core.ProviderWebdav, Enabled: true, WebdavURL: "https://dav.example.test"},
	}}

	accounts := writableObjectAccounts(state)
	if len(accounts) != 2 || accounts[0].ID != "dav" || accounts[1].ID != "cf" {
		t.Fatalf("writable accounts = %+v", accounts)
	}
}

func TestReferencedBackupAccountsIncludesDisabledHistoricalAccount(t *testing.T) {
	disabled := core.CloudflareAccount{
		ID: "old-dav", Provider: core.ProviderWebdav, Enabled: false,
		WebdavURL: "https://old.example.test", WebdavUsername: "user", WebdavPassword: "secret",
	}
	state := core.AppState{Accounts: []core.CloudflareAccount{disabled}}
	game := core.Game{BackupRegistry: []core.BackupRecord{{Filename: "backup.zip", AccountID: disabled.ID}}}

	if accounts := writableObjectAccounts(state); len(accounts) != 0 {
		t.Fatalf("disabled account was writable: %+v", accounts)
	}
	accounts := referencedBackupAccounts(state, game)
	if len(accounts) != 1 || accounts[0].ID != disabled.ID {
		t.Fatalf("historical read accounts = %+v", accounts)
	}
}

func TestBackupRecordHelpersDistinguishSameFilenameAcrossDevices(t *testing.T) {
	game := core.Game{BackupRegistry: []core.BackupRecord{
		{Filename: "same.zip", SourceDeviceID: "device-a", Name: "A"},
		{Filename: "same.zip", SourceDeviceID: "device-b", Name: "B"},
	}}
	recordB := game.BackupRegistry[1]
	recordB.Name = "B updated"
	upsertBackupRecord(&game, recordB)

	if len(game.BackupRegistry) != 2 {
		t.Fatalf("registry length = %d", len(game.BackupRegistry))
	}
	recordA, _, ok := findBackupRecord(game, "device-a:same.zip")
	if !ok || recordA.Name != "A" {
		t.Fatalf("device-a record changed: %+v, %v", recordA, ok)
	}
	updatedB, _, ok := findBackupRecord(game, "device-b:same.zip")
	if !ok || updatedB.Name != "B updated" {
		t.Fatalf("device-b record was not updated: %+v, %v", updatedB, ok)
	}
}

func TestCleanupOlderAutoBackupsOnlyTouchesCurrentDevice(t *testing.T) {
	dataDir := t.TempDir()
	oldA := core.BackupRecord{Filename: "old-a.zip", SourceDeviceID: "device-a", Type: "auto", Status: core.BackupStatusReady, CreatedAt: time.Unix(1, 0)}
	newA := core.BackupRecord{Filename: "new-a.zip", SourceDeviceID: "device-a", Type: "auto", Status: core.BackupStatusReady, CreatedAt: time.Unix(2, 0)}
	oldB := core.BackupRecord{Filename: "old-b.zip", SourceDeviceID: "device-b", Type: "auto", Status: core.BackupStatusReady, CreatedAt: time.Unix(1, 0)}
	game := core.Game{ID: "game", BackupRegistry: []core.BackupRecord{oldA, oldB, newA}}
	for _, record := range game.BackupRegistry {
		path := core.BackupLocalPathForRecord(dataDir, game.ID, record)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(core.BackupRecordID(record)), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cleanupOlderAutoBackupArtifacts(&game, newA, dataDir)
	if len(game.BackupRegistry) != 2 {
		t.Fatalf("registry after cleanup = %+v", game.BackupRegistry)
	}
	if _, _, ok := findBackupRecord(game, core.BackupRecordID(oldB)); !ok {
		t.Fatal("another device's automatic backup was removed")
	}
	if _, err := os.Stat(core.BackupLocalPathForRecord(dataDir, game.ID, oldA)); !os.IsNotExist(err) {
		t.Fatalf("old device-a cache still exists: %v", err)
	}
	if _, err := os.Stat(core.BackupLocalPathForRecord(dataDir, game.ID, oldB)); err != nil {
		t.Fatalf("device-b cache was removed: %v", err)
	}
}
