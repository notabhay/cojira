#!/usr/bin/env bash
set -euo pipefail

DEFAULT_VERSION="v0.3.0"
DEFAULT_GITHUB_REPO="notabhay/cojira"

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

script_dir() {
  local src
  src="${BASH_SOURCE[0]}"
  cd "$(dirname "$src")" && pwd
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

bundled_binary_path() {
  local root="$1"
  local os arch
  os="$(detect_os)"
  arch="$(detect_arch)"

  local candidates=(
    "${root}/bin/cojira-${os}-${arch}"
    "${root}/bin/cojira"
  )

  if [ ! -f "${root}/go.mod" ]; then
    candidates+=(
      "${root}/cojira-${os}-${arch}"
      "${root}/cojira"
    )
  fi

  local candidate
  for candidate in "${candidates[@]}"; do
    if [ -x "$candidate" ]; then
      printf '%s' "$candidate"
      return 0
    fi
  done
  return 1
}

seed_env_file() {
  local root="$1"
  local default_conf_url="${COJIRA_DEFAULT_CONFLUENCE_BASE_URL:-https://confluence.example.com/confluence/}"
  local default_jira_url="${COJIRA_DEFAULT_JIRA_BASE_URL:-https://jira.example.com}"

  if [ -f "${root}/.env" ]; then
    return 0
  fi

  if [ -f "${root}/.env.example" ]; then
    cp "${root}/.env.example" "${root}/.env"
    chmod 0600 "${root}/.env" || true
    return 0
  fi

  cat > "${root}/.env" <<EOF
# Confluence
CONFLUENCE_BASE_URL=${default_conf_url}
CONFLUENCE_API_TOKEN=

# Jira
JIRA_BASE_URL=${default_jira_url}
JIRA_API_TOKEN=
EOF
  chmod 0600 "${root}/.env" || true
}

cleanup_bundle_workspace() {
  local root="$1"
  local self_path="$2"

  rm -rf "${root}/bin" "${root}/examples"
  if [ ! -f "${root}/go.mod" ]; then
    rm -f "${root}/cojira" "${root}"/cojira-*
  fi
  rm -f "${root}/.env.example" "${root}/COJIRA-BOOTSTRAP.md"
  rm -f "${root}/cojira.zip"
  rm -f "${root}"/cojira-*.zip
  rm -f "${self_path}"
}

refresh_workspace_prompts() {
  local bin_dst="$1"
  local root="$2"

  if [ ! -x "$bin_dst" ]; then
    die "Installed binary not found: ${bin_dst}"
  fi

  log "Refreshing workspace prompt files in: ${root}"
  (
    cd "$root"
    "$bin_dst" bootstrap
  )
}

install_bundled_binary() {
  local root="$1"
  local install_dir="$2"
  local self_path="$3"

  local bundled_bin
  bundled_bin="$(bundled_binary_path "$root")" || die "No bundled binary found for this platform in ${root}"

  mkdir -p "$install_dir"
  local bin_dst="${install_dir}/cojira"

  cp "$bundled_bin" "$bin_dst"
  chmod 0755 "$bin_dst"

  log "Installed bundled binary: ${bin_dst}"
  "$bin_dst" --version

  refresh_workspace_prompts "$bin_dst" "$root"
  seed_env_file "$root"
  cleanup_bundle_workspace "$root" "$self_path"

  log ""
  log "Next:"
  log "  Open ${root}/.env, fill in the Jira and Confluence tokens, then tell your agent to verify setup."
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

  rm -rf "${go_install_dir}/go" 2>/dev/null || true
  tar -xzf "$tarball" -C "$go_install_dir" || die "Failed to extract Go toolchain"

  [ -x "$go_bin" ] || die "Go toolchain install failed: ${go_bin} not found"
  printf '%s' "$go_bin"
}

install_from_remote_source() {
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

  local github_repo="${COJIRA_GITHUB_REPO:-$DEFAULT_GITHUB_REPO}"
  local install_dir="$1"
  local bootstrap_root="$2"

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
  local bin_dst="${install_dir}/cojira"

  log "Building cojira (${version}) with ${go_cmd}..."
  (cd "$src_dir" && CGO_ENABLED=0 "$go_cmd" build -trimpath -ldflags "-s -w -X github.com/notabhay/cojira/internal/version.Version=${version}" -o "$bin_dst" .) || die "Build failed"

  log "Installed: ${bin_dst}"
  "$bin_dst" --version

  refresh_workspace_prompts "$bin_dst" "$bootstrap_root"
  seed_env_file "$bootstrap_root"

  log ""
  log "Next:"
  log "  Open ${bootstrap_root}/.env, fill in the Jira and Confluence tokens, then tell your agent to verify setup."
}

main() {
  local root install_dir self_path
  root="$(script_dir)"
  self_path="${root}/$(basename "${BASH_SOURCE[0]}")"
  install_dir="${COJIRA_INSTALL_DIR:-${GOBIN:-$HOME/.local/bin}}"

  if bundled_binary_path "$root" >/dev/null 2>&1; then
    install_bundled_binary "$root" "$install_dir" "$self_path"
    return 0
  fi

  install_from_remote_source "$install_dir" "$root"
}

main "$@"
