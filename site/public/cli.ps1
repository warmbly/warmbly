<#
.SYNOPSIS
    Installs the warmbly CLI on Windows.

.DESCRIPTION
    One static binary, no toolchain, no admin rights. Downloads the archive for
    this machine's architecture from the GitHub release, checks it against the
    published checksum, unpacks it into a per-user directory and puts that
    directory on the user PATH.

    Re-running it upgrades in place.

.EXAMPLE
    irm https://warmbly.com/cli.ps1 | iex

.EXAMPLE
    & ([scriptblock]::Create((irm https://warmbly.com/cli.ps1))) -Version v1.4.0

.EXAMPLE
    & ([scriptblock]::Create((irm https://warmbly.com/cli.ps1))) -Uninstall

.LINK
    https://docs.warmbly.com/api/cli/
#>
[CmdletBinding()]
param(
    # Where the binary goes. Defaults to a per-user directory so nothing here
    # needs an elevated shell.
    [string]$Dir = $env:WARMBLY_INSTALL_DIR,

    # A release tag to pin, for example v1.4.0. Defaults to the newest release.
    [string]$Version = $env:WARMBLY_CLI_VERSION,

    # Download from a mirror of the release assets instead of GitHub, for an
    # egress-restricted network.
    [string]$BaseUrl = $env:WARMBLY_CLI_BASE_URL,

    # Leave the user PATH alone.
    [switch]$NoModifyPath,

    # Print what would happen and change nothing.
    [switch]$DryRun,

    # Remove the binary and its PATH entry.
    [switch]$Uninstall,

    # Reinstall even when the version already matches.
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Repo     = 'warmbly/warmbly'
$Releases = "https://github.com/$Repo/releases"
$Docs     = 'https://docs.warmbly.com/api/cli/'

function Write-Step { param($m) Write-Host "> $m" -ForegroundColor Cyan }
function Write-Ok   { param($m) Write-Host "✓ $m" -ForegroundColor Green }
function Write-Warn { param($m) Write-Host "! $m" -ForegroundColor Yellow }
function Write-Fail { param($m) Write-Host "✗ $m" -ForegroundColor Red; exit 1 }

# Windows on ARM runs amd64 binaries under emulation, but a native build is
# published, so the architecture is read rather than assumed.
function Get-Arch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) { $arch = $env:PROCESSOR_ARCHITEW6432 }
    switch ($arch) {
        'AMD64' { return 'amd64' }
        'ARM64' { return 'arm64' }
        default {
            Write-Fail @"
No published build for $arch.
We publish amd64 and arm64. Build from source with:
  go install github.com/$Repo/cmd/cli@latest
"@
        }
    }
}

function Get-AssetUrl {
    param($Name)
    if ($BaseUrl)  { return "$($BaseUrl.TrimEnd('/'))/$Name" }
    if ($Version)  { return "$Releases/download/$Version/$Name" }
    return "$Releases/latest/download/$Name"
}

function Get-InstallDir {
    if ($Dir) { return $Dir }
    return (Join-Path $env:LOCALAPPDATA 'Warmbly\bin')
}

# The user PATH is read from the registry rather than from $env:PATH, because
# the session copy already has machine entries merged in and writing that back
# would move machine-wide entries into the user scope.
function Add-ToUserPath {
    param($Target)

    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($null -eq $current) { $current = '' }
    $entries = $current -split ';' | Where-Object { $_ -ne '' }

    if ($entries -contains $Target) {
        Write-Ok "$Target is already on your PATH"
        return
    }
    if ($NoModifyPath) {
        Write-Warn "$Target is not on your PATH. Add it yourself, or re-run without -NoModifyPath."
        return
    }
    if ($DryRun) {
        Write-Host "    would add $Target to the user PATH"
        return
    }

    $updated = (@($entries) + $Target) -join ';'
    [Environment]::SetEnvironmentVariable('Path', $updated, 'User')
    # The registry change reaches new processes only, so this session gets the
    # entry too. Without it, the very next command in this window fails.
    $env:Path = "$env:Path;$Target"
    Write-Ok "added $Target to your PATH"
    Write-Warn 'Open a new terminal for other programs to see it.'
}

function Install-Completions {
    param($Source)

    $completion = Join-Path $Source 'completions\warmbly.powershell'
    if (-not (Test-Path $completion)) { return }

    $profilePath = $PROFILE.CurrentUserAllHosts
    $marker = '# Added by the warmbly CLI installer'

    if ($DryRun) {
        Write-Host "    would add completions to $profilePath"
        return
    }
    if ((Test-Path $profilePath) -and (Select-String -Path $profilePath -Pattern ([regex]::Escape($marker)) -Quiet)) {
        return
    }

    $dest = Join-Path (Get-InstallDir) 'warmbly.completion.ps1'
    Copy-Item $completion $dest -Force

    New-Item -ItemType Directory -Force -Path (Split-Path $profilePath) | Out-Null
    Add-Content -Path $profilePath -Value "`n$marker`n. `"$dest`""
    Write-Ok "wrote completions and referenced them from $profilePath"
}

function Invoke-Uninstall {
    $target = Get-InstallDir
    $exe = Join-Path $target 'warmbly.exe'
    $removed = $false

    if (Test-Path $exe) {
        if ($DryRun) { Write-Host "would remove $exe" }
        else { Remove-Item $exe -Force; Write-Ok "removed $exe" }
        $removed = $true
    }

    $completion = Join-Path $target 'warmbly.completion.ps1'
    if (Test-Path $completion) {
        if (-not $DryRun) { Remove-Item $completion -Force }
        $removed = $true
    }

    if (-not $DryRun) {
        $current = [Environment]::GetEnvironmentVariable('Path', 'User')
        if ($current) {
            $kept = $current -split ';' | Where-Object { $_ -ne '' -and $_ -ne $target }
            [Environment]::SetEnvironmentVariable('Path', ($kept -join ';'), 'User')
        }
    }

    if (-not $removed) { Write-Warn "nothing to remove: no warmbly.exe in $target" }

    $config = Join-Path $env:APPDATA 'warmbly'
    if (Test-Path $config) {
        Write-Host ''
        Write-Host "Your sign-ins are still in $config."
        Write-Host "Remove them with: Remove-Item -Recurse '$config'"
    }
}

function Invoke-Install {
    $arch   = Get-Arch
    $target = Get-InstallDir
    $asset  = "warmbly_windows_$arch.zip"

    Write-Step 'Installing the warmbly CLI'
    Write-Host "    platform: windows/$arch"
    Write-Host "    version:  $(if ($Version) { $Version } else { 'latest' })"
    Write-Host "    into:     $target"
    Write-Host ''

    $exe = Join-Path $target 'warmbly.exe'
    if ((Test-Path $exe) -and $Version -and -not $Force) {
        $current = (& $exe version 2>$null | Select-Object -First 1) -split ' ' | Select-Object -Index 1
        if ($current -eq $Version) {
            Write-Ok "warmbly $current is already installed in $target"
            return
        }
    }

    if ($DryRun) {
        Write-Host "would download $(Get-AssetUrl $asset)"
        Write-Host "would verify it against $(Get-AssetUrl 'checksums.txt')"
        Write-Host "would install $exe"
        Install-Completions ''
        Add-ToUserPath $target
        return
    }

    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("warmbly-" + [guid]::NewGuid())
    New-Item -ItemType Directory -Force -Path $tmp | Out-Null
    try {
        Write-Step "Downloading $asset"
        $zip = Join-Path $tmp $asset
        try {
            Invoke-WebRequest -Uri (Get-AssetUrl $asset) -OutFile $zip -UseBasicParsing
        } catch {
            Write-Fail "could not download $(Get-AssetUrl $asset)`nIf you pinned -Version, check the tag exists: $Releases"
        }

        # The checksum is why this is safer than a bare download: a truncated
        # transfer and a tampered one are indistinguishable to Expand-Archive.
        try {
            $sums = Join-Path $tmp 'checksums.txt'
            Invoke-WebRequest -Uri (Get-AssetUrl 'checksums.txt') -OutFile $sums -UseBasicParsing
            $want = (Select-String -Path $sums -Pattern ([regex]::Escape($asset)) | Select-Object -First 1).Line -split '\s+' | Select-Object -First 1
            $got  = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()
            if (-not $want) {
                Write-Warn "checksums.txt has no entry for $asset; continuing without verification"
            } elseif ($want.ToLower() -ne $got) {
                Write-Fail "checksum mismatch for $asset.`n  expected $want`n  got      $got`nNothing was installed."
            } else {
                Write-Ok 'checksum verified'
            }
        } catch {
            Write-Warn 'could not fetch checksums.txt; continuing without verification'
        }

        Write-Step 'Unpacking'
        $unpacked = Join-Path $tmp 'x'
        Expand-Archive -Path $zip -DestinationPath $unpacked -Force
        $source = Join-Path $unpacked 'warmbly.exe'
        if (-not (Test-Path $source)) { Write-Fail 'the archive did not contain warmbly.exe' }

        New-Item -ItemType Directory -Force -Path $target | Out-Null
        Copy-Item $source $exe -Force

        $installed = (& $exe version 2>$null | Select-Object -First 1)
        Write-Ok "installed $installed to $exe"

        Install-Completions $unpacked
        Add-ToUserPath $target

        Write-Host ''
        Write-Host 'Next: warmbly auth login'
        Write-Host "Docs: $Docs"
    } finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }
}

if ($Uninstall) { Invoke-Uninstall } else { Invoke-Install }
