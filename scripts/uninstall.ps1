#Requires -Version 5.1
<#
.SYNOPSIS
Remove the Kivgraph bundle and its managed launchers.

.DESCRIPTION
Configuration, repository registrations and graph state are user data and are
preserved. Use -Yes for non-interactive use.
#>

[CmdletBinding()]
param([switch]$Yes)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Fail([string]$Message) {
    Write-Error "kivgraph uninstall: $Message"
    exit 1
}

$installRoot = if ($env:KIVGRAPH_INSTALL_ROOT) { $env:KIVGRAPH_INSTALL_ROOT }
               else { Join-Path $env:LOCALAPPDATA 'Programs\kivgraph' }
$binDir = if ($env:KIVGRAPH_BIN_DIR) { $env:KIVGRAPH_BIN_DIR }
          else { Join-Path $env:LOCALAPPDATA 'Programs\kivgraph-bin' }

foreach ($pair in @(
        @{ Name = 'KIVGRAPH_INSTALL_ROOT'; Value = $installRoot },
        @{ Name = 'KIVGRAPH_BIN_DIR'; Value = $binDir })) {
    if (-not [System.IO.Path]::IsPathRooted($pair.Value)) { Fail "$($pair.Name) must be absolute" }
}
try {
    $installRoot = [System.IO.Path]::GetFullPath($installRoot)
    $binDir = [System.IO.Path]::GetFullPath($binDir)
}
catch {
    Fail 'installation path is not valid'
}
foreach ($pair in @(
        @{ Name = 'KIVGRAPH_INSTALL_ROOT'; Value = $installRoot },
        @{ Name = 'KIVGRAPH_BIN_DIR'; Value = $binDir })) {
    if ($pair.Value.TrimEnd('\', '/').Length -le 2) { Fail "$($pair.Name) must not be a drive root" }
}

$root = Get-Item -LiteralPath $installRoot -Force -ErrorAction SilentlyContinue
$bundlePresent = $false
if ($null -ne $root) {
    if ($root.Attributes -band [System.IO.FileAttributes]::ReparsePoint) {
        Fail "refusing symbolic-link installation root: $installRoot"
    }
    if (-not $root.PSIsContainer) { Fail "installation root is not a directory: $installRoot" }
    if (-not (Test-Path -LiteralPath (Join-Path $installRoot 'manifest.json') -PathType Leaf) -or
        -not (Test-Path -LiteralPath (Join-Path $installRoot 'bin\kivgraph.exe') -PathType Leaf)) {
        Fail "refusing to remove a directory that is not a Kivgraph installation: $installRoot"
    }
    $bundlePresent = $true
}

function Test-ManagedLauncher([string]$Path, [string]$Target) {
    $item = Get-Item -LiteralPath $Path -Force -ErrorAction SilentlyContinue
    if ($null -eq $item) { return $false }
    if ($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) { return $false }
    if (-not $item.PSIsContainer) {
        $body = Get-Content -LiteralPath $Path -Raw -ErrorAction SilentlyContinue
        return [bool]($body -and $body.Contains($Target))
    }
    return $false
}

$launchers = @()
foreach ($pair in @(
        @{ Name = 'kivgraph.cmd'; Target = (Join-Path $installRoot 'bin\kivgraph.exe') },
        @{ Name = 'kivgraph-ts-worker.cmd'; Target = (Join-Path $installRoot 'bin\kivgraph-ts-worker.cmd') })) {
    $path = Join-Path $binDir $pair.Name
    if (Test-ManagedLauncher $path $pair.Target) {
        $launchers += $path
    }
    elseif ($null -ne (Get-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue)) {
        Fail "refusing to remove unrelated launcher: $path"
    }
}

if (-not $bundlePresent -and $launchers.Count -eq 0) {
    Write-Host 'kivgraph uninstall: no managed installation found'
    exit 0
}

if (-not $Yes) {
    $answer = Read-Host "Remove Kivgraph bundle $installRoot and $($launchers.Count) launcher(s)? [y/N]"
    if ($answer.ToLowerInvariant() -notin @('y', 'yes')) {
        Write-Host 'kivgraph uninstall: cancelled'
        exit 0
    }
}

foreach ($launcher in $launchers) {
    Remove-Item -LiteralPath $launcher -Force
}
if ($bundlePresent) { Remove-Item -LiteralPath $installRoot -Recurse -Force }

Write-Host 'kivgraph uninstall: removed the managed bundle and launchers'
Write-Host 'kivgraph uninstall: configuration and graph state were preserved'
