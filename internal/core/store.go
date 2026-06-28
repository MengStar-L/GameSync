package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

type Store struct {
	mu      sync.Mutex
	baseDir string
	path    string
	state   AppState
}

func NewStore(baseDir string) (*Store, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	store := &Store{
		baseDir: baseDir,
		path:    filepath.Join(baseDir, "state.json"),
	}

	if err := store.load(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *Store) DataDir() string {
	return s.baseDir
}

func (s *Store) Snapshot() AppState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneState(s.state)
}

func (s *Store) ExportState() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.MarshalIndent(redactAppStateSecrets(s.state), "", "  ")
}

// ImportState restores app state from an exported backup payload.
func (s *Store) ImportState(data []byte) error {
	var imported AppState
	if err := json.Unmarshal(data, &imported); err != nil {
		return fmt.Errorf("瑙ｆ瀽瀵煎叆鐨勭姸鎬佸浠藉け璐? %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	currentDevice := s.state.Device

	s.state.Games = imported.Games
	s.state.Accounts = imported.Accounts
	s.state.Preferences = imported.Preferences
	s.state.Activities = imported.Activities
	clearGameLocalPaths(s.state.Games)
	s.state.CatalogSync.Dirty = true

	s.state.Device = currentDevice

	if s.state.Games == nil {
		s.state.Games = []Game{}
	}
	if s.state.Accounts == nil {
		s.state.Accounts = []CloudflareAccount{}
	}
	if s.state.Activities == nil {
		s.state.Activities = []SyncActivity{}
	}
	s.ensureTombstonesLocked()
	s.normalizeCatalogTimestampsLocked()
	s.normalizeAccountsLocked()
	s.reorderAccountsLocked()
	s.assignAccountNamesLocked()

	return s.saveLocked()
}

func (s *Store) IsFirstLaunch() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.state.Games) == 0 && len(s.state.Accounts) == 0
}

func (s *Store) UpsertAccount(account CloudflareAccount) (CloudflareAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureTombstonesLocked()

	account.ID = strings.TrimSpace(account.ID)
	account.Name = strings.TrimSpace(account.Name)
	account.AccountID = strings.TrimSpace(account.AccountID)
	account.APIToken = strings.TrimSpace(account.APIToken)
	account.D1DatabaseID = strings.TrimSpace(account.D1DatabaseID)
	account.R2Bucket = strings.TrimSpace(account.R2Bucket)
	account.R2AccessKeyID = strings.TrimSpace(account.R2AccessKeyID)
	account.R2SecretAccessKey = strings.TrimSpace(account.R2SecretAccessKey)
	account.VerificationState = strings.TrimSpace(account.VerificationState)

	if account.AccountID == "" || account.R2Bucket == "" || account.R2AccessKeyID == "" || account.R2SecretAccessKey == "" {
		return CloudflareAccount{}, errors.New("Cloudflare account R2 config is incomplete")
	}

	isFirstAccount := len(s.state.Accounts) == 0
	if isFirstAccount {
		account.IsPrimary = true
	}
	if !isFirstAccount {
		account.IsPrimary = false
		for index := range s.state.Accounts {
			if s.state.Accounts[index].ID == account.ID {
				account.IsPrimary = s.state.Accounts[index].IsPrimary
				break
			}
		}
	}
	if account.IsPrimary && (account.APIToken == "" || account.D1DatabaseID == "") {
		return CloudflareAccount{}, errors.New("primary Cloudflare account D1 config is incomplete")
	}
	if account.ID == "" {
		account.ID = NewID()
	}
	if account.VerificationState == "" {
		account.VerificationState = "pending"
	}
	now := time.Now()

	updated := false
	for index := range s.state.Accounts {
		if s.state.Accounts[index].ID == account.ID {
			if accountCatalogChanged(s.state.Accounts[index], account) {
				account.CatalogUpdatedAt = now
			} else {
				account.CatalogUpdatedAt = s.state.Accounts[index].CatalogUpdatedAt
			}
			s.state.Accounts[index] = account
			updated = true
			break
		}
	}
	if !updated {
		account.CatalogUpdatedAt = now
		s.state.Accounts = append(s.state.Accounts, account)
	}
	delete(s.state.Tombstones.Accounts, account.ID)
	s.normalizeAccountsLocked()
	s.reorderAccountsLocked()
	s.assignAccountNamesLocked()

	if err := s.saveLocked(); err != nil {
		return CloudflareAccount{}, err
	}

	return account, nil
}
func (s *Store) DeleteAccount(accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureTombstonesLocked()

	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return errors.New("account id is empty")
	}

	nextAccounts := make([]CloudflareAccount, 0, len(s.state.Accounts))
	found := false
	for _, account := range s.state.Accounts {
		if account.ID == accountID {
			found = true
			continue
		}
		nextAccounts = append(nextAccounts, account)
	}
	if !found {
		return fmt.Errorf("account %s not found", accountID)
	}
	now := time.Now()
	s.state.Tombstones.Accounts[accountID] = now
	s.state.Accounts = nextAccounts
	s.normalizeAccountsLocked()
	s.reorderAccountsLocked()
	s.assignAccountNamesLocked()

	fallback := s.firstEnabledAccountLocked("")
	for index := range s.state.Games {
		changed := false
		if s.state.Games[index].StorageAccountID == accountID {
			s.state.Games[index].StorageAccountID = fallback
			changed = true
		}
		if s.state.Games[index].BackupStorageAccountID == accountID {
			s.state.Games[index].BackupStorageAccountID = fallback
			changed = true
		}
		for filename, mappedAccountID := range s.state.Games[index].BackupLocations {
			if mappedAccountID == accountID {
				s.state.Games[index].BackupLocations[filename] = fallback
				changed = true
			}
		}
		if changed {
			s.state.Games[index].StorageUpdatedAt = now
			s.state.Games[index].CatalogUpdatedAt = now
		}
	}

	return s.saveLocked()
}

func (s *Store) SetRecoveryStatus(update func(*RecoveryStatus)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	update(&s.state.RecoveryStatus)
	return s.saveLocked()
}

func (s *Store) MarkCatalogDirty() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.state.CatalogSync.Dirty = true
	s.state.CatalogSync.LastQueuedAt = &now
	s.state.CatalogSync.LastError = ""
	return s.saveLocked()
}

func (s *Store) MarkCatalogSyncAttempt() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.state.CatalogSync.LastAttemptAt = &now
	return s.saveLocked()
}

