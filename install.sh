#!/usr/bin/env bash
# 🦫 Castor Installer
# One-liner install:
#   curl -fsSL https://raw.githubusercontent.com/ostamand/castor/main/install.sh | bash

set -euo pipefail

REPO="ostamand/castor"
RAW_URL="https://raw.githubusercontent.com/${REPO}/main"
API_URL="https://api.github.com/repos/${REPO}/releases/latest"
BINARY_NAME="castor"

# Colors
BOLD="$(tput bold 2>/dev/null || echo '')"
AMBER="$(tput setaf 214 2>/dev/null || echo '')"
GREEN="$(tput setaf 34 2>/dev/null || echo '')"
RESET="$(tput sgr0 2>/dev/null || echo '')"

echo "${BOLD}${AMBER}"
cat << 'EOF'
   ____          _             
  / ___|__ _ ___| |_ ___  _ __ 
 | |   / _` / __| __/ _ \| '__|
 | |__| (_| \__ \ || (_) | |   
  \____\__,_|___/\__\___/|_|   
EOF
echo "${RESET}"
echo "${BOLD}🦫 Installing Castor — Developer Cold-Storage Vault & Archiver...${RESET}"
echo

# 1. Detect OS and Architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

case "$OS" in
  linux|darwin) ;;
  *)
    echo "Unsupported operating system: $OS" >&2
    exit 1
    ;;
esac

echo "Detected platform: ${BOLD}${OS}/${ARCH}${RESET}"

# 2. Determine installation directory
INSTALL_DIR="${CASTOR_INSTALL_DIR:-}"
USE_SUDO=false

if [ -z "$INSTALL_DIR" ]; then
  if [ -w "/usr/local/bin" ]; then
    INSTALL_DIR="/usr/local/bin"
  elif sudo -n true 2>/dev/null; then
    INSTALL_DIR="/usr/local/bin"
    USE_SUDO=true
  else
    INSTALL_DIR="${HOME}/.local/bin"
  fi
fi

mkdir -p "$INSTALL_DIR"

if [ ! -w "$INSTALL_DIR" ] && command -v sudo >/dev/null 2>&1; then
  USE_SUDO=true
fi

case ":$PATH:" in
  *:"$INSTALL_DIR":*) ;;
  *)
    echo "Note: ${INSTALL_DIR} is not currently in your \$PATH. Consider adding it to your ~/.bashrc or ~/.zshrc."
    ;;
esac

TMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

TARGET_BIN="${TMP_DIR}/${BINARY_NAME}"
DOWNLOADED=false

# 3. Check if running inside local castor repository
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd || pwd)"
if [ -f "${SCRIPT_DIR}/go.mod" ] && grep -q "github.com/${REPO}" "${SCRIPT_DIR}/go.mod" 2>/dev/null; then
  if command -v go >/dev/null 2>&1; then
    echo "Detected local Castor repository at ${SCRIPT_DIR}."
    echo "Building binary from local source..."
    (cd "$SCRIPT_DIR" && go build -ldflags="-s -w" -o "$TARGET_BIN" ./cmd/castor)
    DOWNLOADED=true
  fi
fi

# 4. Attempt to download latest precompiled release binary if not built locally
if [ "$DOWNLOADED" = false ]; then
  echo "Fetching latest release information from GitHub..."
  RELEASE_JSON="$(curl -sSL -H "Accept: application/vnd.github.v3+json" "$API_URL" 2>/dev/null || echo '{}')"
  TAG_NAME="$(echo "$RELEASE_JSON" | grep '"tag_name":' | head -n1 | cut -d '"' -f 4 || echo '')"

  ASSET_NAME="castor-${OS}-${ARCH}"
  DOWNLOAD_URL=""

  if [ -n "$TAG_NAME" ]; then
    DOWNLOAD_URL="$(echo "$RELEASE_JSON" | grep "browser_download_url.*${ASSET_NAME}" | head -n1 | cut -d '"' -f 4 || echo '')"
  fi

  if [ -n "$DOWNLOAD_URL" ]; then
    echo "Downloading ${TAG_NAME} (${ASSET_NAME})..."
    if curl -fsSL "$DOWNLOAD_URL" -o "$TARGET_BIN"; then
      chmod +x "$TARGET_BIN"
      DOWNLOADED=true
    fi
  fi
fi

# 5. Fallback if precompiled release asset is not yet available: build from Go remote
if [ "$DOWNLOADED" = false ]; then
  if command -v go >/dev/null 2>&1; then
    echo "No precompiled binary asset found for ${OS}/${ARCH}. Building from source with Go..."
    GOBIN="${TMP_DIR}" go install "github.com/${REPO}/cmd/castor@latest"
    if [ -f "${TMP_DIR}/castor" ]; then
      DOWNLOADED=true
    fi
  else
    echo "Error: Precompiled release asset not found, and 'go' is not installed." >&2
    echo "Please install Go (https://go.dev) or build from repository: https://github.com/${REPO}" >&2
    exit 1
  fi
fi

# 5. Install binary
echo "Installing binary to ${INSTALL_DIR}/${BINARY_NAME}..."
if [ "$USE_SUDO" = true ]; then
  sudo install -m 755 "$TARGET_BIN" "${INSTALL_DIR}/${BINARY_NAME}"
else
  install -m 755 "$TARGET_BIN" "${INSTALL_DIR}/${BINARY_NAME}"
fi

# 6. Install LLM Agent Skills into ~/.gemini/config/skills/
SKILLS_DIR="${HOME}/.gemini/config/skills"
echo "Installing Castor LLM agent skills to ${SKILLS_DIR}..."
mkdir -p "${SKILLS_DIR}/castor-cli" "${SKILLS_DIR}/castor-customizer"

if [ -d "${SCRIPT_DIR}/skills/castor-cli" ]; then
  cp -f "${SCRIPT_DIR}/skills/castor-cli/SKILL.md" "${SKILLS_DIR}/castor-cli/SKILL.md"
  cp -f "${SCRIPT_DIR}/skills/castor-customizer/SKILL.md" "${SKILLS_DIR}/castor-customizer/SKILL.md"
else
  curl -fsSL "${RAW_URL}/skills/castor-cli/SKILL.md" -o "${SKILLS_DIR}/castor-cli/SKILL.md" 2>/dev/null || true
  curl -fsSL "${RAW_URL}/skills/castor-customizer/SKILL.md" -o "${SKILLS_DIR}/castor-customizer/SKILL.md" 2>/dev/null || true
fi

echo
echo "${GREEN}${BOLD}✔ Castor installed successfully!${RESET}"
echo
echo "Quickstart:"
echo "  1. Run the interactive onboarding wizard:"
echo "     ${BOLD}castor init${RESET}"
echo "  2. Discover and register projects to back up:"
echo "     ${BOLD}castor add ~/Work/git -r --git-only${RESET}"
echo "  3. Inspect drift and remote sync status:"
echo "     ${BOLD}castor status${RESET}"
echo "  4. Turn on automated nightly backups:"
echo "     ${BOLD}castor schedule on${RESET}"
echo
