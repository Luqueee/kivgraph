#!/usr/bin/env bash
set -euo pipefail

# Install the latest published MCP bundle for this host. The release archive is
# verified before it replaces an existing installation.

usage() {
  cat <<'EOF'
Usage: scripts/install.sh

Download and install the latest published Kivgraph MCP release. Supported
hosts are Linux/x86_64 and Darwin/arm64; no Go, Node.js, or pnpm is required.

Environment:
  KIVGRAPH_INSTALL_ROOT  Bundle directory (default: ~/.local/opt/kivgraph)
  KIVGRAPH_BIN_DIR        Launcher directory (default: ~/.local/bin)
  KIVGRAPH_RELEASE_BASE_URL  Releases URL (default: GitHub Luqueee/kivgraph)
  KIVGRAPH_VERSION        Pin a release tag such as vX.Y.Z-dev.1 instead of latest
  KIVGRAPH_GITHUB_TOKEN   Optional token for a private GitHub repository
  KIVGRAPH_CONFIGURE      Configure after install: ask (default), 1, or 0
EOF
}

fail() {
  printf 'kivgraph install: %s\n' "$*" >&2
  exit 1
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi
[[ "$#" -eq 0 ]] || fail "unknown argument: $1 (use --help for usage)"

for command in curl tar find install mktemp grep readlink; do
  command -v "$command" >/dev/null 2>&1 ||
    fail "required command not found: $command"
done
command -v sha256sum >/dev/null 2>&1 || command -v shasum >/dev/null 2>&1 ||
  fail 'required command not found: sha256sum or shasum'

host_system=$(uname -s)
host_machine=$(uname -m)
case "$host_system/$host_machine" in
  Linux/x86_64) platform=linux-amd64 ;;
  Darwin/arm64) platform=darwin-arm64 ;;
  *)
    fail "unsupported host $host_system/$host_machine (supported: Linux/x86_64, Darwin/arm64 for Apple Silicon; Intel Macs are not published)"
    ;;
esac
bundle_name="kivgraph-$platform"

[[ -n "${HOME:-}" ]] || fail 'HOME is not set'

install_root=${KIVGRAPH_INSTALL_ROOT:-"$HOME/.local/opt/kivgraph"}
bin_dir=${KIVGRAPH_BIN_DIR:-"$HOME/.local/bin"}
release_base=${KIVGRAPH_RELEASE_BASE_URL:-"https://github.com/Luqueee/kivgraph/releases"}
requested_version=${KIVGRAPH_VERSION:-}
configure_mode=${KIVGRAPH_CONFIGURE:-ask}
release_base=${release_base%/}

