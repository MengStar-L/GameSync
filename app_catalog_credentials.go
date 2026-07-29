package main

import (
	"strings"
	"time"

	"gamesync/internal/core"
)

func normalizeRemoteCatalogForMerge(catalog core.RemoteCatalog, encrypted map[string]core.EncryptedCredentialBlob) (core.RemoteCatalog, map[string]core.EncryptedCredentialBlob) {
	normalized, aliases := core.NormalizeRemoteCatalogWebdavIdentities(catalog, time.Now())
	if len(aliases) == 0 {
		return normalized, encrypted
	}
	credentials := make(map[string]core.EncryptedCredentialBlob, len(encrypted))
	for id, blob := range encrypted {
		if aliases[id] == "" {
			credentials[id] = blob
		}
	}
	for id, blob := range encrypted {
		if replacement := aliases[id]; replacement != "" {
			if _, exists := credentials[replacement]; !exists {
				credentials[replacement] = blob
			}
		}
	}
	return normalized, credentials
}

func encryptCatalogCredentials(accounts []core.CloudflareAccount, password string) (map[string]core.EncryptedCredentialBlob, error) {
	encrypted := make(map[string]core.EncryptedCredentialBlob, len(accounts))
	password = strings.TrimSpace(password)
	if password == "" {
		return encrypted, nil
	}
	for _, account := range accounts {
		if strings.TrimSpace(account.ID) == "" {
			continue
		}
		blob, err := core.EncryptAccountCredentials(account, password)
		if err != nil {
			return nil, err
		}
		encrypted[account.ID] = blob
	}
	return encrypted, nil
}

func decryptCatalogCredentials(catalog core.RemoteCatalog, encrypted map[string]core.EncryptedCredentialBlob, password string) (core.RemoteCatalog, map[string]error) {
	failures := map[string]error{}
	password = strings.TrimSpace(password)
	for index := range catalog.Accounts {
		account := catalog.Accounts[index]
		blob, ok := encrypted[account.ID]
		if !ok || password == "" {
			continue
		}
		decrypted, err := core.DecryptAccountCredentials(account, blob, password)
		if err != nil {
			failures[account.ID] = err
			account.VerificationState = "invalid"
			account.LastError = msgRecoveryPasswordDecryptFailed
		} else {
			account = decrypted
			account.CredentialsBackedUp = true
			account.VerificationState = "pending"
			account.LastError = ""
		}
		catalog.Accounts[index] = account
	}
	return catalog, failures
}

func catalogAccountHasWritableCredentials(account core.CloudflareAccount) bool {
	if core.AccountProvider(account) == core.ProviderWebdav {
		return strings.TrimSpace(account.WebdavURL) != "" &&
			strings.TrimSpace(account.WebdavUsername) != "" &&
			strings.TrimSpace(account.WebdavPassword) != ""
	}
	return strings.TrimSpace(account.AccountID) != "" &&
		strings.TrimSpace(account.APIToken) != "" &&
		strings.TrimSpace(account.D1DatabaseID) != "" &&
		strings.TrimSpace(account.R2Bucket) != "" &&
		strings.TrimSpace(account.R2AccessKeyID) != "" &&
		strings.TrimSpace(account.R2SecretAccessKey) != ""
}

func prepareCatalogForOrdinaryMerge(local core.AppState, catalog core.RemoteCatalog, encrypted map[string]core.EncryptedCredentialBlob, password string) (core.RemoteCatalog, map[string]error) {
	localByID := make(map[string]core.CloudflareAccount, len(local.Accounts))
	primaryID := ""
	for _, account := range local.Accounts {
		localByID[account.ID] = account
		if account.IsPrimary {
			primaryID = account.ID
		}
	}

	for index := range catalog.Accounts {
		remote := &catalog.Accounts[index]
		if localAccount, ok := localByID[remote.ID]; ok {
			mergeAccountSecrets(remote, localAccount)
		}
	}
	catalog, failures := decryptCatalogCredentials(catalog, encrypted, password)
	for index := range catalog.Accounts {
		account := &catalog.Accounts[index]
		if account.ID == primaryID {
			account.IsPrimary = true
			account.Enabled = true
			continue
		}
		account.IsPrimary = false
		if !catalogAccountHasWritableCredentials(*account) {
			account.Enabled = false
			if account.VerificationState == "" {
				account.VerificationState = "invalid"
			}
		}
	}
	return catalog, failures
}

func mergeAccountSecrets(target *core.CloudflareAccount, source core.CloudflareAccount) {
	if target == nil {
		return
	}
	if target.APIToken == "" {
		target.APIToken = source.APIToken
	}
	if target.R2AccessKeyID == "" {
		target.R2AccessKeyID = source.R2AccessKeyID
	}
	if target.R2SecretAccessKey == "" {
		target.R2SecretAccessKey = source.R2SecretAccessKey
	}
	if target.WebdavPassword == "" {
		target.WebdavPassword = source.WebdavPassword
	}
}
