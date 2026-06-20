#!/bin/sh
# install.sh — install taskbind globally
# Usage:
#   curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-taskbind/main/install.sh | sh
#   curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-taskbind/main/install.sh | sh -s -- -b /usr/local/bin

set -e

# --- defaults ---
DEFAULT_BINDIR="${HOME}/.local/bin"
BINDIR="${BINDIR:-$DEFAULT_BINDIR}"
REPO="mrSamDev/agentic-taskbind"
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
    URL="https://github.com/${REPO}/releases/latest/download/taskbind_${OS}_${ARCH}"
  else
    URL="https://github.com/${REPO}/releases/download/${VERSION}/taskbind_${OS}_${ARCH}"
  fi

  say "Downloading taskbind from $URL"
  TMPFILE=$(mktemp)
  if curl -sfL "$URL" -o "$TMPFILE" 2>/dev/null; then
    chmod +x "$TMPFILE"
    mkdir -p "$BINDIR"
    mv "$TMPFILE" "${BINDIR}/taskbind"
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
  git clone --depth=1 "https://github.com/${REPO}.git" "$TMPDIR/agentic-taskbind" 2>/dev/null

  say "Building taskbind…"
  cd "$TMPDIR/agentic-taskbind" && go build -o taskbind ./cmd/taskbind/

  mkdir -p "$BINDIR"
  mv "${TMPDIR}/agentic-taskbind/taskbind" "${BINDIR}/taskbind"
  say "Built ${BINDIR}/taskbind"
}

# --- 3. ensure BINDIR is on PATH in shell rc ---
ensure_on_path() {
  # resolve absolute path
  if [ -d "$BINDIR" ]; then
    ABS_BINDIR="$(cd "$BINDIR" 2>/dev/null && pwd)"
  else
    case "$BINDIR" in
      /*) ABS_BINDIR="$BINDIR" ;;
      *)  ABS_BINDIR="$(pwd)/$BINDIR" ;;
    esac
  fi

  # already on PATH — done
  case ":$PATH:" in
    *":${ABS_BINDIR}:"*) return 0 ;;
  esac

  LINE="export PATH=\"\$PATH:${ABS_BINDIR}\""

  case "$SHELL" in
    */zsh) RCFILE="$HOME/.zshrc" ;;
    */bash) RCFILE="$HOME/.bashrc" ;;
    *)     RCFILE="" ;;
  esac

  if [ -n "$RCFILE" ]; then
    if grep -qF "$ABS_BINDIR" "$RCFILE" 2>/dev/null; then
      say "taskbind already on PATH in $RCFILE"
    else
      echo "" >> "$RCFILE"
      echo "# taskbind" >> "$RCFILE"
      echo "$LINE" >> "$RCFILE"
      say "Added taskbind to PATH in $RCFILE"
      say "Run: source $RCFILE"
    fi
  else
    echo ""
    echo "  Add taskbind to your PATH by running:"
    echo "    $LINE"
    echo ""
  fi
}

# --- main ---
if download_binary; then
  say "Installed ${BINDIR}/taskbind (release)"
else
  build_from_source
fi

ensure_on_path

say "Done!"
echo ""
echo "Usage:"
echo ""
echo "  taskbind init              # set up taskbind in current project"
echo "  taskbind task dispatch ...  # create a task"
echo "  taskbind task claim-next ... # claim a task"
echo ""
echo "Docs: https://github.com/${REPO}"
echo ""
echo "Skills: https://github.com/${REPO}/tree/main/skills"