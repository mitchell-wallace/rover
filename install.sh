#!/usr/bin/env bash
set -euo pipefail

TOOL_NAME="rover"
REPO="mitchell-wallace/${TOOL_NAME}"
INSTALL_DIR="$HOME/.local/bin"

# Version resolution: positional arg > ROVER_VERSION env var > latest
VERSION="${1:-${ROVER_VERSION:-}}"

# Strip leading 'v' if present for consistency
VERSION="${VERSION#v}"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    linux) OS="linux" ;;
    darwin) OS="darwin" ;;
    *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# If no version specified, fetch latest release tag
if [ -z "${VERSION}" ]; then
    LATEST_URL="https://api.github.com/repos/${REPO}/releases/latest"
    TAG=$(curl -fsSL "$LATEST_URL" | grep '"tag_name":' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')

    if [ -z "$TAG" ]; then
        echo "Failed to fetch latest release tag"
        exit 1
    fi

    VERSION="${TAG#v}"
fi

ASSET="rover_${VERSION}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/v${VERSION}/${ASSET}"

echo "Installing ${TOOL_NAME} v${VERSION}..."
echo "Downloading ${ASSET}..."
curl -fsSL "$DOWNLOAD_URL" -o "/tmp/${ASSET}"

# Create install directory
mkdir -p "$INSTALL_DIR"

# Extract binary
tar -xzf "/tmp/${ASSET}" -C "$INSTALL_DIR" rover
chmod +x "$INSTALL_DIR/rover"
rm -f "/tmp/${ASSET}"

echo "Installed rover to ${INSTALL_DIR}/rover"

# Update PATH in shell configs if missing
add_to_path() {
    FILE="$1"
    LINE="export PATH=\"\$HOME/.local/bin:\$PATH\""
    if [ -f "$FILE" ]; then
        if ! grep -Fxq "$LINE" "$FILE"; then
            echo "$LINE" >> "$FILE"
            echo "Updated $FILE"
        fi
    fi
}

add_to_path "$HOME/.bashrc"
add_to_path "$HOME/.zshrc"

FISH_CONFIG="$HOME/.config/fish/config.fish"
if [ -f "$FISH_CONFIG" ]; then
    FISH_LINE="set -gx PATH \"\$HOME/.local/bin\" \$PATH"
    if ! grep -Fxq "$FISH_LINE" "$FISH_CONFIG"; then
        echo "$FISH_LINE" >> "$FISH_CONFIG"
        echo "Updated $FISH_CONFIG"
    fi
fi

echo "Installation complete. Restart your shell or run 'source ~/.bashrc' (or equivalent) to update PATH."
echo "Next: run 'rover doctor' to check prerequisites."
