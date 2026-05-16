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
#   ORYXAI_BIN_DIR=/path        install location (default: auto-detected)
#   ORYXAI_USE_SUDO=1           allow sudo fallback when no writable dir found
#   ORYXAI_VERSION=v0.1.2       pin a specific version (default: latest)
#   ORYXAI_REPO=OryxAIcode/Oryxai  override the GitHub repo
#   ORYXAI_NO_VERIFY=1          skip SHA256 verification (NOT RECOMMENDED)
#
# Install location resolution (in order):
#   1. $ORYXAI_BIN_DIR if set
#   2. /usr/local/bin if writable without sudo
#   3. $HOME/.local/bin (created if missing) — POSIX user-bin convention
#
# Sudo is NEVER used unless ORYXAI_USE_SUDO=1 is explicitly set. We've
# seen too many users mistake the sudo prompt for an API-key prompt.
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

REPO="${ORYXAI_REPO:-OryxAIcode/Oryxai}"
VERSION="${ORYXAI_VERSION:-latest}"

# Resolve BIN_DIR. Order: explicit override → writable /usr/local/bin →
# ~/.local/bin. We intentionally do NOT auto-sudo: users typing their
# API key into a sudo prompt is the most common confused-UX failure
# mode we've seen.
if [ -n "${ORYXAI_BIN_DIR:-}" ]; then
  BIN_DIR="$ORYXAI_BIN_DIR"
elif [ -w /usr/local/bin ]; then
  BIN_DIR="/usr/local/bin"
else
  BIN_DIR="$HOME/.local/bin"
fi

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

# ---- install -----------------------------------------------------------
# Try BIN_DIR. If it's not writable, create it (for $HOME/.local/bin).
# Only fall back to sudo when the user explicitly opted in with
# ORYXAI_USE_SUDO=1 — we'd rather print a clear next-step than ask for
# a password the user wasn't expecting.

mkdir -p "$BIN_DIR" 2>/dev/null || true

if [ -w "$BIN_DIR" ]; then
  install -m 0755 "$TMPDIR_PATH/oryxai" "$BIN_DIR/oryxai"
elif [ "${ORYXAI_USE_SUDO:-0}" = "1" ]; then
  echo "→ $BIN_DIR not writable; using sudo (ORYXAI_USE_SUDO=1)"
  sudo install -m 0755 "$TMPDIR_PATH/oryxai" "$BIN_DIR/oryxai"
else
  echo "oryxai install: $BIN_DIR is not writable." >&2
  echo "  Either re-run with ORYXAI_BIN_DIR=\$HOME/.local/bin," >&2
  echo "  or with ORYXAI_USE_SUDO=1 to allow sudo." >&2
  exit 1
fi

echo
echo "✓ Installed $BIN_DIR/oryxai"
"$BIN_DIR/oryxai" version

# PATH hint when we land in $HOME/.local/bin and the user doesn't yet
# have it on PATH. Skip in non-interactive shells where setting up
# shell rc files would be presumptuous.
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;  # already on PATH
  *)
    echo
    echo "⚠ $BIN_DIR is not on your PATH yet."
    echo "  Add this to your shell rc (~/.zshrc or ~/.bashrc):"
    echo "      export PATH=\"$BIN_DIR:\$PATH\""
    echo "  Then open a new terminal, or run:  export PATH=\"$BIN_DIR:\$PATH\""
    ;;
esac

echo
echo "Next: run \`oryxai install\` and paste your API key from https://oryxai.dev/keys"
