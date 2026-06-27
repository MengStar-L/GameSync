package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const d1BaseURL = "https://api.cloudflare.com/client/v4"

var ensuredD1Schemas sync.Map

type D1Client struct {
	accountID  string
	databaseID string
	apiToken   string
	httpClient *http.Client
}

type d1QueryRequest struct {
	SQL    string `json:"sql"`
	Params []any  `json:"params,omitempty"`
}

type d1APIError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message"`
}

type d1StatementResult struct {
	Success bool             `json:"success"`
	Results []map[string]any `json:"results"`
}

type d1QueryEnvelope struct {
	Success bool                `json:"success"`
	Errors  []d1APIError        `json:"errors"`
	Result  []d1StatementResult `json:"result"`
}

type tokenVerifyResult struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	ExpiresOn *time.Time `json:"expires_on"`
	NotBefore *time.Time `json:"not_before"`
}

type tokenVerifyEnvelope struct {
	Success bool              `json:"success"`
	Errors  []d1APIError      `json:"errors"`
	Result  tokenVerifyResult `json:"result"`
}

type R2Client struct {
	bucket string
	client *s3.Client
}

type CloudflareGateway struct {
	D1 *D1Client
	R2 *R2Client
}

type EncryptedCredentialBlob struct {
	Version    int    `json:"version"`
	KDF        string `json:"kdf"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func NewCloudflareGateway(ctx context.Context, account CloudflareAccount) (*CloudflareGateway, error) {
	if strings.TrimSpace(account.AccountID) == "" || strings.TrimSpace(account.APIToken) == "" || strings.TrimSpace(account.D1DatabaseID) == "" {
		return nil, errors.New(msgD1ConfigIncomplete)
	}
	if strings.TrimSpace(account.R2Bucket) == "" || strings.TrimSpace(account.R2AccessKeyID) == "" || strings.TrimSpace(account.R2SecretAccessKey) == "" {
		return nil, errors.New(msgR2ConfigIncomplete)
	}

	r2, err := NewR2Client(ctx, account)
	if err != nil {
		return nil, err
	}

	return &CloudflareGateway{
		D1: NewD1Client(account),
		R2: r2,
	}, nil
}

func NewSplitCloudflareGateway(ctx context.Context, metadataAccount CloudflareAccount, storageAccount CloudflareAccount) (*CloudflareGateway, error) {
	if strings.TrimSpace(metadataAccount.AccountID) == "" || strings.TrimSpace(metadataAccount.APIToken) == "" || strings.TrimSpace(metadataAccount.D1DatabaseID) == "" {
		return nil, errors.New(msgPrimaryD1ConfigIncomplete)
	}
	if strings.TrimSpace(storageAccount.AccountID) == "" || strings.TrimSpace(storageAccount.R2Bucket) == "" || strings.TrimSpace(storageAccount.R2AccessKeyID) == "" || strings.TrimSpace(storageAccount.R2SecretAccessKey) == "" {
		return nil, errors.New(msgStorageR2ConfigIncomplete)
	}

	r2, err := NewR2Client(ctx, storageAccount)
	if err != nil {
		return nil, err
	}

	return &CloudflareGateway{
		D1: NewD1Client(metadataAccount),
		R2: r2,
	}, nil
}

func NewD1Client(account CloudflareAccount) *D1Client {
	return &D1Client{
		accountID:  account.AccountID,
		databaseID: account.D1DatabaseID,
		apiToken:   account.APIToken,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *D1Client) EnsureSchema(ctx context.Context) error {
	schemaKey := strings.TrimSpace(c.accountID) + ":" + strings.TrimSpace(c.databaseID)
	if schemaKey != ":" {
		if ensured, ok := ensuredD1Schemas.Load(schemaKey); ok {
			if done, valid := ensured.(bool); valid && done {
				return nil
			}
		}
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS sync_manifests (
			game_id TEXT PRIMARY KEY,
			version INTEGER NOT NULL,
			manifest_json TEXT NOT NULL,
			total_bytes INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			updated_by_device TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS sync_revisions (
			game_id TEXT NOT NULL,
			version INTEGER NOT NULL,
			manifest_json TEXT NOT NULL,
			total_bytes INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			updated_by_device TEXT NOT NULL,
			PRIMARY KEY (game_id, version)
		);`,
		`CREATE TABLE IF NOT EXISTS games_catalog (
			game_id TEXT PRIMARY KEY,
			game_json TEXT NOT NULL,
			storage_account_id TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS accounts_registry (
			account_id TEXT PRIMARY KEY,
			account_json TEXT NOT NULL,
			encrypted_credentials_json TEXT,
			is_primary INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS devices_registry (
			device_id TEXT PRIMARY KEY,
			device_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS app_preferences (
			key TEXT PRIMARY KEY,
			value_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS catalog_tombstones (
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			deleted_at TEXT NOT NULL,
			PRIMARY KEY (entity_type, entity_id)
		);`,
		`CREATE TABLE IF NOT EXISTS catalog_meta (
			key TEXT PRIMARY KEY,
			int_value INTEGER NOT NULL DEFAULT 0,
			text_value TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		);`,
	}

	for _, statement := range statements {
		if _, err := c.Query(ctx, statement); err != nil {
			return err
		}
	}
	if err := c.ensureColumn(ctx, "games_catalog", "display_order", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := c.ensureColumn(ctx, "games_catalog", "display_order_updated_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	if schemaKey != ":" {
		ensuredD1Schemas.Store(schemaKey, true)
	}
	return nil
}

