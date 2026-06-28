# Release and Auto Update

GameSync uses GitHub Releases as the update channel. A tag such as `v0.1.1` triggers GitHub Actions, builds the Windows executable, creates a release ZIP, writes `latest.json`, and uploads all artifacts.

## Release Flow

1. Commit all code changes.
2. Create and push a tag:

```powershell
git tag v0.1.1
git push origin v0.1.1
```

3. GitHub Actions runs `.github/workflows/release.yml`.
4. The release contains:
   - `GameSync-v0.1.1-windows-amd64.zip`
   - `gamesync-updater-v0.1.1-windows-amd64.exe`
   - `latest.json`
   - `checksums.txt`

## Runtime Update Flow

1. The app calls `CheckForUpdates`.
2. It reads `latest.json` from the configured GitHub Release URL.
3. It compares the current build version with the manifest version.
4. If an update is available, the user clicks `更新并重启`.
5. The app downloads the ZIP, verifies SHA256 and size, copies `gamesync-updater.exe` to a temporary helper location, starts it, and exits.
6. The helper waits for the main process to exit, verifies the ZIP again, extracts to staging, backs up replaced files, copies the new files into the app directory, and restarts `GameSync.exe`.

## Build-Time Variables

The release workflow injects these Go variables with `-ldflags`:

```text
gamesync/internal/core.Version
gamesync/internal/core.Commit
gamesync/internal/core.BuildDate
gamesync/internal/core.UpdateChannel
gamesync/internal/core.UpdateRepo
gamesync/internal/core.UpdateManifestURL
```

Local development builds default to version `0.1.0`, commit `dev`, and no update source. In that state the Settings page reports that updates are not configured instead of failing.

## Safety Rules

- Update downloads must use HTTPS GitHub Release URLs.
- If `UpdateRepo` is configured, update URLs must stay under that repository.
- The ZIP SHA256 must match `latest.json`.
- ZIP entries are rejected if they try to escape the staging directory.
- The helper only replaces files in the app installation directory.
- User data under `%AppData%/GameSync` is not inside the release ZIP and is not replaced.

## Manual Verification

Before publishing a public release:

```powershell
go test ./...
cd frontend
npm run build
cd ..
wails build -clean
go build -o build/bin/gamesync-updater.exe ./cmd/gamesync-updater
.\scripts\package-release.ps1 -Version "0.1.1"
```

Then test update with a draft or private release before pushing the public tag.
