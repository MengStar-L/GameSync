package core

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Updater struct {
	HTTPClient *http.Client
	Options    UpdateOptions
}

type semver struct {
	major int
	minor int
	patch int
	pre   string
}

func NewUpdater(options UpdateOptions) *Updater {
	if strings.TrimSpace(options.CurrentVersion) == "" {
		options.CurrentVersion = Version
	}
	if strings.TrimSpace(options.Channel) == "" {
		options.Channel = UpdateChannel
	}
	if strings.TrimSpace(options.Platform) == "" {
		options.Platform = PlatformKey()
	}
	if strings.TrimSpace(options.ManifestURL) == "" {
		options.ManifestURL = UpdateManifestURL
	}
	if strings.TrimSpace(options.Repo) == "" {
		options.Repo = UpdateRepo
	}
	return &Updater{
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		Options:    options,
	}
}

func (u *Updater) Check(ctx context.Context) (UpdateCheckResult, error) {
	options := u.Options
	currentVersion := cleanVersion(options.CurrentVersion)
	channel := firstVersionValue(options.Channel, "stable")
	platform := firstVersionValue(options.Platform, PlatformKey())
	manifestURL := strings.TrimSpace(options.ManifestURL)
	if manifestURL == "" && strings.TrimSpace(options.Repo) != "" {
		manifestURL = fmt.Sprintf("https://github.com/%s/releases/latest/download/latest.json", strings.Trim(strings.TrimSpace(options.Repo), "/"))
	}
	if manifestURL == "" {
		return UpdateCheckResult{
			Status:         UpdateStatusUnconfigured,
			CurrentVersion: currentVersion,
			Channel:        channel,
			Platform:       platform,
			Message:        "未配置更新源",
		}, nil
	}

	manifest, err := u.FetchManifest(ctx, manifestURL)
	if err != nil {
		return UpdateCheckResult{}, err
	}
	asset, err := manifest.Validate(platform)
	if err != nil {
		return UpdateCheckResult{}, err
	}
	asset = asset.Normalize()
	if err := validateGitHubReleaseURL(asset.URL, options.Repo); err != nil {
		return UpdateCheckResult{}, err
	}

	latestVersion := cleanVersion(manifest.Version)
	if strings.EqualFold(channel, "stable") && strings.Contains(latestVersion, "-") {
		return UpdateCheckResult{
			Status:         UpdateStatusLatest,
			CurrentVersion: currentVersion,
			LatestVersion:  currentVersion,
			Channel:        channel,
			Platform:       platform,
			Message:        "稳定通道已忽略预发布版本",
		}, nil
	}
	if strings.TrimSpace(manifest.MinSupportedVersion) != "" && compareVersions(currentVersion, manifest.MinSupportedVersion) < 0 {
		return UpdateCheckResult{
			Status:         UpdateStatusBlocked,
			CurrentVersion: currentVersion,
			LatestVersion:  latestVersion,
			Channel:        channel,
			Platform:       platform,
			Notes:          strings.TrimSpace(manifest.Notes),
			PublishedAt:    manifest.PublishedAt,
			Asset:          asset,
			Message:        "当前版本过旧，请手动下载最新版本",
		}, nil
	}
	if compareVersions(latestVersion, currentVersion) <= 0 {
		return UpdateCheckResult{
			Status:         UpdateStatusLatest,
			CurrentVersion: currentVersion,
			LatestVersion:  latestVersion,
			Channel:        channel,
			Platform:       platform,
			Notes:          strings.TrimSpace(manifest.Notes),
			PublishedAt:    manifest.PublishedAt,
			Message:        "当前已是最新版本",
		}, nil
	}
	return UpdateCheckResult{
		Status:         UpdateStatusAvailable,
		CurrentVersion: currentVersion,
		LatestVersion:  latestVersion,
		Channel:        channel,
		Platform:       platform,
		Notes:          strings.TrimSpace(manifest.Notes),
		PublishedAt:    manifest.PublishedAt,
		Asset:          asset,
		Message:        "发现新版本",
	}, nil
}