func (c *D1Client) SaveRemoteCatalog(ctx context.Context, catalog RemoteCatalog, encryptedCredentials map[string]EncryptedCredentialBlob, device DeviceInfo) (int64, error) {
	if err := c.EnsureSchema(ctx); err != nil {
		return 0, err
	}

	now := time.Now()
	nowText := now.Format(time.RFC3339Nano)
	orderUpdatedAt := now
	if catalog.Preferences != nil && !catalog.Preferences.GameOrderUpdatedAt.IsZero() {
		orderUpdatedAt = catalog.Preferences.GameOrderUpdatedAt
	}
	orderUpdatedAtText := orderUpdatedAt.Format(time.RFC3339Nano)
	for index, game := range catalog.Games {
		updatedAt := catalogTimestamp(game.CatalogUpdatedAt)
		publicGame := game
		publicGame.CatalogUpdatedAt = updatedAt
		publicGame.InstallPath = ""
		publicGame.SavePath = ""
		gameJSON, err := json.Marshal(publicGame)
		if err != nil {
			return 0, fmt.Errorf("encode game catalog: %w", err)
		}
		updatedAtText := updatedAt.Format(time.RFC3339Nano)
		if _, err := c.Query(ctx, `INSERT INTO games_catalog (game_id, game_json, storage_account_id, display_order, display_order_updated_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(game_id) DO UPDATE SET
				game_json = CASE WHEN excluded.updated_at >= games_catalog.updated_at THEN excluded.game_json ELSE games_catalog.game_json END,
				storage_account_id = CASE WHEN excluded.updated_at >= games_catalog.updated_at THEN excluded.storage_account_id ELSE games_catalog.storage_account_id END,
				updated_at = CASE WHEN excluded.updated_at >= games_catalog.updated_at THEN excluded.updated_at ELSE games_catalog.updated_at END,
				display_order = CASE WHEN excluded.display_order_updated_at >= games_catalog.display_order_updated_at THEN excluded.display_order ELSE games_catalog.display_order END,
				display_order_updated_at = CASE WHEN excluded.display_order_updated_at >= games_catalog.display_order_updated_at THEN excluded.display_order_updated_at ELSE games_catalog.display_order_updated_at END;`,
			game.ID, string(gameJSON), game.StorageAccountID, index, orderUpdatedAtText, updatedAtText); err != nil {
			return 0, err
		}
		_, _ = c.Query(ctx, `DELETE FROM catalog_tombstones WHERE entity_type = 'game' AND entity_id = ? AND deleted_at <= ?;`, game.ID, updatedAtText)
	}

	for _, account := range catalog.Accounts {
		updatedAt := catalogTimestamp(account.CatalogUpdatedAt)
		publicAccount := account
		publicAccount.CatalogUpdatedAt = updatedAt
		publicAccount.APIToken = ""
		// The primary account is the trust anchor and must stay local. Secondary
		// R2 credentials are stored in the primary account's D1 catalog so a
		// verified primary account can restore them directly.
		if account.IsPrimary {
			publicAccount.R2AccessKeyID = ""
			publicAccount.R2SecretAccessKey = ""
		}
		accountJSON, err := json.Marshal(publicAccount)
		if err != nil {
			return 0, fmt.Errorf("encode account registry: %w", err)
		}
		var encryptedJSON string
		if blob, ok := encryptedCredentials[account.ID]; ok {
			content, err := json.Marshal(blob)
			if err != nil {
				return 0, fmt.Errorf("encode encrypted credentials: %w", err)
			}
			encryptedJSON = string(content)
		}
		updatedAtText := updatedAt.Format(time.RFC3339Nano)
		if _, err := c.Query(ctx, `INSERT INTO accounts_registry (account_id, account_json, encrypted_credentials_json, is_primary, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(account_id) DO UPDATE SET
				account_json = CASE WHEN excluded.updated_at >= accounts_registry.updated_at THEN excluded.account_json ELSE accounts_registry.account_json END,
				encrypted_credentials_json = CASE
					WHEN excluded.encrypted_credentials_json = '' THEN accounts_registry.encrypted_credentials_json
					WHEN excluded.updated_at < accounts_registry.updated_at THEN accounts_registry.encrypted_credentials_json
					ELSE excluded.encrypted_credentials_json
				END,
				is_primary = CASE WHEN excluded.updated_at >= accounts_registry.updated_at THEN excluded.is_primary ELSE accounts_registry.is_primary END,
				updated_at = CASE WHEN excluded.updated_at >= accounts_registry.updated_at THEN excluded.updated_at ELSE accounts_registry.updated_at END;`,
			account.ID, string(accountJSON), encryptedJSON, boolInt(account.IsPrimary), updatedAtText); err != nil {
			return 0, err
		}
		_, _ = c.Query(ctx, `DELETE FROM catalog_tombstones WHERE entity_type = 'account' AND entity_id = ? AND deleted_at <= ?;`, account.ID, updatedAtText)
	}

	deviceJSON, err := json.Marshal(device)
	if err != nil {
		return 0, fmt.Errorf("encode device registry: %w", err)
	}
	_, err = c.Query(ctx, `INSERT INTO devices_registry (device_id, device_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET
			device_json = excluded.device_json,
			updated_at = excluded.updated_at;`, device.ID, string(deviceJSON), nowText)
	if err != nil {
		return 0, err
	}

	sharedPreferences := catalog.Preferences
	if sharedPreferences == nil {
		sharedPreferences = &RemotePreferences{}
	}
	preferencesJSON, err := json.Marshal(sharedPreferences)
	if err != nil {
		return 0, fmt.Errorf("encode shared preferences: %w", err)
	}
	preferencesUpdatedAt := maxTime(
		sharedPreferences.RawgAPIKeyUpdatedAt,
		sharedPreferences.SteamGridDBAPIKeyUpdatedAt,
		sharedPreferences.FavoriteGamesUpdatedAt,
		sharedPreferences.TagOrderUpdatedAt,
		sharedPreferences.GameOrderUpdatedAt,
	)
	if preferencesUpdatedAt.IsZero() {
		preferencesUpdatedAt = now
	}
	_, err = c.Query(ctx, `INSERT INTO app_preferences (key, value_json, updated_at)
		VALUES ('shared', ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
		WHERE excluded.updated_at >= app_preferences.updated_at;`, string(preferencesJSON), preferencesUpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}

	if err := c.saveCatalogTombstones(ctx, "game", catalog.Tombstones.Games); err != nil {
		return 0, err
	}
	if err := c.saveCatalogTombstones(ctx, "account", catalog.Tombstones.Accounts); err != nil {
		return 0, err
	}
	return c.IncrementCatalogRevision(ctx)
}

