#!/usr/bin/env bash
set -euo pipefail

# Grove installer — downloads the latest release binary from GitHub.
# Usage: curl -sSL https://raw.githubusercontent.com/lukemelnik/grove/main/scripts/install.sh | bash

REPO="lukemelnik/grove"
INSTALL_DIR="${GROVE_INSTALL_DIR:-/usr/local/bin}"

fail() { echo "Error: $1" >&2; exit 1; }

# Detect OS
case "$(uname -s)" in
  Darwin)  os="darwin" ;;
  Linux)   os="linux" ;;
  MINGW*|MSYS*|CYGWIN*) os="windows" ;;
  *) fail "unsupported OS: $(uname -s)" ;;
esac

# Detect architecture
case "$(uname -m)" in
  x86_64|amd64)  arch="x86_64" ;;
  arm64|aarch64)  arch="arm64" ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

# Fetch latest version tag
echo "Fetching latest Grove release..."
tag="$(curl -sSf "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' | head -1 | cut -d'"' -f4)"

[ -n "$tag" ] || fail "could not determine latest release"
version="${tag#v}"

# Build download URL
if [ "$os" = "windows" ]; then
  archive="grove_${version}_${os}_${arch}.zip"
else
  archive="grove_${version}_${os}_${arch}.tar.gz"
fi
url="https://github.com/${REPO}/releases/download/${tag}/${archive}"

# Download and extract
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

echo "Downloading grove ${version} (${os}/${arch})..."
curl -sSfL "$url" -o "${tmpdir}/${archive}" || fail "download failed — check that ${tag} has a release for ${os}/${arch}"

cd "$tmpdir"
if [ "$os" = "windows" ]; then
  unzip -q "$archive"
else
  tar xzf "$archive"
fi

# Install
if [ -w "$INSTALL_DIR" ]; then
  mv grove "$INSTALL_DIR/grove"
else
  echo "Installing to ${INSTALL_DIR} (requires sudo)..."
  sudo mv grove "$INSTALL_DIR/grove"
fi

echo "grove ${version} installed to ${INSTALL_DIR}/grove"
