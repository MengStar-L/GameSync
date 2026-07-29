package core

import (
	"strings"
)

const protectedSecretPrefix = "gamesync-secret:v1:"

func protectAppStateSecrets(state AppState) (AppState, error) {
	protected := cloneState(state)
	for index := range protected.Accounts {
		var err error
		if protected.Accounts[index].APIToken, err = protectStateSecret(protected.Accounts[index].APIToken); err != nil {
			return AppState{}, err
		}
		if protected.Accounts[index].R2AccessKeyID, err = protectStateSecret(protected.Accounts[index].R2AccessKeyID); err != nil {
			return AppState{}, err
		}
		if protected.Accounts[index].R2SecretAccessKey, err = protectStateSecret(protected.Accounts[index].R2SecretAccessKey); err != nil {
			return AppState{}, err
		}
		if protected.Accounts[index].WebdavPassword, err = protectStateSecret(protected.Accounts[index].WebdavPassword); err != nil {
			return AppState{}, err
		}
	}
	var err error
	if protected.Preferences.RawgAPIKey, err = protectStateSecret(protected.Preferences.RawgAPIKey); err != nil {
		return AppState{}, err
	}
	if protected.Preferences.SteamGridDBAPIKey, err = protectStateSecret(protected.Preferences.SteamGridDBAPIKey); err != nil {
		return AppState{}, err
	}
	return protected, nil
}

func unprotectAppStateSecrets(state *AppState) error {
	if state == nil {
		return nil
	}
	for index := range state.Accounts {
		var err error
		if state.Accounts[index].APIToken, err = unprotectStateSecret(state.Accounts[index].APIToken); err != nil {
			return err
		}
		if state.Accounts[index].R2AccessKeyID, err = unprotectStateSecret(state.Accounts[index].R2AccessKeyID); err != nil {
			return err
		}
		if state.Accounts[index].R2SecretAccessKey, err = unprotectStateSecret(state.Accounts[index].R2SecretAccessKey); err != nil {
			return err
		}
		if state.Accounts[index].WebdavPassword, err = unprotectStateSecret(state.Accounts[index].WebdavPassword); err != nil {
			return err
		}
	}
	var err error
	if state.Preferences.RawgAPIKey, err = unprotectStateSecret(state.Preferences.RawgAPIKey); err != nil {
		return err
	}
	if state.Preferences.SteamGridDBAPIKey, err = unprotectStateSecret(state.Preferences.SteamGridDBAPIKey); err != nil {
		return err
	}
	return nil
}

func redactAppStateSecrets(state AppState) AppState {
	redacted := cloneState(state)
	for index := range redacted.Accounts {
		redacted.Accounts[index].APIToken = ""
		redacted.Accounts[index].R2AccessKeyID = ""
		redacted.Accounts[index].R2SecretAccessKey = ""
		redacted.Accounts[index].WebdavPassword = ""
	}
	redacted.Preferences.RawgAPIKey = ""
	redacted.Preferences.SteamGridDBAPIKey = ""
	return redacted
}

func protectStateSecret(value string) (string, error) {
	if strings.TrimSpace(value) == "" || strings.HasPrefix(value, protectedSecretPrefix) {
		return value, nil
	}
	protected, err := protectSecret(value)
	if err != nil {
		return "", err
	}
	return protectedSecretPrefix + protected, nil
}

func unprotectStateSecret(value string) (string, error) {
	if !strings.HasPrefix(value, protectedSecretPrefix) {
		return value, nil
	}
	return unprotectSecret(strings.TrimPrefix(value, protectedSecretPrefix))
}
