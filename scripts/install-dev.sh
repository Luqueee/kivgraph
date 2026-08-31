#!/usr/bin/env bash
set -euo pipefail

# Build the current checkout with the distribution flags, atomically replace
# the binary in an existing bundle, and restart its supervised daemon. The
# previous binary remains available for rollback until the restart works.

usage() {
  cat <<'EOF'
Usage: scripts/install-dev.sh [--bundle DIRECTORY] [--no-restart]

Build and install the current binary without publishing a release. By default
the script builds a complete local bundle, atomically replaces the executable
at ~/.local/opt/kivgraph/bin/kivgraph, and restarts an installed daemon. Other
bundle files are deliberately left unchanged.

Options:
  --bundle DIRECTORY  Install an already-built bundle instead of building one
  --no-restart        Replace the bundle without inspecting or restarting the daemon

Environment:
  KIVGRAPH_INSTALL_ROOT  Existing bundle directory (default: ~/.local/opt/kivgraph)
EOF
}

fail() {
  printf 'kivgraph dev install: %s\n' "$*" >&2
  exit 1
}

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
bundle_source=""
restart=true
while (( $# > 0 )); do
  case "$1" in
    --bundle)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      bundle_source=$2
      shift 2
      ;;
    --no-restart)
      restart=false
      shift
      ;;
    --help | -h)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1 (use --help for usage)"
      ;;
  esac
done

for command in awk cp mktemp mv uname; do
  command -v "$command" >/dev/null 2>&1 || fail "required command not found: $command"
done
if command -v sha256sum >/dev/null 2>&1; then
  sha256_of() { sha256sum "$1" | awk '{print $1}'; }
  sha256_check() { sha256sum -c "$1"; }
elif command -v shasum >/dev/null 2>&1; then
  sha256_of() { shasum -a 256 "$1" | awk '{print $1}'; }
  sha256_check() { shasum -a 256 -c "$1"; }
else
  fail "required command not found: sha256sum or shasum"
fi

case "$(uname -s)/$(uname -m)" in
  Linux/x86_64)
    host=linux
    native_relative=lib/liblbug.so
    ;;
  Darwin/arm64)
    host=darwin
    native_relative=lib/liblbug.dylib
    ;;
  *) fail "this development installer supports Linux/x86_64 and Darwin/arm64" ;;
esac

