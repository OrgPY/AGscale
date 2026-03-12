#!/usr/bin/env bash
set -euo pipefail

REPO="${AGSCALE_REPO:-yangpuyu/agscale}"
VERSION="${AGSCALE_VERSION:-latest}"
INSTALL_DIR="${AGSCALE_INSTALL_DIR:-/usr/local/bin}"
BINARY_NAME="${AGSCALE_BINARY_NAME:-agscale}"

usage() {
  cat <<EOF
Install agscale CLI from GitHub Releases.

Usage:
  $0 [--repo owner/repo] [--version vX.Y.Z|latest] [--install-dir /usr/local/bin] [--binary-name agscale]

Environment overrides:
  AGSCALE_REPO, AGSCALE_VERSION, AGSCALE_INSTALL_DIR, AGSCALE_BINARY_NAME
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      REPO="$2"
      shift 2
      ;;
    --version)
      VERSION="$2"
      shift 2
      ;;
    --install-dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    --binary-name)
      BINARY_NAME="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac
case "$OS" in
  linux|darwin) ;;
  *)
    echo "unsupported OS: $OS" >&2
    exit 1
    ;;
esac

api_get() {
  local url="$1"
  if [[ -n "${GITHUB_TOKEN:-}" ]]; then
    curl -fsSL -H "Authorization: Bearer ${GITHUB_TOKEN}" "$url"
  else
    curl -fsSL "$url"
  fi
}

if [[ "$VERSION" == "latest" ]]; then
  release_json="$(api_get "https://api.github.com/repos/${REPO}/releases/latest")"
else
  release_json="$(api_get "https://api.github.com/repos/${REPO}/releases/tags/${VERSION}")"
fi

tag="$(printf '%s' "$release_json" | grep -oE '"tag_name":[[:space:]]*"[^"]+"' | head -n1 | cut -d'"' -f4)"
if [[ -z "$tag" ]]; then
  echo "failed to resolve release tag from GitHub API response" >&2
  exit 1
fi

asset_name="agscale_${tag}_${OS}_${ARCH}"
asset_url="$(printf '%s' "$release_json" | grep -oE '"browser_download_url":[[:space:]]*"[^"]+"' | cut -d'"' -f4 | grep "/${asset_name}$" | head -n1 || true)"
checksums_url="$(printf '%s' "$release_json" | grep -oE '"browser_download_url":[[:space:]]*"[^"]+"' | cut -d'"' -f4 | grep '/checksums.txt$' | head -n1 || true)"

if [[ -z "$asset_url" ]]; then
  echo "release asset not found: ${asset_name} (repo=${REPO}, tag=${tag})" >&2
  exit 1
fi
if [[ -z "$checksums_url" ]]; then
  echo "checksums.txt not found in release assets" >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

curl -fsSL "$asset_url" -o "${tmp}/${asset_name}"
curl -fsSL "$checksums_url" -o "${tmp}/checksums.txt"

expected="$(grep " ${asset_name}$" "${tmp}/checksums.txt" | awk '{print $1}' | head -n1 || true)"
if [[ -z "$expected" ]]; then
  echo "checksum entry not found for ${asset_name}" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "${tmp}/${asset_name}" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "${tmp}/${asset_name}" | awk '{print $1}')"
else
  echo "no sha256 tool found (need sha256sum or shasum)" >&2
  exit 1
fi

if [[ "$expected" != "$actual" ]]; then
  echo "checksum verification failed for ${asset_name}" >&2
  exit 1
fi

if [[ ! -d "$INSTALL_DIR" ]]; then
  mkdir -p "$INSTALL_DIR" 2>/dev/null || true
fi

if [[ -w "$INSTALL_DIR" ]]; then
  install -m 0755 "${tmp}/${asset_name}" "${INSTALL_DIR}/${BINARY_NAME}"
else
  sudo install -m 0755 "${tmp}/${asset_name}" "${INSTALL_DIR}/${BINARY_NAME}"
fi

echo "installed ${BINARY_NAME} ${tag} -> ${INSTALL_DIR}/${BINARY_NAME}"
echo "run: ${BINARY_NAME} version"
