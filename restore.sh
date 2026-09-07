#!/usr/bin/env bash
# ==============================================================================
# 🦫 Castor Archive Decrypt & Restore Tool
# ==============================================================================
# Standalone, zero-dependency recovery script to decrypt and extract Castor
# archives using standard unix utilities (age, zstd, tar, git).
#
# Usage:
#   ./restore.sh [OPTIONS] <archive-file> [destination-directory]
#
# One-liner without cloning:
#   curl -fsSL https://raw.githubusercontent.com/ostamand/castor/main/restore.sh | bash -s -- <archive-file> [dest]
# ==============================================================================

set -euo pipefail

# Text formatting
BOLD="$(tput bold 2>/dev/null || echo '')"
GREEN="$(tput setaf 34 2>/dev/null || echo '')"
YELLOW="$(tput setaf 214 2>/dev/null || echo '')"
RED="$(tput setaf 196 2>/dev/null || echo '')"
CYAN="$(tput setaf 37 2>/dev/null || echo '')"
DIM="$(tput dim 2>/dev/null || echo '')"
RESET="$(tput sgr0 2>/dev/null || echo '')"

print_banner() {
  echo "${BOLD}${CYAN}🦫 Castor · Standalone Archive Restorer${RESET}"
  echo
}

usage() {
  cat << EOF
${BOLD}Usage:${RESET}
  $(basename "$0") [OPTIONS] <archive-file> [destination-directory]

${BOLD}Description:${RESET}
  Decrypts, decompresses, and reconstitutes Castor archives without needing
  the Castor CLI. Automatically handles Age decryption, Zstandard/XZ
  decompression, tar extraction, and full Git repository reconstitution.

${BOLD}Arguments:${RESET}
  <archive-file>           Path to the Castor archive (e.g. myproject.tar.zst.age)
  [destination-directory]  Directory to unpack into (default: ./<target-name>)

${BOLD}Options:${RESET}
  -k, --key <key-or-path>  Age secret key (AGE-SECRET-KEY-1...) or path to key file
  -f, --file <path>        Extract a single file to stdout (e.g. -f .env)
  -l, --list               List files in the archive without extracting
  -o, --force              Overwrite existing destination files without prompting
      --no-git             Skip Git repository reconstitution (.castor/repo.bundle)
  -v, --verbose            Show verbose extraction details
  -h, --help               Show this help message

${BOLD}Examples:${RESET}
  # Extract an archive to ./my-app
  ./restore.sh my-app.tar.zst.age

  # Extract to a specific directory with key file:
  ./restore.sh -k ~/.config/castor/keys.txt my-app.tar.zst.age ~/Projects/my-app

  # Pass secret key directly via environment variable:
  AGE_SECRET_KEY="AGE-SECRET-KEY-1..." ./restore.sh my-app.tar.zst.age

  # Print a single file (like .env or config.json) straight to stdout:
  ./restore.sh -f .env my-app.tar.zst.age > .env

  # Inspect contents without extracting:
  ./restore.sh -l my-app.tar.zst.age

EOF
  exit 0
}

# ------------------------------------------------------------------------------
# Arguments Parsing
# ------------------------------------------------------------------------------
KEY_INPUT="${AGE_SECRET_KEY:-${CASTOR_AGE_KEY:-}}"
EXTRACT_FILE=""
LIST_ONLY=false
FORCE=false
SKIP_GIT=false
VERBOSE=false
ARCHIVE_PATH=""
DEST_DIR=""

