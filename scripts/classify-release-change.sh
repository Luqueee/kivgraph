#!/usr/bin/env bash
set -euo pipefail

# classify-release-change.sh decides whether a revision contains only the
# metadata commit that prepares a release. It is deliberately conservative:
# anything it cannot prove to be a release-only change returns false, so the
# caller falls back to the complete CI.

usage() {
	printf 'usage: %s <event> <base-commit> <head-commit>\n' "$0" >&2
	printf '       %s selftest\n' "$0" >&2
}

release_subject='^chore\(release\): prepare (v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?)( \(#[0-9]+\))?$'
script_path=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/$(basename -- "${BASH_SOURCE[0]}")

is_allowed_path() {
	local path=$1 release_tag=$2
	case "$path" in
	README.md | \
	docs/installation.md | \
	internal/version/version.go | \
	landing/src/content/docs/install.md | \
	landing/src/content/releases/*.md)
		if [[ "$path" == landing/src/content/releases/*.md ]]; then
			[[ "$path" == "landing/src/content/releases/$release_tag.md" ]] || return 1
		fi
		return 0
      ;;
    *)
      return 1
      ;;
  esac
}

classify() {
	local event=$1 base=$2 head=$3 subject path release_tag changed_paths
	if [[ "$event" == push ]] && ! git merge-base --is-ancestor "$base" "$head" 2>/dev/null; then
		printf 'false\n'
		return 0
	fi
  [[ "$base" =~ ^[0-9a-fA-F]{40}$ && "$head" =~ ^[0-9a-fA-F]{40}$ ]] || {
    printf 'false\n'
    return 0
  }
  git cat-file -e "$base^{commit}" 2>/dev/null || {
    printf 'false\n'
    return 0
  }
  git cat-file -e "$head^{commit}" 2>/dev/null || {
    printf 'false\n'
    return 0
  }

  subject=$(git show -s --format=%s "$head") || {
    printf 'false\n'
    return 0
  }
	[[ "$subject" =~ $release_subject ]] || {
		printf 'false\n'
		return 0
	}
	release_tag=${BASH_REMATCH[1]}

  # The complete commit range matters for a push containing more than one
  # commit: inspecting only HEAD would let a release commit hide an earlier
  # code change in the same push. The final tree is not enough here because an
  # earlier change could be reverted before HEAD and disappear from it.
	if ! changed_paths=$(git log --format= --name-only "$base..$head" | sed '/^$/d' | sort -u); then
		printf 'false\n'
		return 0
	fi
	while IFS= read -r path; do
		[[ -n "$path" ]] || continue
		is_allowed_path "$path" "$release_tag" || {
			printf 'false\n'
			return 0
		}
	done <<<"$changed_paths"

  printf 'true\n'
}

selftest() {
  local root base head got allowed_release_head force_base
  root=$(mktemp -d "${TMPDIR:-/tmp}/kivgraph-release-classifier.XXXXXX")
  trap 'rm -rf -- "$root"' RETURN
  git -C "$root" init -q
  git -C "$root" config user.name test
  git -C "$root" config user.email test@example.invalid
  mkdir -p "$root/docs" "$root/internal/version" "$root/landing/src/content/docs" \
    "$root/landing/src/content/releases"
  printf 'base\n' >"$root/README.md"
  printf 'installation\n' >"$root/docs/installation.md"
  printf 'package version\nvar Value = "0.9.4"\n' >"$root/internal/version/version.go"
  printf 'install\n' >"$root/landing/src/content/docs/install.md"
  printf 'notes\n' >"$root/landing/src/content/releases/v0.9.4.md"
  git -C "$root" add .
  git -C "$root" commit -q -m 'base'
  base=$(git -C "$root" rev-parse HEAD)

  printf 'changed\n' >>"$root/README.md"
  git -C "$root" add README.md
  git -C "$root" commit -q -m 'chore(release): prepare v0.9.4'
  head=$(git -C "$root" rev-parse HEAD)
	got=$(cd "$root" && "$script_path" pull_request "$base" "$head")
	[[ "$got" == true ]] || { printf 'selftest: allowed release = %s\n' "$got" >&2; return 1; }
	allowed_release_head=$head

  printf 'development notes\n' >"$root/landing/src/content/releases/v0.9.5-dev.1.md"
  git -C "$root" add landing/src/content/releases/v0.9.5-dev.1.md
  git -C "$root" commit -q -m 'chore(release): prepare v0.9.5-dev.1'
  head=$(git -C "$root" rev-parse HEAD)
	got=$(cd "$root" && "$script_path" pull_request "$base" "$head")
  [[ "$got" == true ]] || { printf 'selftest: allowed development release = %s\n' "$got" >&2; return 1; }

  printf 'old notes\n' >"$root/landing/src/content/releases/v0.9.5.md"
  git -C "$root" add landing/src/content/releases/v0.9.5.md
  git -C "$root" commit -q -m 'chore(release): prepare v0.9.4'
  head=$(git -C "$root" rev-parse HEAD)
	got=$(cd "$root" && "$script_path" pull_request "$base" "$head")
  [[ "$got" == false ]] || { printf 'selftest: wrong release note = %s\n' "$got" >&2; return 1; }

  printf 'code\n' >"$root/internal/unrelated.go"
  git -C "$root" add internal/unrelated.go
  git -C "$root" commit -q -m 'chore(release): prepare v0.9.5'
  head=$(git -C "$root" rev-parse HEAD)
	got=$(cd "$root" && "$script_path" pull_request "$base" "$head")
  [[ "$got" == false ]] || { printf 'selftest: forbidden path = %s\n' "$got" >&2; return 1; }

  git -C "$root" rm -q internal/unrelated.go
  git -C "$root" commit -q -m 'chore(release): prepare v0.9.6'
  head=$(git -C "$root" rev-parse HEAD)
	got=$(cd "$root" && "$script_path" pull_request "$base" "$head")
  [[ "$got" == false ]] || { printf 'selftest: multi-commit range = %s\n' "$got" >&2; return 1; }

  printf 'docs-only\n' >>"$root/README.md"
  git -C "$root" add README.md
  git -C "$root" commit -q -m 'docs: update release instructions'
  head=$(git -C "$root" rev-parse HEAD)
	got=$(cd "$root" && "$script_path" pull_request "$base" "$head")
	[[ "$got" == false ]] || { printf 'selftest: wrong subject = %s\n' "$got" >&2; return 1; }

	force_base=$(git -C "$root" rev-parse HEAD)
	got=$(cd "$root" && "$script_path" push "$force_base" "$allowed_release_head")
	[[ "$got" == false ]] || { printf 'selftest: non-fast-forward push = %s\n' "$got" >&2; return 1; }

	got=$(cd "$root" && "$script_path" pull_request not-a-commit "$head")
  [[ "$got" == false ]] || { printf 'selftest: invalid base = %s\n' "$got" >&2; return 1; }
  printf 'selftest: ok\n'
}

if [[ "${1:-}" == selftest ]]; then
  [[ "$#" -eq 1 ]] || { usage; exit 2; }
  selftest
  exit 0
fi

[[ "$#" -eq 3 ]] || { usage; exit 2; }
classify "$1" "$2" "$3"