func (c *D1Client) LoadCatalogRevision(ctx context.Context) (int64, error) {
	if err := c.EnsureSchema(ctx); err != nil {
		return 0, err
	}
	rows, err := c.Query(ctx, `SELECT int_value FROM catalog_meta WHERE key = 'revision' LIMIT 1;`)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return asInt64(rows[0]["int_value"]), nil
}

func (c *D1Client) IncrementCatalogRevision(ctx context.Context) (int64, error) {
	now := time.Now().Format(time.RFC3339Nano)
	if _, err := c.Query(ctx, `INSERT INTO catalog_meta (key, int_value, text_value, updated_at)
		VALUES ('revision', 1, '', ?)
		ON CONFLICT(key) DO UPDATE SET
			int_value = catalog_meta.int_value + 1,
			updated_at = excluded.updated_at;`, now); err != nil {
		return 0, err
	}
	return c.LoadCatalogRevision(ctx)
}

func (c *D1Client) LoadRemoteCatalog(ctx context.Context) (RemoteCatalog, map[string]EncryptedCredentialBlob, error) {
	if err := c.EnsureSchema(ctx); err != nil {
		return RemoteCatalog{}, nil, err
	}

	catalog := RemoteCatalog{
		Accounts: []CloudflareAccount{},
		Games:    []Game{},
		Tombstones: CatalogTombstones{
			Games:    map[string]time.Time{},
			Accounts: map[string]time.Time{},
		},
	}
	if revision, err := c.LoadCatalogRevision(ctx); err == nil {
		catalog.Revision = revision
	}
	rows, err := c.Query(ctx, `SELECT game_json, updated_at FROM games_catalog ORDER BY display_order ASC, updated_at ASC;`)
	if err != nil {
		return catalog, nil, err
	}
	for _, row := range rows {
		var game Game
		if err := json.Unmarshal([]byte(fmt.Sprintf("%v", row["game_json"])), &game); err == nil && strings.TrimSpace(game.ID) != "" {
			if game.CatalogUpdatedAt.IsZero() {
				game.CatalogUpdatedAt = parseCatalogTime(row["updated_at"])
			}
			catalog.Games = append(catalog.Games, game)
		}
	}

	encrypted := make(map[string]EncryptedCredentialBlob)
	rows, err = c.Query(ctx, `SELECT account_id, account_json, encrypted_credentials_json, updated_at FROM accounts_registry ORDER BY is_primary DESC, updated_at ASC;`)
	if err != nil {
		return catalog, nil, err
	}
	for _, row := range rows {
		var account CloudflareAccount
		if err := json.Unmarshal([]byte(fmt.Sprintf("%v", row["account_json"])), &account); err == nil && strings.TrimSpace(account.ID) != "" {
			if account.CatalogUpdatedAt.IsZero() {
				account.CatalogUpdatedAt = parseCatalogTime(row["updated_at"])
			}
			catalog.Accounts = append(catalog.Accounts, account)
		}

		encryptedJSON := strings.TrimSpace(fmt.Sprintf("%v", row["encrypted_credentials_json"]))
		if encryptedJSON == "" || encryptedJSON == "<nil>" {
			continue
		}
		var blob EncryptedCredentialBlob
		if err := json.Unmarshal([]byte(encryptedJSON), &blob); err == nil {
			encrypted[fmt.Sprintf("%v", row["account_id"])] = blob
		}
	}

	rows, err = c.Query(ctx, `SELECT value_json FROM app_preferences WHERE key = 'shared' LIMIT 1;`)
	if err != nil {
		return catalog, nil, err
	}
	if len(rows) > 0 {
		var preferences RemotePreferences
		if err := json.Unmarshal([]byte(fmt.Sprintf("%v", rows[0]["value_json"])), &preferences); err == nil {
			catalog.Preferences = &preferences
		}
	}

	rows, err = c.Query(ctx, `SELECT entity_type, entity_id, deleted_at FROM catalog_tombstones;`)
	if err != nil {
		return catalog, nil, err
	}
	for _, row := range rows {
		entityID := strings.TrimSpace(fmt.Sprintf("%v", row["entity_id"]))
		if entityID == "" {
			continue
		}
		deletedAt := parseCatalogTime(row["deleted_at"])
		switch fmt.Sprintf("%v", row["entity_type"]) {
		case "game":
			catalog.Tombstones.Games[entityID] = deletedAt
		case "account":
			catalog.Tombstones.Accounts[entityID] = deletedAt
		}
	}

	return catalog, encrypted, nil
}

