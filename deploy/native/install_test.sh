#!/usr/bin/env bash
set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT

# shellcheck source=install.sh
source "$script_dir/install.sh"

fail_test() {
  printf 'native installer test: %s\n' "$*" >&2
  exit 1
}

assert_fails() {
  if ("$@" >/dev/null 2>&1); then
    fail_test "expected command to fail: $*"
  fi
}

create_bundle() {
  local root="$1"
  local version="$2"
  mkdir -p "$root/bin" "$root/frontend" "$root/catalog" "$root/deploy"
  : >"$root/bin/tokenhub"
  : >"$root/bin/node"
  cp "$script_dir/tokenhub-run" "$root/bin/tokenhub-run"
  : >"$root/frontend/server.js"
  : >"$root/catalog/model-catalog.yaml"
  cp "$script_dir/tokenhub.service" "$root/deploy/tokenhub.service"
  printf '%s\n' "$version" >"$root/VERSION"
}

[ "$(normalize_version v0.3.3)" = "0.3.3" ] || fail_test "version normalization failed"
assert_fails normalize_version "0.3"
validate_port "08" "test port"
assert_fails validate_port "0" "test port"
assert_fails validate_safe_path "/opt/../etc" "test path"

SERVICE_USER="tokenhub"
SERVICE_NAME="tokenhub"
validate_identifiers
SERVICE_NAME="../tokenhub"
assert_fails validate_identifiers
SERVICE_NAME="tokenhub"

valid_bundle="$test_root/valid"
valid_archive="$test_root/valid.tar.gz"
create_bundle "$valid_bundle" "0.3.3"
tar -czf "$valid_archive" -C "$valid_bundle" .
validate_release_archive "$valid_archive"
validate_staged_release "$valid_bundle" "0.3.3"
assert_fails validate_staged_release "$valid_bundle" "0.3.2"

linked_bundle="$test_root/linked"
linked_archive="$test_root/linked.tar.gz"
create_bundle "$linked_bundle" "0.3.3"
ln -s /etc/passwd "$linked_bundle/frontend/external"
tar -czf "$linked_archive" -C "$linked_bundle" .
assert_fails validate_release_archive "$linked_archive"
assert_fails validate_staged_release "$linked_bundle" "0.3.3"

runner_root="$test_root/runner"
mkdir -p "$runner_root/bin" "$runner_root/frontend"
cp "$script_dir/tokenhub-run" "$runner_root/bin/tokenhub-run"
cat >"$runner_root/bin/tokenhub" <<'EOF'
#!/usr/bin/env bash
[ "${TOKENHUB_RUNNER_MARKER:-}" = "loaded" ] || exit 12
sleep 0.2
exit 7
EOF
cat >"$runner_root/bin/node" <<'EOF'
#!/usr/bin/env bash
trap 'exit 0' TERM
while :; do sleep 1; done
EOF
: >"$runner_root/frontend/server.js"
printf 'TOKENHUB_RUNNER_MARKER=loaded\n' >"$runner_root/tokenhub.env"
chmod 0755 "$runner_root/bin/tokenhub-run" "$runner_root/bin/tokenhub" "$runner_root/bin/node"

set +e
TOKENHUB_CONFIG_FILE="$runner_root/tokenhub.env" "$runner_root/bin/tokenhub-run"
runner_status=$?
set -e
[ "$runner_status" -eq 7 ] || fail_test "runner returned $runner_status instead of backend status 7"

run_linux_integration() {
  local fixtures="$test_root/fixtures"
  local fake_bin="$test_root/fake-bin"
  local install_root="$test_root/opt/tokenhub"
  local config_dir="$test_root/etc/tokenhub"
  local state_dir="$test_root/var/lib/tokenhub"
  local systemd_dir="$test_root/etc/systemd/system"
  local integration_bundle="$test_root/integration-bundle"
  local asset

  mkdir -p "$fixtures" "$fake_bin" "$systemd_dir"
  create_bundle "$integration_bundle" "0.3.3"
  chmod 0755 "$integration_bundle/bin/tokenhub" "$integration_bundle/bin/node" "$integration_bundle/bin/tokenhub-run"
  for asset in \
    tokenhub_0.3.3_linux_amd64.tar.gz \
    tokenhub_0.3.3_linux_arm64.tar.gz; do
    tar -czf "$fixtures/$asset" -C "$integration_bundle" .
  done
  (
    cd "$fixtures"
    sha256sum tokenhub_*.tar.gz >checksums.txt
  )

  cat >"$fake_bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
destination=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      destination="$2"
      shift 2
      ;;
    *)
      url="$1"
      shift
      ;;
  esac
done
[ -n "$destination" ] && [ -n "$url" ]
cp "$TOKENHUB_TEST_FIXTURES/${url##*/}" "$destination"
EOF
  cat >"$fake_bin/systemctl" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$TOKENHUB_TEST_SYSTEMCTL_LOG"
