//go:build !windows

package core

import (
	"encoding/base64"
	"strings"
)

const fallbackSecretPrefix = "base64:"

func protectSecret(value string) (string, error) {
	return fallbackSecretPrefix + base64.StdEncoding.EncodeToString([]byte(value)), nil
}

func unprotectSecret(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, fallbackSecretPrefix) {
		return value, nil
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, fallbackSecretPrefix))
	if err != nil {
		return "", err
	}
	return string(data), nil
}
