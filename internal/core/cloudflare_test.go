package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type d1RoundTripFunc func(*http.Request) (*http.Response, error)

func (fn d1RoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func d1TestResponse(t *testing.T, rows []map[string]any, changes int64) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"success": true,
		"errors":  []any{},
		"result": []any{map[string]any{
			"success": true,
			"results": rows,
			"meta":    map[string]any{"changes": changes},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func newD1HandoffTestClient(t *testing.T, transport d1RoundTripFunc) *D1Client {
	t.Helper()
	accountID := "account-" + NewID()
	databaseID := "database-" + NewID()
	schemaKey := accountID + ":" + databaseID
	ensuredD1Schemas.Store(schemaKey, true)
	t.Cleanup(func() { ensuredD1Schemas.Delete(schemaKey) })
	client := NewD1Client(CloudflareAccount{AccountID: accountID, D1DatabaseID: databaseID, APIToken: "token"})
	client.httpClient = &http.Client{Transport: transport}
	return client
}

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
		PinnedTags:                 []string{"tag-a", "tag-a", "tag-b"},
		PinnedTagsUpdatedAt:        now,
		SidebarNavOrder:            []string{"page:all-games", "tag:tag-a", "tag:tag-a"},
		SidebarNavOrderUpdatedAt:   now,
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
	if len(preferences.PinnedTags) != 2 || preferences.PinnedTags[0] != "tag-a" || preferences.PinnedTags[1] != "tag-b" {
		t.Fatalf("pinned tags were not preserved and normalized: %+v", preferences.PinnedTags)
	}
	if len(preferences.SidebarNavOrder) != 2 || preferences.SidebarNavOrder[0] != "page:all-games" || preferences.SidebarNavOrder[1] != "tag:tag-a" {
		t.Fatalf("sidebar nav order was not preserved and normalized: %+v", preferences.SidebarNavOrder)
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

func TestD1LoadStorageHandoffReadsCatalogMeta(t *testing.T) {
	want := StorageHandoff{
		TransactionID: "tx-load", SourceAccountID: "source", TargetAccountID: "target",
		State: StorageHandoffCommitted, Generation: 3,
	}
	payload, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	client := newD1HandoffTestClient(t, func(request *http.Request) (*http.Response, error) {
		var query d1QueryRequest
		if err := json.NewDecoder(request.Body).Decode(&query); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(query.SQL, "SELECT int_value, text_value FROM catalog_meta") {
			t.Fatalf("unexpected D1 query: %s", query.SQL)
		}
		return d1TestResponse(t, []map[string]any{{"int_value": 3, "text_value": string(payload)}}, 0), nil
	})

	got, err := client.LoadStorageHandoff(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.TransactionID != want.TransactionID || got.Generation != want.Generation || got.State != want.State {
		t.Fatalf("loaded handoff = %+v, want %+v", got, want)
	}
}

func TestD1SaveStorageHandoffUsesConditionalWriteResult(t *testing.T) {
	tests := []struct {
		name               string
		handoff            StorageHandoff
		expectedGeneration int64
		changes            int64
		wantSQL            string
		wantChanged        bool
	}{
		{
			name: "insert first generation", handoff: StorageHandoff{TransactionID: "tx-1", SourceAccountID: "a", TargetAccountID: "b", State: StorageHandoffPrepared, Generation: 1},
			expectedGeneration: 0, changes: 1, wantSQL: "WHERE NOT EXISTS",
		},
		{
			name: "reject occupied first generation", handoff: StorageHandoff{TransactionID: "tx-1", SourceAccountID: "a", TargetAccountID: "b", State: StorageHandoffPrepared, Generation: 1},
			expectedGeneration: 0, changes: 0, wantSQL: "WHERE NOT EXISTS", wantChanged: true,
		},
		{
			name: "commit same transaction", handoff: StorageHandoff{TransactionID: "tx-1", SourceAccountID: "a", TargetAccountID: "b", State: StorageHandoffCommitted, Generation: 1},
			expectedGeneration: 1, changes: 1, wantSQL: "json_extract(text_value, '$.transactionId') = ?",
		},
		{
			name: "reject different same-generation transaction", handoff: StorageHandoff{TransactionID: "tx-other", SourceAccountID: "a", TargetAccountID: "b", State: StorageHandoffCommitted, Generation: 1},
			expectedGeneration: 1, changes: 0, wantSQL: "json_extract(text_value, '$.transactionId') = ?", wantChanged: true,
		},
		{
			name: "advance generation", handoff: StorageHandoff{TransactionID: "tx-2", SourceAccountID: "b", TargetAccountID: "c", State: StorageHandoffPrepared, Generation: 2},
			expectedGeneration: 1, changes: 1, wantSQL: "SET int_value = ?",
		},
		{
			name: "reject stale generation advance", handoff: StorageHandoff{TransactionID: "tx-2", SourceAccountID: "b", TargetAccountID: "c", State: StorageHandoffPrepared, Generation: 2},
			expectedGeneration: 1, changes: 0, wantSQL: "SET int_value = ?", wantChanged: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newD1HandoffTestClient(t, func(request *http.Request) (*http.Response, error) {
				if request.Header.Get("Authorization") != "Bearer token" {
					t.Fatalf("authorization header = %q", request.Header.Get("Authorization"))
				}
				var query d1QueryRequest
				if err := json.NewDecoder(request.Body).Decode(&query); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(query.SQL, test.wantSQL) {
					t.Fatalf("D1 query %q does not contain %q", query.SQL, test.wantSQL)
				}
				return d1TestResponse(t, nil, test.changes), nil
			})

			err := client.SaveStorageHandoffIfGeneration(context.Background(), test.handoff, test.expectedGeneration)
			if test.wantChanged && !errors.Is(err, ErrStorageHandoffChanged) {
				t.Fatalf("error = %v, want ErrStorageHandoffChanged", err)
			}
			if !test.wantChanged && err != nil {
				t.Fatal(err)
			}
		})
	}
}