while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help)
      usage
      ;;
    -k|--key)
      shift
      [ $# -gt 0 ] || { echo "${RED}Error: --key requires an argument${RESET}" >&2; exit 1; }
      KEY_INPUT="$1"
      ;;
    -f|--file)
      shift
      [ $# -gt 0 ] || { echo "${RED}Error: --file requires an argument${RESET}" >&2; exit 1; }
      EXTRACT_FILE="$1"
      ;;
    -l|--list)
      LIST_ONLY=true
      ;;
    -o|--force|--overwrite)
      FORCE=true
      ;;
    --no-git)
      SKIP_GIT=true
      ;;
    -v|--verbose)
      VERBOSE=true
      ;;
    -*)
      echo "${RED}Error: Unknown option '$1'${RESET}" >&2
      echo "Run '$(basename "$0") --help' for usage." >&2
      exit 1
      ;;
    *)
      if [ -z "$ARCHIVE_PATH" ]; then
        ARCHIVE_PATH="$1"
      elif [ -z "$DEST_DIR" ]; then
        DEST_DIR="$1"
      else
        echo "${RED}Error: Unexpected extra argument '$1'${RESET}" >&2
        exit 1
      fi
      ;;
  esac
  shift
done

if [ -z "$ARCHIVE_PATH" ]; then
  print_banner
  echo "${RED}Error: No archive file specified.${RESET}" >&2
  echo "Usage: $(basename "$0") [OPTIONS] <archive-file> [destination-directory]" >&2
  echo "Run '$(basename "$0") --help' for options." >&2
  exit 1
fi

if [ ! -f "$ARCHIVE_PATH" ]; then
  echo "${RED}Error: Archive file not found: '$ARCHIVE_PATH'${RESET}" >&2
  exit 1
fi

# ------------------------------------------------------------------------------
# Dependency Verification & Automatic Helper
# ------------------------------------------------------------------------------
TEMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "$TEMP_DIR"
}
trap cleanup EXIT INT TERM

AGE_BIN=""
if command -v age >/dev/null 2>&1; then
  AGE_BIN="age"
elif command -v rage >/dev/null 2>&1; then
  AGE_BIN="rage"
fi

install_age_helper() {
  echo "${YELLOW}ℹ 'age' (encryption tool) is not installed in your PATH.${RESET}" >&2
  echo "  Castor archives are encrypted with Age (https://github.com/FiloSottile/age)." >&2
  echo >&2

  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
  esac

  # Offer automatic standalone binary download
  if command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1; then
    AUTO_DOWNLOAD=false
    if [ ! -t 0 ]; then
      # Non-interactive environment (CI, script, curl | bash): automatically download
      AUTO_DOWNLOAD=true
    else
      echo -n "${BOLD}Would you like to temporarily download the standalone 'age' binary? [Y/n] ${RESET}" >&2
      read -r resp
      case "$resp" in
        [nN][oO]|[nN])
          echo >&2
          echo "Please install age manually using your package manager:" >&2
          echo "  • macOS:        brew install age" >&2
          echo "  • Ubuntu/Debian: apt install age" >&2
          echo "  • Fedora:        dnf install age" >&2
          echo "  • Arch Linux:    pacman -S age" >&2
          exit 1
          ;;
        *)
          AUTO_DOWNLOAD=true
          ;;
      esac
    fi

    if [ "$AUTO_DOWNLOAD" = true ]; then
      echo "⬇ Downloading standalone age for ${OS}-${ARCH}..." >&2
      DL_URL="https://github.com/FiloSottile/age/releases/download/v1.3.2/age-v1.3.2-${OS}-${ARCH}.tar.gz"
      mkdir -p "$TEMP_DIR/age_bin"
      if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$DL_URL" | tar -xz -C "$TEMP_DIR/age_bin"
      else
        wget -qO- "$DL_URL" | tar -xz -C "$TEMP_DIR/age_bin"
      fi
      AGE_BIN="$TEMP_DIR/age_bin/age/age"
      chmod +x "$AGE_BIN"
      echo "${GREEN}✔ Standalone age downloaded successfully.${RESET}" >&2
      echo >&2
    fi
  else
    echo "${RED}Error: Neither 'age' nor 'curl/wget' found. Please install 'age'.${RESET}" >&2
    exit 1
  fi
}

# Determine if archive is Age-encrypted
IS_ENCRYPTED=false
case "$ARCHIVE_PATH" in
  *.age) IS_ENCRYPTED=true ;;
esac

if [ "$IS_ENCRYPTED" = true ] && [ -z "$AGE_BIN" ]; then
  install_age_helper
