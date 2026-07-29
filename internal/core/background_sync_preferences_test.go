package core

import (
	"os"
	"path/filepath"
	"testing"
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
		})
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
