param(
  [string]$ExecutablePath = "build/bin/GameSync.exe",
  [string]$PreviewPath = "build/windows/exe-icon-preview.png"
)

$ErrorActionPreference = "Stop"

if (!(Test-Path -LiteralPath $ExecutablePath)) {
  throw "Executable not found at $ExecutablePath"
}

Add-Type -AssemblyName System.Drawing

$icon = [System.Drawing.Icon]::ExtractAssociatedIcon((Resolve-Path -LiteralPath $ExecutablePath).Path)
if ($null -eq $icon) {
  throw "No associated icon found in $ExecutablePath"
}

$bitmap = $icon.ToBitmap()
try {
  $previewDir = Split-Path -Parent $PreviewPath
  if ($previewDir) {
    New-Item -ItemType Directory -Force -Path $previewDir | Out-Null
  }
  $bitmap.Save($PreviewPath, [System.Drawing.Imaging.ImageFormat]::Png)

  $hasBrandPixel = $false
  for ($y = 0; $y -lt $bitmap.Height -and -not $hasBrandPixel; $y++) {
    for ($x = 0; $x -lt $bitmap.Width; $x++) {
      $pixel = $bitmap.GetPixel($x, $y)
      if ($pixel.R -gt 180 -and $pixel.G -lt 190 -and $pixel.B -gt 120) {
        $hasBrandPixel = $true
        break
      }
    }
  }

  if (-not $hasBrandPixel) {
    throw "Associated icon does not look like the GameSync brand icon. Preview saved to $PreviewPath"
  }
} finally {
  $bitmap.Dispose()
  $icon.Dispose()
}

Write-Host "Verified GameSync icon in $ExecutablePath"
