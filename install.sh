#!/bin/bash
# dotgenie installer
# Usage: curl -fsSL https://raw.githubusercontent.com/simonm/dotgenie-cli/main/install.sh | bash

set -euo pipefail

REPO="${DOTGENIE_REPO:-simonm/dotgenie-cli}"
INSTALL_DIR="${DOTGENIE_INSTALL_DIR:-$HOME/.local/bin}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() { echo -e "${GREEN}[INFO]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; exit 1; }

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Linux*)  echo "linux" ;;
        Darwin*) echo "darwin" ;;
        *)       error "Unsupported OS: $(uname -s)" ;;
    esac
}

# Detect architecture
detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        armv7l)        echo "armv7" ;;
        *)             error "Unsupported architecture: $(uname -m)" ;;
    esac
}

# Get latest release version
get_latest_version() {
    curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | \
        grep '"tag_name":' | \
        sed -E 's/.*"([^"]+)".*/\1/'
}

main() {
    info "Installing dotgenie..."

    OS=$(detect_os)
    ARCH=$(detect_arch)
    info "Detected: ${OS}/${ARCH}"

    # Get version
    if [ -n "${DOTGENIE_VERSION:-}" ]; then
        VERSION="$DOTGENIE_VERSION"
    else
        info "Fetching latest version..."
        VERSION=$(get_latest_version)
        if [ -z "$VERSION" ]; then
            error "Could not determine latest version"
        fi
    fi
    info "Version: ${VERSION}"

    # Construct download URL
    FILENAME="dotgenie_${VERSION#v}_${OS}_${ARCH}.tar.gz"
    URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"

    # Create install directory
    mkdir -p "$INSTALL_DIR"

    # Download and extract
    info "Downloading ${URL}..."
    TEMP_DIR=$(mktemp -d)
    trap "rm -rf $TEMP_DIR" EXIT

    if ! curl -fsSL "$URL" -o "$TEMP_DIR/dotgenie.tar.gz"; then
        error "Failed to download dotgenie. Check if the release exists."
    fi

    info "Extracting..."
    tar -xzf "$TEMP_DIR/dotgenie.tar.gz" -C "$TEMP_DIR"

    # Install binary
    if [ -f "$TEMP_DIR/dotgenie" ]; then
        mv "$TEMP_DIR/dotgenie" "$INSTALL_DIR/dotgenie"
        chmod +x "$INSTALL_DIR/dotgenie"
    else
        error "Binary not found in archive"
    fi

    info "Installed to ${INSTALL_DIR}/dotgenie"

    # Check if in PATH
    if ! echo "$PATH" | tr ':' '\n' | grep -q "^${INSTALL_DIR}$"; then
        warn "Add ${INSTALL_DIR} to your PATH:"
        echo ""
        echo "  # Add to ~/.bashrc or ~/.zshrc:"
        echo "  export PATH=\"\$PATH:${INSTALL_DIR}\""
        echo ""
    fi

    # Verify installation
    if command -v dotgenie &> /dev/null; then
        info "Installation complete!"
        dotgenie version
    else
        info "Installation complete! Restart your shell or run:"
        echo "  export PATH=\"\$PATH:${INSTALL_DIR}\""
    fi

    echo ""
    info "Get started:"
    echo "  dotgenie init https://github.com/you/dotfiles"
    echo "  dotgenie apply"
}

main "$@"