[[ "$install_root" == /* ]] || fail "KIVGRAPH_INSTALL_ROOT must be absolute"
[[ "$bin_dir" == /* ]] || fail "KIVGRAPH_BIN_DIR must be absolute"
[[ "$install_root" != "/" && "$bin_dir" != "/" ]] || fail 'installation paths must not be /'
if [[ -n "$requested_version" && ! "$requested_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  fail "invalid KIVGRAPH_VERSION: $requested_version"
fi
case "$configure_mode" in
  ask|1|0) ;;
  *) fail 'KIVGRAPH_CONFIGURE must be ask, 1, or 0' ;;
esac

archive_name="$bundle_name.tar.gz"
if [[ -n "$requested_version" ]]; then
  download_base="$release_base/download/$requested_version"
else
  download_base="$release_base/latest/download"
fi

download_parent=$(mktemp -d "${TMPDIR:-/tmp}/kivgraph-download.XXXXXX")
staging_parent=""
backup_root=""
new_root_installed=false
created_launchers=()
cleanup() {
  local status=$?
  if (( status != 0 )); then
    # Bash 3.2, the stock macOS shell, treats an empty array expansion as an
    # unbound variable under `set -u`, which would abort the rollback.
    for launcher in ${created_launchers[@]+"${created_launchers[@]}"}; do
      rm -f -- "$launcher"
    done
    if [[ "$new_root_installed" == true ]]; then
      rm -rf -- "$install_root"
    fi
    if [[ -n "$backup_root" && ( -e "$backup_root" || -L "$backup_root" ) ]]; then
      mv -- "$backup_root" "$install_root" || true
    fi
  fi
  if [[ -n "$staging_parent" ]]; then
    rm -rf -- "$staging_parent"
  fi
  rm -rf -- "$download_parent"
  exit "$status"
}
trap cleanup EXIT

mkdir -p "$(dirname -- "$install_root")" "$bin_dir"
staging_parent=$(mktemp -d "$(dirname -- "$install_root")/.kivgraph-install.XXXXXX")
archive_path="$download_parent/$archive_name"
checksums_path="$download_parent/SHA256SUMS"

curl_download() {
  local url=$1 destination=$2
  local -a arguments=(
    --fail
    --location
    --silent
    --show-error
    --retry 3
    --connect-timeout 15
    --max-time 900
    --output "$destination"
  )
  if [[ -n "${KIVGRAPH_GITHUB_TOKEN:-}" ]]; then
    arguments+=(--header "Authorization: Bearer ${KIVGRAPH_GITHUB_TOKEN}")
  fi
  curl "${arguments[@]}" "$url"
}

printf 'kivgraph install: downloading %s\n' "${requested_version:-latest}" >&2
curl_download "$download_base/$archive_name" "$archive_path"
curl_download "$download_base/SHA256SUMS" "$checksums_path"

# sha256_check verifies a checksum manifest relative to the current directory.
# macOS ships shasum rather than the coreutils sha256sum used elsewhere in this
# repository, and a host with neither must stop the install rather than let an
# unverified archive through.
sha256_check() {
  local manifest=$1
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c "$manifest"
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 -c "$manifest"
  else
    printf 'no SHA-256 tool found: install coreutils or shasum\n' >&2
    return 1
  fi
}

verify_checksums() {
  local directory=$1 manifest=$2 output
  if ! output=$(cd -- "$directory" && sha256_check "$manifest" 2>&1); then
    printf '%s\n' "$output" >&2
    return 1
  fi
}

# extract_release_checksum copies the single SHA256SUMS line covering one asset
# into a manifest of its own. A release lists every published platform, so
# verifying the whole file would fail on the assets this host never downloads.
extract_release_checksum() {
  local manifest=$1 name=$2 destination=$3 line
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^([0-9a-fA-F]{64})\ \ (.+)$ ]] || continue
    [[ "${BASH_REMATCH[2]}" == "$name" ]] || continue
    printf '%s  %s\n' "${BASH_REMATCH[1]}" "$name" >"$destination"
    return 0
  done <"$manifest"
  return 1
}

archive_checksum_name="$archive_name.sha256"
# check: archive-digest
extract_release_checksum "$checksums_path" "$archive_name" \
  "$download_parent/$archive_checksum_name" ||
  fail "release publishes no $archive_name for $host_system/$host_machine"
verify_checksums "$download_parent" "$archive_checksum_name" ||
  fail 'release archive checksum verification failed'

extract_root="$staging_parent/extracted"
mkdir -p "$extract_root"
archive_entries="$download_parent/archive.entries"
archive_types="$download_parent/archive.types"
tar --list --file "$archive_path" >"$archive_entries" ||
  fail 'release archive cannot be listed'
tar --list --verbose --file "$archive_path" >"$archive_types" ||
  fail 'release archive entry metadata cannot be listed'
# check: entry-paths
while IFS= read -r name; do
  case "$name" in
    "$bundle_name"|"$bundle_name"/*) ;;
    *) fail "release archive contains unsafe path: $name" ;;
  esac
  case "$name" in
    ''|/*|*\\*|..|../*|*/../*|*/..) fail "release archive contains unsafe path: $name" ;;
  esac
done <"$archive_entries"
# check: entry-types
while IFS= read -r entry; do
  case "${entry:0:1}" in
    '-'|'d') ;;
    *) fail "release archive contains unsupported entry type" ;;
  esac
done <"$archive_types"
tar --extract --file "$archive_path" --directory "$extract_root" \
  --no-same-owner --no-same-permissions
bundle="$extract_root/$bundle_name"
[[ -d "$bundle" ]] || fail "release archive is missing $bundle_name/"
# check: no-symlinks
[[ -z "$(find -L "$bundle" -type l -print -quit)" ]] ||
  fail 'release bundle contains symbolic links'
