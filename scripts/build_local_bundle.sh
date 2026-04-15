#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODULE="github.com/notabhay/cojira"

detect_os() {
  case "$(uname -s)" in
    Darwin) printf '%s' "darwin" ;;
    Linux) printf '%s' "linux" ;;
    *) printf '%s' "unsupported" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64) printf '%s' "amd64" ;;
    arm64 | aarch64) printf '%s' "arm64" ;;
    *) printf '%s' "unsupported" ;;
  esac
}

VERSION="${1:-$(git -C "$REPO_ROOT" describe --tags --always --dirty 2>/dev/null || echo "v0.3.0")}"
OS_NAME="$(detect_os)"
ARCH_NAME="$(detect_arch)"

if [ "$OS_NAME" = "unsupported" ] || [ "$ARCH_NAME" = "unsupported" ]; then
  echo "Unsupported platform for local bundle build" >&2
  exit 1
fi

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

BUNDLE_ROOT="${TMPDIR}/cojira"
mkdir -p "${BUNDLE_ROOT}/bin"

GOOS="${OS_NAME}" GOARCH="${ARCH_NAME}" CGO_ENABLED=0 \
  go -C "$REPO_ROOT" build -trimpath \
  -ldflags "-s -w -X ${MODULE}/internal/version.Version=${VERSION#v}" \
  -o "${BUNDLE_ROOT}/bin/cojira-${OS_NAME}-${ARCH_NAME}" .

cp "${REPO_ROOT}/install.sh" "${BUNDLE_ROOT}/install.sh"
cp "${REPO_ROOT}/COJIRA-BOOTSTRAP.md" "${BUNDLE_ROOT}/COJIRA-BOOTSTRAP.md"

OUTPUT_PATH="${REPO_ROOT}/cojira-${VERSION#v}-${OS_NAME}-${ARCH_NAME}.zip"
rm -f "${OUTPUT_PATH}"
(
  cd "$BUNDLE_ROOT"
  zip -qr "$OUTPUT_PATH" .
)

printf 'Wrote %s\n' "$OUTPUT_PATH"
