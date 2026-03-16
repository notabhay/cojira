#!/usr/bin/env bash
set -euo pipefail

DEFAULT_VERSION="beta"
DEFAULT_GITHUB_REPO="notabhay/cojira"
DEFAULT_BOOTSTRAP_OUT="/tmp/cojira/COJIRA-BOOTSTRAP.md"

DEFAULT_GO_VERSION="1.22.0"
DEFAULT_GO_BASE_URL="https://go.dev/dl"

log() {
  printf '%s\n' "$*" >&2
}

die() {
  log "Error: $*"
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "Missing required command: $1"
}

detect_os() {
  local uname_s
  uname_s="$(uname -s)"
  case "$uname_s" in
    Darwin) printf '%s' "darwin" ;;
    Linux) printf '%s' "linux" ;;
    *) die "Unsupported OS: ${uname_s}" ;;
  esac
}

detect_arch() {
  local uname_m
  uname_m="$(uname -m)"
  case "$uname_m" in
    x86_64 | amd64) printf '%s' "amd64" ;;
    arm64 | aarch64) printf '%s' "arm64" ;;
    *) die "Unsupported architecture: ${uname_m}" ;;
  esac
}

ensure_go() {
  local tmpdir="$1"

  if command -v go >/dev/null 2>&1; then
    printf '%s' "go"
    return 0
  fi

  local go_version="${COJIRA_GO_VERSION:-$DEFAULT_GO_VERSION}"
  local go_base_url="${COJIRA_GO_BASE_URL:-$DEFAULT_GO_BASE_URL}"

  local os arch filename url
  os="$(detect_os)"
  arch="$(detect_arch)"

  filename="go${go_version}.${os}-${arch}.tar.gz"
  url="${go_base_url}/${filename}"

  local go_root="${COJIRA_GO_INSTALL_ROOT:-${HOME}/.local/share/cojira/go}"
  local go_install_dir="${go_root}/${go_version}"
  local go_bin="${go_install_dir}/go/bin/go"
  if [ -x "$go_bin" ]; then
    printf '%s' "$go_bin"
    return 0
  fi

  log "Go not found; downloading toolchain: ${url}"
  mkdir -p "$go_install_dir"

  local tarball="${tmpdir}/${filename}"
  curl -fsSL --retry 3 --retry-delay 1 -o "$tarball" "$url" || die "Failed to download Go toolchain"

  # The tarball contains a top-level "go/" directory.
  rm -rf "${go_install_dir}/go" 2>/dev/null || true
  tar -xzf "$tarball" -C "$go_install_dir" || die "Failed to extract Go toolchain"

  [ -x "$go_bin" ] || die "Go toolchain install failed: ${go_bin} not found"
  printf '%s' "$go_bin"
}

main() {
  need_cmd curl
  need_cmd tar
  need_cmd find
  need_cmd uname
  need_cmd sed

  local version="${COJIRA_VERSION:-}"
  local ref="${COJIRA_REF:-}"

  if [ -z "$version" ] && [ -z "$ref" ]; then
    version="$DEFAULT_VERSION"
    ref="refs/heads/${DEFAULT_VERSION}"
  elif [ -z "$ref" ]; then
    case "$version" in
      refs/heads/* | refs/tags/*)
        ref="$version"
        version="${version##*/}"
        ;;
      beta | main | master)
        ref="refs/heads/${version}"
        ;;
      v*)
        ref="refs/tags/${version}"
        ;;
      *)
        version="v${version}"
        ref="refs/tags/${version}"
        ;;
    esac
  elif [ -z "$version" ]; then
    case "$ref" in
      refs/heads/* | refs/tags/*) version="${ref##*/}" ;;
      *) version="$DEFAULT_VERSION" ;;
    esac
  fi

  local github_repo="${COJIRA_GITHUB_REPO:-$DEFAULT_GITHUB_REPO}"
  local install_dir="${COJIRA_INSTALL_DIR:-${GOBIN:-$HOME/.local/bin}}"
  local bootstrap_out="${COJIRA_BOOTSTRAP_OUT:-$DEFAULT_BOOTSTRAP_OUT}"

  local tmpdir
  tmpdir="$(mktemp -d)"
  trap "rm -rf \"$tmpdir\"" EXIT

  local go_cmd
  go_cmd="$(ensure_go "$tmpdir")"

  local src_archive_path extract_dir
  src_archive_path="${tmpdir}/cojira-src.tar.gz"
  extract_dir="${tmpdir}/src"
  mkdir -p "$extract_dir"

  local archive_url
  archive_url="https://github.com/${github_repo}/archive/${ref}.tar.gz"

  log "Downloading source archive (${ref})..."
  curl -fsSL --retry 3 --retry-delay 1 -o "$src_archive_path" "$archive_url" || die "Failed to download source archive"
  tar -xzf "$src_archive_path" -C "$extract_dir" || die "Failed to extract source archive"

  local go_mod_path src_dir
  go_mod_path="$(find "$extract_dir" -maxdepth 4 -name go.mod -print -quit || true)"
  [ -n "$go_mod_path" ] || die "Could not locate go.mod in extracted source"
  src_dir="$(dirname "$go_mod_path")"

  mkdir -p "$install_dir"
  local bin_dst
  bin_dst="${install_dir}/cojira"

  log "Building cojira (${version}) with ${go_cmd}..."
  (cd "$src_dir" && CGO_ENABLED=0 "$go_cmd" build -trimpath -ldflags "-s -w -X github.com/notabhay/cojira/internal/version.Version=${version}" -o "$bin_dst" .) || die "Build failed"

  log "Installed: ${bin_dst}"
  "${bin_dst}" --version

  mkdir -p "$(dirname "$bootstrap_out")"
  log "Writing bootstrap guide + templates to: ${bootstrap_out}"
  "${bin_dst}" bootstrap --output "${bootstrap_out}" --force

  log ""
  log "Next:"
  log "  follow ${bootstrap_out}"
}

main "$@"
