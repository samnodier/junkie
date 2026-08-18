#!/bin/sh
# Installs the junkie terminal client.
#
#   curl -fsSL https://raw.githubusercontent.com/samnodier/junkie/master/install.sh | sh
#
# Downloads the latest release for this machine and puts a binary called
# junkie on the PATH. Set JUNKIE_INSTALL_DIR to choose where; the default is
# ~/.local/bin, which needs no root.
set -eu

REPO=samnodier/junkie
INSTALL_DIR=${JUNKIE_INSTALL_DIR:-$HOME/.local/bin}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "junkie: no release build for $arch" >&2; exit 1 ;;
esac
case "$os" in
  linux | darwin) ;;
  *) echo "junkie: no release build for $os — try: go install github.com/$REPO/cmd/junkie-cli@latest" >&2; exit 1 ;;
esac

# Resolve the latest tag by following the redirect the releases page issues,
# so this needs no JSON parser and no API token.
latest=${JUNKIE_VERSION:-}
if [ -z "$latest" ]; then
  latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's|.*/||')
fi
if [ -z "$latest" ]; then
  echo "junkie: could not find the latest release" >&2
  exit 1
fi
# This repository releases more than the CLI — the TWA build is tagged
# `android` — and GitHub calls whichever was published most recently
# "latest". Only v* tags are CLI releases.
case "$latest" in
  v*) ;;
  *)
    echo "junkie: the latest release is '$latest', which is not a CLI release." >&2
    echo "junkie: pick one from https://github.com/$REPO/releases and set JUNKIE_VERSION=vX.Y.Z" >&2
    exit 1
    ;;
esac

archive="junkie_${os}_${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/$latest/$archive"

tmp=$(mktemp -d)
# Leave nothing behind, including when the download or the move fails.
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "junkie: downloading $latest for $os/$arch"
if ! curl -fsSL "$url" -o "$tmp/$archive"; then
  echo "junkie: no build at $url" >&2
  exit 1
fi

# Verify against the release's published checksums before running anything
# out of the archive.
if curl -fsSL "https://github.com/$REPO/releases/download/$latest/checksums.txt" -o "$tmp/checksums.txt"; then
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$tmp" && grep " $archive\$" checksums.txt | sha256sum -c -) >/dev/null
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$tmp" && grep " $archive\$" checksums.txt | shasum -a 256 -c -) >/dev/null
  else
    echo "junkie: no sha256 tool found — skipping checksum verification" >&2
  fi
else
  echo "junkie: checksums.txt not published — skipping verification" >&2
fi

tar -xzf "$tmp/$archive" -C "$tmp" junkie
mkdir -p "$INSTALL_DIR"
mv "$tmp/junkie" "$INSTALL_DIR/junkie"
chmod +x "$INSTALL_DIR/junkie"

echo "junkie: installed to $INSTALL_DIR/junkie"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "junkie: add $INSTALL_DIR to your PATH to run it by name" >&2 ;;
esac
echo "junkie: run 'junkie login' to get started"
