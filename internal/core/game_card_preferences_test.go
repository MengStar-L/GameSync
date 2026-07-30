package core

import (
	"testing"
	"time"
)

func TestDefaultPreferencesUseClassicGameCards(t *testing.T) {
	if got := DefaultPreferences().GameCardMode; got != GameCardModeClassic {
		t.Fatalf("default game card mode = %q", got)
	}
}

func TestNormalizeGameCardModeFallsBackToClassic(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "", want: GameCardModeClassic},
		{input: "unknown", want: GameCardModeClassic},
		{input: "  overlay-hover  ", want: GameCardModeOverlayHover},
		{input: GameCardModeOverlayPersistent, want: GameCardModeOverlayPersistent},
	} {
		if got := NormalizeGameCardMode(test.input); got != test.want {
			t.Fatalf("NormalizeGameCardMode(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestSavePreferencesNormalizesAndVersionsGameCardMode(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	prefs := store.Snapshot().Preferences
	prefs.GameCardMode = GameCardModeOverlayHover
	if err := store.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}

	saved := store.Snapshot().Preferences
	if saved.GameCardMode != GameCardModeOverlayHover || saved.GameCardModeUpdatedAt.IsZero() {
		t.Fatalf("saved game card mode = %+v", saved)
	}

	stale := saved
	stale.GameCardMode = GameCardModeOverlayPersistent
	stale.GameCardModeUpdatedAt = saved.GameCardModeUpdatedAt.Add(-time.Minute)
	if err := store.SavePreferences(stale); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Preferences.GameCardMode; got != GameCardModeOverlayHover {
		t.Fatalf("stale preference overwrote mode: %q", got)
	}

	invalid := store.Snapshot().Preferences
	invalid.GameCardMode = "invalid"
	if err := store.SavePreferences(invalid); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Preferences.GameCardMode; got != GameCardModeClassic {
		t.Fatalf("invalid mode did not fall back to classic: %q", got)
	}
}