func (s *Store) MarkCatalogSynced(revision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.state.CatalogSync.Dirty = false
	s.state.CatalogSync.LastKnownRevision = revision
	s.state.CatalogSync.LastSuccessAt = &now
	s.state.CatalogSync.LastError = ""
	return s.saveLocked()
}

func (s *Store) MarkCatalogRevision(revision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if revision > s.state.CatalogSync.LastKnownRevision {
		s.state.CatalogSync.LastKnownRevision = revision
	}
	return s.saveLocked()
}

func (s *Store) MarkCatalogSyncFailed(message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.state.CatalogSync.Dirty = true
	s.state.CatalogSync.LastAttemptAt = &now
	s.state.CatalogSync.LastError = strings.TrimSpace(message)
	return s.saveLocked()
}

func (s *Store) HasPendingCatalogSync() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.CatalogSync.Dirty
}

func (s *Store) LastKnownCatalogRevision() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.CatalogSync.LastKnownRevision
}

func (s *Store) PrimaryAccount() (CloudflareAccount, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, account := range s.state.Accounts {
		if account.IsPrimary {
			return account, true
		}
	}
	return CloudflareAccount{}, false
}

func (s *Store) MergeRemoteCatalog(catalog RemoteCatalog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureTombstonesLocked()
	mergeTombstones(s.state.Tombstones.Games, catalog.Tombstones.Games)
	mergeTombstones(s.state.Tombstones.Accounts, catalog.Tombstones.Accounts)

	accountByID := make(map[string]int, len(s.state.Accounts))
	for index := range s.state.Accounts {
		accountByID[s.state.Accounts[index].ID] = index
	}
	for _, remoteAccount := range catalog.Accounts {
		remoteAccount.ID = strings.TrimSpace(remoteAccount.ID)
		if remoteAccount.ID == "" {
			continue
		}
		remoteUpdatedAt := catalogAccountTime(remoteAccount)
		if deletedAt, deleted := s.state.Tombstones.Accounts[remoteAccount.ID]; deleted && !remoteUpdatedAt.After(deletedAt) {
			continue
		}
		index, ok := accountByID[remoteAccount.ID]
		if !ok && remoteAccount.IsPrimary {
			if matchedIndex := s.matchPrimaryAccountLocked(remoteAccount); matchedIndex != -1 {
				oldID := s.state.Accounts[matchedIndex].ID
				index = matchedIndex
				ok = true
				accountByID[remoteAccount.ID] = matchedIndex
				s.repointStorageAccountLocked(oldID, remoteAccount.ID)
			}
		}
		if ok {
			current := s.state.Accounts[index]
			if current.CatalogUpdatedAt.After(remoteUpdatedAt) {
				continue
			}
			current.ID = remoteAccount.ID
			current.Name = firstNonEmpty(remoteAccount.Name, current.Name)
			current.AccountID = firstNonEmpty(remoteAccount.AccountID, current.AccountID)
			current.APIToken = firstNonEmpty(current.APIToken, remoteAccount.APIToken)
			current.D1DatabaseID = firstNonEmpty(remoteAccount.D1DatabaseID, current.D1DatabaseID)
			current.R2Bucket = firstNonEmpty(remoteAccount.R2Bucket, current.R2Bucket)
			current.R2AccessKeyID = firstNonEmpty(current.R2AccessKeyID, remoteAccount.R2AccessKeyID)
			current.R2SecretAccessKey = firstNonEmpty(current.R2SecretAccessKey, remoteAccount.R2SecretAccessKey)
			current.IsPrimary = remoteAccount.IsPrimary
			current.Enabled = remoteAccount.Enabled
			current.CatalogUpdatedAt = remoteUpdatedAt
			if current.VerificationState == "" {
				current.VerificationState = "pending"
			}
			s.state.Accounts[index] = current
			delete(s.state.Tombstones.Accounts, remoteAccount.ID)
			continue
		}
		if remoteAccount.R2AccessKeyID == "" || remoteAccount.R2SecretAccessKey == "" {
			remoteAccount.APIToken = ""
			remoteAccount.R2AccessKeyID = ""
			remoteAccount.R2SecretAccessKey = ""
			remoteAccount.VerificationState = "invalid"
			remoteAccount.LastError = "涓昏处鍙?catalog 涓己灏戝壇璐﹀彿鍑嵁锛岃閲嶆柊缂栬緫淇濆瓨"
		} else if remoteAccount.VerificationState == "" || remoteAccount.VerificationState == "invalid" {
			remoteAccount.VerificationState = "pending"
			remoteAccount.LastError = ""
		}
		remoteAccount.CatalogUpdatedAt = remoteUpdatedAt
		s.state.Accounts = append(s.state.Accounts, remoteAccount)
		delete(s.state.Tombstones.Accounts, remoteAccount.ID)
	}
	s.state.Accounts = filterDeletedAccounts(s.state.Accounts, s.state.Tombstones.Accounts)

	localGameByID := make(map[string]Game, len(s.state.Games))
	for _, game := range s.state.Games {
		localGameByID[game.ID] = game
	}
	localOrder := make(map[string]int, len(s.state.Games))
	for index, game := range s.state.Games {
		localOrder[game.ID] = index
	}
	mergedGames := make([]Game, 0, len(s.state.Games)+len(catalog.Games))
	seenGames := make(map[string]bool, len(catalog.Games))
	for _, remoteGame := range catalog.Games {
		remoteGame.ID = strings.TrimSpace(remoteGame.ID)
		if remoteGame.ID == "" {
			continue
		}
		remoteUpdatedAt := catalogGameTime(remoteGame)
		if deletedAt, deleted := s.state.Tombstones.Games[remoteGame.ID]; deleted && !remoteUpdatedAt.After(deletedAt) {
			continue
		}
		remoteGame.InstallPath = ""
		remoteGame.SavePath = ""
		if current, ok := localGameByID[remoteGame.ID]; ok {
			remoteGame = mergeGameFields(current, remoteGame)
		} else {
			normalizeGameCatalogTimestamps(&remoteGame, remoteUpdatedAt)
		}
		mergedGames = append(mergedGames, remoteGame)
		seenGames[remoteGame.ID] = true
		delete(s.state.Tombstones.Games, remoteGame.ID)
	}
	for _, localGame := range s.state.Games {
		if !seenGames[localGame.ID] && !tombstoneDeletes(localGame.CatalogUpdatedAt, s.state.Tombstones.Games[localGame.ID]) {
			mergedGames = append(mergedGames, localGame)
		}
	}
	if catalog.Preferences == nil || s.state.Preferences.GameOrderUpdatedAt.After(catalog.Preferences.GameOrderUpdatedAt) {
		slices.SortStableFunc(mergedGames, func(left, right Game) int {
			leftIndex, leftOk := localOrder[left.ID]
			rightIndex, rightOk := localOrder[right.ID]
			if leftOk && rightOk {
				return leftIndex - rightIndex
			}
			if leftOk {
				return -1
			}
			if rightOk {
				return 1
			}
			return strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name))
		})
	} else {
		s.state.Preferences.GameOrderUpdatedAt = catalog.Preferences.GameOrderUpdatedAt
	}
	s.state.Games = mergedGames

	if catalog.Preferences != nil && !s.state.Preferences.TagOrderUpdatedAt.After(catalog.Preferences.TagOrderUpdatedAt) {
		s.state.Preferences.TagOrder = normalizeStringList(catalog.Preferences.TagOrder)
		s.state.Preferences.TagOrderUpdatedAt = catalog.Preferences.TagOrderUpdatedAt
	}
	if catalog.Preferences != nil && !s.state.Preferences.FavoriteGamesUpdatedAt.After(catalog.Preferences.FavoriteGamesUpdatedAt) {
		s.state.Preferences.FavoriteGames = normalizeStringList(catalog.Preferences.FavoriteGames)
		s.state.Preferences.FavoriteGamesUpdatedAt = catalog.Preferences.FavoriteGamesUpdatedAt
	}

	s.normalizeAccountsLocked()
	s.reorderAccountsLocked()
	s.assignAccountNamesLocked()
	now := time.Now()
	s.state.RecoveryStatus.RemoteCatalogAvailable = true
	s.state.RecoveryStatus.LastCatalogSyncAt = &now
	s.state.RecoveryStatus.LastRecoveryError = ""
	return s.saveLocked()
}

