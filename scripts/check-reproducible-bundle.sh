#!/usr/bin/env bash
#
# check-reproducible-bundle.sh builds a distribution bundle twice from the same
# checkout and compares the whole payload.
#
# AGENTS.md states that a clean distribution build must be reproducible between
# checkouts of the same commit, toolchain and platform, and that the comparison
# is the full payload rather than manifest.json. Nothing verified it. A build
# that quietly embeds a timestamp, a path or an ordering from the filesystem
# breaks the property that lets anyone check a published artifact against its
# source, and it breaks it silently.
#
# Two builds in one job cover the same-checkout half. What they cannot cover is
# a second machine, so this proves reproducibility across invocations and not
# across hosts.
#
# Usage: scripts/check-reproducible-bundle.sh [--target OS/ARCH]

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target_argument=()
if [[ $# -gt 0 ]]; then
	target_argument=("$@")
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The digest tool is chosen by availability, the way every other script here
# does it, and fails closed without one.
digest_of() {
	local directory="$1" output="$2"
	if command -v sha256sum >/dev/null 2>&1; then
		(cd "$directory" && find . -type f -print0 | sort -z | xargs -0 sha256sum) >"$output"
	elif command -v shasum >/dev/null 2>&1; then
		(cd "$directory" && find . -type f -print0 | sort -z | xargs -0 shasum -a 256) >"$output"
	else
		printf 'check-reproducible-bundle: no sha256 tool available\n' >&2
		exit 1
	fi
}

build() {
	local destination="$1"
	"$root/scripts/build-bundle.sh" "${target_argument[@]+"${target_argument[@]}"}" "$destination" >"$work/build.log" 2>&1 ||
		{
			printf 'check-reproducible-bundle: build failed\n' >&2
			tail -20 "$work/build.log" >&2
			exit 1
		}
}

printf 'check-reproducible-bundle: first build\n'
build "$work/first"
digest_of "$work/first" "$work/first.sha256"

printf 'check-reproducible-bundle: second build\n'
build "$work/second"
digest_of "$work/second" "$work/second.sha256"

# The payload is compared by content, and the paths come out of the same
# relative walk, so a file that only one build produced shows up as a missing
# line rather than a silent pass.
if ! diff -u "$work/first.sha256" "$work/second.sha256" >"$work/diff"; then
	printf 'check-reproducible-bundle: two builds of the same checkout differ\n' >&2
	head -40 "$work/diff" >&2
	exit 1
fi

printf 'check-reproducible-bundle: ok, %s files identical\n' "$(wc -l <"$work/first.sha256" | tr -d ' ')"
