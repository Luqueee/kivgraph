#!/usr/bin/env bash
set -euo pipefail

# Build an installable bundle for a supported distribution target without
# mutating indexed repositories or benchmark inputs. The output directory is
# generated and may be removed.
#
# The bundle links LadybugDB natively, so the host must be the target: there is
# no cross-compilation path for cgo here.

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd -- "$root"
mcp_only=false
requested_version=${KIVGRAPH_VERSION:-}
requested_target=${KIVGRAPH_TARGET:-}
output_argument=""
usage() {
  printf 'usage: %s [--target OS/ARCH] [--mcp-only] [--version VERSION] [OUTPUT_DIR]\n' "$0" >&2
  printf 'supported targets: linux/amd64, darwin/arm64\n' >&2
}
while (( $# > 0 )); do
  case "$1" in
    --mcp-only)
      mcp_only=true
      shift
      ;;
    --target)
      [[ $# -ge 2 ]] || { usage; exit 2; }
      requested_target=$2
      shift 2
      ;;
    --version)
      [[ $# -ge 2 ]] || { usage; exit 2; }
      requested_version=$2
      shift 2
      ;;
    --help)
      usage
      exit 0
      ;;
    --*)
      usage
      exit 2
      ;;
    *)
      [[ -z "$output_argument" ]] || { usage; exit 2; }
      output_argument=$1
      shift
      ;;
  esac
done
if [[ -n "$requested_version" && ! "$requested_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  printf 'build-bundle: invalid release version: %s\n' "$requested_version" >&2
  exit 2
fi

fail() {
  printf 'build-bundle: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

# host_target names the only bundle this machine can produce. cgo links the
# pinned LadybugDB library, so a bundle is always built by its own platform.
host_target() {
  local system machine
  system="$(uname -s)"
  machine="$(uname -m)"
  case "${system}/${machine}" in
    Linux/x86_64) printf 'linux/amd64' ;;
    Darwin/arm64) printf 'darwin/arm64' ;;
    MINGW*/x86_64 | MSYS*/x86_64 | CYGWIN*/x86_64) printf 'windows/amd64' ;;
    *) fail "unsupported host ${system}/${machine}: supported targets are linux/amd64, darwin/arm64 and windows/amd64" ;;
  esac
}

target=$(host_target)
if [[ -n "$requested_target" && "$requested_target" != "$target" ]]; then
  fail "requested target $requested_target cannot be built on a $target host"
fi
target_os=${target%/*}
target_arch=${target#*/}
bundle_name="kivgraph-${target_os}-${target_arch}"

# What differs per target, and nothing else: where the native library goes,
# what it is called, and the relative search path the executable carries to
# find it. Windows has no third answer to the last of those -- it has no answer
# at all, which is why the library moves instead.
case "$target" in
  linux/amd64)
    native_library_name=liblbug.so
    native_library_subdir=lib
    program_suffix=''
    # $ORIGIN is expanded by the dynamic loader, not by the shell.
    rpath='$ORIGIN/../lib'
    ;;
  darwin/arm64)
    native_library_name=liblbug.dylib
    native_library_subdir=lib
    program_suffix=''
    # The pinned dylib declares @rpath/liblbug.dylib as its install name and
    # carries a linker ad-hoc signature that copying preserves. Rewriting the
    # install name would invalidate it, so the executable carries the rpath.
    rpath='@loader_path/../lib'
    ;;
  windows/amd64)
    native_library_name=lbug_shared.dll
    # There is no RUNPATH here and no flag that adds one: Windows resolves a
    # DLL from the directory of the executable that needs it, so the library
    # sits beside the binary rather than in a lib/ the binary points at. That
    # is a change to the bundle layout and not a flag, which is why the
    # directory is a variable and the check below asserts adjacency rather
    # than a path.
    native_library_subdir=bin
    program_suffix='.exe'
    rpath=''
    ;;
esac

