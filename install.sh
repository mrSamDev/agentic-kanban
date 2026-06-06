#!/bin/sh
# install.sh — install kanban into your project
# Usage:
#   curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-kanban/main/install.sh | sh
#   curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-kanban/main/install.sh | sh -s -- -b /usr/local/bin

set -e

# --- defaults ---
BINDIR="${BINDIR:-".kanban"}"
REPO="mrSamDev/agentic-kanban"
VERSION="${VERSION:-latest}"

# --- colors ---
if [ -t 1 ]; then
  GREEN='\033[0;32m'; BOLD='\033[1m'; NC='\033[0m'
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

# --- 4. auto-export KANBAN_DB helper ---
add_kanban_db_to_profile() {
  PROFILE_FILE=""

  # Detect shell profile
  if [ -n "$ZSH_VERSION" ] || [ -f "$HOME/.zshrc" ]; then
    PROFILE_FILE="$HOME/.zshrc"
  elif [ -n "$BASH_VERSION" ] || [ -f "$HOME/.bashrc" ]; then
    PROFILE_FILE="$HOME/.bashrc"
  elif [ -f "$HOME/.profile" ]; then
    PROFILE_FILE="$HOME/.profile"
  fi

  if [ -z "$PROFILE_FILE" ]; then
    return
  fi

  # Check if already present
  if grep -q "export KANBAN_DB=" "$PROFILE_FILE" 2>/dev/null; then
    return
  fi

  # Append a default that users can override per-project
  cat >> "$PROFILE_FILE" << 'PROFILE_EOF'

# agentic-kanban: per-project db location (override per shell)
export KANBAN_DB="${KANBAN_DB:-"$(pwd)/.kanban/kanban.db"}"
PROFILE_EOF

  say "Added 'export KANBAN_DB=...' to $PROFILE_FILE"
  say "Reload: source $PROFILE_FILE"
}

# --- 5. create .kanban dir + init a sample task ---
init_board() {
  mkdir -p ".kanban"

  if [ ! -f ".kanban/kanban.db" ]; then
    "${BINDIR}/kanban" --db ".kanban/kanban.db" task dispatch \
      --title "Hello kanban" \
      --role worker \
      --priority 100 \
      --db ".kanban/kanban.db" 2>/dev/null || true
    say "Created .kanban/kanban.db with a sample task"
  fi
}

# --- main ---
if download_binary; then
  say "Installed ${BINDIR}/kanban (release)"
else
  build_from_source
fi

add_to_gitignore
add_kanban_db_to_profile

# Only auto-init when installing into project dir (not global)
case "$BINDIR" in
  .kanban)
    init_board
    ;;
esac

say "Done!"
echo ""
echo "Usage:"
echo "  cd my-project"
echo "  export KANBAN_DB=\"\$(pwd)/.kanban/kanban.db\""
echo "  ${BINDIR}/kanban --db \"\$KANBAN_DB\" task claim-next --agent my-agent --role worker"
echo ""
echo "Skills:"
echo "  https://github.com/${REPO}/tree/main/skills"