func (s *Store) UpsertGame(game Game) (Game, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureTombstonesLocked()

	game.ID = strings.TrimSpace(game.ID)
	game.Name = strings.TrimSpace(game.Name)
	game.InstallPath = strings.TrimSpace(game.InstallPath)
	game.SavePath = strings.TrimSpace(game.SavePath)
	game.CoverPath = strings.TrimSpace(game.CoverPath)
	game.CoverSourceType = strings.TrimSpace(game.CoverSourceType)
	game.CoverSource = strings.TrimSpace(game.CoverSource)
	game.CoverLocalPath = strings.TrimSpace(game.CoverLocalPath)
	game.CoverCloudAccountID = strings.TrimSpace(game.CoverCloudAccountID)
	game.CoverCloudKey = strings.TrimSpace(game.CoverCloudKey)
	game.CoverMimeType = strings.TrimSpace(game.CoverMimeType)
	game.Description = strings.TrimSpace(game.Description)
	game.Released = strings.TrimSpace(game.Released)
	game.Website = strings.TrimSpace(game.Website)
	game.RawgSlug = strings.TrimSpace(game.RawgSlug)
	game.RawgURL = strings.TrimSpace(game.RawgURL)
	game.StorageAccountID = strings.TrimSpace(game.StorageAccountID)
	game.AutoBackupAccountID = strings.TrimSpace(game.AutoBackupAccountID)
	game.BackupStorageAccountID = strings.TrimSpace(game.BackupStorageAccountID)
	game.BackupCount = 0
	normalizeBackupFields(&game)
	game.LaunchRestoreOverride = normalizeLaunchRestoreOverride(game.LaunchRestoreOverride)

	if game.Name == "" {
		return Game{}, errors.New("娓告垙鍚嶇О涓嶈兘涓虹┖")
	}
	if game.SavePath == "" {
		return Game{}, errors.New("瀛樻。鐩綍涓嶈兘涓虹┖")
	}
	if game.ID == "" {
		game.ID = NewID()
	}
	if game.CoverSourceType == "" {
		switch {
		case strings.HasPrefix(strings.ToLower(game.CoverPath), "http://"), strings.HasPrefix(strings.ToLower(game.CoverPath), "https://"):
			game.CoverSourceType = "remote_url"
			if game.CoverSource == "" {
				game.CoverSource = game.CoverPath
			}
		case game.CoverPath != "":
			game.CoverSourceType = "local_file"
			if game.CoverSource == "" {
				game.CoverSource = game.CoverPath
			}
		}
	}
	if len(game.Tags) == 0 {
		game.Tags = []string{}
	}
	if game.Sync.IncludePatterns == nil && game.Sync.ExcludePatterns == nil && !game.Sync.Enabled && game.Sync.ConflictStrategy == "" {
		game.Sync = DefaultSyncConfig()
	}
	if game.Sync.ConflictStrategy == "" {
		game.Sync.ConflictStrategy = "manual"
	}
	if game.Sync.IncludePatterns == nil {
		game.Sync.IncludePatterns = []string{"*"}
	}
	if game.Sync.ExcludePatterns == nil {
		game.Sync.ExcludePatterns = []string{}
	}
	s.normalizeGameStorageRoutingLocked(&game)
	now := time.Now()
	delete(s.state.Tombstones.Games, game.ID)

	updated := false
	for index := range s.state.Games {
		if s.state.Games[index].ID == game.ID {
			current := s.state.Games[index]
			if game.Anchor.LastManifest.Hash == "" && s.state.Games[index].Anchor.LastManifest.Hash != "" {
				game.Anchor = s.state.Games[index].Anchor
			}
			if game.LastSync == nil && s.state.Games[index].LastSync != nil {
				lastSyncCopy := *s.state.Games[index].LastSync
				game.LastSync = &lastSyncCopy
			}
			applyGameChangeTimestamps(&game, current, now)
			s.state.Games[index] = game
			updated = true
			break
		}
	}
	if !updated {
		game.MetadataUpdatedAt = now
		game.TagsUpdatedAt = now
		game.SyncConfigUpdatedAt = now
		game.StorageUpdatedAt = now
		game.RuntimeUpdatedAt = now
		game.CatalogUpdatedAt = now
		s.state.Games = append(s.state.Games, game)
	}

	if err := s.saveLocked(); err != nil {
		return Game{}, err
	}

	return game, nil
}

