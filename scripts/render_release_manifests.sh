#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ./scripts/render_release_manifests.sh --version v0.3.0 --windows-amd64-sha256 <sha256> [--repo owner/name] [--output-dir path]

Renders Winget, Scoop, and Chocolatey release manifests from the templates in packaging/.
EOF
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

VERSION=""
REPO="notabhay/cojira"
WINDOWS_AMD64_SHA256=""
OUTPUT_DIR=""

while [ $# -gt 0 ]; do
  case "$1" in
    --version)
      VERSION="${2:-}"
      shift 2
      ;;
    --repo)
      REPO="${2:-}"
      shift 2
      ;;
    --windows-amd64-sha256)
      WINDOWS_AMD64_SHA256="${2:-}"
      shift 2
      ;;
    --output-dir)
      OUTPUT_DIR="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [ -z "${VERSION}" ] || [ -z "${WINDOWS_AMD64_SHA256}" ]; then
  usage >&2
  exit 1
fi

TAG_VERSION="${VERSION}"
CANONICAL_VERSION="${VERSION#v}"
if [ -z "${OUTPUT_DIR}" ]; then
  OUTPUT_DIR="${REPO_ROOT}/packaging/generated/${CANONICAL_VERSION}"
fi

WINDOWS_AMD64_ZIP_URL="https://github.com/${REPO}/releases/download/${TAG_VERSION}/cojira_${CANONICAL_VERSION}_windows_amd64.zip"

render() {
  local src="$1"
  local dst="$2"
  mkdir -p "$(dirname "${dst}")"
  sed \
    -e "s|__VERSION__|${CANONICAL_VERSION}|g" \
    -e "s|__TAG_VERSION__|${TAG_VERSION}|g" \
    -e "s|__REPO__|${REPO}|g" \
    -e "s|__WINDOWS_AMD64_ZIP_URL__|${WINDOWS_AMD64_ZIP_URL}|g" \
    -e "s|__WINDOWS_AMD64_SHA256__|${WINDOWS_AMD64_SHA256}|g" \
    "${src}" > "${dst}"
}

WINGET_DIR="${OUTPUT_DIR}/winget/notabhay.cojira/${CANONICAL_VERSION}"
SCOOP_DIR="${OUTPUT_DIR}/scoop"
CHOCO_DIR="${OUTPUT_DIR}/chocolatey"

render "${REPO_ROOT}/packaging/winget/cojira.yaml.tpl" "${WINGET_DIR}/notabhay.cojira.yaml"
render "${REPO_ROOT}/packaging/winget/cojira.installer.yaml.tpl" "${WINGET_DIR}/notabhay.cojira.installer.yaml"
render "${REPO_ROOT}/packaging/winget/cojira.locale.en-US.yaml.tpl" "${WINGET_DIR}/notabhay.cojira.locale.en-US.yaml"
render "${REPO_ROOT}/packaging/scoop/cojira.json.tpl" "${SCOOP_DIR}/cojira.json"
render "${REPO_ROOT}/packaging/chocolatey/cojira.nuspec.tpl" "${CHOCO_DIR}/cojira.nuspec"
render "${REPO_ROOT}/packaging/chocolatey/tools/chocolateyinstall.ps1.tpl" "${CHOCO_DIR}/tools/chocolateyinstall.ps1"
render "${REPO_ROOT}/packaging/chocolatey/tools/chocolateyuninstall.ps1.tpl" "${CHOCO_DIR}/tools/chocolateyuninstall.ps1"

cat <<EOF
Rendered release manifests:
  Winget:      ${WINGET_DIR}
  Scoop:       ${SCOOP_DIR}/cojira.json
  Chocolatey:  ${CHOCO_DIR}
EOF
