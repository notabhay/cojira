[CmdletBinding()]
param(
  [string]$InstallDir = $(if ($env:COJIRA_INSTALL_DIR) { $env:COJIRA_INSTALL_DIR } elseif ($env:GOBIN) { $env:GOBIN } else { Join-Path $HOME ".local\bin" }),
  [string]$Version = $(if ($env:COJIRA_VERSION) { $env:COJIRA_VERSION } else { "v0.3.0" }),
  [string]$DefaultConfluenceBaseUrl = $(if ($env:COJIRA_DEFAULT_CONFLUENCE_BASE_URL) { $env:COJIRA_DEFAULT_CONFLUENCE_BASE_URL } else { "https://confluence.example.com/confluence/" }),
  [string]$DefaultJiraBaseUrl = $(if ($env:COJIRA_DEFAULT_JIRA_BASE_URL) { $env:COJIRA_DEFAULT_JIRA_BASE_URL } else { "https://jira.example.com" })
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Write-Log {
  param([string]$Message)
  Write-Host $Message
}

function Fail {
  param([string]$Message)
  throw $Message
}

function Get-ScriptRoot {
  Split-Path -Parent $PSCommandPath
}

function Get-PlatformArch {
  $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
  switch ($arch) {
    "X64" { return "amd64" }
    "Arm64" { return "arm64" }
    default { Fail "Unsupported Windows architecture: $arch" }
  }
}

function Find-BundledBinary {
  param([string]$Root)

  $arch = Get-PlatformArch
  $candidates = @(
    (Join-Path $Root "bin\cojira-windows-$arch.exe"),
    (Join-Path $Root "bin\cojira.exe")
  )

  if (-not (Test-Path (Join-Path $Root "go.mod") -PathType Leaf)) {
    $candidates += @(
      (Join-Path $Root "cojira-windows-$arch.exe"),
      (Join-Path $Root "cojira.exe")
    )
  }

  foreach ($candidate in $candidates) {
    if (Test-Path $candidate -PathType Leaf) {
      return $candidate
    }
  }

  return $null
}

function Seed-EnvFile {
  param(
    [string]$Root,
    [string]$ConfluenceBaseUrl,
    [string]$JiraBaseUrl
  )

  $envPath = Join-Path $Root ".env"
  if (Test-Path $envPath -PathType Leaf) {
    return
  }

  $examplePath = Join-Path $Root ".env.example"
  if (Test-Path $examplePath -PathType Leaf) {
    Copy-Item $examplePath $envPath -Force
    return
  }

  $content = @"
# Confluence
CONFLUENCE_BASE_URL=$ConfluenceBaseUrl
CONFLUENCE_API_TOKEN=

# Jira
JIRA_BASE_URL=$JiraBaseUrl
JIRA_API_TOKEN=
"@
  Set-Content -Path $envPath -Value $content -NoNewline
}

function Add-InstallDirToPath {
  param([string]$Directory)

  $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
  $parts = @()
  if ($currentPath) {
    $parts = $currentPath.Split(';', [System.StringSplitOptions]::RemoveEmptyEntries)
  }

  if ($parts -contains $Directory) {
    return
  }

  $newPath = if ($currentPath) { "$currentPath;$Directory" } else { $Directory }
  [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
  Write-Log "Added $Directory to the user PATH. Open a new shell to pick it up."
}

function Refresh-WorkspacePrompts {
  param(
    [string]$BinaryPath,
    [string]$Root
  )

  if (-not (Test-Path $BinaryPath -PathType Leaf)) {
    Fail "Installed binary not found: $BinaryPath"
  }

  Write-Log "Refreshing workspace prompt files in: $Root"
  Push-Location $Root
  try {
    & $BinaryPath "bootstrap"
  } finally {
    Pop-Location
  }
}

function Cleanup-BundleWorkspace {
  param([string]$Root)

  Remove-Item -Recurse -Force -ErrorAction SilentlyContinue (Join-Path $Root "bin")
  Remove-Item -Recurse -Force -ErrorAction SilentlyContinue (Join-Path $Root "examples")

  if (-not (Test-Path (Join-Path $Root "go.mod") -PathType Leaf)) {
    Get-ChildItem -Path $Root -Filter "cojira-*" -File -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue
    Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $Root "cojira.exe")
  }

  Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $Root ".env.example")
  Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $Root "COJIRA-BOOTSTRAP.md")
  Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $Root "cojira.zip")
}

