package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackgroundSyncPreferenceDefaultsAndPreservesExplicitDisabled(t *testing.T) {
	for _, test := range []struct {
		name  string
		prefs string
		want  int
	}{
		{name: "legacy missing field", prefs: `{"startupSyncMode":"smart","conflictPolicy":"manual"}`, want: 60},
		{name: "explicit disabled", prefs: `{"startupSyncMode":"smart","conflictPolicy":"manual","backgroundSyncIntervalSeconds":0}`, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			dataDir := t.TempDir()
			content := `{"device":{"id":"device-1","name":"test","platform":"windows/amd64"},"preferences":` + test.prefs + `}`
			if err := os.WriteFile(filepath.Join(dataDir, "state.json"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := NewStore(dataDir)
			if err != nil {
				t.Fatal(err)
			}
			if got := store.Snapshot().Preferences.BackgroundSyncIntervalSeconds; got != test.want {
				t.Fatalf("interval = %d, want %d", got, test.want)
			}
			if store.Snapshot().Preferences.SyncSettingsUpdatedAt.IsZero() {
				t.Fatal("legacy synchronized settings were not assigned an initial timestamp")
			}
		})
	}
}

func TestLegacyAPIKeysReceiveInitialSyncTimestamps(t *testing.T) {
	dataDir := t.TempDir()
	content := `{"device":{"id":"device-1","name":"test","platform":"windows/amd64"},"preferences":{"autoSyncOnLaunch":true,"startupSyncMode":"smart","conflictPolicy":"manual","backgroundSyncIntervalSeconds":60,"rawgApiKey":"rawg","steamGridDbApiKey":"sgdb"}}`
	if err := os.WriteFile(filepath.Join(dataDir, "state.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	prefs := store.Snapshot().Preferences
	if prefs.RawgAPIKeyUpdatedAt.IsZero() || prefs.SteamGridDBAPIKeyUpdatedAt.IsZero() {
		t.Fatalf("legacy API keys were not assigned initial timestamps: %+v", prefs)
	}
}

func TestSavePreferencesRejectsUnsupportedBackgroundSyncInterval(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prefs := store.Snapshot().Preferences
	prefs.BackgroundSyncIntervalSeconds = 45
	if err := store.SavePreferences(prefs); err == nil {
		t.Fatal("unsupported background sync interval was accepted")
	}
}

func TestSavePreferencesTracksSynchronizedGroups(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	base := store.Snapshot().Preferences
	base.SyncSettingsUpdatedAt = time.Now().Add(-time.Hour)
	base.RawgAPIKeyUpdatedAt = time.Now().Add(-time.Hour)
	if err := store.SavePreferences(base); err != nil {
		t.Fatal(err)
	}

	changed := store.Snapshot().Preferences
	oldSettingsAt := changed.SyncSettingsUpdatedAt
	oldRawgAt := changed.RawgAPIKeyUpdatedAt
	changed.BackgroundSyncIntervalSeconds = 30
	if err := store.SavePreferences(changed); err != nil {
		t.Fatal(err)
	}

	got := store.Snapshot().Preferences
	if !got.SyncSettingsUpdatedAt.After(oldSettingsAt) {
		t.Fatal("sync settings timestamp was not advanced")
	}
	if !got.RawgAPIKeyUpdatedAt.Equal(oldRawgAt) {
		t.Fatal("unmodified RAWG timestamp changed")
	}
}

func TestSavePreferencesRejectsStaleSynchronizedGroups(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	fresh := store.Snapshot().Preferences
	fresh.BackgroundSyncIntervalSeconds = 30
	fresh.RawgAPIKey = "fresh-key"
	if err := store.SavePreferences(fresh); err != nil {
		t.Fatal(err)
	}
	current := store.Snapshot().Preferences

	stale := current
	stale.BackgroundSyncIntervalSeconds = 300
	stale.SyncSettingsUpdatedAt = current.SyncSettingsUpdatedAt.Add(-time.Minute)
	stale.RawgAPIKey = "stale-key"
	stale.RawgAPIKeyUpdatedAt = current.RawgAPIKeyUpdatedAt.Add(-time.Minute)
	if err := store.SavePreferences(stale); err != nil {
		t.Fatal(err)
	}

	got := store.Snapshot().Preferences
	if got.BackgroundSyncIntervalSeconds != 30 || got.RawgAPIKey != "fresh-key" {
		t.Fatalf("stale synchronized fields overwrote current values: %+v", got)
	}

	clear := got
	clear.RawgAPIKey = ""
	if err := store.SavePreferences(clear); err != nil {
		t.Fatal(err)
	}
	if store.Snapshot().Preferences.RawgAPIKey != "" {
		t.Fatal("clearing an API key was not accepted")
	}
}

func TestCatalogCheckFailureDoesNotCreatePendingUpload(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkCatalogCheckFailed("offline"); err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot()
	if state.CatalogSync.Dirty || state.CatalogSync.LastError != "offline" {
		t.Fatalf("catalog state = %+v", state.CatalogSync)
	}
	if err := store.MarkCatalogDirty(); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkCatalogCheckFailed("still offline"); err != nil {
		t.Fatal(err)
	}
	if !store.Snapshot().CatalogSync.Dirty {
		t.Fatal("check failure cleared an existing pending upload")
	}
}
