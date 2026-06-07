#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "→ Building kanban binary..."
go build -ldflags="-s -w" -o "$ROOT/kanban" ./cmd/kanban/
echo "→ Done: $(file "$ROOT/kanban" | awk -F: '{print $2}')"
echo "→ Size: $(du -sh "$ROOT/kanban" | awk '{print $1}')"

INSTALL="${INSTALL:-$HOME/.local/bin}"
mkdir -p "$INSTALL"
cp "$ROOT/kanban" "$INSTALL/kanban"
echo "→ Installed to $INSTALL/kanban"