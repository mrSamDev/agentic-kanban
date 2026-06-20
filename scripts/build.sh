#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "→ Building taskbind binary..."
go build -ldflags="-s -w" -o "$ROOT/taskbind" ./cmd/taskbind/
echo "→ Done: $(file "$ROOT/taskbind" | awk -F: '{print $2}')"
echo "→ Size: $(du -sh "$ROOT/taskbind" | awk '{print $1}')"

INSTALL="${INSTALL:-$HOME/.local/bin}"
mkdir -p "$INSTALL"
cp "$ROOT/taskbind" "$INSTALL/taskbind"
echo "→ Installed to $INSTALL/taskbind"