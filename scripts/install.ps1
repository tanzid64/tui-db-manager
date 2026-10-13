# dbk — Windows PowerShell installation script
[CmdletBinding()]
param (
    [string]$Version = "",
    [string]$Repo = "tanzid64/tui-db-manager",
    [string]$InstallDir = "$env:LOCALAPPDATA\Programs\dbk"
)

$ErrorActionPreference = "Stop"

Write-Host "==> Installing dbk for Windows..." -ForegroundColor Cyan

# 1. Architecture Detection
$arch = $env:PROCESSOR_ARCHITECTURE.ToLower()
switch ($arch) {
    "amd64" { $arch = "amd64" }
    "arm64" { $arch = "arm64" }
    default {
        Write-Error "Unsupported architecture: $arch. dbk supports amd64 and arm64."
        exit 1
    }
}

# 2. Resolve Version
if ([string]::IsNullOrEmpty($Version) -or $Version -eq "latest") {
    Write-Host "==> Fetching latest release info for $Repo..."
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
        $Version = $release.tag_name
    } catch {
        Write-Error "No published release found on GitHub for $Repo. Please push a release tag or install using 'go install github.com/$Repo/cmd/dbk@latest'."
        exit 1
    }
}

$versionNum = $Version.TrimStart('v')
$assetName = "dbk_${versionNum}_windows_${arch}.zip"
$downloadUrl = "https://github.com/$Repo/releases/download/$Version/$assetName"
$checksumUrl = "https://github.com/$Repo/releases/download/$Version/checksums.txt"

# 3. Temp Directory
$tempDir = Join-Path $env:TEMP ("dbk-install-" + [System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $tempDir | Out-Null

try {
    $zipPath = Join-Path $tempDir $assetName
    Write-Host "==> Downloading $assetName..."
    Invoke-WebRequest -Uri $downloadUrl -OutFile $zipPath

    # 4. Checksum Verification
    $checksumPath = Join-Path $tempDir "checksums.txt"
    try {
        Invoke-WebRequest -Uri $checksumUrl -OutFile $checksumPath
        $checksumContent = Get-Content $checksumPath
        $matchingLine = $checksumContent | Where-Object { $_ -match [regex]::Escape($assetName) }
        if ($matchingLine) {
            $expectedHash = ($matchingLine -split '\s+')[0].Trim()
            $actualHash = (Get-FileHash -Path $zipPath -Algorithm SHA256).Hash.ToLower()
            if ($expectedHash -ne $actualHash) {
                Write-Error "Checksum verification failed! Expected: $expectedHash, Actual: $actualHash"
                exit 1
            }
            Write-Host "==> SHA-256 Checksum verified." -ForegroundColor Green
        }
    } catch {
        Write-Warning "Could not retrieve checksums.txt, skipping hash validation."
    }

    # 5. Extract Archive
    Write-Host "==> Extracting to $InstallDir..."
    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }
    Expand-Archive -Path $zipPath -DestinationPath $tempDir -Force
    Copy-Item -Path (Join-Path $tempDir "dbk.exe") -Destination (Join-Path $InstallDir "dbk.exe") -Force

    # 6. Add to PATH
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $pathParts = $userPath -split ';'
    if ($pathParts -notcontains $InstallDir) {
        Write-Host "==> Adding $InstallDir to User PATH..." -ForegroundColor Cyan
        $newPath = "$userPath;$InstallDir"
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        $env:Path = "$env:Path;$InstallDir"
    }

    Write-Host "==> Installation complete! Run 'dbk' to launch the database backup manager." -ForegroundColor Green
} finally {
    Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
}
