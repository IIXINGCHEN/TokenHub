#!/usr/bin/env bash
set -Eeuo pipefail

GITHUB_REPOSITORY="${TOKENHUB_RELEASE_REPOSITORY:-wangle201210/TokenHub}"
INSTALL_ROOT="${TOKENHUB_INSTALL_ROOT:-/opt/tokenhub}"
CONFIG_DIR="${TOKENHUB_CONFIG_DIR:-/etc/tokenhub}"
STATE_DIR="${TOKENHUB_STATE_DIR:-/var/lib/tokenhub}"
SYSTEMD_DIR="${TOKENHUB_SYSTEMD_DIR:-/etc/systemd/system}"
SERVICE_USER="${TOKENHUB_SERVICE_USER:-tokenhub}"
SERVICE_GROUP="${TOKENHUB_SERVICE_GROUP:-}"
SERVICE_NAME="${TOKENHUB_SERVICE_NAME:-tokenhub}"
BACKEND_PORT="${TOKENHUB_BACKEND_PORT:-8080}"
FRONTEND_PORT="${TOKENHUB_FRONTEND_PORT:-3000}"
VERSION=""
COMMAND="install"
PURGE=false
TEMP_DIR=""
DOWNLOADED_ARCHIVE=""
GENERATED_ADMIN_PASSWORD=""
CREATED_SERVICE_USER=false

usage() {
  cat <<'EOF'
TokenHub native installer

Usage:
  install.sh install [--version VERSION]
  install.sh upgrade [--version VERSION]
  install.sh rollback --version VERSION
  install.sh uninstall [--purge]
  install.sh status

Environment:
  TOKENHUB_RELEASE_REPOSITORY  GitHub owner/repository (default: wangle201210/TokenHub)
  TOKENHUB_PUBLIC_HOST         Public hostname or IP used in generated URLs
  TOKENHUB_PUBLIC_BASE_URL     Public backend URL override
  TOKENHUB_API_BASE_URL        Browser-facing backend URL override
  TOKENHUB_CORS_ALLOWED_ORIGINS
  TOKENHUB_BACKEND_PORT        Backend port (default: 8080)
  TOKENHUB_FRONTEND_PORT       Admin console port (default: 3000)
  TOKENHUB_SERVICE_USER        Linux service user (default: tokenhub)
EOF
}

fail() {
  printf 'TokenHub installer: %s\n' "$*" >&2
  exit 1
}

info() {
  printf '[TokenHub] %s\n' "$*"
}

cleanup() {
  if [ -n "$TEMP_DIR" ] && [ -d "$TEMP_DIR" ]; then
    rm -rf -- "$TEMP_DIR"
  fi
}

