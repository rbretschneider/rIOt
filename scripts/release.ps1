#Requires -Version 5.1
<#
.SYNOPSIS
    Automates rIOt releases: stages all changes, commits, tags, and pushes.

.DESCRIPTION
    Finds the latest semver tag, bumps the version, commits all staged/unstaged
    changes, creates an annotated tag, and pushes to origin with tags.

.PARAMETER Bump
    Which semver component to increment: major, minor, or patch (default: patch).

.PARAMETER Message
    Custom commit message. If omitted, defaults to the tag name (e.g. "v2.34.0").

.PARAMETER DryRun
    Show what would happen without making any changes.

.EXAMPLE
    .\scripts\release.ps1                        # patch bump, auto-commit
    .\scripts\release.ps1 -Bump minor            # minor bump
    .\scripts\release.ps1 -Bump patch -Message "Fix auth race condition"
    .\scripts\release.ps1 -DryRun                # preview only
#>
param(
    [ValidateSet('major', 'minor', 'patch')]
    [string]$Bump = 'patch',

    [string]$Message,

    [switch]$DryRun
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# --- Ensure we're in a git repo on the main branch ---
$branch = git rev-parse --abbrev-ref HEAD 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Error "Not a git repository."
    exit 1
}
if ($branch -ne 'main') {
    Write-Warning "Current branch is '$branch', not 'main'. Continue? (y/N)"
    $answer = Read-Host
    if ($answer -notin @('y', 'Y', 'yes')) { exit 0 }
}

# --- Find latest semver tag ---
$tags = git tag --sort=-v:refname 2>&1
$latest = ($tags | Where-Object { $_ -match '^v\d+\.\d+\.\d+$' }) | Select-Object -First 1

if (-not $latest) {
    Write-Error "No semver tags found (expected vMAJOR.MINOR.PATCH)."
    exit 1
}

if ($latest -notmatch '^v(\d+)\.(\d+)\.(\d+)$') {
    Write-Error "Could not parse tag: $latest"
    exit 1
}

$major = [int]$Matches[1]
$minor = [int]$Matches[2]
$patch = [int]$Matches[3]

switch ($Bump) {
    'major' { $major++; $minor = 0; $patch = 0 }
    'minor' { $minor++; $patch = 0 }
    'patch' { $patch++ }
}

$newTag = "v$major.$minor.$patch"

# --- Check for changes ---
$status = git status --porcelain 2>&1
if (-not $status) {
    Write-Host "No changes to commit. Tagging HEAD as $newTag."
    $commitNeeded = $false
} else {
    $commitNeeded = $true
}

# --- Determine commit message ---
if (-not $Message) {
    $Message = $newTag
}

# --- Preview ---
Write-Host ""
Write-Host "  Latest tag:  $latest"
Write-Host "  New tag:     $newTag  ($Bump bump)"
if ($commitNeeded) {
    Write-Host "  Commit msg:  $Message"
}
Write-Host ""

if ($DryRun) {
    Write-Host "[DRY RUN] No changes made."
    exit 0
}

# --- Execute ---
if ($commitNeeded) {
    git add -A
    if ($LASTEXITCODE -ne 0) { Write-Error "git add failed"; exit 1 }

    git commit -m $Message
    if ($LASTEXITCODE -ne 0) { Write-Error "git commit failed"; exit 1 }
}

git tag -a $newTag -m $newTag
if ($LASTEXITCODE -ne 0) { Write-Error "git tag failed"; exit 1 }

git push origin main --tags
if ($LASTEXITCODE -ne 0) { Write-Error "git push failed"; exit 1 }

Write-Host ""
Write-Host "Released $newTag" -ForegroundColor Green