fi

# Determine decompression tool
DECOMPRESSOR="cat"
case "$ARCHIVE_PATH" in
  *.zst*|*.zstd*)
    if command -v zstd >/dev/null 2>&1; then
      DECOMPRESSOR="zstd -d -c -q"
    else
      echo "${RED}Error: 'zstd' is required to decompress this archive.${RESET}" >&2
      echo "Please install zstd (e.g. 'brew install zstd' or 'apt install zstd')." >&2
      exit 1
    fi
    ;;
  *.xz*)
    if command -v xz >/dev/null 2>&1; then
      DECOMPRESSOR="xz -d -c"
    else
      echo "${RED}Error: 'xz' is required to decompress this archive.${RESET}" >&2
      exit 1
    fi
    ;;
esac

if ! command -v tar >/dev/null 2>&1; then
  echo "${RED}Error: 'tar' utility is required.${RESET}" >&2
  exit 1
fi

# ------------------------------------------------------------------------------
# Secret Key Resolution
# ------------------------------------------------------------------------------
KEY_FILE=""
if [ "$IS_ENCRYPTED" = true ]; then
  # 1. Check if user passed an existing file path
  if [ -n "$KEY_INPUT" ] && [ -f "$KEY_INPUT" ]; then
    KEY_FILE="$KEY_INPUT"
  # 2. Check if user passed key string directly
  elif [ -n "$KEY_INPUT" ]; then
    KEY_FILE="$TEMP_DIR/key.txt"
    echo "$KEY_INPUT" > "$KEY_FILE"
    chmod 600 "$KEY_FILE"
  # 3. Check default Castor keys file if present
  elif [ -f "$HOME/.config/castor/keys.txt" ]; then
    KEY_FILE="$HOME/.config/castor/keys.txt"
  # 4. Prompt user interactively
  else
    if [ -t 0 ]; then
      echo -n "${BOLD}🔑 Enter Age Secret Key (AGE-SECRET-KEY-1...): ${RESET}"
      stty -echo 2>/dev/null || true
      read -r PROMPTED_KEY
      stty echo 2>/dev/null || true
      echo
      if [ -z "$PROMPTED_KEY" ]; then
        echo "${RED}Error: No secret key provided.${RESET}" >&2
        exit 1
      fi
      KEY_FILE="$TEMP_DIR/key.txt"
      echo "$PROMPTED_KEY" > "$KEY_FILE"
      chmod 600 "$KEY_FILE"
    else
      echo "${RED}Error: Archive is encrypted, but no secret key was provided.${RESET}" >&2
      echo "Use '-k <key>', or set AGE_SECRET_KEY='AGE-SECRET-KEY-1...'." >&2
      exit 1
    fi
  fi
fi

# ------------------------------------------------------------------------------
# Execute Operations
# ------------------------------------------------------------------------------

# Helper pipeline that outputs raw uncompressed tar stream to stdout
stream_tar() {
  if [ "$IS_ENCRYPTED" = true ]; then
    "$AGE_BIN" -d -i "$KEY_FILE" "$ARCHIVE_PATH" | $DECOMPRESSOR
  else
    $DECOMPRESSOR "$ARCHIVE_PATH"
  fi
}

# Mode 1: Single file extraction to stdout
if [ -n "$EXTRACT_FILE" ]; then
  # Clean leading ./ or / from query
  CLEAN_QUERY="${EXTRACT_FILE#./}"
  CLEAN_QUERY="${CLEAN_QUERY#/}"
  stream_tar | tar -xO "$CLEAN_QUERY" 2>/dev/null || {
    echo "${RED}Error: File '$EXTRACT_FILE' not found in archive.${RESET}" >&2
    exit 1
  }
  exit 0
fi

# Mode 2: List archive contents
if [ "$LIST_ONLY" = true ]; then
  print_banner
  echo "Contents of ${BOLD}$(basename "$ARCHIVE_PATH")${RESET}:"
  echo "───────────────────────────────────────────────────────────────────────"
  stream_tar | tar -tvf -
  exit 0
