param(
  [Parameter(Mandatory = $true)][string]$Version,
  [string]$Platform = "windows-amd64",
  [string]$BuildDir = "build/bin",
  [string]$OutputDir = "dist/release"
)

$ErrorActionPreference = "Stop"

$appExe = Join-Path $BuildDir "GameSync.exe"
$updaterExe = Join-Path $BuildDir "gamesync-updater.exe"
if (!(Test-Path -LiteralPath $appExe)) {
  throw "GameSync.exe not found at $appExe"
}
if (!(Test-Path -LiteralPath $updaterExe)) {
  throw "gamesync-updater.exe not found at $updaterExe"
}

New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null
$staging = Join-Path $OutputDir "staging-$Version-$Platform"
if (Test-Path -LiteralPath $staging) {
  Remove-Item -LiteralPath $staging -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $staging | Out-Null

Copy-Item -LiteralPath $appExe -Destination (Join-Path $staging "GameSync.exe") -Force
Copy-Item -LiteralPath $updaterExe -Destination (Join-Path $staging "gamesync-updater.exe") -Force

$zipName = "GameSync-v$Version-$Platform.zip"
$zipPath = Join-Path $OutputDir $zipName
if (Test-Path -LiteralPath $zipPath) {
  Remove-Item -LiteralPath $zipPath -Force
}
Compress-Archive -Path (Join-Path $staging "*") -DestinationPath $zipPath -Force

$updaterName = "gamesync-updater-v$Version-$Platform.exe"
$legacyUpdaterAsset = Join-Path $OutputDir $updaterName
if (Test-Path -LiteralPath $legacyUpdaterAsset) {
  Remove-Item -LiteralPath $legacyUpdaterAsset -Force
}

$checksumPath = Join-Path $OutputDir "checksums.txt"
if (Test-Path -LiteralPath $checksumPath) {
  Remove-Item -LiteralPath $checksumPath -Force
}
Get-ChildItem -LiteralPath $OutputDir -File |
  Where-Object { $_.Name -ne "checksums.txt" } |
  ForEach-Object {
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash.ToLowerInvariant()
    "$hash  $($_.Name)" | Add-Content -LiteralPath $checksumPath -Encoding UTF8
  }

Remove-Item -LiteralPath $staging -Recurse -Force
Write-Host "Packaged $zipPath"
