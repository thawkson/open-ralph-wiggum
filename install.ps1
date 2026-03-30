# Install script for Ralph Wiggum CLI (Windows, Go standalone binary)

$ErrorActionPreference = "Stop"

Write-Host "Installing Ralph Wiggum CLI (Go standalone)..."

# Check for Go
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
  Write-Error "Go is required but not installed. Install Go: https://go.dev/doc/install"
  exit 1
}

# Warn if no agent CLI is currently installed
$hasOpenCode = Get-Command opencode -ErrorAction SilentlyContinue
$hasClaude = Get-Command claude -ErrorAction SilentlyContinue
$hasCodex = Get-Command codex -ErrorAction SilentlyContinue
$hasCopilot = Get-Command copilot -ErrorAction SilentlyContinue
if (-not $hasOpenCode -and -not $hasClaude -and -not $hasCodex -and -not $hasCopilot) {
  Write-Warning "No supported agent CLI found yet (opencode, claude, codex, copilot)."
  Write-Warning "You can install one later and then run: ralph --help"
}

# Get script directory
$scriptDir = $PSScriptRoot
$installDir = if ($env:RALPH_INSTALL_DIR) { $env:RALPH_INSTALL_DIR } else { Join-Path $HOME "bin" }
$binaryPath = Join-Path $installDir "ralph.exe"

if (-not (Test-Path $installDir)) {
  New-Item -ItemType Directory -Path $installDir | Out-Null
}

# Build binary
Write-Host "Building ralph binary..."
Push-Location $scriptDir
go build -o $binaryPath ./cmd/ralph

Pop-Location

Write-Host ""
Write-Host "Installation complete!"
Write-Host "Binary installed to: $binaryPath"
Write-Host ""
Write-Host "If '$installDir' is not on PATH, add it in your shell profile."
Write-Host ""
Write-Host "Usage:"
Write-Host ""
Write-Host "  CLI Loop:"
Write-Host "    ralph \"Your task\" --max-iterations 10"
Write-Host "    ralph --help"
Write-Host ""
Write-Host "Learn more: https://ghuntley.com/ralph/"