fi

# Mode 3: Full archive reconstitution
# Determine destination directory if omitted
if [ -z "$DEST_DIR" ]; then
  BASE="$(basename "$ARCHIVE_PATH")"
  # Strip known archive extensions
  TARGET_NAME="$BASE"
  TARGET_NAME="${TARGET_NAME%.age}"
  TARGET_NAME="${TARGET_NAME%.zst}"
  TARGET_NAME="${TARGET_NAME%.zstd}"
  TARGET_NAME="${TARGET_NAME%.xz}"
  TARGET_NAME="${TARGET_NAME%.tar}"
  DEST_DIR="./$TARGET_NAME"
fi

print_banner
echo "Source archive:      ${BOLD}$ARCHIVE_PATH${RESET}"
echo "Destination:         ${BOLD}$DEST_DIR${RESET}"
echo

if [ -d "$DEST_DIR" ] && [ "$(ls -A "$DEST_DIR" 2>/dev/null)" ]; then
  if [ "$FORCE" = false ]; then
    if [ -t 0 ]; then
      echo -n "${YELLOW}⚠️ Destination directory '$DEST_DIR' is not empty. Overwrite? [y/N]: ${RESET}"
      read -r confirm
      case "$confirm" in
        [yY][eE][sS]|[yY]) ;;
        *)
          echo "Restore cancelled."
          exit 0
          ;;
      esac
    else
      echo "${RED}Error: Destination directory '$DEST_DIR' is not empty (use --force to overwrite)${RESET}" >&2
      exit 1
    fi
  fi
fi

mkdir -p "$DEST_DIR"

echo "⏳ Decrypting and extracting files..."
if [ "$VERBOSE" = true ]; then
  stream_tar | tar -xv -C "$DEST_DIR"
else
  stream_tar | tar -x -C "$DEST_DIR"
fi

# ------------------------------------------------------------------------------
# Git Repository Reconstitution
# ------------------------------------------------------------------------------
BUNDLE_FILE="$DEST_DIR/.castor/repo.bundle"
if [ -f "$BUNDLE_FILE" ] && [ "$SKIP_GIT" = false ]; then
  if command -v git >/dev/null 2>&1; then
    echo "📦 Reconstituting Git repository from embedded bundle..."
    GIT_DIR="$DEST_DIR/.git"
    rm -rf "$GIT_DIR"

    # Clone bundle directly as bare mirror into .git
    git clone --mirror "$BUNDLE_FILE" "$GIT_DIR" >/dev/null 2>&1 || \
      git clone "$BUNDLE_FILE" "$GIT_DIR" >/dev/null 2>&1

    # Convert to standard working-tree git repo
    git -C "$DEST_DIR" config core.bare false

    # Determine default branch
    DEFAULT_BRANCH="$(git -C "$DEST_DIR" symbolic-ref --short HEAD 2>/dev/null || echo "main")"
    git -C "$DEST_DIR" checkout "$DEFAULT_BRANCH" >/dev/null 2>&1 || true
    git -C "$DEST_DIR" reset --mixed >/dev/null 2>&1 || true

    # Restore stash if present
    if git -C "$DEST_DIR" rev-parse --verify refs/stash >/dev/null 2>&1; then
      STASH_OID="$(git -C "$DEST_DIR" rev-parse --verify refs/stash 2>/dev/null || echo "")"
      if [ -n "$STASH_OID" ]; then
        git -C "$DEST_DIR" update-ref -m "restored stash" refs/stash "$STASH_OID" >/dev/null 2>&1 || true
      fi
    fi

    rm -rf "$DEST_DIR/.castor"
    echo "${GREEN}✔ Full Git history, branch heads, and stashes restored.${RESET}"
  else
    echo "${YELLOW}ℹ Note: '.castor/repo.bundle' found. Install Git to reconstruct full version history.${RESET}"
  fi
fi

echo
echo "${GREEN}${BOLD}✔ Restore complete!${RESET} Files extracted to: ${BOLD}$DEST_DIR${RESET}"
