package core

import (
	"runtime"
	"strings"
	"time"
)

var (
	Version           = "0.1.0"
	Commit            = "dev"
	BuildDate         = ""
	UpdateChannel     = "stable"
	UpdateRepo        = ""
	UpdateManifestURL = ""
)

type AppInfo struct {
	Version           string `json:"version"`
	Commit            string `json:"commit"`
	BuildDate         string `json:"buildDate"`
	UpdateChannel     string `json:"updateChannel"`
	UpdateRepo        string `json:"updateRepo"`
	UpdateManifestURL string `json:"updateManifestUrl"`
	Platform          string `json:"platform"`
}

func CurrentAppInfo() AppInfo {
	return AppInfo{
		Version:           cleanVersion(Version),
		Commit:            strings.TrimSpace(Commit),
		BuildDate:         strings.TrimSpace(BuildDate),
		UpdateChannel:     firstVersionValue(UpdateChannel, "stable"),
		UpdateRepo:        strings.TrimSpace(UpdateRepo),
		UpdateManifestURL: strings.TrimSpace(UpdateManifestURL),
		Platform:          PlatformKey(),
	}
}

func PlatformKey() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

func cleanVersion(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	if version == "" {
		return "0.0.0"
	}
	return version
}

func firstVersionValue(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func BuildDateTime() time.Time {
	if strings.TrimSpace(BuildDate) == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339, strings.TrimSpace(BuildDate))
	return parsed
}
