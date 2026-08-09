#!/usr/bin/env bash
set -euo pipefail

# Install the latest published Linux amd64 MCP bundle. The release archive is
# verified before it replaces an existing installation.

usage() {
  cat <<'EOF'
Usage: scripts/install.sh

Download and install the latest published Ladygraph MCP release. The script
requires a Linux amd64 host and installs without Go, Node.js, or pnpm.

Environment:
  LADYGRAPH_INSTALL_ROOT  Bundle directory (default: ~/.local/opt/ladygraph)
  LADYGRAPH_BIN_DIR        Launcher directory (default: ~/.local/bin)
  LADYGRAPH_RELEASE_BASE_URL  Releases URL (default: GitHub Luqueee/ladygraph)
  LADYGRAPH_VERSION        Pin a release tag such as v0.1.0 instead of latest
  LADYGRAPH_GITHUB_TOKEN   Optional token for a private GitHub repository
EOF
}

fail() {
  printf 'ladygraph install: %s\n' "$*" >&2
  exit 1
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi
[[ "$#" -eq 0 ]] || fail "unknown argument: $1 (use --help for usage)"

for command in curl tar sha256sum find install mktemp grep readlink; do
  command -v "$command" >/dev/null 2>&1 ||
    fail "required command not found: $command"
done

[[ "$(uname -s)" == "Linux" ]] || fail "host must be Linux"
[[ "$(uname -m)" == "x86_64" ]] || fail "host must be x86_64"
[[ -n "${HOME:-}" ]] || fail 'HOME is not set'

install_root=${LADYGRAPH_INSTALL_ROOT:-"$HOME/.local/opt/ladygraph"}
bin_dir=${LADYGRAPH_BIN_DIR:-"$HOME/.local/bin"}
release_base=${LADYGRAPH_RELEASE_BASE_URL:-"https://github.com/Luqueee/ladygraph/releases"}
requested_version=${LADYGRAPH_VERSION:-}
release_base=${release_base%/}

[[ "$install_root" == /* ]] || fail "LADYGRAPH_INSTALL_ROOT must be absolute"
[[ "$bin_dir" == /* ]] || fail "LADYGRAPH_BIN_DIR must be absolute"
[[ "$install_root" != "/" && "$bin_dir" != "/" ]] || fail 'installation paths must not be /'
if [[ -n "$requested_version" && ! "$requested_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  fail "invalid LADYGRAPH_VERSION: $requested_version"
fi

archive_name=ladygraph-linux-amd64.tar.gz
if [[ -n "$requested_version" ]]; then
  download_base="$release_base/download/$requested_version"
else
  download_base="$release_base/latest/download"
fi

download_parent=$(mktemp -d "${TMPDIR:-/tmp}/ladygraph-download.XXXXXX")
staging_parent=""
backup_root=""
new_root_installed=false
created_launchers=()
cleanup() {
  local status=$?
  if (( status != 0 )); then
    for launcher in "${created_launchers[@]}"; do
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
staging_parent=$(mktemp -d "$(dirname -- "$install_root")/.ladygraph-install.XXXXXX")
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
  if [[ -n "${LADYGRAPH_GITHUB_TOKEN:-}" ]]; then
    arguments+=(--header "Authorization: Bearer ${LADYGRAPH_GITHUB_TOKEN}")
  fi
  curl "${arguments[@]}" "$url"
}

printf 'ladygraph install: downloading %s\n' "${requested_version:-latest}" >&2
curl_download "$download_base/$archive_name" "$archive_path"
curl_download "$download_base/SHA256SUMS" "$checksums_path"

verify_checksums() {
  local directory=$1 output
  if ! output=$(cd -- "$directory" && sha256sum -c SHA256SUMS 2>&1); then
    printf '%s\n' "$output" >&2
    return 1
  fi
}

verify_checksums "$download_parent" ||
  fail 'release archive checksum verification failed'

extract_root="$staging_parent/extracted"
mkdir -p "$extract_root"
archive_entries="$download_parent/archive.entries"
archive_types="$download_parent/archive.types"
tar --list --file "$archive_path" >"$archive_entries" ||
  fail 'release archive cannot be listed'
tar --list --verbose --file "$archive_path" >"$archive_types" ||
  fail 'release archive entry metadata cannot be listed'
while IFS= read -r name; do
  case "$name" in
    ladygraph-linux-amd64|ladygraph-linux-amd64/*) ;;
    *) fail "release archive contains unsafe path: $name" ;;
  esac
  case "$name" in
    ''|/*|*\\*|..|../*|*/../*|*/..) fail "release archive contains unsafe path: $name" ;;
  esac
done <"$archive_entries"
while IFS= read -r entry; do
  case "${entry:0:1}" in
    '-'|'d') ;;
    *) fail "release archive contains unsupported entry type" ;;
  esac
done <"$archive_types"
tar --extract --file "$archive_path" --directory "$extract_root" \
  --no-same-owner --no-same-permissions --no-overwrite-dir
bundle="$extract_root/ladygraph-linux-amd64"
[[ -d "$bundle" ]] || fail 'release archive is missing ladygraph-linux-amd64/'
[[ -z "$(find -L "$bundle" -type l -print -quit)" ]] ||
  fail 'release bundle contains symbolic links'
verify_checksums "$bundle" || fail 'bundle checksum verification failed'
[[ -x "$bundle/bin/ladygraph" ]] || fail 'bundle is missing bin/ladygraph'
[[ -x "$bundle/bin/ladygraph-ts-worker" ]] || fail 'bundle is missing bin/ladygraph-ts-worker'
installed_version=$("$bundle/bin/ladygraph" version)
[[ "$installed_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] ||
  fail "bundle reported invalid version: $installed_version"
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
  printf '#!/usr/bin/env bash\nset -euo pipefail\n# Managed by the Ladygraph release installer.\nexec %q "$@"\n' \
    "$target" >"$temporary"
  chmod 0755 "$temporary"
  mv -- "$temporary" "$launcher"
  created_launchers+=("$launcher")
}

validate_launcher "$bin_dir/ladygraph" "$install_root/bin/ladygraph"
validate_launcher "$bin_dir/ladygraph-ts-worker" "$install_root/bin/ladygraph-ts-worker"

if [[ -e "$install_root" || -L "$install_root" ]]; then
  backup_root="${install_root}.previous"
  [[ ! -e "$backup_root" && ! -L "$backup_root" ]] ||
    fail "previous installation exists: $backup_root"
  mv -- "$install_root" "$backup_root"
fi
mv -- "$bundle" "$install_root"
new_root_installed=true
create_launcher ladygraph "$install_root/bin/ladygraph"
create_launcher ladygraph-ts-worker "$install_root/bin/ladygraph-ts-worker"

if [[ -n "$backup_root" ]]; then
  rm -rf -- "$backup_root"
  backup_root=""
fi
new_root_installed=false

printf 'ladygraph install: installed %s in %s\n' "$installed_version" "$install_root"
printf 'ladygraph install: launchers in %s\n' "$bin_dir"
if [[ ":${PATH}:" != *":${bin_dir}:"* ]]; then
  printf 'ladygraph install: add %s to PATH before using ladygraph\n' "$bin_dir"
fi
printf 'ladygraph install: run "ladygraph init" before starting the MCP server\n'
