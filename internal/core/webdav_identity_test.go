package core

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeWebdavAccountUsesNamespaceIdentity(t *testing.T) {
	first, err := NormalizeWebdavAccount(CloudflareAccount{
		Provider: ProviderWebdav, WebdavURL: "HTTPS://DAV.Example.test:443/base/",
		WebdavRoot: "/GameSync//", WebdavUsername: "first", WebdavPassword: "one",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NormalizeWebdavAccount(CloudflareAccount{
		Provider: ProviderWebdav, WebdavURL: "https://dav.example.test/base",
		WebdavRoot: "GameSync", WebdavUsername: "second", WebdavPassword: "two",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || !strings.HasPrefix(first.ID, "webdav-") || len(first.ID) != len("webdav-")+64 {
		t.Fatalf("stable IDs = %q, %q", first.ID, second.ID)
	}
	if first.WebdavURL != "https://dav.example.test/base" || first.WebdavRoot != "GameSync" {
		t.Fatalf("normalized namespace = %q / %q", first.WebdavURL, first.WebdavRoot)
	}

	other, err := NormalizeWebdavAccount(CloudflareAccount{
		Provider: ProviderWebdav, WebdavURL: second.WebdavURL,
		WebdavRoot: "Other", WebdavUsername: second.WebdavUsername, WebdavPassword: second.WebdavPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == first.ID {
		t.Fatal("different roots received the same WebDAV identity")
	}
}

func TestNormalizeWebdavAccountsMarksCanonicalMetadataDirty(t *testing.T) {
	now := time.Now()
	account, err := NormalizeWebdavAccount(CloudflareAccount{
		Provider: ProviderWebdav, WebdavURL: "https://dav.example.test/base", WebdavRoot: "GameSync",
	})
	if err != nil {
		t.Fatal(err)
	}
	account.Provider = " WEBDAV "
	account.WebdavURL = "HTTPS://DAV.Example.test:443/base/"
	account.WebdavRoot = "/GameSync//"

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.state.Accounts = []CloudflareAccount{account}
	store.state.CatalogSync.Dirty = false

	aliases := store.normalizeWebdavAccountsLocked(now)
	if len(aliases) != 0 {
		t.Fatalf("canonical ID unexpectedly produced aliases: %+v", aliases)
	}
	got := store.state.Accounts[0]
	if got.Provider != ProviderWebdav || got.WebdavURL != "https://dav.example.test/base" || got.WebdavRoot != "GameSync" {
		t.Fatalf("canonical metadata = %+v", got)
	}
	if !store.state.CatalogSync.Dirty {
		t.Fatal("canonical metadata change did not mark catalog dirty")
	}
}

func TestNormalizeWebdavAccountsRepointsLegacyReferences(t *testing.T) {
	now := time.Now()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.state.Accounts = []CloudflareAccount{
		{ID: "legacy-local", Provider: ProviderWebdav, WebdavURL: "https://dav.example.test/root/", WebdavRoot: "GameSync", WebdavUsername: "user", WebdavPassword: "password", IsPrimary: true, Enabled: true, VerificationState: "valid", CatalogUpdatedAt: now},
		{ID: "legacy-remote", Provider: ProviderWebdav, WebdavURL: "https://DAV.example.test:443/root", WebdavRoot: "/GameSync/", WebdavUsername: "other", IsPrimary: false, Enabled: false, VerificationState: "invalid", CatalogUpdatedAt: now.Add(-time.Hour)},
	}
	store.state.Games = []Game{{
		ID: "game", StorageAccountID: "legacy-local", AutoBackupAccountID: "legacy-remote",
		BackupStorageAccountID: "legacy-remote", CoverCloudAccountID: "legacy-remote",
		CoverPath:       remoteCoverReference("legacy-remote", "covers/game/hash.jpg"),
		CoverSourceType: "local_file",
		CoverSource:     remoteCoverReference("legacy-remote", "covers/game/hash.jpg"),
		Anchor:          SyncAnchor{StorageAccountID: "legacy-local"},
		BackupLocations: map[string]string{"save.zip": "legacy-remote"},
		BackupRegistry:  []BackupRecord{{Filename: "save.zip", AccountID: "legacy-remote"}},
	}}
	store.state.Activities = []SyncActivity{{ID: "activity", AccountID: "legacy-remote"}}
	store.state.LastStorageHandoff = &StorageHandoff{SourceAccountID: "legacy-local", TargetAccountID: "legacy-remote"}
	store.state.StorageMigration = &StorageMigrationState{
		SourceAccountID: "legacy-local", TargetAccountID: "legacy-remote",
		Items:       []StorageMigrationItem{{SourceAccountID: "legacy-remote"}},
		TargetGames: []Game{{ID: "target", CoverCloudAccountID: "legacy-remote", StorageAccountID: "legacy-local"}},
	}
	store.ensureTombstonesLocked()

	aliases := store.normalizeWebdavAccountsLocked(now)
	if len(store.state.Accounts) != 1 {
		t.Fatalf("accounts = %+v", store.state.Accounts)
	}
	canonical := store.state.Accounts[0]
	if canonical.ID == "legacy-local" || canonical.ID == "legacy-remote" || canonical.WebdavPassword != "password" || !canonical.IsPrimary || !canonical.Enabled {
		t.Fatalf("canonical account = %+v", canonical)
	}
	if aliases["legacy-local"] != canonical.ID || aliases["legacy-remote"] != canonical.ID {
		t.Fatalf("aliases = %+v", aliases)
	}

	game := store.state.Games[0]
	for name, value := range map[string]string{
		"storage": game.StorageAccountID, "auto": game.AutoBackupAccountID,
		"backup": game.BackupStorageAccountID, "cover": game.CoverCloudAccountID,
		"anchor": game.Anchor.StorageAccountID, "location": game.BackupLocations["save.zip"],
		"record": game.BackupRegistry[0].AccountID, "activity": store.state.Activities[0].AccountID,
		"handoff-source":   store.state.LastStorageHandoff.SourceAccountID,
		"handoff-target":   store.state.LastStorageHandoff.TargetAccountID,
		"migration-source": store.state.StorageMigration.SourceAccountID,
		"migration-target": store.state.StorageMigration.TargetAccountID,
		"migration-item":   store.state.StorageMigration.Items[0].SourceAccountID,
		"target-storage":   store.state.StorageMigration.TargetGames[0].StorageAccountID,
		"target-cover":     store.state.StorageMigration.TargetGames[0].CoverCloudAccountID,
	} {
		if value != canonical.ID {
			t.Errorf("%s reference = %q, want %q", name, value, canonical.ID)
		}
	}
	wantCoverReference := remoteCoverReference(canonical.ID, "covers/game/hash.jpg")
	if game.CoverPath != wantCoverReference || game.CoverSource != wantCoverReference {
		t.Fatalf("portable cover references = %q, %q, want %q", game.CoverPath, game.CoverSource, wantCoverReference)
	}
	if _, ok := store.state.Tombstones.Accounts[canonical.ID]; ok {
		t.Fatal("canonical account was tombstoned")
	}
	if _, ok := store.state.Tombstones.Accounts["legacy-local"]; !ok {
		t.Fatal("legacy local ID was not tombstoned")
	}
	if _, ok := store.state.Tombstones.Accounts["legacy-remote"]; !ok {
		t.Fatal("legacy remote ID was not tombstoned")
	}
}

func TestNormalizeWebdavAccountsRepairsCoverReferenceAfterLegacyAccountRemoval(t *testing.T) {
	now := time.Now()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := NormalizeWebdavAccount(CloudflareAccount{
		Provider: ProviderWebdav, WebdavURL: "https://dav.example.test/root", WebdavRoot: "GameSync",
		WebdavUsername: "user", WebdavPassword: "password", IsPrimary: true, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	objectKey := "covers/game/hash.jpg"
	legacyReference := remoteCoverReference("removed-legacy-account", objectKey)
	store.state.Accounts = []CloudflareAccount{canonical}
	store.state.Games = []Game{{
		ID: "game", CoverPath: legacyReference, CoverSource: legacyReference,
		CoverCloudAccountID: canonical.ID, CoverCloudKey: objectKey,
	}}
	store.state.CatalogSync.Dirty = false

	aliases := store.normalizeWebdavAccountsLocked(now)
	if len(aliases) != 0 {
		t.Fatalf("aliases = %+v, want none after legacy account removal", aliases)
	}
	want := remoteCoverReference(canonical.ID, objectKey)
	game := store.state.Games[0]
	if game.CoverPath != want || game.CoverSource != want {
		t.Fatalf("portable cover references = %q, %q, want %q", game.CoverPath, game.CoverSource, want)
	}
	if !store.state.CatalogSync.Dirty {
		t.Fatal("cover reference repair did not mark catalog dirty")
	}
	store.state.CatalogSync.Dirty = false
	store.normalizeWebdavAccountsLocked(now.Add(time.Minute))
	if store.state.CatalogSync.Dirty {
		t.Fatal("idempotent cover reference normalization marked catalog dirty again")
	}
}

func TestNormalizeWebdavAccountsDisablesOtherNamespaces(t *testing.T) {
	now := time.Now()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.state.Accounts = []CloudflareAccount{
		{ID: "primary", Provider: ProviderWebdav, WebdavURL: "https://dav.example.test", WebdavRoot: "GameSync", WebdavUsername: "u", WebdavPassword: "p", IsPrimary: true, Enabled: true},
		{ID: "other", Provider: ProviderWebdav, WebdavURL: "https://dav.example.test", WebdavRoot: "Other", WebdavUsername: "u", WebdavPassword: "p", Enabled: true},
	}
	store.normalizeWebdavAccountsLocked(now)
	if len(store.state.Accounts) != 2 {
		t.Fatalf("accounts were deleted: %+v", store.state.Accounts)
	}
	for _, account := range store.state.Accounts {
		if account.IsPrimary {
			if !account.Enabled {
				t.Fatalf("primary namespace was disabled: %+v", account)
			}
			continue
		}
		if account.Enabled || account.LastError != msgWebdavDifferentNamespace {
			t.Fatalf("other namespace was not marked for migration: %+v", account)
		}
	}
}

func TestMergeSameWebdavAccountKeepsVerifiedCredentialPair(t *testing.T) {
	verifiedAt := time.Now().Add(-time.Hour)
	merged := mergeSameWebdavAccount(
		CloudflareAccount{
			WebdavUsername: "verified-user", WebdavPassword: "verified-password",
			VerificationState: "valid", LastVerifiedAt: &verifiedAt, CatalogUpdatedAt: time.Now().Add(-2 * time.Hour),
		},
		CloudflareAccount{
			WebdavUsername: "unverified-user", VerificationState: "invalid", CatalogUpdatedAt: time.Now(),
		},
	)
	if merged.WebdavUsername != "verified-user" || merged.WebdavPassword != "verified-password" || merged.VerificationState != "valid" {
		t.Fatalf("verified credentials were split during merge: %+v", merged)
	}
}
