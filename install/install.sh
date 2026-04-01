#!/bin/sh
set -e

REPO="filfreire/idlebat"

# Detect OS
OS="$(uname -s)"
case "$OS" in
    Linux*)  OS=linux ;;
    Darwin*) OS=darwin ;;
    *)       echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64)  ARCH=amd64 ;;
    amd64)   ARCH=amd64 ;;
    aarch64) ARCH=arm64 ;;
    arm64)   ARCH=arm64 ;;
    *)       echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

ASSET="idlebat_${OS}_${ARCH}.tar.gz"
echo "Downloading idlebat for ${OS}/${ARCH}..."

# Get latest release download URL
URL="https://github.com/${REPO}/releases/latest/download/${ASSET}"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

curl -fsSL "$URL" -o "${TMPDIR}/${ASSET}"
tar -xzf "${TMPDIR}/${ASSET}" -C "$TMPDIR"

# Install to /usr/local/bin or ~/.local/bin
if [ -w /usr/local/bin ]; then
    INSTALL_DIR=/usr/local/bin
elif [ -d "$HOME/.local/bin" ]; then
    INSTALL_DIR="$HOME/.local/bin"
else
    mkdir -p "$HOME/.local/bin"
    INSTALL_DIR="$HOME/.local/bin"
fi

mv "${TMPDIR}/idlebat" "${INSTALL_DIR}/idlebat"
chmod +x "${INSTALL_DIR}/idlebat"

echo ""
echo "idlebat installed to ${INSTALL_DIR}/idlebat"
"${INSTALL_DIR}/idlebat" --version
echo ""

if [ "$INSTALL_DIR" = "$HOME/.local/bin" ]; then
    case ":$PATH:" in
        *":$HOME/.local/bin:"*) ;;
        *) echo "Add ~/.local/bin to your PATH:" ; echo "  export PATH=\"\$HOME/.local/bin:\$PATH\"" ;;
    esac
fi
