package core

import (
	"testing"
	"time"
)

func TestRemoteCatalogPreferencesStripsAPIKeys(t *testing.T) {
	now := time.Now()
	preferences := remoteCatalogPreferences(&RemotePreferences{
		RawgAPIKey:                 "rawg-secret",
		SteamGridDBAPIKey:          "sgdb-secret",
		RawgAPIKeyUpdatedAt:        now,
		SteamGridDBAPIKeyUpdatedAt: now,
		FavoriteGames:              []string{"game-1", "game-1"},
		FavoriteGamesUpdatedAt:     now,
		TagOrder:                   []string{"tag-a", "tag-a"},
		TagOrderUpdatedAt:          now,
		GameOrderUpdatedAt:         now,
	})

	if preferences.RawgAPIKey != "" || preferences.SteamGridDBAPIKey != "" {
		t.Fatalf("remote preferences leaked api keys: %+v", preferences)
	}
	if !preferences.RawgAPIKeyUpdatedAt.IsZero() || !preferences.SteamGridDBAPIKeyUpdatedAt.IsZero() {
		t.Fatalf("remote preferences kept api key timestamps: %+v", preferences)
	}
	if len(preferences.FavoriteGames) != 1 || preferences.FavoriteGames[0] != "game-1" {
		t.Fatalf("favorite games were not preserved and normalized: %+v", preferences.FavoriteGames)
	}
	if len(preferences.TagOrder) != 1 || preferences.TagOrder[0] != "tag-a" {
		t.Fatalf("tag order was not preserved and normalized: %+v", preferences.TagOrder)
	}
}

func TestR2UsageCacheRoundTripAndInvalidation(t *testing.T) {
	client := &R2Client{bucket: "bucket-" + NewID()}

	if _, ok := client.cachedUsageBytes(); ok {
		t.Fatal("empty cache returned a value")
	}
	client.cacheUsageBytes(42)
	if got, ok := client.cachedUsageBytes(); !ok || got != 42 {
		t.Fatalf("cache miss after storing usage: got=%d ok=%v", got, ok)
	}
	client.invalidateUsageCache()
	if _, ok := client.cachedUsageBytes(); ok {
		t.Fatal("cache hit after invalidation")
	}
}
