param(
  [Parameter(Mandatory = $true)][string]$Version,
  [Parameter(Mandatory = $true)][string]$Repo,
  [string]$Platform = "windows-amd64",
  [Parameter(Mandatory = $true)][string]$Sha256,
  [Parameter(Mandatory = $true)][Int64]$Size,
  [string]$Channel = "stable",
  [string]$Notes = "",
  [string]$MinSupportedVersion = "0.1.0",
  [Parameter(Mandatory = $true)][string]$OutputPath
)

$ErrorActionPreference = "Stop"

$artifactName = "GameSync-v$Version-$Platform.zip"
$assetUrl = "https://github.com/$Repo/releases/download/v$Version/$artifactName"
$publishedAt = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

$manifest = [ordered]@{
  version = $Version
  channel = $Channel
  publishedAt = $publishedAt
  notes = $Notes
  minSupportedVersion = $MinSupportedVersion
  platforms = [ordered]@{
    $Platform = [ordered]@{
      url = $assetUrl
      sha256 = $Sha256.ToLowerInvariant()
      size = $Size
    }
  }
}

$dir = Split-Path -Parent $OutputPath
if ($dir) {
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
}
$manifest | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $OutputPath -Encoding UTF8
Write-Host "Wrote $OutputPath"
