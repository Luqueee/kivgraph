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
    MINGW*/x86_64 | MSYS*/x86_64 | CYGWIN*/x86_64) printf 'windows/amd64' ;;
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

# The archive format is read per platform rather than per tool: the Windows
# asset is a zip where every other one is a gzip, and a single field for the
# tool would have to be wrong for one of them.
#
# The carriage return is stripped because a jq built for Windows ends its lines
# the way that platform does, and the last field of the line is the one that
# carries it. Nothing downstream compares a URL or a digest for equality, so
# this only ever showed up as an archive format that matched no case -- while
# printing as though it did, because the terminal ate the character.
read -r asset_url asset_sha256 asset_format < <(
  jq -r --arg target "$target" \
    '.tools[] | select(.name=="rust-analyzer") | .platforms[] | select(.target==$target) | "\(.url) \(.sha256) \(.archive_format)"' \
    "$manifest" | tr -d '\r'
)
[[ -n "${asset_url:-}" && "$asset_url" != "null" ]] ||
  fail "the manifest pins no rust-analyzer asset for $target"

destination="${1:-$root/.tooling/rust-analyzer/$version/${target%/*}-${target#*/}}"
mkdir -p "$destination"
# The binary carries the extension that makes it one where a platform decides
# that from the name, because it is resolved by exec.LookPath, which does.
binary="$destination/rust-analyzer"
if [[ "$target" == windows/* ]]; then
  binary="$binary.exe"
fi

if [[ -f "$binary" ]] && [[ -f "$destination/.sha256" ]] &&
  [[ "$(cat "$destination/.sha256")" == "$asset_sha256" ]]; then
  printf '%s\n' "$destination"
  exit 0
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

curl --fail --silent --show-error --location "$asset_url" --output "$work/asset"
verify_digest "$work/asset" "$asset_sha256"
case "$asset_format" in
  gz)
    gunzip -c "$work/asset" >"$work/rust-analyzer"
    ;;
  zip)
    command -v unzip >/dev/null 2>&1 || fail 'unzip is required to extract a zip asset'
    # The zip carries a debug database beside the program; only the program is
    # installed, and it is named rather than globbed so a second executable
    # appearing upstream is a failure here instead of a coin toss.
    unzip -q -o -j "$work/asset" 'rust-analyzer.exe' -d "$work"
    mv "$work/rust-analyzer.exe" "$work/rust-analyzer"
    ;;
  *)
    fail "the manifest pins an archive format this script cannot read: $asset_format"
    ;;
esac
install -m 0755 "$work/rust-analyzer" "$binary"
printf '%s\n' "$asset_sha256" >"$destination/.sha256"

while read -r license_name license_url license_sha256; do
  [[ -n "$license_name" ]] || continue
  curl --fail --silent --show-error --location "$license_url" --output "$work/$license_name"
  verify_digest "$work/$license_name" "$license_sha256"
  install -m 0644 "$work/$license_name" "$destination/$license_name"
done < <(
  jq -r '.tools[] | select(.name=="rust-analyzer") | .license_files[] | "\(.name) \(.url) \(.sha256)"' \
    "$manifest" | tr -d '\r'
)

printf '%s\n' "$destination"
