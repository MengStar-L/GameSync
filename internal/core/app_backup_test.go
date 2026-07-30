package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func portableBackupFixture(t *testing.T) AppState {
	t.Helper()
	account, err := NormalizeWebdavAccount(CloudflareAccount{
		Provider:       ProviderWebdav,
		WebdavURL:      "https://dav.example.com/root",
		WebdavRoot:     "GameSync",
		WebdavUsername: "user",
		WebdavPassword: "dav-secret",
		IsPrimary:      true,
		Enabled:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return AppState{
		Device:   DeviceInfo{ID: "old-device", Name: "old", Platform: "windows/amd64"},
		Accounts: []CloudflareAccount{account},
		Games: []Game{{
			ID: "game-1", Name: "Game", InstallPath: `C:\Games\Game`, SavePath: `C:\Saves\Game`,
			CoverLocalPath: `C:\covers\1.jpg`, CoverCloudKey: "covers/game-1.jpg",
			BackupLocations:       map[string]string{"save.zip": account.ID},
			LaunchRestoreOverride: &LaunchRestoreOverride{Filename: "manual.zip", Active: true},
		}},
		Preferences: Preferences{
			BackgroundSyncIntervalSeconds: 30,
			GameCardMode:                  GameCardModeOverlayPersistent,
			GameCardModeUpdatedAt:         time.Now().UTC(),
			DefaultInstallDir:             `C:\Games`,
			DefaultSaveDir:                `C:\Saves`,
			DefaultSteamInstallDir:        `D:\Steam`,
			DefaultSteamSaveDir:           `D:\SteamSaves`,
			DefaultThirdInstallDir:        `E:\Games`,
			DefaultThirdSaveDir:           `E:\Saves`,
			RawgAPIKey:                    "rawg-secret",
			SteamGridDBAPIKey:             "sgdb-secret",
		},
		Activities: []SyncActivity{{ID: "activity-1", GameID: "game-1", Status: "succeeded"}},
		Tombstones: CatalogTombstones{Games: map[string]time.Time{"deleted": time.Now().UTC()}},
		StorageMigration: &StorageMigrationState{Items: []StorageMigrationItem{{
			GameID: "game-1", LocalPath: `C:\migration\object.zip`, TargetKey: "objects/hash",
		}}, TargetGames: []Game{{
			ID: "target-game", InstallPath: `D:\Games\Target`, SavePath: `D:\Saves\Target`,
			CoverLocalPath: `D:\covers\target.jpg`, LaunchRestoreOverride: &LaunchRestoreOverride{Filename: "auto.zip", Active: true},
		}}},
	}
}

func TestPortableBackupIncludesSecretsAndExcludesMachinePaths(t *testing.T) {
	state := portableBackupFixture(t)
	data, err := EncodePortableBackup(state, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"dav-secret", "rawg-secret", "sgdb-secret"} {
		if !bytes.Contains(data, []byte(secret)) {
			t.Fatalf("backup omitted plaintext credential %q", secret)
		}
	}
	for _, machinePath := range []string{`C:\Games\Game`, `C:\Saves\Game`, `C:\covers\1.jpg`, `C:\Games`, `D:\Steam`, `E:\Saves`, `C:\migration\object.zip`, `D:\Games\Target`, `D:\Saves\Target`, `D:\covers\target.jpg`} {
		if bytes.Contains(data, []byte(machinePath)) {
			t.Fatalf("backup retained machine path %q", machinePath)
		}
	}

	decoded, err := DecodePortableBackup(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Accounts[0].WebdavPassword != "dav-secret" || decoded.Preferences.RawgAPIKey != "rawg-secret" {
		t.Fatalf("decoded backup lost credentials: %+v", decoded)
	}
	if decoded.Games[0].InstallPath != "" || decoded.Games[0].SavePath != "" || decoded.Games[0].CoverLocalPath != "" ||
		decoded.Games[0].LaunchRestoreOverride != nil || decoded.Preferences.DefaultInstallDir != "" ||
		decoded.StorageMigration.Items[0].LocalPath != "" || decoded.StorageMigration.TargetGames[0].SavePath != "" ||
		decoded.StorageMigration.TargetGames[0].LaunchRestoreOverride != nil {
		t.Fatalf("decoded backup retained machine paths: %+v", decoded)
	}
	if decoded.Games[0].BackupLocations["save.zip"] == "" || len(decoded.Activities) != 1 || len(decoded.Tombstones.Games) != 1 {
		t.Fatalf("decoded backup lost non-path state: %+v", decoded)
	}
	if decoded.Preferences.GameCardMode != GameCardModeOverlayPersistent || decoded.Preferences.GameCardModeUpdatedAt.IsZero() {
		t.Fatalf("decoded backup lost game card mode: %+v", decoded.Preferences)
	}
}

func TestDecodePortableBackupAcceptsLegacyAppState(t *testing.T) {
	want := portableBackupFixture(t)
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodePortableBackup(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Device.ID != want.Device.ID || got.Accounts[0].WebdavPassword != "dav-secret" {
		t.Fatalf("legacy backup did not decode: %+v", got)
	}
}

func TestDecodePortableBackupRejectsUnsupportedVersion(t *testing.T) {
	_, err := DecodePortableBackup([]byte(`{"formatVersion":99,"state":{}}`))
	if err == nil || !strings.Contains(err.Error(), "99") {
		t.Fatalf("unsupported version error = %v", err)
	}
}

func TestImportPortableBackupPreservesDeviceAndClearsInjectedPaths(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	currentDevice := store.Snapshot().Device
	state := portableBackupFixture(t)
	data, err := json.Marshal(PortableBackup{FormatVersion: PortableBackupFormatVersion, ExportedAt: time.Now(), State: state})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ImportState(data); err != nil {
		t.Fatal(err)
	}

	got := store.Snapshot()
	if got.Device.ID != currentDevice.ID || got.Device.Name != currentDevice.Name {
		t.Fatalf("current device was replaced: got=%+v want=%+v", got.Device, currentDevice)
	}
	if got.Games[0].InstallPath != "" || got.Games[0].SavePath != "" || got.Games[0].CoverLocalPath != "" ||
		got.Games[0].LaunchRestoreOverride != nil || got.Preferences.DefaultInstallDir != "" ||
		got.StorageMigration.Items[0].LocalPath != "" || got.StorageMigration.TargetGames[0].SavePath != "" ||
		got.StorageMigration.TargetGames[0].LaunchRestoreOverride != nil {
		t.Fatalf("import retained injected machine paths: %+v", got)
	}
	if got.Accounts[0].WebdavPassword != "dav-secret" || got.Preferences.SteamGridDBAPIKey != "sgdb-secret" {
		t.Fatalf("import lost credentials: %+v", got)
	}
	if !got.CatalogSync.Dirty {
		t.Fatal("import did not queue catalog synchronization")
	}
}

func TestImportPortableBackupRollsBackOnSaveFailure(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	before := store.Snapshot()
	data, err := EncodePortableBackup(portableBackupFixture(t), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	blockedPath := filepath.Join(dataDir, "state-target-directory")
	if err := os.Mkdir(blockedPath, 0o755); err != nil {
		t.Fatal(err)
	}
	store.path = blockedPath
	if err := store.ImportState(data); err == nil {
		t.Fatal("import unexpectedly succeeded when state replacement was blocked")
	}
	if got := store.Snapshot(); !reflect.DeepEqual(got, before) {
		t.Fatalf("failed import changed in-memory state: got=%+v want=%+v", got, before)
	}
}
