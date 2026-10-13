#!/bin/sh

# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

# Install the ocr (Open Code Review) CLI from GitHub releases or this checkout.
#   curl -fsSL https://open-codereview.ai/install.sh | sh
# Prefer to inspect first:
#   curl -fsSL https://open-codereview.ai/install.sh -o install.sh
#   less install.sh && sh install.sh
# Local source: sh install.sh --local
# Local installs default to $HOME/.local/bin and require Go, Git, and Make.
# Env: OCR_INSTALL_DIR (release: /usr/local/bin; local: $HOME/.local/bin),
# OCR_VERSION (release only; default latest),
# OCR_GITHUB_MIRROR (default unset; download the binary through a mirror domain).
set -eu

main() {
  REPO="alibaba/open-code-review"
  BIN="ocr"
  ASSET_PREFIX="opencodereview"
  INSTALL_DIR="${OCR_INSTALL_DIR:-/usr/local/bin}"

  case "${1:-}" in
    --local)
      [ "$#" -eq 1 ] || err "usage: sh install.sh [--local]"
      install_local
      return
      ;;
    --help|-h)
      printf 'Usage: sh install.sh [--local]\n\n'
      printf 'Without arguments, install a GitHub release into /usr/local/bin.\n'
      printf 'With --local, build this checkout and install into $HOME/.local/bin.\n'
      printf 'Set OCR_INSTALL_DIR to override either destination.\n'
      printf 'OCR_VERSION and OCR_GITHUB_MIRROR apply only to release installation.\n'
      return
      ;;
    "") [ "$#" -eq 0 ] || err "usage: sh install.sh [--local]" ;;
    *) err "unknown argument: $1 (use --help)" ;;
  esac

  VERSION="${OCR_VERSION:-}"

  command -v curl >/dev/null 2>&1 || err "curl is required"

  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    darwin|linux) ;;
    *) err "unsupported OS: $os (on Windows use: irm https://open-codereview.ai/install.ps1 | iex)" ;;
  esac

  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) err "unsupported architecture: $arch" ;;
  esac

  if [ -z "$VERSION" ]; then
    release_json="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest")" ||
      err "failed to fetch latest release info from github api"
    VERSION="$(printf '%s' "$release_json" |
      sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
    [ -n "$VERSION" ] || err "could not resolve latest release tag"
  fi

  asset="${ASSET_PREFIX}-${os}-${arch}"
  prefix="$(printf '%s' "${OCR_GITHUB_MIRROR:-}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
  prefix="${prefix#https://}"
  prefix="${prefix#http://}"
  prefix="${prefix%/}"
  case "$prefix" in *[[:space:]]*) err "OCR_GITHUB_MIRROR contains spaces: '$prefix'" ;; esac
  if [ -n "$prefix" ]; then
    printf 'warning: downloading from unofficial GitHub mirror "%s" (checksum integrity is not guaranteed)\n' "$prefix" >&2
    base="https://${prefix}/github.com/$REPO/releases/download/$VERSION"
  else
    base="https://github.com/$REPO/releases/download/$VERSION"
  fi
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' INT TERM EXIT

  printf 'downloading %s %s (%s/%s)...\n' "$BIN" "$VERSION" "$os" "$arch"
  curl -fL --progress-bar --connect-timeout 5 --speed-limit 1024 --speed-time 30 -o "$tmp/$asset" "$base/$asset" ||
    err "download failed: $base/$asset"

  curl -fsSL --connect-timeout 5 --max-time 15 -o "$tmp/sha256sum.txt" "$base/sha256sum.txt" ||
    err "sha256sum.txt download failed"

  want="$(awk -v a="$asset" '$2 == a {print tolower($1)}' "$tmp/sha256sum.txt")"
  [ -n "$want" ] || err "no checksum entry for $asset in sha256sum.txt"
  got="$(sha256 "$tmp/$asset" | awk '{print tolower($1)}')"
  [ "$got" = "$want" ] || err "checksum mismatch for $asset (got $got, want $want)"

  install_binary "$tmp/$asset" "$INSTALL_DIR" "$BIN"

  printf 'installed %s %s -> %s\n' "$BIN" "$VERSION" "$INSTALL_DIR/$BIN"
  post_install_path_notice "$BIN" "$INSTALL_DIR"
}

install_local() {
  case "$(uname -s)" in
    Darwin|Linux) ;;
    *) err "local installation supports Linux and macOS" ;;
  esac
  command -v go >/dev/null 2>&1 || err "Go is required for local installation"
  command -v git >/dev/null 2>&1 || err "Git is required for local installation"
  command -v make >/dev/null 2>&1 || err "Make is required for local installation"

  [ -f "$0" ] || err "--local requires a script file; run sh install.sh --local from the checkout"
  source_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)" ||
    err "could not locate the source checkout"
  [ -f "$source_dir/go.mod" ] && [ -f "$source_dir/Makefile" ] &&
    [ -d "$source_dir/cmd/opencodereview" ] ||
    err "--local requires install.sh from a source checkout"

  if [ -n "${OCR_INSTALL_DIR:-}" ]; then
    INSTALL_DIR="$OCR_INSTALL_DIR"
  else
    [ -n "${HOME:-}" ] || err "HOME is unset; set OCR_INSTALL_DIR for local installation"
    INSTALL_DIR="$HOME/.local/bin"
  fi

  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' INT TERM EXIT
  printf 'building %s from %s...\n' "$BIN" "$source_dir"
  host_os="$(go env GOHOSTOS)" || err "could not detect the Go host OS"
  host_arch="$(go env GOHOSTARCH)" || err "could not detect the Go host architecture"
  GOOS="$host_os" GOARCH="$host_arch" make -C "$source_dir" build DIST_DIR="$tmp" ||
    err "local build failed"
  "$tmp/opencodereview" version >/dev/null 2>&1 ||
    err "local build is not runnable on this host"
  install_binary "$tmp/opencodereview" "$INSTALL_DIR" "$BIN"
  printf 'installed %s from local source -> %s\n' "$BIN" "$INSTALL_DIR/$BIN"
  post_install_path_notice "$BIN" "$INSTALL_DIR"
}

# Install the staged binary (mode 0755), escalating with sudo only when needed.
# Using install(1) under sudo gives the binary root ownership in system dirs.
install_binary() {
  src="$1"
  dir="$2"
  bin="$3"
  if mkdir -p "$dir" 2>/dev/null && [ -w "$dir" ]; then
    install -m 0755 "$src" "$dir/$bin"
  elif command -v sudo >/dev/null 2>&1; then
    printf 'note: %s is not writable; escalating with sudo\n' "$dir"
    sudo mkdir -p "$dir"
    sudo install -m 0755 "$src" "$dir/$bin"
  else
    err "$dir is not writable and sudo is unavailable; set OCR_INSTALL_DIR to a writable path"
  fi
}

post_install_path_notice() {
  bin="$1"
  install_dir="$2"
  case ":$PATH:" in
    *":$install_dir:"*) ;;
    *) printf 'note: %s is not on your PATH; add it or run %s/%s directly\n' "$install_dir" "$install_dir" "$bin"; return ;;
  esac
  command -v "$bin" >/dev/null 2>&1 || printf 'note: open a new shell so %s resolves on PATH\n' "$bin"
}

# Print the SHA-256 of a file, preferring shasum (macOS) over sha256sum (Linux).
sha256() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1"
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1"
  else
    err "shasum or sha256sum is required for checksum verification"
  fi
}

err() { printf 'error: %s\n' "$1" >&2; exit 1; }

main "$@"
