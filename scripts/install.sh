#!/usr/bin/env bash
set -euo pipefail

# Install only the STDIO MCP server. The web viewer is intentionally omitted.

usage() {
  cat <<'EOF'
Usage: scripts/install.sh

Build and install an MCP-only Ladygraph bundle from this checkout. When the
script is piped from the repository URL, it clones the repository first.

Environment:
  LADYGRAPH_INSTALL_ROOT  Bundle directory (default: ~/.local/opt/ladygraph)
  LADYGRAPH_BIN_DIR       Launcher directory (default: ~/.local/bin)
  LADYGRAPH_REPOSITORY    Repository URL for piped installs
  LADYGRAPH_REF           Branch or tag for piped installs (default: main)
EOF
}

fail() {
  printf 'ladygraph install: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi
[[ "$#" -eq 0 ]] || fail "unknown argument: $1 (use --help for usage)"

for command in git go node pnpm curl install find sort sha256sum stat tr awk tar; do
  require_command "$command"
done

[[ "$(uname -s)" == "Linux" ]] || fail "host must be Linux"
[[ "$(uname -m)" == "x86_64" ]] || fail "host must be x86_64"
[[ -n "${HOME:-}" ]] || fail 'HOME is not set'

install_root=${LADYGRAPH_INSTALL_ROOT:-"$HOME/.local/opt/ladygraph"}
bin_dir=${LADYGRAPH_BIN_DIR:-"$HOME/.local/bin"}
[[ "$install_root" == /* ]] || fail "LADYGRAPH_INSTALL_ROOT must be absolute"
[[ "$bin_dir" == /* ]] || fail "LADYGRAPH_BIN_DIR must be absolute"
[[ "$install_root" != "/" && "$bin_dir" != "/" ]] || fail 'installation paths must not be /'

source_checkout=""
staging_parent=""
backup_root=""
new_root_installed=false
cleanup() {
  local status=$?
  if (( status != 0 )); then
    if [[ "$new_root_installed" == true ]]; then
      rm -rf -- "$install_root"
    fi
    if [[ -n "$backup_root" ]]; then
      if [[ -e "$backup_root" || -L "$backup_root" ]]; then
        mv -- "$backup_root" "$install_root" || true
      fi
    fi
  fi
  if [[ -n "$staging_parent" ]]; then
    rm -rf -- "$staging_parent"
  fi
  if [[ -n "$source_checkout" ]]; then
    rm -rf -- "$source_checkout"
  fi
  exit "$status"
}
trap cleanup EXIT

script_source=${BASH_SOURCE[0]:-}
root=""
if [[ -n "$script_source" && -f "$script_source" ]]; then
  script_dir=$(cd -- "$(dirname -- "$script_source")" && pwd -P)
  root=$(cd -- "$script_dir/.." && pwd -P)
fi
if [[ -z "$root" || ! -f "$root/go.mod" || ! -x "$root/scripts/build-linux-amd64.sh" ]]; then
  source_checkout=$(mktemp -d "${TMPDIR:-/tmp}/ladygraph-source.XXXXXX")
  repository=${LADYGRAPH_REPOSITORY:-https://github.com/Luqueee/ladygraph.git}
  ref=${LADYGRAPH_REF:-main}
  printf 'ladygraph install: cloning %s (%s)\n' "$repository" "$ref" >&2
  git clone --depth 1 --branch "$ref" "$repository" "$source_checkout" >&2
  root="$source_checkout"
fi

[[ -x "$root/scripts/build-linux-amd64.sh" ]] || fail "build script not found in $root"
[[ -x "$root/scripts/fetch-ladybug.sh" ]] || fail "LadybugDB fetch script is not executable"

mkdir -p "$(dirname -- "$install_root")" "$bin_dir"
staging_parent=$(mktemp -d "$(dirname -- "$install_root")/.ladygraph-install.XXXXXX")
bundle="$staging_parent/bundle"

printf 'ladygraph install: building MCP-only bundle\n' >&2
"$root/scripts/build-linux-amd64.sh" --mcp-only "$bundle" >&2
[[ -x "$bundle/bin/ladygraph" ]] || fail 'bundle is missing bin/ladygraph'
[[ -x "$bundle/bin/ladygraph-ts-worker" ]] || fail 'bundle is missing bin/ladygraph-ts-worker'
[[ ! -e "$bundle/web" ]] || fail 'MCP-only bundle unexpectedly contains web assets'
(
  cd -- "$bundle"
  sha256sum -c SHA256SUMS >/dev/null
)

if [[ -e "$install_root" || -L "$install_root" ]]; then
  backup_root="${install_root}.previous"
  [[ ! -e "$backup_root" && ! -L "$backup_root" ]] ||
    fail "previous installation exists: $backup_root"
  mv -- "$install_root" "$backup_root"
fi
mv -- "$bundle" "$install_root"
new_root_installed=true

wrapper_dir="$staging_parent/wrappers"
mkdir -p "$wrapper_dir"
ladygraph_target=$(printf '%q' "$install_root/bin/ladygraph")
ladygraph_worker_target=$(printf '%q' "$install_root/bin/ladygraph-ts-worker")
printf '#!/usr/bin/env bash\nset -euo pipefail\nexec %s "$@"\n' \
  "$ladygraph_target" >"$wrapper_dir/ladygraph"
printf '#!/usr/bin/env bash\nset -euo pipefail\nexec %s "$@"\n' \
  "$ladygraph_worker_target" >"$wrapper_dir/ladygraph-ts-worker"
chmod 0755 "$wrapper_dir/ladygraph" "$wrapper_dir/ladygraph-ts-worker"
for command in ladygraph ladygraph-ts-worker; do
  rm -f -- "$bin_dir/$command"
  mv -- "$wrapper_dir/$command" "$bin_dir/$command"
done

if [[ -n "$backup_root" ]]; then
  rm -rf -- "$backup_root"
  backup_root=""
fi
new_root_installed=false

printf 'ladygraph install: MCP installed in %s\n' "$install_root"
printf 'ladygraph install: launchers installed in %s\n' "$bin_dir"
if [[ ":${PATH}:" != *":${bin_dir}:"* ]]; then
  printf 'ladygraph install: add %s to PATH before using ladygraph\n' "$bin_dir"
fi
printf 'ladygraph install: run "ladygraph init" to create its configuration\n'