function Install-BundledBinary {
  param(
    [string]$Root,
    [string]$Destination
  )

  $bundledBinary = Find-BundledBinary -Root $Root
  if (-not $bundledBinary) {
    Fail "No bundled binary found for this platform."
  }

  New-Item -ItemType Directory -Force -Path $Destination | Out-Null
  $binaryPath = Join-Path $Destination "cojira.exe"

  Copy-Item $bundledBinary $binaryPath -Force
  Unblock-File -Path $binaryPath -ErrorAction SilentlyContinue

  Write-Log "Installed bundled binary: $binaryPath"
  & $binaryPath "--version"

  Refresh-WorkspacePrompts -BinaryPath $binaryPath -Root $Root
  Seed-EnvFile -Root $Root -ConfluenceBaseUrl $DefaultConfluenceBaseUrl -JiraBaseUrl $DefaultJiraBaseUrl
  Add-InstallDirToPath -Directory $Destination
  Cleanup-BundleWorkspace -Root $Root

  Write-Log ""
  Write-Log "Next:"
  Write-Log "  Open $Root\.env, fill in the Jira and Confluence tokens, then tell your agent to verify setup."
}

function Find-GoCommand {
  $goCommand = Get-Command go -ErrorAction SilentlyContinue
  if ($goCommand) {
    return $goCommand.Source
  }
  return $null
}

function Install-From-LocalSource {
  param(
    [string]$Root,
    [string]$Destination,
    [string]$RequestedVersion
  )

  $goCommand = Find-GoCommand
  if (-not $goCommand) {
    Fail "No bundled binary found and Go is not installed. Use a release bundle or install Go first."
  }

  if (-not (Test-Path (Join-Path $Root "go.mod") -PathType Leaf)) {
    Fail "No bundled binary found and the current directory is not a cojira source checkout."
  }

  New-Item -ItemType Directory -Force -Path $Destination | Out-Null
  $binaryPath = Join-Path $Destination "cojira.exe"
  $normalizedVersion = if ($RequestedVersion.StartsWith("v")) { $RequestedVersion.Substring(1) } else { $RequestedVersion }
  $ldflags = "-s -w -X github.com/notabhay/cojira/internal/version.Version=$normalizedVersion"

  Write-Log "Building cojira from local source with $goCommand..."
  Push-Location $Root
  try {
    & $goCommand "build" "-trimpath" "-ldflags" $ldflags "-o" $binaryPath "."
  } finally {
    Pop-Location
  }

  Write-Log "Installed: $binaryPath"
  & $binaryPath "--version"

  Refresh-WorkspacePrompts -BinaryPath $binaryPath -Root $Root
  Seed-EnvFile -Root $Root -ConfluenceBaseUrl $DefaultConfluenceBaseUrl -JiraBaseUrl $DefaultJiraBaseUrl
  Add-InstallDirToPath -Directory $Destination

  Write-Log ""
  Write-Log "Next:"
  Write-Log "  Open $Root\.env, fill in the Jira and Confluence tokens, then tell your agent to verify setup."
}

function Main {
  $root = Get-ScriptRoot
  $bundledBinary = Find-BundledBinary -Root $root
  if ($bundledBinary) {
    Install-BundledBinary -Root $root -Destination $InstallDir
    return
  }

  Install-From-LocalSource -Root $root -Destination $InstallDir -RequestedVersion $Version
}

Main
