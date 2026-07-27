#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT

fail_test() {
  printf 'container entrypoint test: %s\n' "$*" >&2
  exit 1
}

create_bundle() {
  local root="$1"
  local version="$2"
  local build_id="${3:-}"
  local marker="$4"

  mkdir -p "$root/bin" "$root/frontend" "$root/catalog" "$root/deploy"
  : >"$root/bin/tokenhub"
  : >"$root/bin/node"
  cat >"$root/bin/tokenhub-run" <<'EOF'
#!/bin/sh
set -eu
release_dir="$(cd "$(dirname "$0")/.." && pwd)"
cp "$release_dir/MARKER" "$TOKENHUB_TEST_OUTPUT"
EOF
  : >"$root/frontend/server.js"
  : >"$root/catalog/model-catalog.yaml"
  printf '{"providers":[]}\n' >"$root/catalog/provider-catalog.json"
  : >"$root/deploy/tokenhub.service"
  printf '%s\n' "$version" >"$root/VERSION"
  printf '%s\n' "$marker" >"$root/MARKER"
  if [ -n "$build_id" ]; then
    printf '%s\n' "$build_id" >"$root/BUILD_ID"
  fi
  chmod 0755 "$root/bin/tokenhub" "$root/bin/node" "$root/bin/tokenhub-run"
}

run_entrypoint() {
  local image_root="$1"
  TOKENHUB_CONTAINER_IMAGE_ROOT="$image_root" \
    TOKENHUB_INSTALL_ROOT="$test_root/install" \
    TOKENHUB_RUN_MODE=all \
    TOKENHUB_TEST_OUTPUT="$test_root/output" \
    sh "$script_dir/tokenhub-entrypoint"
}

first_id="$(printf '1%.0s' {1..64})"
second_id="$(printf '2%.0s' {1..64})"
first_image="$test_root/image-first"
second_image="$test_root/image-second"
create_bundle "$first_image" "0.3.0" "$first_id" "first source build"
create_bundle "$second_image" "0.3.0" "$second_id" "second source build"

run_entrypoint "$first_image"
[ "$(<"$test_root/output")" = "first source build" ] ||
  fail_test "initial image bundle was not activated"
[ "$(sed -n '2p' "$test_root/install/.container-image-version")" = "$first_id" ] ||
  fail_test "initial build identity was not persisted"

managed_release="$test_root/install/releases/0.3.1"
create_bundle "$managed_release" "0.3.1" "" "panel-managed release"
ln -sfn "releases/0.3.1" "$test_root/install/current"
run_entrypoint "$first_image"
[ "$(<"$test_root/output")" = "panel-managed release" ] ||
  fail_test "ordinary restart replaced the panel-managed release"

run_entrypoint "$second_image"
[ "$(<"$test_root/output")" = "second source build" ] ||
  fail_test "same-version image with a new build identity stayed stale"
[ "$(<"$test_root/install/current/MARKER")" = "second source build" ] ||
  fail_test "new image content was not installed"
[ "$(sed -n '2p' "$test_root/install/.container-image-version")" = "$second_id" ] ||
  fail_test "new build identity was not persisted"

printf 'container entrypoint tests passed\n'
