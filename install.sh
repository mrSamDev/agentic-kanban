#!/bin/sh
# install.sh — install kanban into your project
# Usage: curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-kanban/main/install.sh | sh
#        curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-kanban/main/install.sh | sh -s -- -b /usr/local/bin

set -e

# --- defaults ---
BINDIR="${BINDIR:-".kanban"}"
REPO="mrSamDev/agentic-kanban"
VERSION="${VERSION:-latest}"

# --- colors (optional) ---
if [ -t 1 ]; then
  GREEN='\033[0;32m'
  BOLD='\033[1m'
  NC='\033[0m'
else
  GREEN=''; BOLD=''; NC=''
fi

say() { printf "${GREEN}==>${NC} ${BOLD}%s${NC}\n" "$*"; }

# --- parse flags ---
while getopts "b:v:" o; do
  case "$o" in
    b) BINDIR="$OPTARG" ;;
    v) VERSION="$OPTARG" ;;
    *) echo "Usage: $0 [-b bindir] [-v version]" >&2; exit 1 ;;
  esac
done

# --- detect os/arch ---
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
esac

# --- 1. try pre-built binary from GitHub releases ---
download_binary() {
  if [ "$VERSION" = "latest" ]; then
    URL="https://github.com/${REPO}/releases/latest/download/kanban_${OS}_${ARCH}"
  else
    URL="https://github.com/${REPO}/releases/download/${VERSION}/kanban_${OS}_${ARCH}"
  fi

  say "Downloading kanban from $URL"
  TMPFILE=$(mktemp)
  if curl -sfL "$URL" -o "$TMPFILE" 2>/dev/null; then
    chmod +x "$TMPFILE"
    mkdir -p "$BINDIR"
    mv "$TMPFILE" "${BINDIR}/kanban"
    return 0
  fi
  rm -f "$TMPFILE"
  return 1
}

# --- 2. fallback: build from source ---
build_from_source() {
  say "No release binary found. Building from source (requires Go)…"

  if ! command -v go >/dev/null 2>&1; then
    echo "Error: go is not installed. Install Go or wait until a release is published." >&2
    exit 1
  fi

  TMPDIR=$(mktemp -d)
  trap 'rm -rf "$TMPDIR"' EXIT

  say "Cloning ${REPO}…"
  git clone --depth=1 "https://github.com/${REPO}.git" "$TMPDIR/agentic-kanban" 2>/dev/null

  say "Building kanban…"
  cd "$TMPDIR/agentic-kanban" && go build -o kanban ./cmd/kanban/

  mkdir -p "$BINDIR"
  mv "${TMPDIR}/agentic-kanban/kanban" "${BINDIR}/kanban"
  say "Built ${BINDIR}/kanban"
}

# --- 3. .gitignore helper ---
add_to_gitignore() {
  GITIGNORE="$(pwd)/.gitignore"
  ENTRY="$(echo "$BINDIR" | sed 's:^\./::')/kanban"

  if [ -f "$GITIGNORE" ]; then
    if ! grep -qF "$ENTRY" "$GITIGNORE" 2>/dev/null; then
      echo "$ENTRY" >> "$GITIGNORE"
      say "Added '$ENTRY' to .gitignore"
    fi
  else
    echo "$ENTRY" > "$GITIGNORE"
    say "Created .gitignore with '$ENTRY'"
  fi
}

# --- main ---
if download_binary; then
  say "Installed ${BINDIR}/kanban (release)"
else
  build_from_source
fi

add_to_gitignore

echo ""
say "kanban installed at $(cd "$BINDIR" && pwd)/kanban"
echo ""
echo "Quick start:"
echo "  export KANBAN_DB=\"\$(pwd)/.kanban/kanban.db\""
echo "  ${BINDIR}/kanban --db \"\$KANBAN_DB\" task dispatch --title \"My first task\" --role worker"
echo "  ${BINDIR}/kanban --db \"\$KANBAN_DB\" task claim-next --agent my-agent --role worker"
echo ""
echo "Skills (markdown docs):"
echo "  https://github.com/${REPO}/tree/main/skills"