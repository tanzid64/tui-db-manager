#!/bin/sh
set -e

# dbk — POSIX installation script for Linux and macOS

REPO="${DBK_REPO:-tanzid64/tui-db-manager}"
INSTALL_DIR="${DBK_INSTALL_DIR:-}"

# Colors (disabled if not terminal or NO_COLOR set)
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  BOLD="$(printf '\033[1m')"
  GREEN="$(printf '\033[32m')"
  RED="$(printf '\033[31m')"
  YELLOW="$(printf '\033[33m')"
  RESET="$(printf '\033[0m')"
else
  BOLD=""
  GREEN=""
  RED=""
  YELLOW=""
  RESET=""
fi

log_info() {
  printf "%s==>%s %s%s\n" "$GREEN" "$RESET" "$BOLD" "$1" "$RESET"
}

log_warn() {
  printf "%sWARNING:%s %s\n" "$YELLOW" "$RESET" "$1"
}

log_error() {
  printf "%sERROR:%s %s\n" "$RED" "$RESET" "$1" >&2
}

# Handle --uninstall / -u flag
if [ "${1:-}" = "--uninstall" ] || [ "${1:-}" = "-u" ]; then
  log_info "Initiating clean uninstallation of dbk..."
  
  # 1. If dbk command is available, invoke its native uninstall to clean up schedules & data
  if command -v dbk >/dev/null 2>&1; then
    log_info "Running 'dbk uninstall --all --yes'..."
    dbk uninstall --all --yes 2>/dev/null || true
  fi

  # 2. Remove binary locations
  for b in "/usr/local/bin/dbk" "$HOME/.local/bin/dbk" "${INSTALL_DIR:-}/dbk"; do
    if [ -f "$b" ]; then
      log_info "Removing binary: $b"
      rm -f "$b" 2>/dev/null || sudo rm -f "$b" 2>/dev/null || log_warn "Could not remove $b. Try: sudo rm $b"
    fi
  done

  # 3. Clean configuration & database directory
  CONFIG_DIR="${DBK_CONFIG_DIR:-$HOME/.config/dbk}"
  if [ -d "$CONFIG_DIR" ]; then
    log_info "Removing configuration and database directory: $CONFIG_DIR"
    rm -rf "$CONFIG_DIR"
  fi

  # 4. Clean any residual systemd user units
  if [ -d "$HOME/.config/systemd/user" ]; then
    rm -f "$HOME/.config/systemd/user/dbk-"*.service "$HOME/.config/systemd/user/dbk-"*.timer 2>/dev/null || true
    if command -v systemctl >/dev/null 2>&1; then
      systemctl --user daemon-reload 2>/dev/null || true
    fi
  fi

  log_info "Clean uninstall complete! All dbk binaries, schedules, and data have been removed."
  exit 0
fi

# 1. OS Detection
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux*)  OS="linux" ;;
  darwin*) OS="darwin" ;;
  *)
    log_error "Unsupported operating system: $OS. dbk supports Linux and macOS."
    exit 1
    ;;
esac

# 2. Arch Detection
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    log_error "Unsupported architecture: $ARCH. dbk supports amd64 and arm64."
    exit 1
    ;;
esac

# 3. Resolve Target Version
if [ -z "${VERSION:-}" ] || [ "$VERSION" = "latest" ]; then
  log_info "Detecting latest release of $REPO..."
  VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || true)"
  if [ -z "$VERSION" ]; then
    log_error "No published release found on GitHub for $REPO."
    log_error "Please ensure a release tag (e.g. v1.0.0) has been pushed to GitHub."
    log_error "To install directly from source, run: go install github.com/$REPO/cmd/dbk@latest"
    exit 1
  fi
fi

# Strip leading 'v' if present for asset naming template
VERSION_NUM="$(echo "$VERSION" | sed 's/^v//')"
ASSET_NAME="dbk_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET_NAME"
CHECKSUM_URL="https://github.com/$REPO/releases/download/$VERSION/checksums.txt"

# 4. Prepare Temp Directory
TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'dbk')"
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

log_info "Downloading dbk $VERSION for $OS/$ARCH..."
if ! curl -fsSL "$DOWNLOAD_URL" -o "$TMP_DIR/$ASSET_NAME"; then
  log_error "Failed to download $DOWNLOAD_URL"
  log_error "Ensure a GitHub Release with assets exists for version $VERSION."
  log_error "To install directly from source, run: go install github.com/$REPO/cmd/dbk@latest"
  exit 1
fi

# 5. Checksum Verification
if curl -fsSL "$CHECKSUM_URL" -o "$TMP_DIR/checksums.txt" 2>/dev/null; then
  log_info "Verifying SHA-256 checksum..."
  EXPECTED_SHA="$(grep "$ASSET_NAME" "$TMP_DIR/checksums.txt" | awk '{print $1}')"
  if [ -n "$EXPECTED_SHA" ]; then
    if command -v sha256sum >/dev/null 2>&1; then
      ACTUAL_SHA="$(sha256sum "$TMP_DIR/$ASSET_NAME" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
      ACTUAL_SHA="$(shasum -a 256 "$TMP_DIR/$ASSET_NAME" | awk '{print $1}')"
    else
      ACTUAL_SHA=""
    fi

    if [ -n "$ACTUAL_SHA" ]; then
      if [ "$EXPECTED_SHA" != "$ACTUAL_SHA" ]; then
        log_error "Checksum verification failed!"
        log_error "Expected: $EXPECTED_SHA"
        log_error "Actual:   $ACTUAL_SHA"
        exit 1
      fi
      log_info "Checksum verified: $ACTUAL_SHA"
    fi
  fi
fi

# 6. Extract Archive
log_info "Extracting binary..."
tar -xzf "$TMP_DIR/$ASSET_NAME" -C "$TMP_DIR"
if [ ! -f "$TMP_DIR/dbk" ]; then
  log_error "Extraction failed: dbk binary not found in archive."
  exit 1
fi
chmod +x "$TMP_DIR/dbk"

# 7. Select Installation Directory
if [ -z "$INSTALL_DIR" ]; then
  if [ -w "/usr/local/bin" ] || [ "$(id -u)" -eq 0 ]; then
    INSTALL_DIR="/usr/local/bin"
  else
    INSTALL_DIR="$HOME/.local/bin"
  fi
fi

mkdir -p "$INSTALL_DIR"
log_info "Installing to $INSTALL_DIR/dbk..."
mv "$TMP_DIR/dbk" "$INSTALL_DIR/dbk"

# 8. Check PATH
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    log_warn "$INSTALL_DIR is not in your current PATH."
    log_warn "Add it to your shell configuration file (e.g. ~/.bashrc or ~/.zshrc):"
    log_warn "  export PATH=\"\$PATH:$INSTALL_DIR\""
    ;;
esac

log_info "Installation complete! Run 'dbk' to start the interactive backup manager."
