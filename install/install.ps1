$ErrorActionPreference = "Stop"

$repo = "filfreire/idlebat"
$arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
$asset = "idlebat_windows_${arch}.zip"

Write-Host "Downloading idlebat for windows/${arch}..."

$url = "https://github.com/${repo}/releases/latest/download/${asset}"
$installDir = Join-Path $env:LOCALAPPDATA "idlebat"
$tmpFile = Join-Path $env:TEMP $asset

# Download
Invoke-WebRequest -Uri $url -OutFile $tmpFile -UseBasicParsing

# Extract
if (Test-Path $installDir) { Remove-Item $installDir -Recurse -Force }
New-Item -ItemType Directory -Path $installDir -Force | Out-Null
Expand-Archive -Path $tmpFile -DestinationPath $installDir -Force
Remove-Item $tmpFile -Force

# Add to user PATH if not already there
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$installDir;$userPath", "User")
    Write-Host ""
    Write-Host "Added $installDir to user PATH. Restart your terminal for it to take effect."
}

Write-Host ""
& (Join-Path $installDir "idlebat.exe") --version
Write-Host "idlebat installed to $installDir"
