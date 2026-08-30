#!/usr/bin/env bash
set -euo pipefail

# Remove the bundle and launchers written by scripts/install.sh. Configuration,
# repository registrations and graph state are user data and remain in place.

usage() {
  cat <<'EOF'
Usage: scripts/uninstall.sh [--yes]

Remove the Kivgraph release bundle and its managed launchers. Configuration,
repository registrations and graph state are preserved.

Options:
  --yes  do not ask for confirmation (required for non-interactive use)

Environment:
  KIVGRAPH_INSTALL_ROOT  bundle directory (default: ~/.local/opt/kivgraph)
  KIVGRAPH_BIN_DIR        launcher directory (default: ~/.local/bin)
EOF
}

fail() {
  printf 'kivgraph uninstall: %s\n' "$*" >&2
  exit 1
}

yes=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --yes) yes=true ;;
    --help|-h) usage; exit 0 ;;
    *) fail "unknown argument: $1 (use --help for usage)" ;;
  esac
  shift
done

[[ -n "${HOME:-}" ]] || fail 'HOME is not set'
install_root=${KIVGRAPH_INSTALL_ROOT:-"$HOME/.local/opt/kivgraph"}
bin_dir=${KIVGRAPH_BIN_DIR:-"$HOME/.local/bin"}
[[ "$install_root" == /* && "$bin_dir" == /* ]] ||
  fail 'installation paths must be absolute'
[[ "$install_root" != "/" && "$bin_dir" != "/" ]] ||
  fail 'installation paths must not be /'
case "$install_root/$bin_dir" in
  */..|*/../*|../*|..)
    fail 'installation paths must not contain a parent-directory component'
    ;;
esac
[[ ! -L "$install_root" ]] || fail "refusing symbolic-link installation root: $install_root"

launcher_is_managed() {
	local launcher=$1 target=$2
	[[ -e "$launcher" || -L "$launcher" ]] || return 1
  [[ ! -L "$launcher" ]] || return 1
  [[ -f "$launcher" ]] || return 1
  grep -Fqx -- '# Managed by the Kivgraph release installer.' "$launcher" || return 1
  grep -Fq -- "$target" "$launcher"
}

bundle_present=false
if [[ -e "$install_root" ]]; then
  [[ -d "$install_root" ]] || fail "installation root is not a directory: $install_root"
  [[ -f "$install_root/manifest.json" && -x "$install_root/bin/kivgraph" ]] ||
    fail "refusing to remove a directory that is not a Kivgraph installation: $install_root"
  bundle_present=true
fi

managed_launchers=()
if launcher_is_managed "$bin_dir/kivgraph" "$install_root/bin/kivgraph"; then
  managed_launchers+=("$bin_dir/kivgraph")
elif [[ -e "$bin_dir/kivgraph" || -L "$bin_dir/kivgraph" ]]; then
  fail "refusing to remove unrelated launcher: $bin_dir/kivgraph"
fi
if launcher_is_managed "$bin_dir/kivgraph-ts-worker" "$install_root/bin/kivgraph-ts-worker"; then
  managed_launchers+=("$bin_dir/kivgraph-ts-worker")
elif [[ -e "$bin_dir/kivgraph-ts-worker" || -L "$bin_dir/kivgraph-ts-worker" ]]; then
  fail "refusing to remove unrelated launcher: $bin_dir/kivgraph-ts-worker"
fi

if [[ "$bundle_present" == false && ${#managed_launchers[@]} -eq 0 ]]; then
  printf 'kivgraph uninstall: no managed installation found\n'
  exit 0
fi

if [[ "$yes" == false ]]; then
  [[ -t 0 && -t 1 ]] || fail 'confirmation required; rerun with --yes'
  printf 'Remove Kivgraph bundle %s and %d launcher(s)? [y/N] ' \
    "$install_root" "${#managed_launchers[@]}" >&2
  IFS= read -r answer || answer=''
  answer=$(printf '%s' "$answer" | tr '[:upper:]' '[:lower:]')
  case "$answer" in
    y|yes) ;;
    *) printf 'kivgraph uninstall: cancelled\n' >&2; exit 0 ;;
  esac
fi

# The exact path was validated above, and only files carrying the launcher's
# installed target are removed. This keeps the command safe in the presence of
# another program named kivgraph or a user-owned directory beside the bundle.
for launcher in "${managed_launchers[@]+${managed_launchers[@]}}"; do
  rm -f -- "$launcher"
done
if [[ "$bundle_present" == true ]]; then
  rm -rf -- "$install_root"
fi

printf 'kivgraph uninstall: removed the managed bundle and launchers\n'
printf 'kivgraph uninstall: configuration and graph state were preserved\n'
