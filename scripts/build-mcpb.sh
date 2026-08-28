#!/usr/bin/env bash
#
# build-mcpb.sh packages a built Kivgraph bundle as an MCP Bundle (`.mcpb`),
# which is the artifact the official MCP Registry accepts for a server that
# ships prebuilt binaries.
#
# Why a second archive next to the `.tar.gz`, rather than registering the
# tarball: the registry only accepts `npm`, `pypi`, `nuget`, `cargo`, `oci` and
# `mcpb` packages. A tarball on a GitHub release is none of them, and the other
# five would each mean distributing Kivgraph a way it is not distributed today.
# `mcpb` is the one that describes what already exists -- a prebuilt binary that
# needs no toolchain on the reader's machine.
#
# The layout is the part worth reading before changing anything:
#
#   manifest.json      the MCPB manifest, which MUST be at the archive root
#   server/            the Kivgraph bundle, verbatim
#
# The bundle is nested and not flattened because **it already carries its own
# `manifest.json`** -- the provenance one, with the release, the commit and the
# schema versions that `scripts/build-bundle.sh` writes and the release workflow
# asserts. Flattening would put two different files at the same path and one of
# them would win silently. Nesting keeps both: `manifest.json` is MCPB's,
# `server/manifest.json` is ours, and neither had to change shape.
#
# It also keeps the executable's relative RUNPATH intact. The bundle's binary
# finds `lib/liblbug.so` relative to itself, so the tree has to move as a unit;
# any layout that separates `bin/` from `lib/` produces an archive that installs
# and then cannot start.
#
# Usage:
#   scripts/build-mcpb.sh --bundle DIR --version X.Y.Z --target os/arch \
#                         --output FILE.mcpb

set -euo pipefail

bundle=""
version=""
target=""
output=""

while [[ $# -gt 0 ]]; do
	case "$1" in
	--bundle)
		bundle="$2"
		shift 2
		;;
	--version)
		version="$2"
		shift 2
		;;
	--target)
		target="$2"
		shift 2
		;;
	--output)
		output="$2"
		shift 2
		;;
	*)
		printf 'build-mcpb: unknown argument %s\n' "$1" >&2
		exit 2
		;;
	esac
done

for required in bundle version target output; do
	if [[ -z "${!required}" ]]; then
		printf 'build-mcpb: --%s is required\n' "$required" >&2
		exit 2
	fi
done

if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	printf 'build-mcpb: --version %s is not X.Y.Z\n' "$version" >&2
	exit 2
fi

target_os="${target%%/*}"
target_arch="${target##*/}"
if [[ "$target_os" == "$target" || -z "$target_arch" ]]; then
	printf 'build-mcpb: --target %s is not os/arch\n' "$target" >&2
	exit 2
fi

# MCPB names platforms the way Node's `process.platform` does, so Windows is
# `win32` rather than `windows`. The identifier belongs to the format, not to
# us, and `$target_os` is the name the release matrix uses -- the two have to
# be mapped here or the manifest claims a platform no client recognises.
case "$target_os" in
linux) mcpb_platform="linux" ;;
darwin) mcpb_platform="darwin" ;;
windows) mcpb_platform="win32" ;;
*)
	printf 'build-mcpb: no MCPB platform identifier for %s\n' "$target_os" >&2
	exit 2
	;;
esac

program="bin/kivgraph"
if [[ "$target_os" == windows ]]; then
	program="bin/kivgraph.exe"
fi

# The binary and the native library are what make this archive worth shipping.
# Packaging a bundle missing either produces a `.mcpb` that installs and then
# fails at the first tool call, which is the failure a reader cannot diagnose.
#
# Presence and executability are separate checks because on Windows only the
# first one means anything. There is no executable mode bit there; what `-x`
# reports is the shell's guess from the extension, so asserting it would be
# asserting a property of the shell rather than of the file.
if [[ ! -f "$bundle/$program" ]]; then
	printf 'build-mcpb: %s/%s is missing\n' "$bundle" "$program" >&2
	exit 1
fi
if [[ "$target_os" != windows && ! -x "$bundle/$program" ]]; then
	printf 'build-mcpb: %s/%s is not executable\n' "$bundle" "$program" >&2
	exit 1
fi
if [[ ! -f "$bundle/manifest.json" ]]; then
	printf 'build-mcpb: %s/manifest.json is missing, so this is not a built bundle\n' "$bundle" >&2
	exit 1
fi

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$@"
	else
		shasum -a 256 "$@"
	fi
}

staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT

mkdir -p "$staging/server"
# `cp -R src/. dst` copies the contents rather than the directory itself, and
# preserves the executable bits the bundle set.
cp -R "$bundle/." "$staging/server/"