func (s *Store) DeleteGame(gameID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureTombstonesLocked()

	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return errors.New("game id is empty")
	}

	nextGames := make([]Game, 0, len(s.state.Games))
	found := false
	for _, game := range s.state.Games {
		if game.ID == gameID {
			found = true
			continue
		}
		nextGames = append(nextGames, game)
	}
	if !found {
		return fmt.Errorf("game %s not found", gameID)
	}

	s.state.Tombstones.Games[gameID] = time.Now()
	s.state.Games = nextGames
	filteredFavorites := make([]string, 0, len(s.state.Preferences.FavoriteGames))
	for _, favoriteID := range s.state.Preferences.FavoriteGames {
		if favoriteID != gameID {
			filteredFavorites = append(filteredFavorites, favoriteID)
		}
	}
	if !equalStringSlices(filteredFavorites, s.state.Preferences.FavoriteGames) {
		s.state.Preferences.FavoriteGames = filteredFavorites
		s.state.Preferences.FavoriteGamesUpdatedAt = time.Now()
	}
	return s.saveLocked()
}

func (s *Store) ReorderGames(gameIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	gameMap := make(map[string]Game)
	for _, g := range s.state.Games {
		gameMap[g.ID] = g
	}

	var newGames []Game
	for _, id := range gameIDs {
		if g, ok := gameMap[id]; ok {
			newGames = append(newGames, g)
			delete(gameMap, id)
		}
	}

	for _, g := range s.state.Games {
		if _, ok := gameMap[g.ID]; ok {
			newGames = append(newGames, g)
		}
	}

	s.state.Games = newGames
	s.state.Preferences.GameOrderUpdatedAt = time.Now()
	return s.saveLocked()
}

func (s *Store) SavePreferences(preferences Preferences) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	preferences.StartupSyncMode = strings.TrimSpace(preferences.StartupSyncMode)
	preferences.ConflictPolicy = strings.TrimSpace(preferences.ConflictPolicy)
	preferences.DefaultInstallDir = strings.TrimSpace(preferences.DefaultInstallDir)
	preferences.DefaultSaveDir = strings.TrimSpace(preferences.DefaultSaveDir)
	preferences.DefaultSteamInstallDir = strings.TrimSpace(preferences.DefaultSteamInstallDir)
	preferences.DefaultSteamSaveDir = strings.TrimSpace(preferences.DefaultSteamSaveDir)
	preferences.DefaultThirdInstallDir = strings.TrimSpace(preferences.DefaultThirdInstallDir)
	preferences.DefaultThirdSaveDir = strings.TrimSpace(preferences.DefaultThirdSaveDir)
	preferences.RawgAPIKey = strings.TrimSpace(preferences.RawgAPIKey)
	preferences.SteamGridDBAPIKey = strings.TrimSpace(preferences.SteamGridDBAPIKey)
	preferences.FavoriteGames = normalizeStringList(preferences.FavoriteGames)
	preferences.TagOrder = normalizeStringList(preferences.TagOrder)

	if preferences.StartupSyncMode == "" {
		preferences.StartupSyncMode = "smart"
	}
	if preferences.ConflictPolicy == "" {
		preferences.ConflictPolicy = "manual"
	}
	if !equalStringSlices(s.state.Preferences.FavoriteGames, preferences.FavoriteGames) {
		preferences.FavoriteGamesUpdatedAt = time.Now()
	} else if preferences.FavoriteGamesUpdatedAt.IsZero() {
		preferences.FavoriteGamesUpdatedAt = s.state.Preferences.FavoriteGamesUpdatedAt
	}
	if s.state.Preferences.RawgAPIKey != preferences.RawgAPIKey {
		preferences.RawgAPIKeyUpdatedAt = time.Now()
	} else if preferences.RawgAPIKeyUpdatedAt.IsZero() {
		preferences.RawgAPIKeyUpdatedAt = s.state.Preferences.RawgAPIKeyUpdatedAt
	}
	if s.state.Preferences.SteamGridDBAPIKey != preferences.SteamGridDBAPIKey {
		preferences.SteamGridDBAPIKeyUpdatedAt = time.Now()
	} else if preferences.SteamGridDBAPIKeyUpdatedAt.IsZero() {
		preferences.SteamGridDBAPIKeyUpdatedAt = s.state.Preferences.SteamGridDBAPIKeyUpdatedAt
	}
	if !equalStringSlices(s.state.Preferences.TagOrder, preferences.TagOrder) {
		preferences.TagOrderUpdatedAt = time.Now()
	} else if preferences.TagOrderUpdatedAt.IsZero() {
		preferences.TagOrderUpdatedAt = s.state.Preferences.TagOrderUpdatedAt
	}
	if preferences.GameOrderUpdatedAt.IsZero() {
		preferences.GameOrderUpdatedAt = s.state.Preferences.GameOrderUpdatedAt
	}

	s.state.Preferences = preferences
	return s.saveLocked()
}

func (s *Store) UpdateGameSync(gameID string, anchor SyncAnchor, summary SyncSummary) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index := range s.state.Games {
		if s.state.Games[index].ID != gameID {
			continue
		}

		s.state.Games[index].Anchor = anchor
		summaryCopy := summary
		s.state.Games[index].LastSync = &summaryCopy
		now := time.Now()
		s.state.Games[index].RuntimeUpdatedAt = now
		s.state.Games[index].CatalogUpdatedAt = maxTimeValue(
			s.state.Games[index].MetadataUpdatedAt,
			s.state.Games[index].TagsUpdatedAt,
			s.state.Games[index].SyncConfigUpdatedAt,
			s.state.Games[index].StorageUpdatedAt,
			s.state.Games[index].RuntimeUpdatedAt,
		)
		return s.saveLocked()
	}

	return fmt.Errorf("game %s not found", gameID)
}

func (s *Store) UpdateGameCoverCache(gameID string, localPath string, mimeType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	gameID = strings.TrimSpace(gameID)
	localPath = strings.TrimSpace(localPath)
	mimeType = strings.TrimSpace(mimeType)
	if gameID == "" {
		return errors.New("game id is empty")
	}

	for index := range s.state.Games {
		if s.state.Games[index].ID != gameID {
			continue
		}
		if s.state.Games[index].CoverLocalPath == localPath &&
			(mimeType == "" || s.state.Games[index].CoverMimeType == mimeType) {
			return nil
		}
		s.state.Games[index].CoverLocalPath = localPath
		if mimeType != "" {
			s.state.Games[index].CoverMimeType = mimeType
		}
		return s.saveLocked()
	}

	return fmt.Errorf("game %s not found", gameID)
}

