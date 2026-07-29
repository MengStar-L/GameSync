package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	pathpkg "path"
	"strings"
	"time"
)

const webdavStableIDPrefix = "webdav-"

// NormalizeWebdavAccount makes the remote namespace, rather than the device,
// the identity source for a WebDAV storage connection.
func NormalizeWebdavAccount(account CloudflareAccount) (CloudflareAccount, error) {
	if AccountProvider(account) != ProviderWebdav {
		return account, nil
	}
	canonicalURL, err := normalizeWebdavURL(account.WebdavURL)
	if err != nil {
		return CloudflareAccount{}, err
	}
	root := normalizeWebdavRoot(account.WebdavRoot)
	hash := sha256.Sum256([]byte(canonicalURL + "\n" + root))
	account.Provider = ProviderWebdav
	account.WebdavURL = canonicalURL
	account.WebdavRoot = root
	account.ID = webdavStableIDPrefix + hex.EncodeToString(hash[:])
	return account, nil
}

func normalizeWebdavURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return "", errors.New(msgWebdavURLInvalid)
	}
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	} else {
		parsed.Host = hostname
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.RawPath = ""
	cleanedPath := pathpkg.Clean("/" + strings.Trim(strings.TrimSpace(parsed.Path), "/"))
	if cleanedPath == "/" || cleanedPath == "." {
		parsed.Path = ""
	} else {
		parsed.Path = strings.TrimRight(cleanedPath, "/")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func normalizeWebdavRoot(raw string) string {
	raw = strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	root := strings.Trim(pathpkg.Clean("/"+strings.Trim(raw, "/")), "/")
	if root == "" || root == "." {
		return webdavDefaultRoot
	}
	return root
}

func mergeSameWebdavAccount(current, candidate CloudflareAccount) CloudflareAccount {
	merged := current
	if candidate.CatalogUpdatedAt.After(merged.CatalogUpdatedAt) {
		merged.Name = candidate.Name
		merged.CatalogUpdatedAt = candidate.CatalogUpdatedAt
	}
	credentials := preferredWebdavCredentials(current, candidate)
	merged.WebdavUsername = credentials.WebdavUsername
	merged.WebdavPassword = credentials.WebdavPassword
	merged.VerificationState = credentials.VerificationState
	merged.LastVerifiedAt = credentials.LastVerifiedAt
	merged.LastError = credentials.LastError
	merged.UsageWarning = credentials.UsageWarning
	merged.UsedBytes = credentials.UsedBytes
	merged.CredentialsBackedUp = credentials.CredentialsBackedUp
	merged.IsPrimary = merged.IsPrimary || candidate.IsPrimary
	merged.Enabled = merged.Enabled || candidate.Enabled
	if candidate.CatalogUpdatedAt.After(merged.CatalogUpdatedAt) {
		merged.CatalogUpdatedAt = candidate.CatalogUpdatedAt
	}
	return merged
}

func preferredWebdavCredentials(current, candidate CloudflareAccount) CloudflareAccount {
	currentHasPassword := strings.TrimSpace(current.WebdavPassword) != ""
	candidateHasPassword := strings.TrimSpace(candidate.WebdavPassword) != ""
	currentValid := currentHasPassword && current.VerificationState == "valid"
	candidateValid := candidateHasPassword && candidate.VerificationState == "valid"
	switch {
	case candidateValid && !currentValid:
		return candidate
	case candidateValid && currentValid && timePointerAfter(candidate.LastVerifiedAt, current.LastVerifiedAt):
		return candidate
	case currentValid:
		return current
	case candidateHasPassword && !currentHasPassword:
		return candidate
	case currentHasPassword:
		return current
	case strings.TrimSpace(current.WebdavUsername) == "" && strings.TrimSpace(candidate.WebdavUsername) != "":
		return candidate
	default:
		return current
	}
}

func timePointerAfter(candidate, current *time.Time) bool {
	if candidate == nil {
		return false
	}
	return current == nil || candidate.After(*current)
}

func (s *Store) normalizeWebdavAccountsLocked(now time.Time) map[string]string {
	aliases := map[string]string{}
	if len(s.state.Accounts) == 0 {
		return aliases
	}
	s.ensureTombstonesLocked()
	next := make([]CloudflareAccount, 0, len(s.state.Accounts))
	byID := make(map[string]int, len(s.state.Accounts))
	changed := false

	for _, raw := range s.state.Accounts {
		account := raw
		if AccountProvider(account) == ProviderWebdav {
			normalized, err := NormalizeWebdavAccount(account)
			if err == nil {
				account = normalized
				if raw.ID != account.ID || raw.Provider != account.Provider ||
					raw.WebdavURL != account.WebdavURL || raw.WebdavRoot != account.WebdavRoot {
					changed = true
				}
				if strings.TrimSpace(raw.ID) != "" && raw.ID != account.ID {
					aliases[raw.ID] = account.ID
				}
			}
		}
		if index, ok := byID[account.ID]; ok && AccountProvider(account) == ProviderWebdav {
			next[index] = mergeSameWebdavAccount(next[index], account)
			changed = true
			continue
		}
		byID[account.ID] = len(next)
		next = append(next, account)
	}

	if len(next) != len(s.state.Accounts) {
		changed = true
	}
	s.state.Accounts = next
	if len(aliases) > 0 {
		s.repointAccountReferencesLocked(aliases)
		for oldID := range aliases {
			if oldID != "" {
				s.state.Tombstones.Accounts[oldID] = now
			}
		}
	}
	for _, account := range s.state.Accounts {
		delete(s.state.Tombstones.Accounts, account.ID)
	}
	primaryID := ""
	for _, account := range s.state.Accounts {
		if account.IsPrimary {
			primaryID = account.ID
			break
		}
	}
	if primaryID == "" {
		for _, account := range s.state.Accounts {
			if account.Enabled {
				primaryID = account.ID
				break
			}
		}
	}
	for index := range s.state.Accounts {
		account := &s.state.Accounts[index]
		if AccountProvider(*account) != ProviderWebdav {
			continue
		}
		if account.ID == primaryID {
			if account.LastError == msgWebdavDifferentNamespace {
				account.LastError = ""
				changed = true
			}
			continue
		}
		if account.Enabled || account.IsPrimary {
			account.Enabled = false
			account.IsPrimary = false
			account.LastError = msgWebdavDifferentNamespace
			changed = true
		}
	}
	if changed {
		for index := range s.state.Accounts {
			if AccountProvider(s.state.Accounts[index]) == ProviderWebdav && s.state.Accounts[index].CatalogUpdatedAt.Before(now) {
				s.state.Accounts[index].CatalogUpdatedAt = now
			}
		}
		s.state.CatalogSync.Dirty = true
	}
	return aliases
}

func repointedAccountID(value string, aliases map[string]string) string {
	if replacement := aliases[strings.TrimSpace(value)]; replacement != "" {
		return replacement
	}
	return value
}

func repointGameAccountReferences(game *Game, aliases map[string]string) {
	if game == nil {
		return
	}
	game.StorageAccountID = repointedAccountID(game.StorageAccountID, aliases)
	game.AutoBackupAccountID = repointedAccountID(game.AutoBackupAccountID, aliases)
	game.BackupStorageAccountID = repointedAccountID(game.BackupStorageAccountID, aliases)
	game.CoverCloudAccountID = repointedAccountID(game.CoverCloudAccountID, aliases)
	game.Anchor.StorageAccountID = repointedAccountID(game.Anchor.StorageAccountID, aliases)
	for filename, accountID := range game.BackupLocations {
		game.BackupLocations[filename] = repointedAccountID(accountID, aliases)
	}
	for index := range game.BackupRegistry {
		game.BackupRegistry[index].AccountID = repointedAccountID(game.BackupRegistry[index].AccountID, aliases)
	}
}

func (s *Store) repointAccountReferencesLocked(aliases map[string]string) {
	for index := range s.state.Games {
		repointGameAccountReferences(&s.state.Games[index], aliases)
	}
	for index := range s.state.Activities {
		s.state.Activities[index].AccountID = repointedAccountID(s.state.Activities[index].AccountID, aliases)
	}
	if s.state.LastStorageHandoff != nil {
		s.state.LastStorageHandoff.SourceAccountID = repointedAccountID(s.state.LastStorageHandoff.SourceAccountID, aliases)
		s.state.LastStorageHandoff.TargetAccountID = repointedAccountID(s.state.LastStorageHandoff.TargetAccountID, aliases)
	}
	if migration := s.state.StorageMigration; migration != nil {
		migration.SourceAccountID = repointedAccountID(migration.SourceAccountID, aliases)
		migration.TargetAccountID = repointedAccountID(migration.TargetAccountID, aliases)
		for index := range migration.Items {
			migration.Items[index].SourceAccountID = repointedAccountID(migration.Items[index].SourceAccountID, aliases)
		}
		for index := range migration.TargetGames {
			repointGameAccountReferences(&migration.TargetGames[index], aliases)
		}
	}
}

func (s *Store) rememberAccountAliasesLocked(aliases map[string]string) {
	if s.accountAliases == nil {
		s.accountAliases = map[string]string{}
	}
	for oldID, newID := range aliases {
		if oldID != "" && newID != "" && oldID != newID {
			s.accountAliases[oldID] = newID
		}
	}
}

func (s *Store) AccountAliases() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	aliases := make(map[string]string, len(s.accountAliases))
	for oldID, newID := range s.accountAliases {
		aliases[oldID] = newID
	}
	return aliases
}

func NormalizeRemoteCatalogWebdavIdentities(catalog RemoteCatalog, now time.Time) (RemoteCatalog, map[string]string) {
	temporary := &Store{
		state: AppState{
			Accounts:           append([]CloudflareAccount(nil), catalog.Accounts...),
			Games:              append([]Game(nil), catalog.Games...),
			Tombstones:         catalog.Tombstones,
			LastStorageHandoff: catalog.Handoff,
		},
		accountAliases: map[string]string{},
	}
	aliases := temporary.normalizeWebdavAccountsLocked(now)
	catalog.Accounts = temporary.state.Accounts
	catalog.Games = temporary.state.Games
	catalog.Tombstones = temporary.state.Tombstones
	catalog.Handoff = temporary.state.LastStorageHandoff
	return catalog, aliases
}