# check: bundle-checksums
verify_checksums "$bundle" SHA256SUMS || fail 'bundle checksum verification failed'
# check: required-programs
[[ -x "$bundle/bin/kivgraph" ]] || fail 'bundle is missing bin/kivgraph'
[[ -x "$bundle/bin/kivgraph-ts-worker" ]] || fail 'bundle is missing bin/kivgraph-ts-worker'
installed_version=$("$bundle/bin/kivgraph" version)
[[ "$installed_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] ||
  fail "bundle reported invalid version: $installed_version"
# check: version-match
if [[ -n "$requested_version" && "${requested_version#v}" != "$installed_version" ]]; then
  fail "bundle version $installed_version does not match $requested_version"
fi

validate_launcher() {
  local launcher=$1 target=$2
  if [[ ! -e "$launcher" && ! -L "$launcher" ]]; then
    return 0
  fi
  if [[ -L "$launcher" ]]; then
    local resolved
    resolved=$(readlink -f -- "$launcher" 2>/dev/null || true)
    [[ "$resolved" == "$target" ]] ||
      fail "refusing to replace unrelated launcher: $launcher"
    return 0
  fi
  [[ -f "$launcher" ]] && grep -Fq -- "$target" "$launcher" ||
    fail "refusing to replace unrelated launcher: $launcher"
}

create_launcher() {
  local name=$1 target=$2 launcher="$bin_dir/$1"
  if [[ -e "$launcher" || -L "$launcher" ]]; then
    return 0
  fi
  local temporary="$staging_parent/$name.launcher"
  printf '#!/usr/bin/env bash\nset -euo pipefail\n# Managed by the Kivgraph release installer.\nexec %q "$@"\n' \
    "$target" >"$temporary"
  chmod 0755 "$temporary"
  mv -- "$temporary" "$launcher"
  created_launchers+=("$launcher")
}

# check: launcher-ownership
validate_launcher "$bin_dir/kivgraph" "$install_root/bin/kivgraph"
validate_launcher "$bin_dir/kivgraph-ts-worker" "$install_root/bin/kivgraph-ts-worker"

if [[ -e "$install_root" || -L "$install_root" ]]; then
  backup_root="${install_root}.previous"
  [[ ! -e "$backup_root" && ! -L "$backup_root" ]] ||
    fail "previous installation exists: $backup_root"
  mv -- "$install_root" "$backup_root"
fi
mv -- "$bundle" "$install_root"
new_root_installed=true
create_launcher kivgraph "$install_root/bin/kivgraph"
create_launcher kivgraph-ts-worker "$install_root/bin/kivgraph-ts-worker"

if [[ -n "$backup_root" ]]; then
  rm -rf -- "$backup_root"
  backup_root=""
fi
new_root_installed=false

printf 'kivgraph install: installed %s in %s\n' "$installed_version" "$install_root"
printf 'kivgraph install: launchers in %s\n' "$bin_dir"
if [[ ":${PATH}:" != *":${bin_dir}:"* ]]; then
  printf 'kivgraph install: add %s to PATH before using kivgraph\n' "$bin_dir"
fi
printf 'kivgraph install: run "kivgraph configure" to set up MCP, skill, hooks, daemon and instructions\n'

# Configuration is offered only after the bundle and its launchers are safely
# installed. `/dev/tty` is used instead of stdin because the documented curl
# pipeline owns stdin; when there is no terminal the install remains usable and
# the operator gets the exact command to run later. Configuration failures do
# not roll back an already successful bundle installation.
configure_after_install() {
  local answer
  if [[ "$configure_mode" == "0" ]]; then
    printf 'kivgraph install: configuration skipped; run "kivgraph configure" when ready\n'
    return 0
  fi
  if [[ ! -r /dev/tty || ! -w /dev/tty ]]; then
    printf 'kivgraph install: no interactive terminal; run "kivgraph configure" to finish setup\n'
    return 0
  fi
  if [[ "$configure_mode" == "ask" ]]; then
    printf 'kivgraph install: configure MCP clients, skill, hooks, daemon and project instructions now? [Y/n] ' >/dev/tty
    if ! IFS= read -r answer </dev/tty; then
      answer=n
    fi
    case "$answer" in
      ''|y|Y|yes|YES) ;;
      *)
        printf 'kivgraph install: configuration skipped; run "kivgraph configure" when ready\n' >/dev/tty
        return 0
        ;;
    esac
  fi
  if ! "$install_root/bin/kivgraph" configure </dev/tty >/dev/tty 2>/dev/tty; then
    printf 'kivgraph install: configuration did not finish; run "kivgraph configure" to retry\n' >&2
  fi
}

configure_after_install

# The install is finished above and stays finished whatever happens here.
#
# One row, `emitter: installer`, and it is deliberately not the same fact as
# the binary's first run: a bundle can be installed and never launched, which
# is why ADR 0083 keeps the two apart instead of adding them up. The endpoint
# validates every field against a closed set and answers `204` to everything,
# so nothing below can learn anything either.
#
# `-m 3` and the discarded output are the whole error policy. A machine with no
# network, a proxy in the way or an endpoint that is down installs Kivgraph
# exactly the same.
report_installation() {
  local endpoint='https://kivgraph.dev/api/telemetry/first-run'
  local body
  printf -v body '{"emitter":"installer","version":"%s","platform":"%s","channel":"installer"}' \
    "$installed_version" "$platform"
  curl -fsS -m 3 -o /dev/null -X POST "$endpoint" \
    -H 'Content-Type: application/json' --data-binary "$body" >/dev/null 2>&1 || true
}

if [[ "${KIVGRAPH_TELEMETRY:-}" != "0" ]]; then
  printf 'kivgraph install: reporting one install of %s on %s, and nothing else:\n' \
    "$installed_version" "$platform"
  printf 'kivgraph install:   nothing about your code, and no identifier of ours. Your address\n'
  printf 'kivgraph install:   reaches the analytics collector, which hashes it and keeps a country.\n'
  printf 'kivgraph install:   https://kivgraph.dev/telemetry/\n'
  printf 'kivgraph install:   set KIVGRAPH_TELEMETRY=0 to turn it off\n'
  report_installation
fi
