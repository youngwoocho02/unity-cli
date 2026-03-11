#!/bin/sh
set -e

REPO="youngwoocho02/unity-cli"

# Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux)         OS="linux" ;;
  darwin)        OS="darwin" ;;
  mingw*|msys*)  OS="windows" ;;
  *)             echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)   ARCH="amd64" ;;
  aarch64|arm64)   ARCH="arm64" ;;
  *)               echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Binary name and install path
EXT=""
if [ "$OS" = "windows" ]; then
  EXT=".exe"
  INSTALL_DIR="$USERPROFILE/bin"
  mkdir -p "$INSTALL_DIR" 2>/dev/null || true
else
  INSTALL_DIR="/usr/local/bin"
fi

BINARY="unity-cli-${OS}-${ARCH}${EXT}"
URL="https://github.com/${REPO}/releases/latest/download/${BINARY}"

echo "Downloading unity-cli for ${OS}/${ARCH}..."
curl -fsSL "$URL" -o "/tmp/unity-cli${EXT}"

chmod +x "/tmp/unity-cli${EXT}"

if [ "$OS" = "windows" ]; then
  mv "/tmp/unity-cli${EXT}" "${INSTALL_DIR}/unity-cli${EXT}"
elif [ -w "$INSTALL_DIR" ]; then
  mv /tmp/unity-cli "$INSTALL_DIR/unity-cli"
else
  # Fallback to ~/.local/bin if no sudo available
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$INSTALL_DIR"
  mv /tmp/unity-cli "$INSTALL_DIR/unity-cli"

  # Add to PATH if not already there
  case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) echo "export PATH=\"$INSTALL_DIR:\$PATH\"" >> "$HOME/.profile"
       export PATH="$INSTALL_DIR:$PATH"
       echo "Added $INSTALL_DIR to PATH (restart shell or run: source ~/.profile)" ;;
  esac
fi

echo "unity-cli installed to ${INSTALL_DIR}/unity-cli${EXT}"
unity-cli version