func (c *D1Client) saveCatalogTombstones(ctx context.Context, entityType string, tombstones map[string]time.Time) error {
	for entityID, deletedAt := range tombstones {
		entityID = strings.TrimSpace(entityID)
		if entityID == "" || deletedAt.IsZero() {
			continue
		}
		deletedAtText := deletedAt.Format(time.RFC3339Nano)
		if _, err := c.Query(ctx, `INSERT INTO catalog_tombstones (entity_type, entity_id, deleted_at)
			VALUES (?, ?, ?)
			ON CONFLICT(entity_type, entity_id) DO UPDATE SET
				deleted_at = CASE WHEN excluded.deleted_at >= catalog_tombstones.deleted_at THEN excluded.deleted_at ELSE catalog_tombstones.deleted_at END;`,
			entityType, entityID, deletedAtText); err != nil {
			return err
		}
		switch entityType {
		case "game":
			if _, err := c.Query(ctx, `DELETE FROM games_catalog WHERE game_id = ? AND updated_at <= ?;`, entityID, deletedAtText); err != nil {
				return err
			}
		case "account":
			if _, err := c.Query(ctx, `DELETE FROM accounts_registry WHERE account_id = ? AND updated_at <= ?;`, entityID, deletedAtText); err != nil {
				return err
			}
		}
	}
	return nil
}