output_dir=${output_argument:-"$root/dist/$bundle_name"}
if [[ "$output_dir" != /* ]]; then
  output_dir="$root/$output_dir"
fi

for command in go node pnpm git install find sort stat tr awk jq; do
  require_command "$command"
done

# The executable's RUNPATH is rewritten after linking, with the tool each
# object format has for it.
case "$target" in
  darwin/*)
    for command in otool install_name_tool codesign; do
      require_command "$command"
    done
    ;;
  linux/*)
    require_command patchelf
    ;;
  windows/*)
    # Nothing: there is no load command to rewrite, and unzip is required by
    # the fetch scripts that need it rather than here.
    ;;
esac

# sha256_of and sha256_check keep one checksum contract across hosts: macOS
# ships shasum instead of the coreutils sha256sum used on Linux.
if command -v sha256sum >/dev/null 2>&1; then
  sha256_of() { sha256sum "$1" | awk '{print $1}'; }
  sha256_check() { sha256sum -c "$1"; }
elif command -v shasum >/dev/null 2>&1; then
  sha256_of() { shasum -a 256 "$1" | awk '{print $1}'; }
  sha256_check() { shasum -a 256 -c "$1"; }
else
  fail "no SHA-256 tool found: install coreutils or shasum"
fi

# file_bytes reads a file size with the flag its stat understands.
if stat -c '%s' "$root/go.mod" >/dev/null 2>&1; then
  file_bytes() { stat -c '%s' "$1"; }
else
  file_bytes() { stat -f '%z' "$1"; }
fi

[[ -x "$root/scripts/fetch-ladybug.sh" ]] || fail "scripts/fetch-ladybug.sh is not executable"

source_commit=$(git -C "$root" rev-parse HEAD)
source_dirty=false
if [[ -n "$(git -C "$root" status --porcelain --untracked-files=normal)" ]]; then
  source_dirty=true
fi

go_version=$(go version | awk '{print $3}')
node_version=$(node --version)
pnpm_version=$(pnpm --version)
typescript_version=$(pnpm --dir "$root/ts-worker" exec tsc --version | awk '/Version/{print $2; exit}')
[[ -n "$typescript_version" ]] || fail "could not determine TypeScript version"

native_dir=$(cd "$root" && scripts/fetch-ladybug.sh)
native_library="$native_dir/$native_library_name"
[[ -f "$native_library" ]] || fail "LadybugDB library not found: $native_library"
native_verified="$native_dir/.verified"
if [[ ! -f "$native_verified" ]]; then
  native_verified="$(dirname -- "$native_dir")/.verified"
fi
[[ -f "$native_verified" ]] || fail "LadybugDB verification marker not found"
native_archive_sha256=$(<"$native_verified")
[[ "$native_archive_sha256" =~ ^[0-9a-f]{64}$ ]] ||
  fail "invalid LadybugDB archive digest in $native_verified"

binding_version=$(go list -m -f '{{.Version}}' github.com/LadybugDB/go-ladybug)
[[ -n "$binding_version" ]] || fail "could not determine LadybugDB Go binding version"

rm -rf "$output_dir"
# lib/ is created only where something goes in it. An empty directory in a
# bundle is a claim about a layout, and on the platform whose library sits
# beside the executable it would be the wrong claim.
mkdir -p \
  "$output_dir/bin" \
  "$output_dir/grammars" \
  "$output_dir/licenses/third-party" \
  "$output_dir/skills/kivgraph" \
  "$output_dir/tools" \
  "$output_dir/worker/node_modules"

printf 'build-bundle: installing worker dependencies\n' >&2
pnpm --dir "$root/ts-worker" install --frozen-lockfile
pnpm --dir "$root/ts-worker" build

if [[ "$mcp_only" != true && -f "$root/web/package.json" ]]; then
  [[ -f "$root/web/pnpm-lock.yaml" ]] ||
    fail "web package is missing its frozen lockfile: $root/web/pnpm-lock.yaml"
  printf 'build-bundle: installing web dependencies\n' >&2
  pnpm --dir "$root/web" install --frozen-lockfile
  pnpm --dir "$root/web" build
fi

web_dist="$root/web/dist"
web_assets=false
if [[ "$mcp_only" != true && -d "$web_dist" ]]; then
  [[ -f "$web_dist/index.html" ]] ||
    fail "web bundle is missing required entry point: $web_dist/index.html"
  [[ -z "$(find -L "$web_dist" -type l -print -quit)" ]] ||
    fail "web bundle must not contain symbolic links: $web_dist"
  mkdir -p "$output_dir/web"
  cp -aL "$web_dist/." "$output_dir/web/"
  web_assets=true
fi

build_tags=ladybug
if [[ "$web_assets" == true ]]; then
  build_tags=ladybug,webassets
fi

build_id="kivgraph-${source_commit}-${source_dirty}"
ldflags="-buildid=$build_id"
if [[ -n "$requested_version" ]]; then
  ldflags+=" -X github.com/Luqueee/kivgraph/internal/version.Value=$requested_version"
fi

# The relative RUNPATH is the one entry the executable must carry, and it is
# declared here rather than in CGO_LDFLAGS because cgo applies those flags to
# every package it compiles: the linker saw one copy per package, warned about
# each duplicate and kept the first. `-extldflags` reaches the external link
# once, which is the number of times an rpath needs to be named. `-llbug` is
# left out for the same reason -- the binding already declares it, and the
# `-L` above is what makes it resolve against the pinned build.
if [[ -n "$rpath" ]]; then
  ldflags+=" -extldflags=-Wl,-rpath,$rpath"
fi

printf 'build-bundle: compiling Go binary for %s\n' "$target" >&2
CGO_ENABLED=1 \
CGO_CFLAGS="-I$native_dir" \
CGO_LDFLAGS="-L$native_dir" \
GOOS="$target_os" \
GOARCH="$target_arch" \
go build \
  -tags "$build_tags" \
  -trimpath \
  -buildvcs=true \
  -ldflags "$ldflags" \
  -o "$output_dir/bin/kivgraph$program_suffix" \
  ./cmd/kivgraph

# The pinned Go binding declares its own RUNPATH towards its module directory,
# which holds no library and names the machine that built the bundle. The
# executable is rewritten to carry exactly the relative entry, so a bundle does
# not depend on where its builder kept its module cache.
normalise_runpath() {
  local binary="$output_dir/bin/kivgraph$program_suffix" observed
  case "$target" in
    darwin/*)
      while read -r observed; do
        [[ -n "$observed" && "$observed" != "$rpath" ]] || continue
        install_name_tool -delete_rpath "$observed" "$binary"
      done < <(otool -l "$binary" | awk '/LC_RPATH/{found=1} found && $1=="path"{print $2; found=0}')
      # Editing the load commands invalidates the linker signature.
      codesign --force --sign - "$binary"
      ;;
    linux/*)
      patchelf --set-rpath "$rpath" "$binary"
      ;;
    windows/*)
      # A PE carries no search path to normalise. What replaces this check is
      # the adjacency assert below, which is the property that actually makes
      # the bundle relocatable here.
      ;;
  esac
}

normalise_runpath

assert_single_runpath() {
  local binary="$output_dir/bin/kivgraph$program_suffix" observed
  case "$target" in
    darwin/*)
      observed=$(otool -l "$binary" | awk '/LC_RPATH/{found=1} found && $1=="path"{print $2; found=0}')
      ;;
    linux/*)
      observed=$(patchelf --print-rpath "$binary")
      ;;
    windows/*)
      # The equivalent claim: the loader looks beside the executable, so the
      # library being there is what a RUNPATH buys elsewhere. Asserted after
      # the library is installed, which is why this returns rather than
      # falling through to a comparison of two empty strings.
      return 0
      ;;
  esac
  [[ "$observed" == "$rpath" ]] ||
    fail "executable declares RUNPATH '$observed', want exactly '$rpath'"
}

assert_single_runpath

mkdir -p "$output_dir/$native_library_subdir"
install -m 0755 "$native_library" "$output_dir/$native_library_subdir/$native_library_name"

# On the platform with no search path to declare, adjacency is the contract, so
# it is asserted where every other target asserts its RUNPATH.
if [[ "$target" == windows/* ]]; then
  [[ -f "$output_dir/bin/$native_library_name" ]] ||
    fail "the native library is not beside the executable, which is the only place this platform looks"
fi

# rust-analyzer is the engine that reads Rust. It is pinned in
# tools/manifest.json and travels inside the bundle so an installation does not
# have to add it by hand; `cargo` is still the caller's, because a workspace
# cannot be loaded without it.
rust_analyzer_dir=$("$root/scripts/fetch-rust-analyzer.sh")
install -m 0755 "$rust_analyzer_dir/rust-analyzer$program_suffix" "$output_dir/bin/rust-analyzer$program_suffix"
mkdir -p "$output_dir/licenses/third-party/rust-analyzer"
install -m 0644 "$rust_analyzer_dir/LICENSE-APACHE" \
  "$output_dir/licenses/third-party/rust-analyzer/LICENSE-APACHE"
install -m 0644 "$rust_analyzer_dir/LICENSE-MIT" \
  "$output_dir/licenses/third-party/rust-analyzer/LICENSE-MIT"
install -m 0644 "$root/tools/manifest.json" "$output_dir/tools/manifest.json"
install -m 0644 "$root/LICENSE" "$output_dir/licenses/LICENSE"
install -m 0644 "$root/THIRD_PARTY_NOTICES.md" "$output_dir/licenses/THIRD_PARTY_NOTICES.md"
install -m 0644 "$root/docs/dependencies/ladybugdb.md" "$output_dir/licenses/ladybugdb-provenance.md"
install -m 0644 "$root/grammars/manifest.json" "$output_dir/grammars/manifest.json"
install -m 0644 "$root/internal/integrations/assets/kivgraph/SKILL.md" "$output_dir/skills/kivgraph/SKILL.md"
typescript_package_dir=$(cd "$root/ts-worker/node_modules/typescript" && pwd -P)
typescript_platform_dir="$(dirname "$typescript_package_dir")/@typescript"
[[ -d "$typescript_platform_dir" ]] ||
  fail "TypeScript platform package directory not found: $typescript_platform_dir"
mkdir -p "$output_dir/worker/node_modules/@typescript"
cp -aL "$typescript_platform_dir/." \
  "$output_dir/worker/node_modules/@typescript/"
cp -aL "$root/ts-worker/node_modules/typescript" \
  "$output_dir/worker/node_modules/typescript"
install -m 0644 "$root/ts-worker/package.json" "$output_dir/worker/package.json"
install -m 0644 "$root/ts-worker/pnpm-lock.yaml" "$output_dir/worker/pnpm-lock.yaml"
cp -a "$root/ts-worker/dist" "$output_dir/worker/dist"
mkdir -p "$output_dir/worker/python-worker"
install -m 0644 "$root/python-worker/index.py" "$output_dir/worker/python-worker/index.py"
install -m 0644 "$root/python-worker/pyright_index.py" "$output_dir/worker/python-worker/pyright_index.py"

# The worker shim is a script, and a script is not portable the way a compiled
# binary is: the shebang form runs where there is an interpreter for one, and
# Windows needs a batch file it will run itself.
if [[ "$target" == windows/* ]]; then
  # Batch defers `shift` until the end of a parenthesised block, so the branch
  # is written with labels rather than an `if (...)`, and `%*` is unaffected by
  # `shift` at all -- which is why the arguments are rebuilt one at a time
  # instead of forwarded wholesale. Both are the kind of thing that appears to
  # work until an argument is dropped silently.
  #
  # `%~dp0` does not resolve a symlink, where the shell form used `pwd -P`
  # because the worker compares its own realpath. Windows installs do not reach
  # the bundle through a link today; if one ever does, this is where it breaks.
  cat > "$output_dir/bin/kivgraph-ts-worker.cmd" <<'BATCH'
@echo off
setlocal enabledelayedexpansion
set "BUNDLE_ROOT=%~dp0.."
set "ENTRY=%BUNDLE_ROOT%\worker\dist\index.js"
if /I "%~1"=="facts" goto facts
goto collect

:facts
set "ENTRY=%BUNDLE_ROOT%\worker\dist\facts-cli.js"
shift

:collect
set "ARGS="
:next
if "%~1"=="" goto run
set "ARGS=!ARGS! "%~1""
shift
goto next

:run
node "%ENTRY%"!ARGS!
exit /b %ERRORLEVEL%
BATCH
else
  cat > "$output_dir/bin/kivgraph-ts-worker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

# pwd -P: the worker entry point compares its own realpath, and an install
# root reached through a symlink would otherwise disagree with it.
bundle_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
if [[ "${1:-}" == "facts" ]]; then
  shift
  exec node "$bundle_root/worker/dist/facts-cli.js" "$@"
fi
exec node "$bundle_root/worker/dist/index.js" "$@"
EOF
  chmod 0755 "$output_dir/bin/kivgraph-ts-worker"
fi

copy_module_licenses() {
  local module version module_dir safe candidate
  while IFS='|' read -r module version module_dir; do
    [[ -n "$module" && -n "$module_dir" ]] || continue
    safe=$(printf '%s@%s' "$module" "$version" | tr '/@' '__')
    for candidate in LICENSE LICENSE.txt COPYING NOTICE; do
      if [[ -f "$module_dir/$candidate" ]]; then
        install -m 0644 "$module_dir/$candidate" \
          "$output_dir/licenses/third-party/${safe}-${candidate}"
      fi
    done
  done < <(go list -m -f '{{.Path}}|{{.Version}}|{{.Dir}}' all)
}

copy_module_licenses

# The relative RUNPATH must be enough to start the executable. Running it
# without any library search variable is the check that proves it.
release_version=$("$output_dir/bin/kivgraph$program_suffix" version)
if [[ -n "$requested_version" && "$release_version" != "$requested_version" ]]; then
  fail "built release version $release_version does not match requested $requested_version"
fi
version_json=$("$output_dir/bin/kivgraph$program_suffix" version --json)
canonical_schema=$(jq -er '.schema' <<<"$version_json") ||
  fail "built binary reported no canonical schema version"
snapshot_row_format=$(jq -er '.snapshot_row_format' <<<"$version_json") ||
  fail "built binary reported no snapshot row format version"
grammar_sha256=$(sha256_of "$output_dir/grammars/manifest.json")
rust_analyzer_version=$(jq -r '.tools[] | select(.name=="rust-analyzer") | .version' "$root/tools/manifest.json")
rust_analyzer_release=$(jq -r '.tools[] | select(.name=="rust-analyzer") | .release' "$root/tools/manifest.json")
rust_analyzer_sha256=$(sha256_of "$output_dir/bin/rust-analyzer$program_suffix")
tools_sha256=$(sha256_of "$output_dir/tools/manifest.json")
ladybug_sha256=$(sha256_of "$output_dir/$native_library_subdir/$native_library_name")
artifact_dirs=(bin worker grammars licenses skills tools)
if [[ "$native_library_subdir" == lib ]]; then
  artifact_dirs+=(lib)
fi
if [[ "$web_assets" == true ]]; then
  artifact_dirs+=(web)
fi

write_artifacts() {
  local first=true file relative sha256 bytes
  while IFS= read -r relative; do
    file="$output_dir/$relative"
    sha256=$(sha256_of "$file")
    bytes=$(file_bytes "$file")
    if [[ "$first" == true ]]; then
      first=false
    else
      printf ',\n'
    fi
    printf '    {"path":"%s","sha256":"%s","bytes":%s}' \
      "$relative" "$sha256" "$bytes"
  done < <(
    cd "$output_dir"
    find "${artifact_dirs[@]}" -type f -print | LC_ALL=C sort
  )
}

cat > "$output_dir/manifest.json" <<EOF
{
  "manifest_version": 1,
  "product": "kivgraph",
  "release": "$release_version",
  "target": {
    "os": "$target_os",
    "arch": "$target_arch"
  },
  "source": {
    "commit": "$source_commit",
    "dirty": $source_dirty
  },
  "toolchain": {
    "go": "$go_version",
    "node": "$node_version",
    "pnpm": "$pnpm_version",
    "typescript": "$typescript_version"
  },
  "ladybugdb": {
    "core": "v0.13.1",
    "binding": "$binding_version",
    "archive_sha256": "$native_archive_sha256",
    "library_sha256": "$ladybug_sha256"
  },
  "schema": {
    "canonical": $canonical_schema,
    "snapshot_row_format": $snapshot_row_format
  },
  "resolver_version": null,
  "grammars": {
    "manifest": "grammars/manifest.json",
    "sha256": "$grammar_sha256"
  },
  "tools": {
    "manifest": "tools/manifest.json",
    "sha256": "$tools_sha256",
    "rust_analyzer": {
      "version": "$rust_analyzer_version",
      "release": "$rust_analyzer_release",
      "binary": "bin/rust-analyzer",
      "sha256": "$rust_analyzer_sha256"
    }
  },
  "artifacts": [
$(write_artifacts)
  ]
}
EOF

write_checksums() {
  local relative
  (
    cd "$output_dir"
    {
      find "${artifact_dirs[@]}" -type f -print
      printf '%s\n' manifest.json
    } | LC_ALL=C sort | while IFS= read -r relative; do
      printf '%s  %s\n' "$(sha256_of "$relative")" "$relative"
    done
  )
}

write_checksums > "$output_dir/SHA256SUMS"
(
  cd "$output_dir"
  sha256_check SHA256SUMS >/dev/null
)

printf 'build-bundle: bundle ready: %s\n' "$output_dir" >&2
printf '%s\n' "$output_dir"