func (u *Updater) FetchManifest(ctx context.Context, manifestURL string) (UpdateManifest, error) {
	if err := validateGitHubReleaseURL(manifestURL, u.Options.Repo); err != nil {
		return UpdateManifest{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return UpdateManifest{}, err
	}
	response, err := u.httpClient().Do(request)
	if err != nil {
		return UpdateManifest{}, fmt.Errorf("fetch update manifest: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return UpdateManifest{}, fmt.Errorf("fetch update manifest: HTTP %d", response.StatusCode)
	}
	var manifest UpdateManifest
	if err := json.NewDecoder(response.Body).Decode(&manifest); err != nil {
		return UpdateManifest{}, fmt.Errorf("decode update manifest: %w", err)
	}
	return manifest, nil
}

func (u *Updater) Download(ctx context.Context, request UpdateDownloadRequest) (UpdateDownloadResult, error) {
	version := cleanVersion(request.Version)
	asset := request.Asset.Normalize()
	if version == "" || version == "0.0.0" {
		return UpdateDownloadResult{}, errors.New("update version is empty")
	}
	if err := validateGitHubReleaseURL(asset.URL, u.Options.Repo); err != nil {
		return UpdateDownloadResult{}, err
	}
	if strings.TrimSpace(asset.SHA256) == "" {
		return UpdateDownloadResult{}, errors.New("update asset sha256 is empty")
	}
	dataDir := strings.TrimSpace(u.Options.DataDir)
	if dataDir == "" {
		return UpdateDownloadResult{}, errors.New("update data dir is empty")
	}
	downloadDir := filepath.Join(dataDir, "updates", version)
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return UpdateDownloadResult{}, fmt.Errorf("create update download dir: %w", err)
	}
	archivePath := filepath.Join(downloadDir, fmt.Sprintf("GameSync-%s-%s.zip", version, PlatformKey()))
	tempPath := archivePath + ".download"
	_ = os.Remove(tempPath)

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return UpdateDownloadResult{}, err
	}
	response, err := u.httpClient().Do(httpRequest)
	if err != nil {
		return UpdateDownloadResult{}, fmt.Errorf("download update: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return UpdateDownloadResult{}, fmt.Errorf("download update: HTTP %d", response.StatusCode)
	}

	file, err := os.Create(tempPath)
	if err != nil {
		return UpdateDownloadResult{}, fmt.Errorf("create update archive: %w", err)
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hasher), response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tempPath)
		return UpdateDownloadResult{}, fmt.Errorf("write update archive: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return UpdateDownloadResult{}, fmt.Errorf("close update archive: %w", closeErr)
	}
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(actualHash, asset.SHA256) {
		_ = os.Remove(tempPath)
		return UpdateDownloadResult{}, fmt.Errorf("update archive sha256 mismatch")
	}
	if asset.Size > 0 && written != asset.Size {
		_ = os.Remove(tempPath)
		return UpdateDownloadResult{}, fmt.Errorf("update archive size mismatch")
	}
	_ = os.Remove(archivePath)
	if err := os.Rename(tempPath, archivePath); err != nil {
		_ = os.Remove(tempPath)
		return UpdateDownloadResult{}, fmt.Errorf("save update archive: %w", err)
	}
	return UpdateDownloadResult{
		Version:     version,
		ArchivePath: archivePath,
		SHA256:      actualHash,
		Size:        written,
	}, nil
}

