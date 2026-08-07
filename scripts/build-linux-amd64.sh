#!/usr/bin/env bash
set -euo pipefail

# Build an installable Linux amd64 bundle without mutating indexed repositories
# or benchmark inputs. The output directory is generated and may be removed.

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd -- "$root"
output_dir=${1:-"$root/dist/ladygraph-linux-amd64"}
if [[ "$output_dir" != /* ]]; then
  output_dir="$root/$output_dir"
fi

fail() {
  printf 'build-linux-amd64: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

for command in go node pnpm git install find sort sha256sum stat tr awk; do
  require_command "$command"
done

[[ "$(uname -s)" == "Linux" ]] || fail "host must be Linux"
[[ "$(uname -m)" == "x86_64" ]] || fail "host must be x86_64"
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
native_library="$native_dir/liblbug.so"
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
mkdir -p \
  "$output_dir/bin" \
  "$output_dir/lib" \
  "$output_dir/grammars" \
  "$output_dir/licenses/third-party" \
  "$output_dir/worker/node_modules"

printf 'build-linux-amd64: installing worker dependencies\n' >&2
pnpm --dir "$root/ts-worker" install --frozen-lockfile
pnpm --dir "$root/ts-worker" build

if [[ -f "$root/web/package.json" ]]; then
  [[ -f "$root/web/pnpm-lock.yaml" ]] ||
    fail "web package is missing its frozen lockfile: $root/web/pnpm-lock.yaml"
  printf 'build-linux-amd64: installing web dependencies\n' >&2
  pnpm --dir "$root/web" install --frozen-lockfile
  pnpm --dir "$root/web" build
fi

web_dist="$root/web/dist"
web_assets=false
if [[ -d "$web_dist" ]]; then
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

build_id="ladygraph-${source_commit}-${source_dirty}"

printf 'build-linux-amd64: compiling Go binary\n' >&2
CGO_ENABLED=1 \
CGO_CFLAGS="-I$native_dir" \
CGO_LDFLAGS="-L$native_dir -llbug -Wl,-rpath,\$ORIGIN/../lib" \
GOOS=linux \
GOARCH=amd64 \
go build \
  -tags "$build_tags" \
  -trimpath \
  -buildvcs=true \
  -ldflags "-buildid=$build_id" \
  -o "$output_dir/bin/ladygraph" \
  ./cmd/ladygraph


install -m 0755 "$native_library" "$output_dir/lib/liblbug.so"
install -m 0644 "$root/LICENSE" "$output_dir/licenses/LICENSE"
install -m 0644 "$root/THIRD_PARTY_NOTICES.md" "$output_dir/licenses/THIRD_PARTY_NOTICES.md"
install -m 0644 "$root/docs/dependencies/ladybugdb.md" "$output_dir/licenses/ladybugdb-provenance.md"
install -m 0644 "$root/grammars/manifest.json" "$output_dir/grammars/manifest.json"
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

cat > "$output_dir/bin/ladygraph-ts-worker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

bundle_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
if [[ "${1:-}" == "facts" ]]; then
  shift
  exec node "$bundle_root/worker/dist/facts-cli.js" "$@"
fi
exec node "$bundle_root/worker/dist/index.js" "$@"
EOF
chmod 0755 "$output_dir/bin/ladygraph-ts-worker"

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

release_version=$(LD_LIBRARY_PATH="$output_dir/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}" \
  "$output_dir/bin/ladygraph" version)
grammar_sha256=$(sha256sum "$output_dir/grammars/manifest.json" | awk '{print $1}')
ladybug_sha256=$(sha256sum "$output_dir/lib/liblbug.so" | awk '{print $1}')
artifact_dirs=(bin lib worker grammars licenses)
if [[ "$web_assets" == true ]]; then
  artifact_dirs+=(web)
fi


write_artifacts() {
  local first=true file relative sha256 bytes
  while IFS= read -r relative; do
    file="$output_dir/$relative"
    sha256=$(sha256sum "$file" | awk '{print $1}')
    bytes=$(stat -c '%s' "$file")
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
  "product": "ladygraph",
  "release": "$release_version",
  "target": {
    "os": "linux",
    "arch": "amd64"
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
    "canonical": 2,
    "snapshot_row_format": 3
  },
  "resolver_version": null,
  "grammars": {
    "manifest": "grammars/manifest.json",
    "sha256": "$grammar_sha256"
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
      sha256sum "$relative"
    done
  )
}

write_checksums > "$output_dir/SHA256SUMS"
(
  cd "$output_dir"
  sha256sum -c SHA256SUMS >/dev/null
)

printf 'build-linux-amd64: bundle ready: %s\n' "$output_dir" >&2
printf '%s\n' "$output_dir"
