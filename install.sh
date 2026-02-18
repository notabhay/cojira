#!/usr/bin/env bash
set -euo pipefail

DEFAULT_VERSION="v0.1.2"
DEFAULT_REPO_BASE_URL="https://git.rakuten-it.com/projects/~abhay.a.sriwastawa/repos/cojira"
DEFAULT_BOOTSTRAP_OUT="/tmp/cojira/COJIRA-BOOTSTRAP.md"

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

sha256_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
    return 0
  fi
  return 1
}

main() {
  need_cmd curl
  need_cmd tar
  need_cmd awk
  need_cmd uname
  need_cmd sed

  local version="${COJIRA_VERSION:-$DEFAULT_VERSION}"
  local repo_base_url="${COJIRA_REPO_BASE_URL:-$DEFAULT_REPO_BASE_URL}"
  local ref="${COJIRA_REF:-refs/tags/${version}}"
  local install_dir="${COJIRA_INSTALL_DIR:-${GOBIN:-$HOME/.local/bin}}"
  local bootstrap_out="${COJIRA_BOOTSTRAP_OUT:-$DEFAULT_BOOTSTRAP_OUT}"

  local os arch archive_name archive_file releases_dir raw_archive_url raw_checksums_url
  os="$(detect_os)"
  arch="$(detect_arch)"

  archive_name="cojira_${version}_${os}_${arch}"
  archive_file="${archive_name}.tar.gz"
  releases_dir="releases/${version}"

  raw_archive_url="${repo_base_url}/raw/${releases_dir}/${archive_file}?at=${ref}"
  raw_checksums_url="${repo_base_url}/raw/${releases_dir}/checksums.txt?at=${ref}"

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
  repo_host="$(printf '%s\n' "$repo_base_url" | sed -E 's|^https?://([^/]+)/.*$|\\1|')"
  [ -n "$repo_host" ] || die "Could not parse host from COJIRA_REPO_BASE_URL: ${repo_base_url}"

  netrc_path="${tmpdir}/.netrc"
  (umask 077 && cat >"$netrc_path" <<EOF
machine ${repo_host}
login ${bb_user}
password ${bb_token}
EOF
  )

  local archive_path checksums_path extract_dir bin_src bin_dst
  archive_path="${tmpdir}/${archive_file}"
  checksums_path="${tmpdir}/checksums.txt"
  extract_dir="${tmpdir}/extract"
  mkdir -p "$extract_dir"

  log "Downloading ${archive_file} (${os}/${arch})..."
  curl -fsSL --retry 3 --retry-delay 1 --netrc-file "$netrc_path" -o "$archive_path" "$raw_archive_url"
  curl -fsSL --retry 3 --retry-delay 1 --netrc-file "$netrc_path" -o "$checksums_path" "$raw_checksums_url"

  local expected actual
  expected="$(awk -v f="$archive_file" '$2 == f { print $1 }' "$checksums_path" | head -n 1 || true)"
  if [ -z "$expected" ]; then
    die "Checksum not found for ${archive_file} in checksums.txt"
  fi
  if ! actual="$(sha256_file "$archive_path")"; then
    die "No sha256 tool found (need sha256sum or shasum)"
  fi
  if [ "$actual" != "$expected" ]; then
    die "Checksum mismatch for ${archive_file}"
  fi

  tar -xzf "$archive_path" -C "$extract_dir"
  bin_src="${extract_dir}/cojira"
  [ -f "$bin_src" ] || die "Binary not found in archive: ${archive_file}"

  mkdir -p "$install_dir"
  bin_dst="${install_dir}/cojira"
  if command -v install >/dev/null 2>&1; then
    install -m 0755 "$bin_src" "$bin_dst"
  else
    cp "$bin_src" "$bin_dst"
    chmod 0755 "$bin_dst"
  fi

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
