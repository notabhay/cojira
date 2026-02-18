#!/usr/bin/env bash
set -euo pipefail

DEFAULT_VERSION="v0.1.2"
DEFAULT_REPO_REST_URL="https://git.rakuten-it.com/rest/api/1.0/projects/~abhay.a.sriwastawa/repos/cojira"
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

parse_host() {
  local url="$1"
  printf '%s\n' "$url" | sed -E 's|^https?://([^/]+)/.*$|\\1|'
}

prompt_tty() {
  local prompt="$1"
  local var_name="$2"
  local default="${3:-}"
  local value=""

  if [ -r /dev/tty ]; then
    if [ -n "$default" ]; then
      read -r -p "${prompt} [${default}]: " value < /dev/tty || true
      value="${value:-$default}"
    else
      read -r -p "${prompt}: " value < /dev/tty || true
    fi
  else
    die "No TTY available; set ${var_name} in the environment"
  fi

  printf -v "$var_name" '%s' "$value"
}

prompt_secret_tty() {
  local prompt="$1"
  local var_name="$2"
  local value=""

  if [ -r /dev/tty ]; then
    read -r -s -p "${prompt}: " value < /dev/tty || true
    printf '\n' > /dev/tty
  else
    die "No TTY available; set ${var_name} in the environment"
  fi

  printf -v "$var_name" '%s' "$value"
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

  local version="${COJIRA_VERSION:-$DEFAULT_VERSION}"
  case "$version" in
    v*) ;;
    *) version="v${version}" ;;
  esac
  local ref="${COJIRA_REF:-refs/tags/${version}}"

  local repo_rest_url="${COJIRA_REPO_REST_URL:-$DEFAULT_REPO_REST_URL}"
  local install_dir="${COJIRA_INSTALL_DIR:-${GOBIN:-$HOME/.local/bin}}"
  local bootstrap_out="${COJIRA_BOOTSTRAP_OUT:-$DEFAULT_BOOTSTRAP_OUT}"

  local bb_user="${COJIRA_BITBUCKET_USER:-${BITBUCKET_USER:-}}"
  local bb_token="${COJIRA_BITBUCKET_TOKEN:-${BITBUCKET_TOKEN:-}}"

  if [ -z "$bb_user" ]; then
    bb_user="$(whoami 2>/dev/null || true)"
  fi
  if [ -z "$bb_user" ]; then
    prompt_tty "Bitbucket username" bb_user ""
  fi
  if [ -z "$bb_token" ]; then
    prompt_secret_tty "Bitbucket HTTP access token (PAT)" bb_token
  fi

  local tmpdir
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT

  local repo_host netrc_path
  repo_host="$(parse_host "$repo_rest_url")"
  [ -n "$repo_host" ] || die "Could not parse host from COJIRA_REPO_REST_URL: ${repo_rest_url}"

  netrc_path="${tmpdir}/.netrc"
  (umask 077 && cat >"$netrc_path" <<EOF
machine ${repo_host}
login ${bb_user}
password ${bb_token}
EOF
  )

  local go_cmd
  go_cmd="$(ensure_go "$tmpdir")"

  local src_archive_path extract_dir
  src_archive_path="${tmpdir}/cojira-src.tar.gz"
  extract_dir="${tmpdir}/src"
  mkdir -p "$extract_dir"

  local archive_url
  archive_url="${repo_rest_url}/archive?at=${ref}&format=tar.gz"

  log "Downloading source archive (${ref})..."
  curl -fsSL --retry 3 --retry-delay 1 --netrc-file "$netrc_path" -o "$src_archive_path" "$archive_url" || die "Failed to download source archive"
  tar -xzf "$src_archive_path" -C "$extract_dir" || die "Failed to extract source archive"

  local go_mod_path src_dir
  go_mod_path="$(find "$extract_dir" -maxdepth 4 -name go.mod -print -quit || true)"
  [ -n "$go_mod_path" ] || die "Could not locate go.mod in extracted source"
  src_dir="$(dirname "$go_mod_path")"

  mkdir -p "$install_dir"
  local bin_dst
  bin_dst="${install_dir}/cojira"

  log "Building cojira (${version}) with ${go_cmd}..."
  (cd "$src_dir" && CGO_ENABLED=0 "$go_cmd" build -trimpath -ldflags "-s -w -X github.com/cojira/cojira/internal/version.Version=${version}" -o "$bin_dst" .) || die "Build failed"

  log "Installed: ${bin_dst}"
  "${bin_dst}" --version

  mkdir -p "$(dirname "$bootstrap_out")"
  log "Writing bootstrap guide + templates to: ${bootstrap_out}"
  "${bin_dst}" bootstrap --output "${bootstrap_out}" --force

  log ""
  log "Next:"
  log "  Open ${bootstrap_out} and follow it to set up this workspace."
}

main "$@"
