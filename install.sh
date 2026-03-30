#!/bin/bash
# Install script for Ralph Wiggum CLI (Go standalone binary)

set -euo pipefail

echo "Installing Ralph Wiggum CLI (Go standalone)..."

if ! command -v go >/dev/null 2>&1; then
  echo "Error: Go is required but not installed."
  echo "Install Go: https://go.dev/doc/install"
  exit 1
fi

# Agent CLIs are optional at install time, but warn early.
if ! command -v opencode >/dev/null 2>&1 && ! command -v claude >/dev/null 2>&1 && ! command -v codex >/dev/null 2>&1 && ! command -v copilot >/dev/null 2>&1; then
  echo "Warning: No supported agent CLI found yet (opencode, claude, codex, copilot)."
  echo "You can install one later and then run: ralph --help"
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
INSTALL_DIR="${RALPH_INSTALL_DIR:-$HOME/.local/bin}"

mkdir -p "$INSTALL_DIR"

echo "Building ralph binary..."
cd "$SCRIPT_DIR"
go build -o "$INSTALL_DIR/ralph" ./cmd/ralph
chmod +x "$INSTALL_DIR/ralph"

echo ""
echo "Installation complete!"
echo "Binary installed to: $INSTALL_DIR/ralph"
echo ""
echo "If '$INSTALL_DIR' is not on your PATH, add it with:"
echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
echo ""
echo "Usage:"
echo "  ralph \"Your task\" --max-iterations 10"
echo "  ralph --help"
echo ""
echo "Learn more: https://ghuntley.com/ralph/"