func (s *Store) RecordActivity(activity SyncActivity) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Activities = append([]SyncActivity{activity}, s.state.Activities...)
	if len(s.state.Activities) > 60 {
		s.state.Activities = s.state.Activities[:60]
	}

	return s.saveLocked()
}

func (s *Store) load() error {
	if _, err := os.Stat(s.path); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat state file: %w", err)
		}

		s.state = defaultState()
		return s.saveLocked()
	}

	content, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read state file: %w", err)
	}

	if err := json.Unmarshal(content, &s.state); err != nil {
		return fmt.Errorf("unmarshal state file: %w", err)
	}
	if err := unprotectAppStateSecrets(&s.state); err != nil {
		return fmt.Errorf("unprotect state secrets: %w", err)
	}

	if s.state.Device.ID == "" {
		hostName, err := os.Hostname()
		if err != nil || strings.TrimSpace(hostName) == "" {
			hostName = "unknown-device"
		}
		s.state.Device = DeviceInfo{
			ID:            NewID(),
			Name:          hostName,
			Platform:      goruntime.GOOS + "/" + goruntime.GOARCH,
			LastStartedAt: time.Now(),
		}
	}
	if s.state.Preferences.StartupSyncMode == "" {
		s.state.Preferences = DefaultPreferences()
	}
	if s.state.Games == nil {
		s.state.Games = []Game{}
	}
	if s.state.Accounts == nil {
		s.state.Accounts = []CloudflareAccount{}
	}
	if s.state.Activities == nil {
		s.state.Activities = []SyncActivity{}
	}
	s.ensureTombstonesLocked()
	s.normalizeAccountsLocked()
	s.reorderAccountsLocked()
	s.assignAccountNamesLocked()
	for index := range s.state.Games {
		normalizeBackupFields(&s.state.Games[index])
		s.normalizeGameStorageRoutingLocked(&s.state.Games[index])
	}

	s.state.Device.LastStartedAt = time.Now()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	state, err := protectAppStateSecrets(s.state)
	if err != nil {
		return fmt.Errorf("protect state secrets: %w", err)
	}
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(s.path), ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state file: %w", err)
	}
	tempPath := tempFile.Name()
	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("write temporary state file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close temporary state file: %w", err)
	}
	if err := os.Chmod(tempPath, 0o644); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("chmod temporary state file: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace state file: %w", err)
	}

	return nil
}

func (s *Store) canonicalAccountsLocked() []CloudflareAccount {
	if len(s.state.Accounts) == 0 {
		return []CloudflareAccount{}
	}
	accounts := make([]CloudflareAccount, len(s.state.Accounts))
	copy(accounts, s.state.Accounts)
	return accounts
}

func (s *Store) hasAccountLocked(accountID string) bool {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return false
	}
	for _, account := range s.state.Accounts {
		if account.ID == accountID {
			return true
		}
	}
	return false
}

func hasActiveAutoBackupRecord(game Game) bool {
	for _, record := range game.BackupRegistry {
		if !strings.EqualFold(strings.TrimSpace(record.Type), "auto") {
			continue
		}
		if record.DeletedAt != nil || strings.TrimSpace(record.Status) == BackupStatusPendingDelete {
			continue
		}
		return true
	}
	return false
}

func (s *Store) normalizeGameStorageRoutingLocked(game *Game) {
	if game == nil {
		return
	}
	if !s.hasAccountLocked(game.StorageAccountID) {
		game.StorageAccountID = s.firstEnabledAccountLocked("")
	}
	if strings.TrimSpace(game.AutoBackupAccountID) == "" && !hasActiveAutoBackupRecord(*game) {
		if !s.hasAccountLocked(game.BackupStorageAccountID) {
			game.BackupStorageAccountID = game.StorageAccountID
		}
	} else if strings.TrimSpace(game.BackupStorageAccountID) == "" && s.hasAccountLocked(game.AutoBackupAccountID) {
		game.BackupStorageAccountID = game.AutoBackupAccountID
	}
}

func (s *Store) firstEnabledAccountLocked(excludeAccountID string) string {
	for _, account := range s.canonicalAccountsLocked() {
		if account.Enabled && account.ID != excludeAccountID {
			return account.ID
		}
	}

	accounts := s.canonicalAccountsLocked()
	if len(accounts) > 0 && accounts[0].ID != excludeAccountID {
		return accounts[0].ID
	}

	return ""
}

func cloneState(state AppState) AppState {
	content, err := json.Marshal(state)
	if err != nil {
		return state
	}

	var cloned AppState
	if err := json.Unmarshal(content, &cloned); err != nil {
		return state
	}

	return cloned
}

func defaultState() AppState {
	hostName, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostName) == "" {
		hostName = "unknown-device"
	}

	return AppState{
		Device: DeviceInfo{
			ID:            NewID(),
			Name:          hostName,
			Platform:      goruntime.GOOS + "/" + goruntime.GOARCH,
			LastStartedAt: time.Now(),
		},
		Accounts:       []CloudflareAccount{},
		Games:          []Game{},
		Preferences:    DefaultPreferences(),
		Activities:     []SyncActivity{},
		RecoveryStatus: RecoveryStatus{},
	}
}

func (s *Store) normalizeAccountsLocked() {
	if len(s.state.Accounts) == 0 {
		return
	}

	primaryIndex := -1
	for index := range s.state.Accounts {
		if s.state.Accounts[index].VerificationState == "" {
			s.state.Accounts[index].VerificationState = "pending"
		}
		if s.state.Accounts[index].IsPrimary {
			if primaryIndex == -1 {
				primaryIndex = index
			} else {
				s.state.Accounts[index].IsPrimary = false
			}
		}
	}

	if primaryIndex != -1 {
		return
	}
	for index := range s.state.Accounts {
		if s.state.Accounts[index].Enabled {
			primaryIndex = index
			break
		}
	}
	if primaryIndex == -1 {
		primaryIndex = 0
	}
	s.state.Accounts[primaryIndex].IsPrimary = true
}

func (s *Store) reorderAccountsLocked() {
	if len(s.state.Accounts) <= 1 {
		return
	}

	reordered := make([]CloudflareAccount, 0, len(s.state.Accounts))
	for _, account := range s.state.Accounts {
		if account.IsPrimary {
			reordered = append(reordered, account)
			break
		}
	}
	for _, account := range s.state.Accounts {
		if account.IsPrimary {
			continue
		}
		reordered = append(reordered, account)
	}
	s.state.Accounts = reordered
}

