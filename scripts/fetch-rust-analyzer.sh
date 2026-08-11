#!/usr/bin/env bash
# Fetch the pinned rust-analyzer binary and its licenses.
#
# The version, the URLs and the digests come from tools/manifest.json. Nothing
# here resolves `latest`: rust-analyzer publishes every day, and a bundle whose
# Rust engine changes between two builds of the same commit is not
# reproducible.
#
# Usage:
#   scripts/fetch-rust-analyzer.sh [destination]
#
# The destination defaults to .tooling/rust-analyzer/<version>/<os>-<arch>.
# The script prints that directory on stdout: it holds the executable and the
# two license texts the bundle distributes with it.

set -euo pipefail

fail() {
  printf 'fetch-rust-analyzer: %s\n' "$1" >&2
  exit 1
}

repository_root() {
  cd "$(dirname "${BASH_SOURCE[0]}")/.."
  pwd
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  fail 'no SHA-256 tool found: install sha256sum or shasum'
}

verify_digest() {
  local file="$1" expected="$2" observed
  observed="$(sha256_of "$file")"
  [[ "$observed" == "$expected" ]] ||
    fail "digest mismatch for $(basename "$file"): expected $expected, got $observed"
}

host_target() {
  local system machine
  system="$(uname -s)"
  machine="$(uname -m)"
  case "${system}/${machine}" in
    Linux/x86_64) printf 'linux/amd64' ;;
    Darwin/arm64) printf 'darwin/arm64' ;;
    *) fail "unsupported host ${system}/${machine}: supported targets are linux/amd64 and darwin/arm64" ;;
  esac
}

for command in curl gunzip jq install; do
  command -v "$command" >/dev/null 2>&1 || fail "required command not found: $command"
done

root="$(repository_root)"
manifest="$root/tools/manifest.json"
[[ -f "$manifest" ]] || fail "tool manifest not found: $manifest"

target="$(host_target)"
version="$(jq -r '.tools[] | select(.name=="rust-analyzer") | .version' "$manifest")"
[[ -n "$version" && "$version" != "null" ]] || fail 'the manifest pins no rust-analyzer version'

read -r asset_url asset_sha256 < <(
  jq -r --arg target "$target" \
    '.tools[] | select(.name=="rust-analyzer") | .platforms[] | select(.target==$target) | "\(.url) \(.sha256)"' \
    "$manifest"
)
[[ -n "${asset_url:-}" && "$asset_url" != "null" ]] ||
  fail "the manifest pins no rust-analyzer asset for $target"

destination="${1:-$root/.tooling/rust-analyzer/$version/${target%/*}-${target#*/}}"
mkdir -p "$destination"
binary="$destination/rust-analyzer"

if [[ -f "$binary" ]] && [[ -f "$destination/.sha256" ]] &&
  [[ "$(cat "$destination/.sha256")" == "$asset_sha256" ]]; then
  printf '%s\n' "$destination"
  exit 0
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

curl --fail --silent --show-error --location "$asset_url" --output "$work/asset.gz"
verify_digest "$work/asset.gz" "$asset_sha256"
gunzip -c "$work/asset.gz" >"$work/rust-analyzer"
install -m 0755 "$work/rust-analyzer" "$binary"
printf '%s\n' "$asset_sha256" >"$destination/.sha256"

while read -r license_name license_url license_sha256; do
  [[ -n "$license_name" ]] || continue
  curl --fail --silent --show-error --location "$license_url" --output "$work/$license_name"
  verify_digest "$work/$license_name" "$license_sha256"
  install -m 0644 "$work/$license_name" "$destination/$license_name"
done < <(
  jq -r '.tools[] | select(.name=="rust-analyzer") | .license_files[] | "\(.name) \(.url) \(.sha256)"' \
    "$manifest"
)

printf '%s\n' "$destination"