EOF
  cat >"$fake_bin/userdel" <<'EOF'
#!/usr/bin/env bash
touch "$TOKENHUB_TEST_USERDEL_LOG"
EOF
  chmod 0755 "$fake_bin/curl" "$fake_bin/systemctl" "$fake_bin/userdel"

  env \
    PATH="$fake_bin:$PATH" \
    TOKENHUB_TEST_FIXTURES="$fixtures" \
    TOKENHUB_TEST_SYSTEMCTL_LOG="$test_root/systemctl.log" \
    TOKENHUB_INSTALL_ROOT="$install_root" \
    TOKENHUB_CONFIG_DIR="$config_dir" \
    TOKENHUB_STATE_DIR="$state_dir" \
    TOKENHUB_SYSTEMD_DIR="$systemd_dir" \
    TOKENHUB_SERVICE_USER=root \
    TOKENHUB_SERVICE_NAME=tokenhub-test \
    TOKENHUB_PUBLIC_HOST=127.0.0.1 \
    bash "$script_dir/install.sh" install --version 0.3.3

  [ "$(tr -d '[:space:]' <"$install_root/current/VERSION")" = "0.3.3" ] ||
    fail_test "integration install did not activate v0.3.3"
  [ -s "$config_dir/tokenhub.env" ] || fail_test "integration install did not create configuration"
  grep -q "$install_root/current/bin/tokenhub-run" "$systemd_dir/tokenhub-test.service" ||
    fail_test "systemd unit was not rendered with the install root"
  if grep -q '@[A-Z_]*@' "$systemd_dir/tokenhub-test.service"; then
    fail_test "systemd unit still contains template placeholders"
  fi
  if grep -q '^EnvironmentFile=-' "$systemd_dir/tokenhub-test.service"; then
    fail_test "systemd unit treats the production environment file as optional"
  fi

  printf 'TOKENHUB_TEST_MARKER=preserved\n' >>"$config_dir/tokenhub.env"
  env \
    PATH="$fake_bin:$PATH" \
    TOKENHUB_TEST_FIXTURES="$fixtures" \
    TOKENHUB_TEST_SYSTEMCTL_LOG="$test_root/systemctl.log" \
    TOKENHUB_INSTALL_ROOT="$install_root" \
    TOKENHUB_CONFIG_DIR="$config_dir" \
    TOKENHUB_STATE_DIR="$state_dir" \
    TOKENHUB_SYSTEMD_DIR="$systemd_dir" \
    TOKENHUB_SERVICE_USER=root \
    TOKENHUB_SERVICE_NAME=tokenhub-test \
    TOKENHUB_PUBLIC_HOST=127.0.0.1 \
    bash "$script_dir/install.sh" upgrade --version 0.3.3
  grep -q '^TOKENHUB_TEST_MARKER=preserved$' "$config_dir/tokenhub.env" ||
    fail_test "integration upgrade replaced existing configuration"

  env \
    PATH="$fake_bin:$PATH" \
    TOKENHUB_TEST_SYSTEMCTL_LOG="$test_root/systemctl.log" \
    TOKENHUB_INSTALL_ROOT="$install_root" \
    TOKENHUB_CONFIG_DIR="$config_dir" \
    TOKENHUB_STATE_DIR="$state_dir" \
    TOKENHUB_SYSTEMD_DIR="$systemd_dir" \
    TOKENHUB_SERVICE_USER=root \
    TOKENHUB_SERVICE_NAME=tokenhub-test \
    bash "$script_dir/install.sh" uninstall
  [ ! -e "$install_root" ] || fail_test "uninstall kept the application directory"
  [ -f "$config_dir/tokenhub.env" ] || fail_test "uninstall removed preserved configuration"
  [ -d "$state_dir" ] || fail_test "uninstall removed preserved state"

  env \
    PATH="$fake_bin:$PATH" \
    TOKENHUB_TEST_SYSTEMCTL_LOG="$test_root/systemctl.log" \
    TOKENHUB_TEST_USERDEL_LOG="$test_root/userdel.log" \
    TOKENHUB_INSTALL_ROOT="$install_root" \
    TOKENHUB_CONFIG_DIR="$config_dir" \
    TOKENHUB_STATE_DIR="$state_dir" \
    TOKENHUB_SYSTEMD_DIR="$systemd_dir" \
    TOKENHUB_SERVICE_USER=root \
    TOKENHUB_SERVICE_NAME=tokenhub-test \
    bash "$script_dir/install.sh" uninstall --purge
  [ ! -e "$config_dir" ] && [ ! -e "$state_dir" ] ||
    fail_test "purge kept configuration or state"
  [ ! -e "$test_root/userdel.log" ] ||
    fail_test "purge attempted to delete a pre-existing service user"
}

if [ "${TOKENHUB_NATIVE_INTEGRATION:-}" = "1" ]; then
  run_linux_integration
fi

printf 'native installer tests passed\n'
