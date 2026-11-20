#!/usr/bin/env bash
# Install ai-critic-server from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/xhd2015/ai-critic/master/install.sh | bash
#   INSTALL_TAG=v0.0.75 bash install.sh
set -euo pipefail

REPO="https://github.com/xhd2015/ai-critic"
BINARY_NAME="ai-critic-server"

error() {
    echo "Error: $*" >&2
    exit 1
}

# Check if curl is available
if ! command -v curl &> /dev/null; then
    echo "Error: curl is required but not installed."
    echo ""
    echo "Install curl with one of the following commands:"
    echo "  Ubuntu/Debian:  sudo apt-get install -y curl"
    echo "  CentOS/RHEL:    sudo yum install -y curl"
    echo "  Fedora:         sudo dnf install -y curl"
    echo "  Alpine:         apk add curl"
    echo "  macOS:          brew install curl"
    exit 1
fi

# Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
    linux)   OS="linux" ;;
    darwin)  OS="darwin" ;;
    *)
        error "unsupported OS: $OS (pre-built binaries exist for linux and darwin)"
        ;;
esac

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64)   ARCH="amd64" ;;
    aarch64|arm64)  ARCH="arm64" ;;
    *)
        error "unsupported architecture: $ARCH (pre-built binaries exist for amd64 and arm64)"
        ;;
esac

# Asset names embed the release tag: ai-critic-server-v0.0.75-linux-amd64
if [ -n "${INSTALL_TAG:-}" ]; then
    TAG="${INSTALL_TAG}"
    if [ "${TAG#v}" = "$TAG" ]; then
        TAG="v${TAG}"
    fi
else
    LATEST_URL="${REPO}/releases/latest"
    echo "Fetching latest release from ${LATEST_URL}..."
    LOCATION=$(curl -sI "$LATEST_URL" | grep -i '^location: ' | tail -1 | tr -d '\r')
    if [ -z "$LOCATION" ]; then
        error "could not resolve the latest release from ${LATEST_URL}"
    fi
    TAG="${LOCATION##*/}"
fi

BASE_URL="${REPO}/releases/download/${TAG}"
ASSET_NAME="${BINARY_NAME}-${TAG}-${OS}-${ARCH}"
DOWNLOAD_URL="${BASE_URL}/${ASSET_NAME}"

echo "Detected: ${OS}/${ARCH}"
echo "Downloading: ${ASSET_NAME}"

OUTPUT="./${BINARY_NAME}"
curl -fSL -o "$OUTPUT" "$DOWNLOAD_URL" || error "failed to download ${DOWNLOAD_URL}"
chmod +x "$OUTPUT"

echo ""
echo "Downloaded: ${OUTPUT}"
echo ""
echo "To start the server, run:"
echo "  ./${BINARY_NAME} keep-alive"