# `serve` is the stdio entry -- the same command `kivgraph mcp install` writes
# into a client configuration, so a reader who installs the bundle and a reader
# who installs the binary run the identical process.
#
# It is not necessarily the server any more. Since ADR 0084 `serve` forwards the
# session to a daemon, installing one if this machine has no supervised daemon
# yet, and answers in process when it finds neither. That choice is made before
# the agent's handshake is read, which is the only moment an in-process server
# is still available to hand it to: a relay that fails afterwards fails the
# command. The bundle cannot express any of it: manifest 0.3 describes a local
# process and its runtime through `server.mcp_config`, and has no field for a
# server url or a remote transport -- the urls it does define name the author
# and the repository. That is exactly why the entry stays stdio and the server
# moved.
#
# `${__dirname}` is substituted by the installing client with the absolute path
# of the unpacked extension. A relative command would be resolved against the
# client's working directory, which is not the extension's.
cat >"$staging/manifest.json" <<MANIFEST
{
  "manifest_version": "0.3",
  "name": "kivgraph",
  "display_name": "Kivgraph",
  "version": "$version",
  "description": "Cross-repository semantic code intelligence for AI coding agents, answered locally from a published code graph.",
  "author": {
    "name": "Luqueee",
    "url": "https://github.com/Luqueee"
  },
  "homepage": "https://kivgraph.dev",
  "documentation": "https://kivgraph.dev/docs/",
  "repository": {
    "type": "git",
    "url": "https://github.com/Luqueee/kivgraph"
  },
  "license": "Apache-2.0",
  "keywords": ["code-intelligence", "code-graph", "static-analysis", "mcp"],
  "server": {
    "type": "binary",
    "entry_point": "server/$program",
    "mcp_config": {
      "command": "\${__dirname}/server/$program",
      "args": ["serve"],
      "env": {}
    }
  },
  "compatibility": {
    "platforms": ["$mcpb_platform"]
  }
}
MANIFEST

output_dir="$(cd "$(dirname "$output")" && pwd)"
output_name="$(basename "$output")"

# The archive is reproducible for the same reason the `.tar.gz` beside it is:
# two builds of one tag must not differ, or the `fileSha256` published to the
# registry stops describing anything.
#
# This is Python rather than `zip` for two reasons, and neither is style. The
# `zip` binary is not installed by default everywhere this script is developed,
# and more importantly it stores whatever mtime the file happens to carry, with
# no flag to override it -- reproducibility would depend on a `touch` pass
# having run first, which is a precondition nothing checks. `zipfile` sets the
# timestamp per entry explicitly, so it cannot be forgotten, and it lets the
# permission bits be written deliberately: `bin/kivgraph`, `bin/rust-analyzer`
# and the native library have to come out executable or the bundle installs and
# cannot start.
#
# 1980-01-01 is the earliest instant a zip entry can express.
# `python3` is the name on Linux and macOS; a Git for Windows shell has only
# `python` on PATH in some images and both in others. Resolving it here beats
# discovering the difference in a release job.
python=""
for candidate in python3 python; do
	if command -v "$candidate" >/dev/null 2>&1; then
		python="$candidate"
		break
	fi
done
if [[ -z "$python" ]]; then
	printf 'build-mcpb: no python3 or python on PATH to write the archive\n' >&2
	exit 1
fi

"$python" - "$staging" "$output_dir/$output_name" <<'PACKAGE'
import os
import stat
import sys
import zipfile

staging, output = sys.argv[1], sys.argv[2]
EPOCH = (1980, 1, 1, 0, 0, 0)

entries = []
for root, directories, files in os.walk(staging):
    directories.sort()
    for name in sorted(directories) + sorted(files):
        absolute = os.path.join(root, name)
        entries.append((os.path.relpath(absolute, staging), absolute))
entries.sort(key=lambda entry: entry[0])

with zipfile.ZipFile(output, "w", zipfile.ZIP_DEFLATED) as archive:
    for relative, absolute in entries:
        is_directory = os.path.isdir(absolute)
        mode = stat.S_IMODE(os.lstat(absolute).st_mode)
        info = zipfile.ZipInfo(relative + ("/" if is_directory else ""), date_time=EPOCH)
        info.external_attr = mode << 16
        if is_directory:
            info.external_attr |= 0x10
            archive.writestr(info, b"")
            continue
        info.compress_type = zipfile.ZIP_DEFLATED
        with open(absolute, "rb") as handle:
            archive.writestr(info, handle.read())
PACKAGE

# The registry requires the published URL to contain "mcp"; the extension is
# what satisfies it, so a rename that drops it breaks publishing rather than
# just looking different.
if [[ "$output_name" != *mcp* ]]; then
	printf 'build-mcpb: %s does not contain "mcp", which the registry requires of the package URL\n' "$output_name" >&2
	exit 1
fi

# Prove the archive is readable and carries both manifests where they belong,
# rather than trusting that the zip that just ran did what it was told.
unzip -l "$output_dir/$output_name" >/dev/null
unzip -p "$output_dir/$output_name" manifest.json | jq -e '.manifest_version == "0.3"' >/dev/null
unzip -p "$output_dir/$output_name" server/manifest.json | jq -e --arg v "$version" '.release == $v' >/dev/null

digest="$(sha256 "$output_dir/$output_name" | awk '{print $1}')"
printf '%s  %s\n' "$digest" "$output_name"
