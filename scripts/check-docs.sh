#!/usr/bin/env bash
#
# check-docs.sh verifies the two documentation invariants this repository states
# and nothing verified.
#
#   1. Every CLAUDE.md is a symlink to the AGENTS.md beside it. They are
#      deliberate symlinks, so a copy is not a second file: it is the same
#      instructions drifting out of date silently, read by whichever agent
#      happens to open that one.
#
#   2. A documentation line under docs/ stays within 84 columns.
#
# The second is a ratchet, and deliberately so: forty existing files predate the
# rule, and a gate that starts red is a gate people learn to ignore. It runs
# over the files a change touches, resolved against a base ref, so drift stops
# without demanding a reflow of the archive.
#
# Usage: scripts/check-docs.sh [base-ref]     (default: origin/main)

set -euo pipefail

readonly MAXIMUM_COLUMNS=84
base="${1:-origin/main}"
failures=0

note() {
	printf '%s\n' "$1" >&2
	failures=$((failures + 1))
}

# --- 1. the symlinks --------------------------------------------------------
while IFS= read -r claude; do
	if [[ ! -L "$claude" ]]; then
		note "$claude is a regular file; it must be a symlink to the AGENTS.md beside it"
		continue
	fi
	target="$(readlink "$claude")"
	if [[ "$target" != "AGENTS.md" ]]; then
		note "$claude points at '$target', want AGENTS.md"
		continue
	fi
	if [[ ! -e "$(dirname "$claude")/AGENTS.md" ]]; then
		note "$claude points at an AGENTS.md that does not exist"
	fi
done < <(find . -name CLAUDE.md -not -path './.git/*' -not -path '*/node_modules/*')

# --- 2. the column ratchet -------------------------------------------------
# The ratchet is per line, not per file. Touching a document must not inherit
# its history: a change that adds one sentence to a page written before the rule
# would otherwise have to reflow the page, which is how a gate turns into a
# reason not to improve documentation.
#
# A base ref that is not fetched (a shallow clone, a fresh worktree) is not a
# reason to pass silently, and not a reason to fail either: the check says it
# skipped and why, which is the one thing a silent gate never does.
if ! git rev-parse --verify --quiet "$base" >/dev/null; then
	printf 'check-docs: base ref %s is unavailable, skipping the column ratchet\n' "$base" >&2
else
	merge_base="$(git merge-base "$base" HEAD)"
	# -U0 emits only the changed lines. The file each hunk belongs to comes
	# from the +++ header, and the line number from the hunk header, counted
	# forward so a violation is reported where it can be read.
	long="$(git diff -U0 "$merge_base" -- 'docs/**/*.md' | awk -v limit="$MAXIMUM_COLUMNS" '
		/^\+\+\+ b\// { file = substr($0, 7); next }
		/^@@ / { split($3, span, ","); line = substr(span[1], 2) - 1; next }
		/^\+/ {
			line++
			text = substr($0, 2)
			if (length(text) > limit) {
				printf "%s:%d: %d columns\n", file, line, length(text)
			}
		}
	')"
	if [[ -n "$long" ]]; then
		while IFS= read -r violation; do note "$violation"; done <<<"$long"
	fi
fi

if ((failures > 0)); then
	printf 'check-docs: %d violation(s)\n' "$failures" >&2
	exit 1
fi
printf 'check-docs: ok\n'
