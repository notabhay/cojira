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

sha256_cmd() {
  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s' "sha256sum"
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    printf '%s' "shasum -a 256"
    return 0
  fi
  echo "Missing required checksum command: sha256sum or shasum" >&2
  exit 1
}

VERSION="${1:-$(git -C "$REPO_ROOT" describe --tags --always --dirty 2>/dev/null || echo "v0.4.2")}"
OS_NAME="${2:-$(detect_os)}"
ARCH_NAME="${3:-$(detect_arch)}"

if [ "$OS_NAME" = "unsupported" ] || [ "$ARCH_NAME" = "unsupported" ]; then
  echo "Unsupported platform for local bundle build" >&2
  exit 1
fi

case "$OS_NAME" in
  darwin | linux | windows) ;;
  *)
    echo "Unsupported target OS: ${OS_NAME}" >&2
    exit 1
    ;;
esac

case "$ARCH_NAME" in
  amd64 | arm64) ;;
  *)
    echo "Unsupported target architecture: ${ARCH_NAME}" >&2
    exit 1
    ;;
esac

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

BUNDLE_ROOT="${TMPDIR}/cojira"
mkdir -p "${BUNDLE_ROOT}/bin"

OUTPUT_BINARY="cojira-${OS_NAME}-${ARCH_NAME}"
if [ "$OS_NAME" = "windows" ]; then
  OUTPUT_BINARY="${OUTPUT_BINARY}.exe"
fi

GOOS="${OS_NAME}" GOARCH="${ARCH_NAME}" CGO_ENABLED=0 \
  go -C "$REPO_ROOT" build -trimpath \
  -ldflags "-s -w -X ${MODULE}/internal/version.Version=${VERSION#v}" \
  -o "${BUNDLE_ROOT}/bin/${OUTPUT_BINARY}" .

cp "${REPO_ROOT}/install.sh" "${BUNDLE_ROOT}/install.sh"
cp "${REPO_ROOT}/install.ps1" "${BUNDLE_ROOT}/install.ps1"
cp "${REPO_ROOT}/.env.example" "${BUNDLE_ROOT}/.env.example"
cp "${REPO_ROOT}/COJIRA-BOOTSTRAP.md" "${BUNDLE_ROOT}/COJIRA-BOOTSTRAP.md"

OUTPUT_PATH="${REPO_ROOT}/cojira-${VERSION#v}-${OS_NAME}-${ARCH_NAME}.zip"
CHECKSUM_PATH="${OUTPUT_PATH}.sha256"
rm -f "${OUTPUT_PATH}"
rm -f "${CHECKSUM_PATH}"
(
  cd "$BUNDLE_ROOT"
  zip -qr "$OUTPUT_PATH" .
)

CHECKSUM_TOOL="$(sha256_cmd)"
eval "$CHECKSUM_TOOL \"\$OUTPUT_PATH\"" | awk '{print $1}' > "$CHECKSUM_PATH"

printf 'Wrote %s\n' "$OUTPUT_PATH"
printf 'Wrote %s\n' "$CHECKSUM_PATH"
