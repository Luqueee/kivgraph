#!/usr/bin/env bash
#
# Verify the platform-independent contract of an installable Kivgraph bundle.
# Platform-specific loader checks remain in the workflow that owns the host
# (for example, RUNPATH and ad-hoc signing on macOS).
#
# Usage: scripts/verify-bundle.sh --bundle DIR --target OS/ARCH \
#   [--version VERSION] [--commit COMMIT] [--source-tools-manifest FILE]

set -euo pipefail

bundle=""
target=""
expected_version=""
expected_commit=""
source_tools_manifest=""

usage() {
	printf 'usage: %s --bundle DIR --target OS/ARCH [--version VERSION] [--commit COMMIT] [--source-tools-manifest FILE]\n' "$0" >&2
}

while (( $# > 0 )); do
	case "$1" in
	--bundle)
		[[ $# -ge 2 ]] || { usage; exit 2; }
		bundle=$2
		shift 2
		;;
	--target)
		[[ $# -ge 2 ]] || { usage; exit 2; }
		target=$2
		shift 2
		;;
	--version)
		[[ $# -ge 2 ]] || { usage; exit 2; }
		expected_version=$2
		shift 2
		;;
	--commit)
		[[ $# -ge 2 ]] || { usage; exit 2; }
		expected_commit=$2
		shift 2
		;;
	--source-tools-manifest)
		[[ $# -ge 2 ]] || { usage; exit 2; }
		source_tools_manifest=$2
		shift 2
		;;
	--help)
		usage
		exit 0
		;;
	*)
		usage
		exit 2
		;;
	esac
done

[[ -n "$bundle" && -d "$bundle" ]] || { usage; exit 2; }
[[ -n "$target" ]] || { usage; exit 2; }

case "$target" in
linux/amd64)
	native_library="lib/liblbug.so"
	program_suffix=""
	;;
darwin/arm64)
	native_library="lib/liblbug.dylib"
	program_suffix=""
	;;
windows/amd64)
	native_library="bin/lbug_shared.dll"
	program_suffix=".exe"
	;;
*)
	printf 'verify-bundle: unsupported target: %s\n' "$target" >&2
	exit 2
	;;
esac

manifest="$bundle/manifest.json"
checksums="$bundle/SHA256SUMS"
binary="$bundle/bin/kivgraph$program_suffix"

test -f "$manifest"
test -f "$checksums"
test -f "$bundle/$native_library"
test -x "$binary"

if command -v sha256sum >/dev/null 2>&1; then
	(cd "$bundle" && sha256sum -c SHA256SUMS)
elif command -v shasum >/dev/null 2>&1; then
	(cd "$bundle" && shasum -a 256 -c SHA256SUMS)
else
	printf 'verify-bundle: no SHA-256 tool found\n' >&2
	exit 1
fi

manifest_release=$(jq -er '.release' "$manifest")
test "$(jq -er '.source.dirty' "$manifest")" = "false"
test "$(jq -er '.target.os + "/" + .target.arch' "$manifest")" = "$target"
test "$("$binary" version)" = "$manifest_release"

if [[ -n "$expected_version" ]]; then
	test "$manifest_release" = "$expected_version"
fi
if [[ -n "$expected_commit" ]]; then
	test "$(jq -er '.source.commit' "$manifest")" = "$expected_commit"
fi

version_json=$("$binary" version --json)
test "$(jq -er '.kivgraph' <<<"$version_json")" = "$manifest_release"
test "$(jq -er '.schema' <<<"$version_json")" = "$(jq -er '.schema.canonical' "$manifest")"
test "$(jq -er '.snapshot_row_format' <<<"$version_json")" = "$(jq -er '.schema.snapshot_row_format' "$manifest")"

# A published non-slim bundle carries the exact analyzer it declares. When a
# source manifest is available, compare against that pin too; otherwise the
# bundle's own manifest is the authoritative input for this isolated check.
test -x "$bundle/bin/rust-analyzer$program_suffix"
test -f "$bundle/tools/manifest.json"
test -f "$bundle/licenses/third-party/rust-analyzer/LICENSE-MIT"
test -f "$bundle/licenses/third-party/rust-analyzer/LICENSE-APACHE"
tools_manifest="$bundle/tools/manifest.json"
if [[ -n "$source_tools_manifest" ]]; then
	test -f "$source_tools_manifest"
	tools_manifest="$source_tools_manifest"
fi
pinned_release=$(jq -er '.tools[] | select(.name == "rust-analyzer") | .release' "$tools_manifest")
test "$(jq -er '.tools.rust_analyzer.release' "$manifest")" = "$pinned_release"
test "$(jq -er '.rust_analyzer' <<<"$version_json")" = "$pinned_release"
reported_version=$("$bundle/bin/rust-analyzer$program_suffix" --version | awk '{print $2}')
test -n "$reported_version"
grep -qF -- "$reported_version" <<<"$pinned_release"

# The viewer is part of a published bundle, while the landing site is not.
test -f "$bundle/web/index.html"
help_output=$("$binary" --help)
grep -q '^  ui \[' <<<"$help_output"
if grep -q 'unavailable: this build carries no web bundle' <<<"$help_output"; then
	printf 'verify-bundle: binary was built without the webassets tag\n' >&2
	exit 1
fi
test ! -e "$bundle/landing"
if awk '{print $2}' "$checksums" | grep -q '^landing/'; then
	printf 'verify-bundle: payload carries the landing site\n' >&2
	exit 1
fi

printf 'verify-bundle: %s passed for %s\n' "$bundle" "$target"
