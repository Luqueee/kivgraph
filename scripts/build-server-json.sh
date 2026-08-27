#!/usr/bin/env bash
#
# build-server-json.sh produces the `server.json` published to the official MCP
# Registry, from the metadata committed at the repository root plus the facts
# that only exist once a release has been built.
#
# The committed `server.json` deliberately carries **no `packages` array**. The
# schema requires only `name`, `description` and `version`, and a package entry
# requires a `fileSha256` -- so a committed package entry would be a checksum of
# an artifact that either does not exist yet or was rebuilt since. A file that
# states a hash nobody verifies is the same class of claim this repository
# refuses everywhere else. The hashes are computed from the artifacts actually
# about to be uploaded, in the job that uploads them, or not written at all.
#
# The registry has no field that says which platform a package is for: a
# `Package` carries `registryType`, `identifier`, `fileSha256`, `transport` and
# no operating system. So both platforms are listed as sibling packages, and
# what distinguishes them is inside each archive -- `compatibility.platforms` in
# its MCPB manifest -- plus the target in the file name. A client that picks the
# wrong one gets a binary that will not execute; there is nowhere in the current
# schema to prevent that, and pretending otherwise by shipping only one platform
# would be worse.
#
# Usage:
#   scripts/build-server-json.sh --version X.Y.Z --tag vX.Y.Z \
#       --package linux/amd64=SHA256 --package darwin/arm64=SHA256 \
#       --output server.published.json

set -euo pipefail

version=""
tag=""
output=""
declare -a packages=()

while [[ $# -gt 0 ]]; do
	case "$1" in
	--version)
		version="$2"
		shift 2
		;;
	--tag)
		tag="$2"
		shift 2
		;;
	--package)
		packages+=("$2")
		shift 2
		;;
	--output)
		output="$2"
		shift 2
		;;
	*)
		printf 'build-server-json: unknown argument %s\n' "$1" >&2
		exit 2
		;;
	esac
done

for required in version tag output; do
	if [[ -z "${!required}" ]]; then
		printf 'build-server-json: --%s is required\n' "$required" >&2
		exit 2
	fi
done
if [[ ${#packages[@]} -eq 0 ]]; then
	printf 'build-server-json: at least one --package target=sha256 is required\n' >&2
	exit 2
fi
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	printf 'build-server-json: --version %s is not X.Y.Z\n' "$version" >&2
	exit 2
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
template="$root/server.json"
test -f "$template"

# A committed `packages` array would mean the generated file silently inherits
# stale entries beside the freshly computed ones.
if jq -e 'has("packages")' "$template" >/dev/null; then
	printf 'build-server-json: %s must not commit a packages array; see this script'\''s header\n' "$template" >&2
	exit 1
fi

# The registry caps the description at 100 characters and answers a publish
# attempt with a `422` when it is longer. It is not in the JSON schema, so
# nothing local catches it: the first run of this integration found it by being
# rejected. Failing here costs a second; failing there costs a version number.
description_length="$(jq -r '.description | length' "$template")"
if [[ "$description_length" -gt 100 ]]; then
	printf 'build-server-json: description is %s characters; the registry rejects more than 100\n' "$description_length" >&2
	exit 1
fi

document="$(jq --arg version "$version" '.version = $version | .packages = []' "$template")"

for entry in "${packages[@]}"; do
	target="${entry%%=*}"
	digest="${entry##*=}"
	if [[ "$target" == "$entry" || -z "$digest" ]]; then
		printf 'build-server-json: --package %s is not target=sha256\n' "$entry" >&2
		exit 2
	fi
	if [[ ! "$digest" =~ ^[0-9a-f]{64}$ ]]; then
		printf 'build-server-json: %s is not a sha256 digest\n' "$digest" >&2
		exit 2
	fi
	asset="kivgraph-${target%%/*}-${target##*/}.mcpb"
	url="https://github.com/Luqueee/kivgraph/releases/download/$tag/$asset"
	document="$(jq \
		--arg identifier "$url" \
		--arg digest "$digest" \
		--arg version "$version" \
		'.packages += [{
			registryType: "mcpb",
			identifier: $identifier,
			version: $version,
			fileSha256: $digest,
			transport: {type: "stdio"}
		}]' <<<"$document")"
done

printf '%s\n' "$document" >"$output"

# The registry requires "mcp" in every package URL, and rejects the publish
# rather than degrading, so it is cheaper to fail here than in the last step of
# a release.
if ! jq -e -r '.packages[] | select(.identifier | contains("mcp") | not) | .identifier' "$output" | grep -q .; then
	:
else
	printf 'build-server-json: a package URL does not contain "mcp"\n' >&2
	exit 1
fi
jq -e '.name and .description and .version and (.packages | length > 0)' "$output" >/dev/null
