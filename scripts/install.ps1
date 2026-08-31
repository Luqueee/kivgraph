#Requires -Version 5.1
<#
.SYNOPSIS
Install the latest published Kivgraph bundle for windows/amd64. Set
KIVGRAPH_VERSION to pin a stable or prerelease tag.

.DESCRIPTION
The Windows counterpart of scripts/install.sh, which cannot run where there is
no POSIX shell. It is a second implementation of one set of pre-extraction
checks, and that is its known cost: two implementations of the same rule drift.
The rules are stated once here in the same order and the same words as the
shell form uses, so a reader can put them side by side, and
install_parity_test.go asserts that neither grows a check the other lacks.

What it verifies, before anything is written outside a temporary directory:

  1. The archive's digest, against the line for this asset in the release's
     SHA256SUMS. A release lists every platform, so the whole file cannot be
     verified by a host that downloads one of them.
  2. Every path inside the archive, before extraction rather than after. An
     entry that escapes the bundle directory has already escaped it by the time
     an extractor has run.
  3. The bundle's own SHA256SUMS, over every file it carries.
  4. That the programs it promises are there.

It does not add anything to PATH. The shell installer does not either, and an
installer that edits an environment is one whose effects outlive an uninstall.
#>

[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Fail([string]$message) {
    Write-Error "install: $message"
    exit 1
}

# The platform is fixed rather than detected: this script only runs on Windows,
# and windows/arm64 is not published because LadybugDB ships no asset for it.
# Naming the limit beats letting a user discover it from a 404.
if ([System.Environment]::Is64BitOperatingSystem -eq $false) {
    Fail 'kivgraph is published for 64-bit Windows only'
}
$architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
if ($architecture -ne [System.Runtime.InteropServices.Architecture]::X64) {
    Fail "unsupported host windows/$architecture (published: windows/amd64; windows/arm64 has no pinned native library)"
}

$bundleName = 'kivgraph-windows-amd64'
$archiveName = "$bundleName.zip"

$installRoot = if ($env:KIVGRAPH_INSTALL_ROOT) { $env:KIVGRAPH_INSTALL_ROOT }
               else { Join-Path $env:LOCALAPPDATA 'Programs\kivgraph' }
$binDir = if ($env:KIVGRAPH_BIN_DIR) { $env:KIVGRAPH_BIN_DIR }
          else { Join-Path $env:LOCALAPPDATA 'Programs\kivgraph-bin' }
$releaseBase = if ($env:KIVGRAPH_RELEASE_BASE_URL) { $env:KIVGRAPH_RELEASE_BASE_URL.TrimEnd('/') }
               else { 'https://github.com/Luqueee/kivgraph/releases' }
$requestedVersion = $env:KIVGRAPH_VERSION

foreach ($pair in @(@{n = 'KIVGRAPH_INSTALL_ROOT'; v = $installRoot }, @{n = 'KIVGRAPH_BIN_DIR'; v = $binDir })) {
    if (-not [System.IO.Path]::IsPathRooted($pair.v)) { Fail "$($pair.n) must be an absolute path" }
    $trimmed = $pair.v.TrimEnd('\', '/')
    if ($trimmed.Length -le 2) { Fail "$($pair.n) must not be a drive root" }
}
if ($requestedVersion -and $requestedVersion -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$') {
    Fail "invalid KIVGRAPH_VERSION: $requestedVersion"
}

$downloadBase = if ($requestedVersion) { "$releaseBase/download/$requestedVersion" }
                else { "$releaseBase/latest/download" }

$staging = Join-Path ([System.IO.Path]::GetTempPath()) ("kivgraph-install-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $staging -Force | Out-Null
$backupRoot = ''
$newRootInstalled = $false
$createdLaunchers = New-Object System.Collections.Generic.List[string]

function Restore-OnFailure {
    # The install is a move, so undoing it is a move back. A partial install is
    # worse than no install: the binary and the worker have to be the pair the
    # bundle shipped, or the first index reports a protocol mismatch.
    foreach ($launcher in $createdLaunchers) {
        Remove-Item -LiteralPath $launcher -Force -ErrorAction SilentlyContinue
    }
    if ($script:newRootInstalled) {
        Remove-Item -LiteralPath $installRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
    if ($script:backupRoot -and (Test-Path -LiteralPath $script:backupRoot)) {
        Move-Item -LiteralPath $script:backupRoot -Destination $installRoot -Force -ErrorAction SilentlyContinue
    }
    Remove-Item -LiteralPath $staging -Recurse -Force -ErrorAction SilentlyContinue
}

function Get-Sha256([string]$path) {
    (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
}

# Test-BundleEntryPath is check 2, and it is a function so that the parity test
# has one name to point at.
#
# The rules are the shell form's, in its order: inside the bundle directory,
# not absolute, no drive, no traversal component, and no backslash -- a zip
# stores forward slashes, so a backslash is not a separator here but a
# character in a name, and a name carrying one was not written by this project.
function Test-BundleEntryPath([string]$name) {
    if ([string]::IsNullOrWhiteSpace($name)) { return $false }
    if ($name -ne $bundleName -and -not $name.StartsWith("$bundleName/")) { return $false }
    if ($name.Contains('\')) { return $false }
    if ($name.StartsWith('/')) { return $false }
    if ([System.IO.Path]::IsPathRooted($name)) { return $false }
    foreach ($component in $name.Split('/')) {
        if ($component -eq '..') { return $false }
    }
    return $true
}

try {
    Add-Type -AssemblyName System.IO.Compression.FileSystem

    $archivePath = Join-Path $staging $archiveName
    $checksumsPath = Join-Path $staging 'SHA256SUMS'
    Invoke-WebRequest -UseBasicParsing -Uri "$downloadBase/$archiveName" -OutFile $archivePath
    Invoke-WebRequest -UseBasicParsing -Uri "$downloadBase/SHA256SUMS" -OutFile $checksumsPath

    # Check 1. The release lists every published platform, so the line for this
    # asset is extracted rather than the file verified whole.
    # check: archive-digest
    $expected = $null
    foreach ($line in Get-Content -LiteralPath $checksumsPath) {
        $fields = $line -split '\s+', 2
        if ($fields.Count -eq 2 -and $fields[1].TrimStart('*', ' ') -eq $archiveName) {
            $expected = $fields[0].ToLowerInvariant()
        }
    }
    if (-not $expected) {
        Fail "the release publishes no checksum for $archiveName, so this platform is not in it"
    }
    $observed = Get-Sha256 $archivePath
    if ($observed -ne $expected) {
        Fail "archive checksum mismatch: expected $expected, observed $observed"
    }

    # Check 2, before a single byte is written outside the staging directory.
    $extractRoot = Join-Path $staging 'extracted'
    New-Item -ItemType Directory -Path $extractRoot -Force | Out-Null
    $zip = [System.IO.Compression.ZipFile]::OpenRead($archivePath)
    try {
        # check: entry-paths
        foreach ($entry in $zip.Entries) {
            if (-not (Test-BundleEntryPath $entry.FullName)) {
                # The backslash gets its own sentence because it is the one
                # a maintainer will hit rather than an attacker: a zip stores
                # forward slashes by specification, and .NET's
                # ZipFile.CreateFromDirectory writes the host's separator
                # instead. A release packed with it fails here, and the
                # message has to say which of the two problems this is.
                if ($entry.FullName.Contains('\')) {
                    Fail ("release archive stores a backslash in $($entry.FullName): " +
                        "a zip uses forward slashes, so this archive was packed by a tool that does not")
                }
                Fail "release archive contains unsafe path: $($entry.FullName)"
            }
        }
        foreach ($entry in $zip.Entries) {
            $destination = Join-Path $extractRoot ($entry.FullName -replace '/', '\')
            # check: entry-types
            if ($entry.FullName.EndsWith('/')) {
                New-Item -ItemType Directory -Path $destination -Force | Out-Null
                continue
            }
            New-Item -ItemType Directory -Path (Split-Path -Parent $destination) -Force | Out-Null
            [System.IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $destination, $true)
        }
    }
    finally {
        $zip.Dispose()
    }

    $bundle = Join-Path $extractRoot $bundleName
    if (-not (Test-Path -LiteralPath $bundle -PathType Container)) {
        Fail "release archive is missing $bundleName/"
    }
    # A reparse point is this platform's symbolic link, and the shell form
    # refuses one for the same reason: an installed tree that points somewhere
    # else is not the tree whose checksums were verified.
    # check: no-symlinks
    $links = Get-ChildItem -LiteralPath $bundle -Recurse -Force |
        Where-Object { $_.Attributes -band [System.IO.FileAttributes]::ReparsePoint }
    if ($links) {
        Fail 'release bundle contains reparse points'
    }

    # Check 3. Every file the bundle carries, against the manifest it carries.
    $manifest = Join-Path $bundle 'SHA256SUMS'
    if (-not (Test-Path -LiteralPath $manifest)) { Fail 'bundle is missing SHA256SUMS' }
    # check: bundle-checksums
    foreach ($line in Get-Content -LiteralPath $manifest) {
        if (-not $line.Trim()) { continue }
        $fields = $line -split '\s+', 2
        if ($fields.Count -ne 2) { Fail "bundle SHA256SUMS has a malformed line: $line" }
        $relative = $fields[1].TrimStart('*', ' ')
        $file = Join-Path $bundle ($relative -replace '/', '\')
        if (-not (Test-Path -LiteralPath $file)) { Fail "bundle is missing a file it lists: $relative" }
        if ((Get-Sha256 $file) -ne $fields[0].ToLowerInvariant()) {
            Fail "bundle checksum verification failed for $relative"
        }
    }

    # Check 4.
    # check: required-programs
    foreach ($program in @('bin\kivgraph.exe', 'bin\kivgraph-ts-worker.cmd', 'bin\lbug_shared.dll')) {
        if (-not (Test-Path -LiteralPath (Join-Path $bundle $program) -PathType Leaf)) {
            Fail "bundle is missing $program"
        }
    }

    # The Visual C++ runtime is a prerequisite rather than a payload: Windows
    # Update services the redistributable and services no copy of it, so a
    # security fix that reaches every other installation would not reach a
    # bundle that carried its own. ADR 0079 takes that trade.
    $runtime = @('vcruntime140.dll', 'vcruntime140_1.dll', 'msvcp140.dll')
    $missing = $runtime | Where-Object { -not (Test-Path (Join-Path $env:SystemRoot "System32\$_")) }
    if ($missing) {
        Write-Host "install: the Microsoft Visual C++ runtime is missing ($($missing -join ', ')); installing it"
        $redist = Join-Path $staging 'vc_redist.x64.exe'
        Invoke-WebRequest -UseBasicParsing -Uri 'https://aka.ms/vs/17/release/vc_redist.x64.exe' -OutFile $redist
        $process = Start-Process -FilePath $redist -ArgumentList '/install', '/quiet', '/norestart' -Wait -PassThru
        # 3010 is "installed, reboot required", which is a success this script
        # has no reason to refuse.
        if ($process.ExitCode -ne 0 -and $process.ExitCode -ne 3010) {
            Fail "the Visual C++ redistributable installer exited with $($process.ExitCode); kivgraph.exe will not start without it"
        }
    }

    $installedVersion = & (Join-Path $bundle 'bin\kivgraph.exe') version
    if ($LASTEXITCODE -ne 0 -or $installedVersion -notmatch '^[0-9]+\.[0-9]+\.[0-9]+') {
        Fail "bundle reported invalid version: $installedVersion"
    }
    # check: version-match
    if ($requestedVersion -and $requestedVersion.TrimStart('v') -ne $installedVersion) {
        Fail "bundle version $installedVersion does not match $requestedVersion"
    }

    New-Item -ItemType Directory -Path $binDir -Force | Out-Null
    New-Item -ItemType Directory -Path (Split-Path -Parent $installRoot) -Force | Out-Null

    # A launcher that is not ours is not replaced. The shell form reads the
    # target out of the script it is about to overwrite; this reads the same
    # thing out of the batch file, because the question is the same one: did
    # this installer write it.
    function Test-Launcher([string]$launcher, [string]$target) {
        if (-not (Test-Path -LiteralPath $launcher)) { return }
        $body = Get-Content -LiteralPath $launcher -Raw -ErrorAction SilentlyContinue
        if (-not $body -or -not $body.Contains($target)) {
            Fail "refusing to replace unrelated launcher: $launcher"
        }
    }
    # check: launcher-ownership
    Test-Launcher (Join-Path $binDir 'kivgraph.cmd') (Join-Path $installRoot 'bin\kivgraph.exe')
    Test-Launcher (Join-Path $binDir 'kivgraph-ts-worker.cmd') (Join-Path $installRoot 'bin\kivgraph-ts-worker.cmd')

    if (Test-Path -LiteralPath $installRoot) {
        $backupRoot = "$installRoot.previous"
        if (Test-Path -LiteralPath $backupRoot) {
            Fail "previous installation exists: $backupRoot"
        }
        Move-Item -LiteralPath $installRoot -Destination $backupRoot
    }
    Move-Item -LiteralPath $bundle -Destination $installRoot
    $newRootInstalled = $true

    foreach ($pair in @(
            @{ name = 'kivgraph.cmd'; target = (Join-Path $installRoot 'bin\kivgraph.exe') },
            @{ name = 'kivgraph-ts-worker.cmd'; target = (Join-Path $installRoot 'bin\kivgraph-ts-worker.cmd') })) {
        $launcher = Join-Path $binDir $pair.name
        if (Test-Path -LiteralPath $launcher) { continue }
        # %* forwards every argument unchanged, which is what this shim is for;
        # it does not consume one, so the batch trap that bites a shim with a
        # subcommand does not apply here.
        $body = "@echo off`r`nrem Managed by the Kivgraph release installer.`r`n`"$($pair.target)`" %*`r`n"
        Set-Content -LiteralPath $launcher -Value $body -Encoding ASCII -NoNewline
        $createdLaunchers.Add($launcher)
    }

    if ($backupRoot) {
        Remove-Item -LiteralPath $backupRoot -Recurse -Force
        $backupRoot = ''
    }
    Remove-Item -LiteralPath $staging -Recurse -Force -ErrorAction SilentlyContinue

    Write-Host "install: kivgraph $installedVersion is in $installRoot"
    $onPath = ($env:PATH -split ';') | Where-Object { $_.TrimEnd('\') -eq $binDir.TrimEnd('\') }
    if (-not $onPath) {
        Write-Host "install: $binDir is not on PATH. To add it for this account:"
        Write-Host "install:   setx PATH `"%PATH%;$binDir`""
    }

    # The install is finished above and stays finished whatever happens here.
    #
    # The same row `install.sh` sends, with the same fields, because two
    # installers reporting two shapes would be two datasets. `emitter` is
    # `installer` and never `binary`: a bundle can be installed and never
    # launched, and ADR 0083 keeps those two facts apart.
    #
    # The timeout and the swallowed error are the whole policy. A machine
    # behind a proxy installs Kivgraph exactly the same.
    if ($env:KIVGRAPH_TELEMETRY -ne '0') {
        Write-Host "install: reporting one install of $installedVersion on windows-amd64, and nothing else:"
        Write-Host "install:   nothing about your code, and no identifier of ours. Your address"
        Write-Host "install:   reaches the analytics collector, which hashes it and keeps a country."
        Write-Host "install:   https://kivgraph.dev/telemetry/"
        Write-Host "install:   set KIVGRAPH_TELEMETRY=0 to turn it off"
        try {
            $body = @{
                emitter  = 'installer'
                version  = $installedVersion
                platform = 'windows-amd64'
                channel  = 'installer'
            } | ConvertTo-Json -Compress
            Invoke-RestMethod -Method Post -TimeoutSec 3 -ContentType 'application/json' `
                -Uri 'https://kivgraph.dev/api/telemetry/first-run' -Body $body | Out-Null
        }
        catch {
            # An install is not a report, and a report that failed is not an
            # install that did.
        }
    }
}
catch {
    Restore-OnFailure
    Fail $_.Exception.Message
}