func (s *Store) ensureTombstonesLocked() {
	if s.state.Tombstones.Games == nil {
		s.state.Tombstones.Games = map[string]time.Time{}
	}
	if s.state.Tombstones.Accounts == nil {
		s.state.Tombstones.Accounts = map[string]time.Time{}
	}
}

func (s *Store) normalizeCatalogTimestampsLocked() {
	now := time.Now()
	for index := range s.state.Games {
		game := &s.state.Games[index]
		normalizeBackupFields(game)
		s.normalizeGameStorageRoutingLocked(game)
		base := game.CatalogUpdatedAt
		if base.IsZero() {
			base = now
		}
		normalizeGameCatalogTimestamps(game, base)
	}
	for index := range s.state.Accounts {
		if s.state.Accounts[index].CatalogUpdatedAt.IsZero() {
			s.state.Accounts[index].CatalogUpdatedAt = now
		}
	}
}

func (s *Store) assignAccountNamesLocked() {
	if len(s.state.Accounts) == 0 {
		return
	}
	nextSecondary := 1
	for index := range s.state.Accounts {
		if s.state.Accounts[index].IsPrimary {
			s.state.Accounts[index].Name = "\u4e3b\u8d26\u53f7"
			continue
		}
		s.state.Accounts[index].Name = fmt.Sprintf("\u526f\u8d26\u53f7 %d", nextSecondary)
		nextSecondary++
	}
}

func (s *Store) matchPrimaryAccountLocked(remote CloudflareAccount) int {
	for index, current := range s.state.Accounts {
		if current.IsPrimary && sameAccountIdentity(current, remote) {
			return index
		}
	}
	return -1
}

func (s *Store) repointStorageAccountLocked(oldID, newID string) {
	oldID = strings.TrimSpace(oldID)
	newID = strings.TrimSpace(newID)
	if oldID == "" || newID == "" || oldID == newID {
		return
	}
	for index := range s.state.Games {
		if s.state.Games[index].StorageAccountID == oldID {
			s.state.Games[index].StorageAccountID = newID
		}
		if s.state.Games[index].BackupStorageAccountID == oldID {
			s.state.Games[index].BackupStorageAccountID = newID
		}
		for filename, mappedAccountID := range s.state.Games[index].BackupLocations {
			if mappedAccountID == oldID {
				s.state.Games[index].BackupLocations[filename] = newID
			}
		}
	}
	for index := range s.state.Activities {
		if s.state.Activities[index].AccountID == oldID {
			s.state.Activities[index].AccountID = newID
		}
	}
}

func sameAccountIdentity(left, right CloudflareAccount) bool {
	return strings.TrimSpace(left.AccountID) != "" &&
		strings.TrimSpace(left.AccountID) == strings.TrimSpace(right.AccountID) &&
		strings.TrimSpace(left.D1DatabaseID) != "" &&
		strings.TrimSpace(left.D1DatabaseID) == strings.TrimSpace(right.D1DatabaseID)
}