[[ -n "${HOME:-}" ]] || fail "HOME is not set"
install_root=${KIVGRAPH_INSTALL_ROOT:-"$HOME/.local/opt/kivgraph"}
[[ "$install_root" == /* ]] || fail "KIVGRAPH_INSTALL_ROOT must be absolute"
[[ "$install_root" != / ]] || fail "KIVGRAPH_INSTALL_ROOT must not be /"
[[ ! -L "$install_root" ]] || fail "refusing symbolic-link installation root: $install_root"
[[ -d "$install_root" && -f "$install_root/manifest.json" && -x "$install_root/bin/kivgraph" ]] ||
  fail "expected an existing Kivgraph installation at $install_root; install a release first"

lock_directory="$install_root/.kivgraph-dev-install.lock"
staging_parent=""
staged_binary=""
previous_binary=""
installed_binary="$install_root/bin/kivgraph"
binary_replaced=false
restart_attempted=false
lock_acquired=false

restart_daemon() {
  case "$host" in
    linux) systemctl --user restart "$supervisor_label.service" ;;
    darwin) launchctl kickstart -k "gui/$(id -u)/$supervisor_label" ;;
  esac
}

cleanup() {
  local status=$? rollback_ok=true
  if (( status != 0 )) && [[ "$binary_replaced" == true ]]; then
    printf 'kivgraph dev install: rolling back to the previous binary\n' >&2
    if ! mv -- "$previous_binary" "$installed_binary"; then
      rollback_ok=false
    fi
    if [[ "$rollback_ok" == true && "$restart_attempted" == true ]]; then
      restart_daemon >/dev/null 2>&1 || true
    fi
  fi
  [[ -z "$staged_binary" ]] || rm -f -- "$staged_binary"
  if [[ "$rollback_ok" == true ]]; then
    [[ -z "$previous_binary" ]] || rm -f -- "$previous_binary"
    [[ -z "$staging_parent" ]] || rm -rf -- "$staging_parent"
  else
    printf 'kivgraph dev install: automatic rollback failed; previous binary remains at %s\n' \
      "$previous_binary" >&2
  fi
  if [[ "$lock_acquired" == true ]] && ! rmdir -- "$lock_directory"; then
    printf 'kivgraph dev install: could not release installation lock: %s\n' "$lock_directory" >&2
    (( status != 0 )) || status=1
  fi
  trap - EXIT
  exit "$status"
}
trap cleanup EXIT

if ! mkdir -- "$lock_directory"; then
  fail "another development installation holds $lock_directory"
fi
lock_acquired=true

supervisor_state=skipped
supervisor_label=""
if [[ "$restart" == true ]]; then
  if ! supervisor_output=$("$install_root/bin/kivgraph" daemon status 2>&1); then
    fail "could not inspect the daemon supervisor; fix 'kivgraph daemon status' or pass --no-restart: $supervisor_output"
  fi
  supervisor_line=$(printf '%s\n' "$supervisor_output" | awk '/^daemon\.supervisor: state=/{print; exit}')
  [[ -n "$supervisor_line" ]] ||
    fail "daemon status returned no machine-readable supervisor state; pass --no-restart to override"
  supervisor_state=${supervisor_line#*state=}
  supervisor_state=${supervisor_state%% *}
  supervisor_label=${supervisor_line#* label=}
  supervisor_label=${supervisor_label%% *}
  case "$supervisor_state" in
    installed)
      [[ "$supervisor_label" == com.kivgraph.daemon.* ]] ||
        fail "refusing unexpected daemon supervisor label: $supervisor_label"
      ;;
    absent) ;;
    stale)
      fail "daemon supervisor is stale; run 'kivgraph daemon install' before replacing the bundle"
      ;;
    *)
      fail "unsupported daemon supervisor state: $supervisor_state"
      ;;
  esac
fi

install_parent=$(dirname -- "$install_root")
mkdir -p -- "$install_parent"
staging_parent=$(mktemp -d "$install_parent/.kivgraph-dev-install.XXXXXX")
candidate="$staging_parent/bundle"
staged_binary=$(mktemp "$install_root/bin/.kivgraph-dev-new.XXXXXX")
previous_binary=$(mktemp "$install_root/bin/.kivgraph-dev-previous.XXXXXX")

if [[ -n "$bundle_source" ]]; then
  [[ "$bundle_source" == /* ]] || bundle_source="$PWD/$bundle_source"
  [[ ! -L "$bundle_source" && -d "$bundle_source" ]] ||
    fail "bundle is not a directory or is a symbolic link: $bundle_source"
  mkdir -p -- "$candidate"
  cp -a "$bundle_source/." "$candidate/"
else
  printf 'kivgraph dev install: building the current checkout\n' >&2
  "$root/scripts/build-bundle.sh" "$candidate" >/dev/null
fi

[[ -f "$candidate/manifest.json" && -f "$candidate/SHA256SUMS" && -x "$candidate/bin/kivgraph" ]] ||
  fail "candidate is not a complete Kivgraph bundle: $candidate"
(
  cd -- "$candidate"
  sha256_check SHA256SUMS >/dev/null
) || fail "candidate bundle checksum verification failed"
"$candidate/bin/kivgraph" version --json >/dev/null ||
  fail "candidate Kivgraph binary does not run on this host"

[[ -f "$candidate/$native_relative" && -f "$install_root/$native_relative" ]] ||
  fail "candidate and installed bundles must both contain $native_relative"
candidate_native=$(sha256_of "$candidate/$native_relative")
installed_native=$(sha256_of "$install_root/$native_relative")
[[ "$candidate_native" == "$installed_native" ]] ||
  fail "candidate uses a different LadybugDB library; install the complete bundle instead"

cp -p -- "$installed_binary" "$previous_binary"
cp -p -- "$candidate/bin/kivgraph" "$staged_binary"
chmod 0755 "$staged_binary"
"$staged_binary" version --json >/dev/null ||
  fail "candidate binary does not run against the installed bundle"
binary_replaced=true
mv -- "$staged_binary" "$installed_binary"

if [[ "$supervisor_state" == installed ]]; then
  restart_attempted=true
  restart_daemon || fail "daemon restart failed"
  printf 'kivgraph dev install: daemon restarted (%s)\n' "$supervisor_label"
elif [[ "$supervisor_state" == absent ]]; then
  printf 'kivgraph dev install: no supervised daemon was installed; bundle replaced only\n'
else
  printf 'kivgraph dev install: daemon restart skipped by request\n'
fi

binary_replaced=false
printf 'kivgraph dev install: installed the current checkout binary at %s\n' "$installed_binary"