func (u *Updater) ApplyAndRestart(download UpdateDownloadResult, executablePath string) error {
	executablePath = strings.TrimSpace(executablePath)
	if executablePath == "" {
		return errors.New("executable path is empty")
	}
	if strings.TrimSpace(download.ArchivePath) == "" {
		return errors.New("update archive path is empty")
	}
	if _, err := os.Stat(download.ArchivePath); err != nil {
		return fmt.Errorf("update archive not found: %w", err)
	}
	targetDir := filepath.Dir(executablePath)
	helperPath := filepath.Join(targetDir, "gamesync-updater.exe")
	if _, err := os.Stat(helperPath); err != nil {
		return fmt.Errorf("update helper not found: %w", err)
	}
	dataDir := strings.TrimSpace(u.Options.DataDir)
	if dataDir == "" {
		return errors.New("update data dir is empty")
	}
	tempHelperDir := filepath.Join(dataDir, "updates", "helper")
	if err := os.MkdirAll(tempHelperDir, 0o755); err != nil {
		return fmt.Errorf("create helper temp dir: %w", err)
	}
	tempHelper := filepath.Join(tempHelperDir, fmt.Sprintf("gamesync-updater-%d.exe", time.Now().UnixNano()))
	if err := copyFile(helperPath, tempHelper); err != nil {
		return fmt.Errorf("stage update helper: %w", err)
	}
	logPath := filepath.Join(dataDir, "updates", "updater.log")
	command := exec.Command(tempHelper,
		"--pid", strconv.Itoa(os.Getpid()),
		"--archive", download.ArchivePath,
		"--target-dir", targetDir,
		"--exe", executablePath,
		"--sha256", download.SHA256,
		"--log", logPath,
	)
	if err := command.Start(); err != nil {
		return fmt.Errorf("start update helper: %w", err)
	}
	return nil
}

func (u *Updater) httpClient() *http.Client {
	if u.HTTPClient != nil {
		return u.HTTPClient
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func compareVersions(left string, right string) int {
	leftVersion := parseSemver(left)
	rightVersion := parseSemver(right)
	if leftVersion.major != rightVersion.major {
		return compareInt(leftVersion.major, rightVersion.major)
	}
	if leftVersion.minor != rightVersion.minor {
		return compareInt(leftVersion.minor, rightVersion.minor)
	}
	if leftVersion.patch != rightVersion.patch {
		return compareInt(leftVersion.patch, rightVersion.patch)
	}
	switch {
	case leftVersion.pre == "" && rightVersion.pre != "":
		return 1
	case leftVersion.pre != "" && rightVersion.pre == "":
		return -1
	case leftVersion.pre < rightVersion.pre:
		return -1
	case leftVersion.pre > rightVersion.pre:
		return 1
	default:
		return 0
	}
}

func parseSemver(version string) semver {
	version = cleanVersion(version)
	version = strings.Split(version, "+")[0]
	base := version
	pre := ""
	if before, after, ok := strings.Cut(version, "-"); ok {
		base = before
		pre = after
	}
	parts := strings.Split(base, ".")
	readPart := func(index int) int {
		if index >= len(parts) {
			return 0
		}
		value, _ := strconv.Atoi(strings.TrimSpace(parts[index]))
		return value
	}
	return semver{
		major: readPart(0),
		minor: readPart(1),
		patch: readPart(2),
		pre:   pre,
	}
}

func compareInt(left int, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func validateGitHubReleaseURL(rawURL string, repo string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return errors.New("update url is empty")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("update url is invalid: %w", err)
	}
	if parsed.Scheme != "https" {
		return errors.New("update url must use https")
	}
	if !strings.EqualFold(parsed.Host, "github.com") {
		return fmt.Errorf("update url host is not allowed: %s", parsed.Host)
	}
	repo = strings.Trim(strings.TrimSpace(repo), "/")
	if repo == "" {
		return nil
	}
	allowedPrefix := "/" + repo + "/releases/"
	if !strings.HasPrefix(parsed.EscapedPath(), allowedPrefix) && !strings.HasPrefix(parsed.Path, allowedPrefix) {
		return fmt.Errorf("update url is outside configured repo %s", repo)
	}
	return nil
}

func copyFile(source string, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	output, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func validateZipEntryPath(root string, name string) (string, error) {
	name = filepath.ToSlash(strings.TrimSpace(name))
	if name == "" || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("unsafe zip entry path: %s", name)
	}
	target := filepath.Join(root, filepath.FromSlash(name))
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", err
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("unsafe zip entry path: %s", name)
	}
	return targetAbs, nil
}

func hasZipTraversal(archivePath string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if _, err := validateZipEntryPath("root", file.Name); err != nil {
			return err
		}
	}
	return nil
}
