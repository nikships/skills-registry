# install.ps1 — install the skills-registry Go CLI on Windows.
#
# Usage:
#   powershell -c "& ([scriptblock]::Create((irm https://raw.githubusercontent.com/nikships/skills-registry/main/install.ps1)))"
#
# Detects the host architecture, downloads the matching zip from GitHub
# Releases, extracts skills-registry.exe into ~\.local\bin, and adds
# that directory to the user PATH if needed.
#
# Supported platforms: windows/amd64, windows/arm64
#
# Environment overrides (mostly for testing / pinning):
#   SKILLS_REGISTRY_REPO     Override owner/repo (default: nikships/skills-registry)
#   SKILLS_REGISTRY_VERSION  Pin to a tag, e.g. v0.5.1 (default: latest)
#   SKILLS_BIN_DIR           Override install dir (default: $HOME\.local\bin)
#   SKILLS_REGISTRY_ARCH     Override detected arch (default: from RuntimeInformation)
#   SKILLS_REGISTRY_URL      Override the full zip URL
#   SKILLS_REGISTRY_TARBALL  Use a local zip file instead of downloading
#   SKILLS_REGISTRY_DRY_RUN  If non-empty, print resolved URL/dest and exit
#
# Exit codes:
#   0  success
#   1  generic / IO failure
#   2  unsupported architecture

param(
    [string]$Repo = $env:SKILLS_REGISTRY_REPO,
    [string]$Version = $env:SKILLS_REGISTRY_VERSION,
    [string]$BinDir = $env:SKILLS_BIN_DIR
)

if (-not $Repo) { $Repo = "nikships/skills-registry" }
if (-not $Version) { $Version = "latest" }
if (-not $BinDir) { $BinDir = "$env:USERPROFILE\.local\bin" }

$Binary = "skills-registry.exe"

function Write-Log($msg) { Write-Host $msg }
function Write-Warn($msg) { Write-Warning $msg }
function Write-Err($msg) { Write-Error $msg }

function Get-Arch {
    $raw = $env:SKILLS_REGISTRY_ARCH
    if (-not $raw) {
        $procArch = [System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture
        $raw = switch ($procArch) {
            ([System.Runtime.InteropServices.Architecture]::X64)     { "amd64" }
            ([System.Runtime.InteropServices.Architecture]::Arm64)   { "arm64" }
            default { $procArch.ToString().ToLower() }
        }
    }
    switch ($raw) {
        "amd64"     { return "amd64" }
        "arm64"     { return "arm64" }
        "aarch64"   { return "arm64" }
        default {
            Write-Err "unsupported architecture: $raw"
            Write-Err "supported architectures: amd64, arm64"
            exit 2
        }
    }
}

function Build-Url($arch) {
    $asset = "skills-registry_windows_$arch.zip"
    if ($Version -eq "latest") {
        return "https://github.com/$Repo/releases/latest/download/$asset"
    }
    else {
        return "https://github.com/$Repo/releases/download/$Version/$asset"
    }
}

$arch = Get-Arch
$url = if ($env:SKILLS_REGISTRY_URL) { $env:SKILLS_REGISTRY_URL } else { Build-Url $arch }
$dest = Join-Path $BinDir $Binary

Write-Log "skills-registry installer"
Write-Log "  platform : windows/$arch"
Write-Log "  url      : $url"
Write-Log "  install  : $dest"

if ($env:SKILLS_REGISTRY_DRY_RUN) {
    Write-Output $url
    exit 0
}

if (-not (Test-Path $BinDir)) {
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
}

$tmp = Join-Path $env:TEMP "skills-registry-install-$(Get-Random)"
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
    $zip = Join-Path $tmp "skills-registry.zip"
    if ($env:SKILLS_REGISTRY_TARBALL) {
        Copy-Item -Path $env:SKILLS_REGISTRY_TARBALL -Destination $zip -Force
    }
    else {
        Write-Log "downloading..."
        try {
            Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing -MaximumRedirection 10
        }
        catch {
            Write-Err "download failed: $url"
            exit 1
        }
    }

    Write-Log "extracting..."
    Expand-Archive -Path $zip -DestinationPath $tmp -Force

    $extracted = Join-Path $tmp $Binary
    if (-not (Test-Path $extracted)) {
        Write-Err "binary '$Binary' not found inside downloaded archive"
        exit 1
    }

    if (Test-Path $dest) {
        Remove-Item -Path $dest -Force
    }
    Move-Item -Path $extracted -Destination $dest -Force

    Write-Log "installed: $dest"

    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($userPath -notlike "*$BinDir*") {
        [Environment]::SetEnvironmentVariable("Path", "$userPath;$BinDir", "User")
        Write-Log "Added $BinDir to your user PATH. Restart your terminal for the change to take effect."
    }

    Write-Output "Run 'skills-registry' to get started."
}
finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
