#!/bin/sh
# install.sh — one-line installer for the OryxAI agent.
#
# Served from https://oryxai.dev/install via nginx alias to this file.
# Detects the user's OS/arch, downloads the matching oryxai binary from
# the GitHub release that matches this script's commit, verifies the
# SHA256 against the published SHA256SUMS, and drops the binary into
# /usr/local/bin (or $ORYXAI_BIN_DIR if set).
#
# Usage:
#   curl -fsSL https://oryxai.dev/install | sh
#
# Environment overrides:
#   ORYXAI_BIN_DIR=/path        install location (default: /usr/local/bin)
#   ORYXAI_VERSION=v0.1.2       pin a specific version (default: latest)
#   ORYXAI_REPO=OryxAIcode/Oryxai  override the GitHub repo
#   ORYXAI_NO_VERIFY=1          skip SHA256 verification (NOT RECOMMENDED)
#
# Security notes:
#   - The script is short and reviewable. Read it before piping to sh.
#   - SHA256SUMS is signed via cosign keyless in the release workflow;
#     this script verifies the SHA256 but does NOT verify cosign by
#     default (would require cosign on every user's box). Run
#     `oryxai verify-release` after install to validate cosign manually
#     (TODO: implement that subcommand).

set -eu

# ---- inputs / defaults --------------------------------------------------

BIN_DIR="${ORYXAI_BIN_DIR:-/usr/local/bin}"
REPO="${ORYXAI_REPO:-OryxAIcode/Oryxai}"
VERSION="${ORYXAI_VERSION:-latest}"

# ---- OS / arch detection ------------------------------------------------

uname_s="$(uname -s)"
case "$uname_s" in
  Darwin)  OS=darwin ;;
  Linux)   OS=linux ;;
  *)
    echo "oryxai install: unsupported OS: $uname_s" >&2
    echo "  Supported: macOS, Linux. Windows users: use WSL." >&2
    exit 1
    ;;
esac

uname_m="$(uname -m)"
case "$uname_m" in
  x86_64|amd64)   ARCH=amd64 ;;
  arm64|aarch64)  ARCH=arm64 ;;
  *)
    echo "oryxai install: unsupported arch: $uname_m" >&2
    exit 1
    ;;
esac

# ---- choose target tarball name ----------------------------------------

# Format must match .goreleaser.yaml archives.name_template for the
# 'oryxai' archive. Keep in sync.
ARCHIVE="oryxai_${OS}_${ARCH}.tar.gz"

# ---- resolve version ---------------------------------------------------

if [ "$VERSION" = "latest" ]; then
  # No tag-fetch network round-trip — just use the redirect target.
  RELEASE_URL="https://github.com/${REPO}/releases/latest/download/${ARCHIVE}"
  CHECKSUM_URL="https://github.com/${REPO}/releases/latest/download/SHA256SUMS"
else
  RELEASE_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"
  CHECKSUM_URL="https://github.com/${REPO}/releases/download/${VERSION}/SHA256SUMS"
fi

# ---- download to temp dir ----------------------------------------------

TMPDIR_PATH="$(mktemp -d 2>/dev/null || mktemp -d -t 'oryxai-install')"
trap 'rm -rf "$TMPDIR_PATH"' EXIT INT TERM

echo "→ Downloading $ARCHIVE for $OS/$ARCH …"
curl --fail --silent --show-error --location \
  --output "$TMPDIR_PATH/$ARCHIVE" \
  "$RELEASE_URL" || {
    echo "oryxai install: download failed from $RELEASE_URL" >&2
    exit 1
  }

# ---- verify SHA256 -----------------------------------------------------

if [ "${ORYXAI_NO_VERIFY:-0}" != "1" ]; then
  echo "→ Verifying SHA256 …"
  curl --fail --silent --show-error --location \
    --output "$TMPDIR_PATH/SHA256SUMS" "$CHECKSUM_URL" || {
      echo "oryxai install: could not download SHA256SUMS (set ORYXAI_NO_VERIFY=1 to skip)" >&2
      exit 1
    }
  # shasum is on macOS by default; sha256sum on most Linux.
  if command -v shasum >/dev/null 2>&1; then
    GOT="$(shasum -a 256 "$TMPDIR_PATH/$ARCHIVE" | awk '{print $1}')"
  elif command -v sha256sum >/dev/null 2>&1; then
    GOT="$(sha256sum "$TMPDIR_PATH/$ARCHIVE" | awk '{print $1}')"
  else
    echo "oryxai install: neither shasum nor sha256sum available; can't verify" >&2
    echo "  Set ORYXAI_NO_VERIFY=1 to install without verification (NOT RECOMMENDED)" >&2
    exit 1
  fi
  WANT="$(grep "  ${ARCHIVE}\$" "$TMPDIR_PATH/SHA256SUMS" | awk '{print $1}')"
  if [ -z "$WANT" ]; then
    echo "oryxai install: $ARCHIVE not present in SHA256SUMS file" >&2
    exit 1
  fi
  if [ "$GOT" != "$WANT" ]; then
    echo "oryxai install: SHA256 mismatch" >&2
    echo "  expected: $WANT" >&2
    echo "  got:      $GOT" >&2
    exit 1
  fi
  echo "✓ SHA256 OK"
fi

# ---- extract -----------------------------------------------------------

tar -xzf "$TMPDIR_PATH/$ARCHIVE" -C "$TMPDIR_PATH" oryxai

# ---- install (sudo only if BIN_DIR not writable) -----------------------

if [ -w "$BIN_DIR" ]; then
  install -m 0755 "$TMPDIR_PATH/oryxai" "$BIN_DIR/oryxai"
else
  echo "→ $BIN_DIR not writable; using sudo"
  sudo install -m 0755 "$TMPDIR_PATH/oryxai" "$BIN_DIR/oryxai"
fi

echo
echo "✓ Installed $BIN_DIR/oryxai"
"$BIN_DIR/oryxai" version
echo
echo "Next: run \`oryxai install\` and paste your API key from oryxai.dev"
