#!/usr/bin/env bash
set -euo pipefail

# validate-release-version.sh accepts the SemVer string used in release
# metadata. Keeping this parser in one script prevents the workflow and the
# packaging helpers from accepting different release channels.

is_semver() {
	local version=$1 major minor patch prerelease build identifier
	local -a identifiers
	if [[ ! "$version" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)(-([^+]+))?(\+([^+]+))?$ ]]; then
		return 1
	fi
	major=${BASH_REMATCH[1]}
	minor=${BASH_REMATCH[2]}
	patch=${BASH_REMATCH[3]}
	prerelease=${BASH_REMATCH[5]:-}
	build=${BASH_REMATCH[7]:-}
	for identifier in "$major" "$minor" "$patch"; do
		[[ "$identifier" == 0 || "$identifier" =~ ^[1-9][0-9]*$ ]] || return 1
	done
	if [[ -n "$prerelease" ]]; then
		IFS=. read -ra identifiers <<<"$prerelease"
		for identifier in "${identifiers[@]}"; do
			[[ -n "$identifier" && "$identifier" =~ ^[0-9A-Za-z-]+$ ]] || return 1
			if [[ "$identifier" =~ ^[0-9]+$ ]]; then
				[[ "$identifier" == 0 || "$identifier" =~ ^[1-9][0-9]*$ ]] || return 1
			fi
		done
	fi
	if [[ -n "$build" ]]; then
		IFS=. read -ra identifiers <<<"$build"
		for identifier in "${identifiers[@]}"; do
			[[ -n "$identifier" && "$identifier" =~ ^[0-9A-Za-z-]+$ ]] || return 1
		done
	fi
}

selftest() {
	local valid invalid
	for valid in \
		0.0.0 \
		1.2.3 \
		1.2.3-dev.1 \
		1.2.3-rc.1+build.7 \
		1.2.3+build-dev; do
		is_semver "$valid" || { printf 'valid version rejected: %s\n' "$valid" >&2; return 1; }
	done
	for invalid in \
		01.2.3 \
		1.02.3 \
		1.2.03 \
		1.2.3- \
		1.2.3-.dev \
		1.2.3-dev..1 \
		1.2.3-01 \
		1.2.3+build..7 \
		1.2.3+build_dev; do
		if is_semver "$invalid"; then
			printf 'invalid version accepted: %s\n' "$invalid" >&2
			return 1
		fi
	done
	printf 'selftest: ok\n'
}

if [[ "${1:-}" == selftest ]]; then
	[[ "$#" -eq 1 ]] || { printf 'usage: %s selftest\n' "$0" >&2; exit 2; }
	selftest
	exit 0
fi

[[ "$#" -eq 1 && -n "$1" ]] || {
	printf 'usage: %s VERSION\n' "$0" >&2
	exit 2
}
is_semver "$1"