func catalogTimestamp(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now()
	}
	return value
}

func parseCatalogTime(value any) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(fmt.Sprintf("%v", value)))
	return parsed
}

func maxTime(values ...time.Time) time.Time {
	var max time.Time
	for _, value := range values {
		if value.After(max) {
			max = value
		}
	}
	return max
}

func (c *D1Client) ensureColumn(ctx context.Context, tableName string, columnName string, definition string) error {
	rows, err := c.Query(ctx, fmt.Sprintf(`PRAGMA table_info(%s);`, tableName))
	if err != nil {
		return err
	}
	for _, row := range rows {
		if strings.EqualFold(fmt.Sprintf("%v", row["name"]), columnName) {
			return nil
		}
	}
	_, err = c.Query(ctx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s;`, tableName, columnName, definition))
	return err
}

func (c *D1Client) LoadRemoteManifest(ctx context.Context, gameID string) (RemoteManifestRecord, error) {
	rows, err := c.Query(ctx, `SELECT game_id, version, manifest_json, updated_at, updated_by_device FROM sync_manifests WHERE game_id = ? LIMIT 1;`, gameID)
	if err != nil {
		return RemoteManifestRecord{}, err
	}
	if len(rows) == 0 {
		return RemoteManifestRecord{}, nil
	}

	row := rows[0]
	record := RemoteManifestRecord{
		GameID:          fmt.Sprintf("%v", row["game_id"]),
		Version:         int(asInt64(row["version"])),
		UpdatedByDevice: fmt.Sprintf("%v", row["updated_by_device"]),
	}

	if updatedAt, err := time.Parse(time.RFC3339Nano, fmt.Sprintf("%v", row["updated_at"])); err == nil {
		record.UpdatedAt = updatedAt
	}
	if err := json.Unmarshal([]byte(fmt.Sprintf("%v", row["manifest_json"])), &record.Manifest); err != nil {
		return RemoteManifestRecord{}, fmt.Errorf("decode remote manifest: %w", err)
	}
	record.Manifest.Version = record.Version

	return record, nil
}

func (c *D1Client) SaveRemoteManifest(ctx context.Context, record RemoteManifestRecord) error {
	manifestJSON, err := json.Marshal(record.Manifest)
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	statement := `INSERT INTO sync_manifests (game_id, version, manifest_json, total_bytes, updated_at, updated_by_device)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(game_id) DO UPDATE SET
			version = excluded.version,
			manifest_json = excluded.manifest_json,
			total_bytes = excluded.total_bytes,
			updated_at = excluded.updated_at,
			updated_by_device = excluded.updated_by_device;`

	if _, err := c.Query(ctx, statement,
		record.GameID,
		record.Version,
		string(manifestJSON),
		record.Manifest.TotalBytes,
		record.UpdatedAt.Format(time.RFC3339Nano),
		record.UpdatedByDevice,
	); err != nil {
		return err
	}

	_, err = c.Query(ctx,
		`INSERT INTO sync_revisions (game_id, version, manifest_json, total_bytes, updated_at, updated_by_device)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(game_id, version) DO NOTHING;`,
		record.GameID,
		record.Version,
		string(manifestJSON),
		record.Manifest.TotalBytes,
		record.UpdatedAt.Format(time.RFC3339Nano),
		record.UpdatedByDevice,
	)
	return err
}

func (c *D1Client) Query(ctx context.Context, sql string, args ...any) ([]map[string]any, error) {
	payload, err := json.Marshal(d1QueryRequest{
		SQL:    sql,
		Params: args,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal d1 request: %w", err)
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("GAMESYNC_DEBUG_D1")), "1") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("GAMESYNC_DEBUG_D1")), "true") {
		fmt.Printf("D1 Query payload: %s\n", string(payload))
	}

	endpoint := fmt.Sprintf("%s/accounts/%s/d1/database/%s/query", d1BaseURL, c.accountID, c.databaseID)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build d1 request: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+c.apiToken)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call d1 query: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read d1 response: %w", err)
	}
	if response.StatusCode >= 300 {
		return nil, fmt.Errorf("d1 query failed: %s", strings.TrimSpace(string(body)))
	}

	var envelope d1QueryEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode d1 response: %w", err)
	}
	if !envelope.Success {
		messages := make([]string, 0, len(envelope.Errors))
		for _, apiErr := range envelope.Errors {
			messages = append(messages, apiErr.Message)
		}
		if len(messages) == 0 {
			messages = append(messages, "unknown d1 error")
		}
		return nil, errors.New(strings.Join(messages, "; "))
	}

	if len(envelope.Result) > 0 {
		return envelope.Result[0].Results, nil
	}
	return nil, nil
}

func (c *D1Client) ClearGameRecords(ctx context.Context, gameID string) error {
	_, err := c.Query(ctx, `DELETE FROM sync_manifests WHERE game_id = ?`, gameID)
	if err != nil {
		return fmt.Errorf("delete from sync_manifests: %w", err)
	}

	_, err = c.Query(ctx, `DELETE FROM sync_revisions WHERE game_id = ?`, gameID)
	if err != nil {
		return fmt.Errorf("delete from sync_revisions: %w", err)
	}

	_, err = c.Query(ctx, `DELETE FROM games_catalog WHERE game_id = ?`, gameID)
	if err != nil {
		return fmt.Errorf("delete from games_catalog: %w", err)
	}

	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func NewR2Client(ctx context.Context, account CloudflareAccount) (*R2Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(account.R2AccessKeyID, account.R2SecretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load r2 config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", account.AccountID))
		options.UsePathStyle = true
	})

	return &R2Client{
		bucket: account.R2Bucket,
		client: client,
	}, nil
}

func (c *R2Client) PutObjectFromFile(ctx context.Context, key string, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file for upload: %w", err)
	}
	defer file.Close()

	_, err = c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   file,
	})
	if err != nil {
		return fmt.Errorf("put r2 object: %w", err)
	}

	return nil
}

func (c *R2Client) DownloadObjectToFile(ctx context.Context, key string, destinationPath string) error {
	response, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("get r2 object: %w", err)
	}
	defer response.Body.Close()

	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return fmt.Errorf("create destination dir: %w", err)
	}

	file, err := os.Create(destinationPath)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, response.Body); err != nil {
		return fmt.Errorf("write destination file: %w", err)
	}

	return nil
}

func (c *R2Client) GetObjectBytes(ctx context.Context, key string) ([]byte, error) {
	response, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get r2 object: %w", err)
	}
	defer response.Body.Close()

	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read r2 object: %w", err)
	}
	return data, nil
}

func VerifyCloudflareAccount(ctx context.Context, account CloudflareAccount) (CloudflareAccount, error) {
	verified := account
	now := time.Now()
	verified.LastVerifiedAt = &now
	verified.TokenExpiresAt = nil
	verified.LastError = ""
	verified.UsageWarning = ""

	r2, err := NewR2Client(ctx, account)
	if err != nil {
		verified.LastError = err.Error()
		return verified, err
	}

	if account.IsPrimary {
		if strings.TrimSpace(account.APIToken) == "" || strings.TrimSpace(account.D1DatabaseID) == "" {
			err := errors.New("primary Cloudflare account D1 config is incomplete")
			verified.LastError = err.Error()
			return verified, err
		}
		tokenMeta, err := NewD1Client(account).VerifyAPIToken(ctx)
		if err != nil {
			verified.LastError = err.Error()
			return verified, err
		}
		verified.TokenExpiresAt = tokenMeta.ExpiresOn
		if err := NewD1Client(account).ValidateAccess(ctx); err != nil {
			verified.LastError = err.Error()
			return verified, err
		}
	}
	if err := r2.ValidateBucketAccess(ctx); err != nil {
		verified.LastError = err.Error()
		return verified, err
	}

	usedBytes, err := r2.FetchAccountUsageBytes(ctx)
	if err != nil {
		verified.UsageWarning = err.Error()
		return verified, nil
	}

	verified.UsedBytes = usedBytes
	verified.LastError = ""
	verified.UsageWarning = ""
	return verified, nil
}

func (c *D1Client) ValidateAccess(ctx context.Context) error {
	if _, err := c.Query(ctx, `SELECT 1 AS ok;`); err != nil {
		return fmt.Errorf(msgD1ValidateFailed, err)
	}
	return nil
}

func (c *D1Client) VerifyAPIToken(ctx context.Context) (tokenVerifyResult, error) {
	endpoint := fmt.Sprintf("%s/user/tokens/verify", d1BaseURL)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return tokenVerifyResult{}, fmt.Errorf("build token verify request: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+c.apiToken)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return tokenVerifyResult{}, fmt.Errorf("verify Cloudflare token failed: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return tokenVerifyResult{}, fmt.Errorf("read token verify response: %w", err)
	}
	if response.StatusCode >= 300 {
		return tokenVerifyResult{}, fmt.Errorf("verify Cloudflare token failed: %s", strings.TrimSpace(string(body)))
	}

	var envelope tokenVerifyEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return tokenVerifyResult{}, fmt.Errorf("decode token verify response: %w", err)
	}
	if !envelope.Success {
		messages := make([]string, 0, len(envelope.Errors))
		for _, apiErr := range envelope.Errors {
			messages = append(messages, apiErr.Message)
		}
		if len(messages) == 0 {
			messages = append(messages, "unknown Cloudflare token verify error")
		}
		return tokenVerifyResult{}, errors.New(strings.Join(messages, "; "))
	}
	return envelope.Result, nil
}

func (c *R2Client) ValidateBucketAccess(ctx context.Context) error {
	if _, err := c.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.bucket)}); err != nil {
		return fmt.Errorf(msgR2BucketValidateFailed, err)
	}
	return nil
}

func (c *R2Client) FetchAccountUsageBytes(ctx context.Context) (int64, error) {
	buckets, err := c.listAccessibleBuckets(ctx)
	if err != nil {
		return 0, err
	}

	var total int64
	for _, bucket := range buckets {
		bucketBytes, err := c.bucketUsageBytes(ctx, bucket)
		if err != nil {
			return 0, err
		}
		total += bucketBytes
	}
	return total, nil
}

func (c *R2Client) listAccessibleBuckets(ctx context.Context) ([]string, error) {
	response, err := c.client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf(msgListR2BucketsFailed, err)
	}

	names := make([]string, 0, len(response.Buckets))
	seen := make(map[string]struct{}, len(response.Buckets))
	for _, bucket := range response.Buckets {
		name := strings.TrimSpace(aws.ToString(bucket.Name))
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	if len(names) == 0 {
		return nil, errors.New(msgNoUsableR2Buckets)
	}
	return names, nil
}

func (c *R2Client) bucketUsageBytes(ctx context.Context, bucket string) (int64, error) {
	paginator := s3.NewListObjectsV2Paginator(c.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	})

	var total int64
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return 0, fmt.Errorf(msgBucketUsageFailed, bucket, err)
		}
		for _, object := range page.Contents {
			total += aws.ToInt64(object.Size)
		}
	}
	return total, nil
}

func (c *R2Client) ClearPrefix(ctx context.Context, prefix string) error {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return errors.New("prefix is empty")
	}
	paginator := s3.NewListObjectsV2Paginator(c.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list objects for prefix %s failed: %w", prefix, err)
		}

		if len(page.Contents) == 0 {
			continue
		}

		var objects []types.ObjectIdentifier
		for _, object := range page.Contents {
			objects = append(objects, types.ObjectIdentifier{
				Key: object.Key,
			})
		}

		_, err = c.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(c.bucket),
			Delete: &types.Delete{
				Objects: objects,
			},
		})
		if err != nil {
			return fmt.Errorf("delete objects for prefix %s failed: %w", prefix, err)
		}
	}
	return nil
}

func (c *R2Client) ClearGameFiles(ctx context.Context, gameID string) error {
	return c.ClearPrefix(ctx, fmt.Sprintf("games/%s/", gameID))
}

func asInt64(value any) int64 {

	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		result, _ := typed.Int64()
		return result
	case string:
		var result int64
		fmt.Sscanf(typed, "%d", &result)
		return result
	default:
		return 0
	}
}