func clearGameLocalPaths(games []Game) {
	for index := range games {
		games[index].InstallPath = ""
		games[index].SavePath = ""
		games[index].CoverLocalPath = ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeStringList(values []string) []string {
	seen := make(map[string]bool, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		normalized = append(normalized, value)
	}
	if normalized == nil {
		return []string{}
	}
	return normalized
}

func normalizeGameCatalogTimestamps(game *Game, fallback time.Time) {
	if fallback.IsZero() {
		fallback = time.Now()
	}
	if game.MetadataUpdatedAt.IsZero() {
		game.MetadataUpdatedAt = fallback
	}
	if game.TagsUpdatedAt.IsZero() {
		game.TagsUpdatedAt = fallback
	}
	if game.SyncConfigUpdatedAt.IsZero() {
		game.SyncConfigUpdatedAt = fallback
	}
	if game.StorageUpdatedAt.IsZero() {
		game.StorageUpdatedAt = fallback
	}
	if game.RuntimeUpdatedAt.IsZero() {
		game.RuntimeUpdatedAt = fallback
	}
	game.CatalogUpdatedAt = maxTimeValue(
		game.MetadataUpdatedAt,
		game.TagsUpdatedAt,
		game.SyncConfigUpdatedAt,
		game.StorageUpdatedAt,
		game.RuntimeUpdatedAt,
	)
}

func applyGameChangeTimestamps(next *Game, current Game, now time.Time) {
	normalizeGameCatalogTimestamps(&current, current.CatalogUpdatedAt)
	next.MetadataUpdatedAt = current.MetadataUpdatedAt
	next.TagsUpdatedAt = current.TagsUpdatedAt
	next.SyncConfigUpdatedAt = current.SyncConfigUpdatedAt
	next.StorageUpdatedAt = current.StorageUpdatedAt
	next.RuntimeUpdatedAt = current.RuntimeUpdatedAt

	if gameMetadataChanged(current, *next) {
		next.MetadataUpdatedAt = now
	}
	if !equalStringSlices(normalizeStringList(current.Tags), normalizeStringList(next.Tags)) {
		next.TagsUpdatedAt = now
	}
	if !syncConfigEqual(current.Sync, next.Sync) {
		next.SyncConfigUpdatedAt = now
	}
	if strings.TrimSpace(current.StorageAccountID) != strings.TrimSpace(next.StorageAccountID) ||
		strings.TrimSpace(current.AutoBackupAccountID) != strings.TrimSpace(next.AutoBackupAccountID) ||
		strings.TrimSpace(current.BackupStorageAccountID) != strings.TrimSpace(next.BackupStorageAccountID) ||
		!equalStringMap(current.BackupLocations, next.BackupLocations) ||
		!equalBackupRegistry(current.BackupRegistry, next.BackupRegistry) ||
		!equalLaunchRestoreOverride(current.LaunchRestoreOverride, next.LaunchRestoreOverride) {
		next.StorageUpdatedAt = now
	}
	if runtimeStateChanged(current, *next) {
		next.RuntimeUpdatedAt = now
	}
	normalizeGameCatalogTimestamps(next, now)
}

func mergeGameFields(local Game, remote Game) Game {
	localPath := local.InstallPath
	localSavePath := local.SavePath
	normalizeBackupFields(&local)
	normalizeBackupFields(&remote)
	normalizeGameCatalogTimestamps(&local, local.CatalogUpdatedAt)
	normalizeGameCatalogTimestamps(&remote, remote.CatalogUpdatedAt)

	merged := local
	if remote.MetadataUpdatedAt.After(local.MetadataUpdatedAt) {
		copyGameMetadata(&merged, remote)
		merged.MetadataUpdatedAt = remote.MetadataUpdatedAt
	}
	if remote.TagsUpdatedAt.After(local.TagsUpdatedAt) {
		merged.Tags = normalizeStringList(remote.Tags)
		merged.TagsUpdatedAt = remote.TagsUpdatedAt
	}
	if remote.SyncConfigUpdatedAt.After(local.SyncConfigUpdatedAt) {
		merged.Sync = remote.Sync
		merged.SyncConfigUpdatedAt = remote.SyncConfigUpdatedAt
	}
	if remote.StorageUpdatedAt.After(local.StorageUpdatedAt) {
		merged.StorageAccountID = remote.StorageAccountID
		merged.AutoBackupAccountID = remote.AutoBackupAccountID
		merged.BackupStorageAccountID = remote.BackupStorageAccountID
		merged.BackupLocations = cloneStringMap(remote.BackupLocations)
		merged.BackupRegistry = cloneBackupRegistry(remote.BackupRegistry)
		merged.LaunchRestoreOverride = cloneLaunchRestoreOverride(remote.LaunchRestoreOverride)
		merged.StorageUpdatedAt = remote.StorageUpdatedAt
	}
	if remote.RuntimeUpdatedAt.After(local.RuntimeUpdatedAt) {
		merged.PlayTime = remote.PlayTime
		merged.LastPlayed = cloneTimePointer(remote.LastPlayed)
		merged.RuntimeUpdatedAt = remote.RuntimeUpdatedAt
	}
	merged.InstallPath = localPath
	merged.SavePath = localSavePath
	normalizeGameCatalogTimestamps(&merged, maxTimeValue(local.CatalogUpdatedAt, remote.CatalogUpdatedAt))
	return merged
}

func copyGameMetadata(target *Game, source Game) {
	target.Name = source.Name
	target.CoverPath = source.CoverPath
	target.CoverSourceType = source.CoverSourceType
	target.CoverSource = source.CoverSource
	target.CoverCloudAccountID = source.CoverCloudAccountID
	target.CoverCloudKey = source.CoverCloudKey
	target.CoverMimeType = source.CoverMimeType
	target.CoverUpdatedAt = source.CoverUpdatedAt
	target.Description = source.Description
	target.Released = source.Released
	target.Rating = source.Rating
	target.RatingTop = source.RatingTop
	target.Metacritic = source.Metacritic
	target.Genres = cloneStringSlice(source.Genres)
	target.Platforms = cloneStringSlice(source.Platforms)
	target.IsSteam = source.IsSteam
	target.Developers = cloneStringSlice(source.Developers)
	target.Publishers = cloneStringSlice(source.Publishers)
	target.Website = source.Website
	target.RawgID = source.RawgID
	target.RawgSlug = source.RawgSlug
	target.RawgURL = source.RawgURL
	target.RawgTags = cloneStringSlice(source.RawgTags)
}

func gameMetadataChanged(left Game, right Game) bool {
	leftCopy := metadataComparable(left)
	rightCopy := metadataComparable(right)
	leftContent, _ := json.Marshal(leftCopy)
	rightContent, _ := json.Marshal(rightCopy)
	return string(leftContent) != string(rightContent)
}

func metadataComparable(game Game) map[string]any {
	return map[string]any{
		"name":                strings.TrimSpace(game.Name),
		"coverPath":           strings.TrimSpace(game.CoverPath),
		"coverSourceType":     strings.TrimSpace(game.CoverSourceType),
		"coverSource":         strings.TrimSpace(game.CoverSource),
		"coverCloudAccountId": strings.TrimSpace(game.CoverCloudAccountID),
		"coverCloudKey":       strings.TrimSpace(game.CoverCloudKey),
		"coverMimeType":       strings.TrimSpace(game.CoverMimeType),
		"coverUpdatedAt":      game.CoverUpdatedAt.UTC().Format(time.RFC3339Nano),
		"description":         strings.TrimSpace(game.Description),
		"released":            strings.TrimSpace(game.Released),
		"rating":              game.Rating,
		"ratingTop":           game.RatingTop,
		"metacritic":          game.Metacritic,
		"genres":              normalizeStringList(game.Genres),
		"platforms":           normalizeStringList(game.Platforms),
		"isSteam":             game.IsSteam,
		"developers":          normalizeStringList(game.Developers),
		"publishers":          normalizeStringList(game.Publishers),
		"website":             strings.TrimSpace(game.Website),
		"rawgId":              game.RawgID,
		"rawgSlug":            strings.TrimSpace(game.RawgSlug),
		"rawgUrl":             strings.TrimSpace(game.RawgURL),
		"rawgTags":            normalizeStringList(game.RawgTags),
	}
}

func syncConfigEqual(left SyncConfig, right SyncConfig) bool {
	leftContent, _ := json.Marshal(left)
	rightContent, _ := json.Marshal(right)
	return string(leftContent) == string(rightContent)
}

func runtimeStateChanged(left Game, right Game) bool {
	leftCopy := map[string]any{
		"anchor":     left.Anchor,
		"lastSync":   left.LastSync,
		"playTime":   left.PlayTime,
		"lastPlayed": left.LastPlayed,
	}
	rightCopy := map[string]any{
		"anchor":     right.Anchor,
		"lastSync":   right.LastSync,
		"playTime":   right.PlayTime,
		"lastPlayed": right.LastPlayed,
	}
	leftContent, _ := json.Marshal(leftCopy)
	rightContent, _ := json.Marshal(rightCopy)
	return string(leftContent) != string(rightContent)
}

func cloneStringSlice(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string{}, values...)
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func cloneBackupRegistry(values []BackupRecord) []BackupRecord {
	if values == nil {
		return nil
	}
	copy := make([]BackupRecord, len(values))
	for index, value := range values {
		copy[index] = value
		copy[index].DeletedAt = cloneTimePointer(value.DeletedAt)
		copy[index].DeleteRetryAt = cloneTimePointer(value.DeleteRetryAt)
	}
	return copy
}

func cloneLaunchRestoreOverride(value *LaunchRestoreOverride) *LaunchRestoreOverride {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func equalStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func equalBackupRegistry(left, right []BackupRecord) bool {
	leftContent, _ := json.Marshal(normalizedBackupRegistry(left))
	rightContent, _ := json.Marshal(normalizedBackupRegistry(right))
	return string(leftContent) == string(rightContent)
}

func equalLaunchRestoreOverride(left, right *LaunchRestoreOverride) bool {
	leftContent, _ := json.Marshal(normalizeLaunchRestoreOverride(left))
	rightContent, _ := json.Marshal(normalizeLaunchRestoreOverride(right))
	return string(leftContent) == string(rightContent)
}

func cloneSyncSummary(value *SyncSummary) *SyncSummary {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func maxTimeValue(values ...time.Time) time.Time {
	var max time.Time
	for _, value := range values {
		if value.After(max) {
			max = value
		}
	}
	return max
}

func normalizeBackupFields(game *Game) {
	if game.BackupLocations == nil {
		game.BackupLocations = map[string]string{}
	}
	normalizedLocations := make(map[string]string, len(game.BackupLocations))
	for filename, accountID := range game.BackupLocations {
		normalizedFilename := strings.TrimSpace(filename)
		normalizedAccountID := strings.TrimSpace(accountID)
		if normalizedFilename == "" || normalizedAccountID == "" {
			continue
		}
		normalizedLocations[normalizedFilename] = normalizedAccountID
	}

	registry := normalizedBackupRegistry(game.BackupRegistry)
	indexByFilename := make(map[string]int, len(registry))
	for index, record := range registry {
		indexByFilename[record.Filename] = index
	}
	for filename, accountID := range normalizedLocations {
		if _, ok := indexByFilename[filename]; ok {
			continue
		}
		registry = append(registry, BackupRecord{
			Filename:  filename,
			AccountID: accountID,
			Type:      backupTypeFromFilename(filename),
		})
	}

	compatLocations := make(map[string]string)
	for _, record := range registry {
		if record.Filename == "" || record.AccountID == "" || record.DeletedAt != nil {
			continue
		}
		compatLocations[record.Filename] = record.AccountID
	}
	game.BackupRegistry = registry
	game.BackupLocations = compatLocations
	game.LaunchRestoreOverride = normalizeLaunchRestoreOverride(game.LaunchRestoreOverride)
}

func normalizedBackupRegistry(records []BackupRecord) []BackupRecord {
	if len(records) == 0 {
		return []BackupRecord{}
	}
	normalized := make([]BackupRecord, 0, len(records))
	seen := make(map[string]int, len(records))
	for _, record := range records {
		record.Filename = strings.TrimSpace(record.Filename)
		record.AccountID = strings.TrimSpace(record.AccountID)
		record.Type = strings.TrimSpace(record.Type)
		record.Name = strings.TrimSpace(record.Name)
		record.SHA256 = strings.TrimSpace(record.SHA256)
		record.SourceDeviceID = strings.TrimSpace(record.SourceDeviceID)
		record.SourceManifestHash = strings.TrimSpace(record.SourceManifestHash)
		record.Status = strings.TrimSpace(record.Status)
		record.LastError = strings.TrimSpace(record.LastError)
		record.LastDeleteError = strings.TrimSpace(record.LastDeleteError)
		if record.Filename == "" {
			continue
		}
		if record.Type == "" {
			record.Type = backupTypeFromFilename(record.Filename)
		}
		if record.Status == "" {
			if record.PendingDelete {
				record.Status = BackupStatusPendingDelete
			} else {
				record.Status = BackupStatusReady
			}
		}
		record.PendingDelete = record.Status == BackupStatusPendingDelete
		if record.Status != BackupStatusUploadFailed && record.Status != BackupStatusDeleteFailed {
			record.LastError = ""
		}
		if record.Status != BackupStatusDeleteFailed {
			record.LastDeleteError = ""
		}
		if index, ok := seen[record.Filename]; ok {
			normalized[index] = record
			continue
		}
		seen[record.Filename] = len(normalized)
		normalized = append(normalized, record)
	}
	return normalized
}

func normalizeLaunchRestoreOverride(value *LaunchRestoreOverride) *LaunchRestoreOverride {
	if value == nil {
		return nil
	}
	normalized := *value
	normalized.Filename = strings.TrimSpace(normalized.Filename)
	normalized.BackupType = strings.TrimSpace(normalized.BackupType)
	normalized.SourceDeviceID = strings.TrimSpace(normalized.SourceDeviceID)
	if normalized.Filename == "" || !normalized.Active {
		return nil
	}
	if normalized.BackupType == "" {
		normalized.BackupType = backupTypeFromFilename(normalized.Filename)
	}
	return &normalized
}

func backupTypeFromFilename(filename string) string {
	if strings.HasPrefix(strings.TrimSpace(filename), "backup_auto_") {
		return "auto"
	}
	return "manual"
}

func catalogGameTime(game Game) time.Time {
	if !game.CatalogUpdatedAt.IsZero() {
		return game.CatalogUpdatedAt
	}
	return time.Time{}
}

func catalogAccountTime(account CloudflareAccount) time.Time {
	if !account.CatalogUpdatedAt.IsZero() {
		return account.CatalogUpdatedAt
	}
	return time.Time{}
}

func mergeTombstones(target map[string]time.Time, source map[string]time.Time) {
	for id, sourceTime := range source {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if current, ok := target[id]; !ok || sourceTime.After(current) {
			target[id] = sourceTime
		}
	}
}

func tombstoneDeletes(updatedAt time.Time, deletedAt time.Time) bool {
	return !deletedAt.IsZero() && !updatedAt.After(deletedAt)
}

func filterDeletedAccounts(accounts []CloudflareAccount, tombstones map[string]time.Time) []CloudflareAccount {
	next := make([]CloudflareAccount, 0, len(accounts))
	for _, account := range accounts {
		if tombstoneDeletes(account.CatalogUpdatedAt, tombstones[account.ID]) {
			continue
		}
		next = append(next, account)
	}
	return next
}

func equalStringSlices(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func accountCatalogChanged(left CloudflareAccount, right CloudflareAccount) bool {
	left.LastVerifiedAt = nil
	right.LastVerifiedAt = nil
	left.LastError = ""
	right.LastError = ""
	left.UsageWarning = ""
	right.UsageWarning = ""
	left.VerificationState = ""
	right.VerificationState = ""
	left.CredentialsBackedUp = false
	right.CredentialsBackedUp = false
	left.CatalogUpdatedAt = time.Time{}
	right.CatalogUpdatedAt = time.Time{}
	leftContent, _ := json.Marshal(left)
	rightContent, _ := json.Marshal(right)
	return string(leftContent) != string(rightContent)
}
