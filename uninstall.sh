#!/bin/bash
# Uninstall script for Ralph Wiggum CLI (Go standalone binary)

set -euo pipefail

echo "Uninstalling Ralph Wiggum CLI..."

INSTALL_DIR="${RALPH_INSTALL_DIR:-$HOME/.local/bin}"
BINARY_PATH="$INSTALL_DIR/ralph"

if [ -f "$BINARY_PATH" ]; then
  rm -f "$BINARY_PATH"
  echo "Removed: $BINARY_PATH"
else
  echo "No binary found at: $BINARY_PATH"
fi

echo ""
echo "Uninstall complete!"
echo "You may also want to remove the cloned repository and .ralph state files."
