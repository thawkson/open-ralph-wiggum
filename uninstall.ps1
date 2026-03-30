# Uninstall script for Ralph Wiggum CLI (Windows, Go standalone binary)

$ErrorActionPreference = "Stop"

Write-Host "Uninstalling Ralph Wiggum CLI..."

$installDir = if ($env:RALPH_INSTALL_DIR) { $env:RALPH_INSTALL_DIR } else { Join-Path $HOME "bin" }
$binaryPath = Join-Path $installDir "ralph.exe"

if (Test-Path $binaryPath) {
  Remove-Item $binaryPath -Force
  Write-Host "Removed: $binaryPath"
} else {
  Write-Host "No binary found at: $binaryPath"
}

Write-Host ""
Write-Host "Uninstall complete!"
Write-Host "You may also want to remove the cloned repository and .ralph state files."