validate_safe_path() {
  local value="$1"
  local label="$2"
  [[ "$value" == /* ]] || fail "$label must be an absolute path"
  [[ "$value" != "/" ]] || fail "$label must not be /"
  [[ "$value" =~ ^/[A-Za-z0-9._/-]+$ ]] || fail "$label contains unsupported characters"
  [[ "${value}/" != *"/../"* && "${value}/" != *"/./"* ]] ||
    fail "$label must not contain . or .. path segments"
}

validate_port() {
  local value="$1"
  local label="$2"
  [[ "$value" =~ ^[0-9]+$ ]] || fail "$label must be a number"
  (( 10#$value >= 1 && 10#$value <= 65535 )) || fail "$label must be between 1 and 65535"
}

validate_identifiers() {
  [[ "$SERVICE_USER" =~ ^[A-Za-z_][A-Za-z0-9_-]*$ ]] ||
    fail "TOKENHUB_SERVICE_USER contains unsupported characters"
  [[ "$SERVICE_NAME" =~ ^[A-Za-z0-9][A-Za-z0-9_.@-]*$ ]] ||
    fail "TOKENHUB_SERVICE_NAME contains unsupported characters"
}

normalize_version() {
  local value="${1#v}"
  [[ "$value" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$ ]] ||
    fail "invalid semantic version: $1"
  printf '%s\n' "$value"
}

parse_args() {
  if [ "$#" -gt 0 ] && [[ "$1" != -* ]]; then
    COMMAND="$1"
    shift
  fi
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -v|--version)
        [ "$#" -ge 2 ] || fail "$1 requires a value"
        VERSION="$(normalize_version "$2")"
        shift 2
        ;;
      --purge)
        PURGE=true
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        fail "unknown argument: $1"
        ;;
    esac
  done
}

require_root() {
  if [ "${EUID:-$(id -u)}" -ne 0 ] && [ "${TOKENHUB_INSTALLER_ALLOW_NON_ROOT:-}" != "1" ]; then
    fail "run this installer as root (for example, with sudo)"
  fi
}

require_platform() {
  [ "$(uname -s)" = "Linux" ] || fail "native installation supports Linux only"
  command -v systemctl >/dev/null 2>&1 || fail "systemd is required on Linux"
  command -v sha256sum >/dev/null 2>&1 || fail "sha256sum is required on Linux"
  validate_safe_path "$SYSTEMD_DIR" "TOKENHUB_SYSTEMD_DIR"
  for command in curl tar sed awk find grep install od tr; do
    command -v "$command" >/dev/null 2>&1 || fail "missing required command: $command"
  done
  [[ "$GITHUB_REPOSITORY" =~ ^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$ ]] ||
    fail "TOKENHUB_RELEASE_REPOSITORY must use owner/repository form"
  validate_safe_path "$INSTALL_ROOT" "TOKENHUB_INSTALL_ROOT"
  validate_safe_path "$CONFIG_DIR" "TOKENHUB_CONFIG_DIR"
  validate_safe_path "$STATE_DIR" "TOKENHUB_STATE_DIR"
  validate_identifiers
  validate_port "$BACKEND_PORT" "TOKENHUB_BACKEND_PORT"
  validate_port "$FRONTEND_PORT" "TOKENHUB_FRONTEND_PORT"
}

platform_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64\n' ;;
    aarch64|arm64) printf 'arm64\n' ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
  esac
}

latest_version() {
  local response
  local tag
  response="$(curl -fsSL \
    -H 'Accept: application/vnd.github+json' \
    -H 'X-GitHub-Api-Version: 2022-11-28' \
    "https://api.github.com/repos/${GITHUB_REPOSITORY}/releases/latest")" ||
    fail "unable to query the latest GitHub Release"
  tag="$(printf '%s\n' "$response" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | sed -n '1p')"
  [ -n "$tag" ] || fail "latest GitHub Release has no tag_name"
  normalize_version "$tag"
}

ensure_service_user() {
  if id "$SERVICE_USER" >/dev/null 2>&1; then
    SERVICE_GROUP="${SERVICE_GROUP:-$(id -gn "$SERVICE_USER")}"
    [[ "$SERVICE_GROUP" =~ ^[A-Za-z_][A-Za-z0-9_-]*$ ]] ||
      fail "TOKENHUB_SERVICE_GROUP contains unsupported characters"
    return
  fi
  command -v useradd >/dev/null 2>&1 || fail "useradd is required to create $SERVICE_USER"
  useradd --system --user-group --home-dir "$STATE_DIR" --shell /usr/sbin/nologin "$SERVICE_USER"
  SERVICE_GROUP="${SERVICE_GROUP:-$(id -gn "$SERVICE_USER")}"
  [[ "$SERVICE_GROUP" =~ ^[A-Za-z_][A-Za-z0-9_-]*$ ]] ||
    fail "TOKENHUB_SERVICE_GROUP contains unsupported characters"
  CREATED_SERVICE_USER=true
}

record_created_service_user() {
  if [ "$CREATED_SERVICE_USER" != true ]; then
    return
  fi
  local marker="$CONFIG_DIR/.service-user-created"
  printf '%s:%s\n' "$SERVICE_USER" "$(id -u "$SERVICE_USER")" >"$marker"
  chmod 0600 "$marker"
}

random_hex() {
  local bytes="$1"
  od -An -N "$bytes" -tx1 /dev/urandom | tr -d ' \n'
}

default_public_host() {
  local host
  host="${TOKENHUB_PUBLIC_HOST:-}"
  if [ -z "$host" ] && command -v hostname >/dev/null 2>&1; then
    host="$(hostname -I 2>/dev/null | awk '{print $1}')"
  fi
  printf '%s\n' "${host:-127.0.0.1}"
}

write_initial_config() {
  local env_file="$CONFIG_DIR/tokenhub.env"
  local config_owner="root"
  local config_mode="0640"
  local directory_mode="0750"
  if [ -f "$env_file" ]; then
    info "Keeping existing configuration at $env_file"
    return
  fi
  if [ "${EUID:-$(id -u)}" -ne 0 ]; then
    config_owner="$SERVICE_USER"
    config_mode="0600"
    directory_mode="0700"
  fi

  local public_host
  local public_base_url
  local frontend_url
  local api_base_url
  local allowed_origins
  local admin_token
  local secret_key
  public_host="$(default_public_host)"
  public_base_url="${TOKENHUB_PUBLIC_BASE_URL:-http://${public_host}:${BACKEND_PORT}}"
  frontend_url="http://${public_host}:${FRONTEND_PORT}"
  api_base_url="${TOKENHUB_API_BASE_URL:-$public_base_url}"
  allowed_origins="${TOKENHUB_CORS_ALLOWED_ORIGINS:-$frontend_url}"
  admin_token="$(random_hex 32)"
  secret_key="$(random_hex 32)"
  GENERATED_ADMIN_PASSWORD="$(random_hex 12)"

  install -d -m "$directory_mode" -o "$config_owner" -g "$SERVICE_GROUP" "$CONFIG_DIR"
  cat >"$env_file" <<EOF
TOKENHUB_ENV=prod
TOKENHUB_HTTP_ADDR=:${BACKEND_PORT}
TOKENHUB_PUBLIC_BASE_URL=${public_base_url}
TOKENHUB_RELEASE_REPOSITORY=${GITHUB_REPOSITORY}
TOKENHUB_CORS_ALLOWED_ORIGINS=${allowed_origins}
TOKENHUB_ADMIN_TOKEN=${admin_token}
TOKENHUB_BOOTSTRAP_ADMIN_PASSWORD=${GENERATED_ADMIN_PASSWORD}
TOKENHUB_SECRET_KEY=${secret_key}
TOKENHUB_DATABASE_URL=sqlite://${STATE_DIR}/tokenhub.db
TOKENHUB_SQLITE_BACKUP_DIR=${STATE_DIR}/backups
TOKENHUB_MODEL_CATALOG_FILE=${INSTALL_ROOT}/current/catalog/model-catalog.yaml
TOKENHUB_INSTALL_ROOT=${INSTALL_ROOT}
TOKENHUB_SEED_DEMO=false
TOKENHUB_FRONTEND_HOST=0.0.0.0
TOKENHUB_FRONTEND_PORT=${FRONTEND_PORT}
TOKENHUB_API_BASE_URL=${api_base_url}
EOF
  chown "$config_owner:$SERVICE_GROUP" "$env_file"
  chmod "$config_mode" "$env_file"
  info "Created configuration at $env_file"
}

prepare_directories() {
  local directory_mode="0750"
  install -d -m "$directory_mode" -o "$SERVICE_USER" -g "$SERVICE_GROUP" "$INSTALL_ROOT"
  install -d -m "$directory_mode" -o "$SERVICE_USER" -g "$SERVICE_GROUP" "$INSTALL_ROOT/releases"
  install -d -m "$directory_mode" -o "$SERVICE_USER" -g "$SERVICE_GROUP" "$STATE_DIR"
  install -d -m "$directory_mode" -o "$SERVICE_USER" -g "$SERVICE_GROUP" "$STATE_DIR/backups"
}

sha256_file() {
  local path="$1"
  sha256sum "$path" | awk '{print $1}'
}

download_release() {
  local version="$1"
  local arch
  local asset
  local base_url
  local archive
  local checksums
  local expected
  local actual
  arch="$(platform_arch)"
  asset="tokenhub_${version}_linux_${arch}.tar.gz"
  base_url="https://github.com/${GITHUB_REPOSITORY}/releases/download/v${version}"
  TEMP_DIR="$(mktemp -d)"
  archive="$TEMP_DIR/$asset"
  checksums="$TEMP_DIR/checksums.txt"

  info "Downloading TokenHub v${version} for linux/${arch}"
  curl -fL --retry 3 --connect-timeout 15 -o "$archive" "$base_url/$asset" ||
    fail "unable to download $asset"
  curl -fL --retry 3 --connect-timeout 15 -o "$checksums" "$base_url/checksums.txt" ||
    fail "unable to download checksums.txt"

  expected="$(awk -v file="$asset" '$2 == file || $2 == "*" file { print $1; exit }' "$checksums")"
  [[ "$expected" =~ ^[0-9a-fA-F]{64}$ ]] || fail "checksums.txt has no valid entry for $asset"
  actual="$(sha256_file "$archive" | tr '[:upper:]' '[:lower:]')"
  expected="$(printf '%s' "$expected" | tr '[:upper:]' '[:lower:]')"
  [ "$actual" = "$expected" ] || fail "SHA-256 verification failed for $asset"
  DOWNLOADED_ARCHIVE="$archive"
}

validate_release_archive() {
  local archive="$1"
  local entry
  local listing
  local entry_type

  tar -tzf "$archive" >/dev/null || fail "release archive is not a valid gzip-compressed tar file"
  while IFS= read -r entry; do
    entry="${entry#./}"
    [ -z "$entry" ] && continue
    [[ "$entry" != /* ]] || fail "release archive contains an absolute path"
    [[ "/$entry/" != *"/../"* ]] || fail "release archive contains path traversal"
  done < <(tar -tzf "$archive")

  while IFS= read -r listing; do
    [ -z "$listing" ] && continue
    entry_type="${listing:0:1}"
    case "$entry_type" in
      -|d) ;;
      *) fail "release archive contains a link or special file" ;;
    esac
  done < <(LC_ALL=C tar -tvzf "$archive")
}

validate_staged_release() {
  local staging="$1"
  local version="$2"
  local link
  local path

  link="$(find "$staging" -type l -print -quit)"
  [ -z "$link" ] || fail "release archive contains a symbolic link"
  for path in \
    bin/tokenhub \
    bin/node \
    bin/tokenhub-run \
    frontend/server.js \
    catalog/model-catalog.yaml \
    deploy/tokenhub.service \
    VERSION; do
    [ -f "$staging/$path" ] || fail "release archive is missing regular file $path"
  done
  [ "$(tr -d '[:space:]' <"$staging/VERSION")" = "$version" ] ||
    fail "release archive VERSION does not match v$version"
}

activate_release() {
  local version="$1"
  local archive="$2"
  local releases_dir="$INSTALL_ROOT/releases"
  local staging="$releases_dir/.${version}.install.$$"
  local target="$releases_dir/$version"
  local next_link="$INSTALL_ROOT/.current.$$"
  local directory_mode="0750"

  rm -rf -- "$staging"
  install -d -m "$directory_mode" -o "$SERVICE_USER" -g "$SERVICE_GROUP" "$staging"
  validate_release_archive "$archive"
  tar --no-same-owner --no-same-permissions -xzf "$archive" -C "$staging"
  validate_staged_release "$staging" "$version"

  chmod 0755 "$staging/bin/tokenhub" "$staging/bin/node" "$staging/bin/tokenhub-run"
  "$staging/bin/node" --version >/dev/null 2>&1 ||
    fail "bundled Node.js runtime cannot run on this host"
  chown -R "$SERVICE_USER:$SERVICE_GROUP" "$staging"
  if [ -e "$target" ]; then
    rm -rf -- "$target"
  fi
  mv "$staging" "$target"
  ln -s "$target" "$next_link"
  if [ -e "$INSTALL_ROOT/current" ] && [ ! -L "$INSTALL_ROOT/current" ]; then
    rm -f -- "$next_link"
    fail "native current path is not a symbolic link"
  fi
  mv -Tf "$next_link" "$INSTALL_ROOT/current"
  chown -h "$SERVICE_USER:$SERVICE_GROUP" "$INSTALL_ROOT/current"
  info "Activated TokenHub v$version"
}

install_service() {
  local template
  local unit
  template="$INSTALL_ROOT/current/deploy/tokenhub.service"
  unit="$SYSTEMD_DIR/${SERVICE_NAME}.service"
  sed \
    -e "s|@SERVICE_USER@|$SERVICE_USER|g" \
    -e "s|@SERVICE_GROUP@|$SERVICE_GROUP|g" \
    -e "s|@CONFIG_DIR@|$CONFIG_DIR|g" \
    -e "s|@INSTALL_ROOT@|$INSTALL_ROOT|g" \
    -e "s|@STATE_DIR@|$STATE_DIR|g" \
    "$template" >"$unit"
  chmod 0644 "$unit"
  systemctl daemon-reload
  systemctl enable "$SERVICE_NAME"
}

restart_service() {
  systemctl restart "$SERVICE_NAME"
}

service_status() {
  systemctl status "$SERVICE_NAME"
}

remove_service() {
  systemctl disable --now "$SERVICE_NAME" 2>/dev/null || true
  rm -f -- "$SYSTEMD_DIR/${SERVICE_NAME}.service"
  systemctl daemon-reload
}

install_or_upgrade() {
  local target_version="$VERSION"
  local archive
  [ -n "$target_version" ] || target_version="$(latest_version)"

  ensure_service_user
  prepare_directories
  write_initial_config
  record_created_service_user
  if [ -f "$INSTALL_ROOT/current/VERSION" ] &&
    [ "$(tr -d '[:space:]' <"$INSTALL_ROOT/current/VERSION")" = "$target_version" ]; then
    info "TokenHub v$target_version is already installed"
    install_service
    restart_service
    return
  fi
  download_release "$target_version"
  archive="$DOWNLOADED_ARCHIVE"
  activate_release "$target_version" "$archive"
  install_service
  restart_service

  info "TokenHub v$target_version is running"
  info "Admin console: http://$(default_public_host):${FRONTEND_PORT}"
  if [ -n "$GENERATED_ADMIN_PASSWORD" ]; then
    info "Initial admin username: admin"
    info "Initial admin password: $GENERATED_ADMIN_PASSWORD"
  fi
  info "Configuration: $CONFIG_DIR/tokenhub.env"
  info "Logs: journalctl -u ${SERVICE_NAME} -f"
}

uninstall_tokenhub() {
  local remove_service_user=false
  local marker="$CONFIG_DIR/.service-user-created"
  local expected_user=""
  local expected_uid=""
  if [ -f "$marker" ]; then
    IFS=: read -r expected_user expected_uid <"$marker" || true
    if [ "$expected_user" = "$SERVICE_USER" ] &&
      [ -n "$expected_uid" ] &&
      [ "$(id -u "$SERVICE_USER" 2>/dev/null || true)" = "$expected_uid" ]; then
      remove_service_user=true
    fi
  fi

  remove_service
  rm -rf -- "$INSTALL_ROOT"
  if [ "$PURGE" = true ]; then
    rm -rf -- "$CONFIG_DIR" "$STATE_DIR"
    if [ "$remove_service_user" = true ]; then
      userdel "$SERVICE_USER" 2>/dev/null || true
    fi
    info "Removed application, configuration, and data"
  else
    info "Removed application; preserved $CONFIG_DIR and $STATE_DIR"
  fi
}

main() {
  trap cleanup EXIT
  parse_args "$@"
  require_root
  validate_safe_path "$INSTALL_ROOT" "TOKENHUB_INSTALL_ROOT"
  validate_safe_path "$CONFIG_DIR" "TOKENHUB_CONFIG_DIR"
  validate_safe_path "$STATE_DIR" "TOKENHUB_STATE_DIR"
  validate_safe_path "$SYSTEMD_DIR" "TOKENHUB_SYSTEMD_DIR"
  validate_identifiers

  case "$COMMAND" in
    install|upgrade)
      require_platform
      install_or_upgrade
      ;;
    rollback)
      require_platform
      [ -n "$VERSION" ] || fail "rollback requires --version"
      install_or_upgrade
      ;;
    uninstall)
      require_platform
      uninstall_tokenhub
      ;;
    status)
      require_platform
      service_status
      ;;
    help)
      usage
      ;;
    *)
      fail "unknown command: $COMMAND"
      ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
