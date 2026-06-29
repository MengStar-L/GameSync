param()

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")
$sourceIcon = Join-Path $repoRoot "resource/im1.png"
$buildDir = Join-Path $repoRoot "build"
$windowsBuildDir = Join-Path $buildDir "windows"
$appIcon = Join-Path $buildDir "appicon.png"
$windowsIcon = Join-Path $windowsBuildDir "icon.ico"

if (!(Test-Path -LiteralPath $sourceIcon)) {
  throw "Icon source not found at $sourceIcon"
}

New-Item -ItemType Directory -Force -Path $buildDir | Out-Null
New-Item -ItemType Directory -Force -Path $windowsBuildDir | Out-Null

Copy-Item -LiteralPath $sourceIcon -Destination $appIcon -Force

# Wails only regenerates build/windows/icon.ico when it is missing.
# Remove stale/default output first; the regenerated icon.ico is tracked for review.
if (Test-Path -LiteralPath $windowsIcon) {
  Remove-Item -LiteralPath $windowsIcon -Force
}

Write-Host "Prepared Wails app icon from $sourceIcon"
