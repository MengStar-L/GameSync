package core

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	UpdateStatusUnconfigured = "unconfigured"
	UpdateStatusLatest       = "latest"
	UpdateStatusAvailable    = "available"
	UpdateStatusBlocked      = "blocked"
)

type UpdateManifest struct {
	Version             string                         `json:"version"`
	Channel             string                         `json:"channel"`
	PublishedAt         time.Time                      `json:"publishedAt" ts_type:"string"`
	Notes               string                         `json:"notes"`
	MinSupportedVersion string                         `json:"minSupportedVersion"`
	Platforms           map[string]UpdatePlatformAsset `json:"platforms"`
}

type UpdatePlatformAsset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type UpdateCheckResult struct {
	Status         string              `json:"status"`
	CurrentVersion string              `json:"currentVersion"`
	LatestVersion  string              `json:"latestVersion"`
	Channel        string              `json:"channel"`
	Platform       string              `json:"platform"`
	Notes          string              `json:"notes"`
	PublishedAt    time.Time           `json:"publishedAt,omitempty" ts_type:"string"`
	Asset          UpdatePlatformAsset `json:"asset,omitempty"`
	Message        string              `json:"message"`
}

type UpdateDownloadRequest struct {
	Version string              `json:"version"`
	Asset   UpdatePlatformAsset `json:"asset"`
}

type UpdateDownloadResult struct {
	Version     string `json:"version"`
	ArchivePath string `json:"archivePath"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
}

type UpdateOptions struct {
	CurrentVersion string
	Channel        string
	Platform       string
	ManifestURL    string
	Repo           string
	DataDir        string
}

func (manifest UpdateManifest) Validate(platform string) (UpdatePlatformAsset, error) {
	manifest.Version = cleanVersion(manifest.Version)
	platform = strings.TrimSpace(platform)
	if manifest.Version == "" || manifest.Version == "0.0.0" {
		return UpdatePlatformAsset{}, errors.New("update manifest version is empty")
	}
	if manifest.Platforms == nil {
		return UpdatePlatformAsset{}, errors.New("update manifest platforms are empty")
	}
	asset, ok := manifest.Platforms[platform]
	if !ok {
		return UpdatePlatformAsset{}, fmt.Errorf("update manifest has no asset for %s", platform)
	}
	if strings.TrimSpace(asset.URL) == "" {
		return UpdatePlatformAsset{}, errors.New("update asset url is empty")
	}
	parsedURL, err := url.Parse(asset.URL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return UpdatePlatformAsset{}, fmt.Errorf("update asset url is invalid: %s", asset.URL)
	}
	if strings.TrimSpace(asset.SHA256) == "" {
		return UpdatePlatformAsset{}, errors.New("update asset sha256 is empty")
	}
	if asset.Size < 0 {
		return UpdatePlatformAsset{}, errors.New("update asset size is invalid")
	}
	return asset, nil
}

func (asset UpdatePlatformAsset) Normalize() UpdatePlatformAsset {
	asset.URL = strings.TrimSpace(asset.URL)
	asset.SHA256 = strings.ToLower(strings.TrimSpace(asset.SHA256))
	return asset
}
