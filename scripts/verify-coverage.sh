#!/usr/bin/env bash
# verify-coverage.sh runs the Go suite, measures statement coverage over the
# product packages, and fails when it falls below the floor.
#
# It runs the suite itself rather than measuring a run someone else did,
# because `-coverpkg=./...` instrumentation is what makes the number right and
# it has to be in force while the tests execute. That also means this replaces
# a plain `go test ./...` rather than following one: running both is the same
# work twice.
#
# The measurement is cross-package on purpose. `go test ./...` credits a
# package only with the coverage its own tests produce, and this repository
# tests across package boundaries deliberately -- index_project.go is exercised
# from internal/mcp, not from internal/mcp/tools -- so the per-package number
# reads several points low and names files as untested that are not. Only
# `-coverpkg=./...` answers "what does the suite reach".
#
# benchmarks/ and internal/testsupport are excluded because they are measuring
# harnesses with no tests of their own: counting them drags the total down by
# fifteen points and reports nothing about the product.
#
# The floor is a ratchet, not a target. Raise it when the suite earns it; a
# change that lowers it is either a regression or a deliberate decision that
# belongs in the commit message. Never lower it to make a build pass.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

floor=${KIVGRAPH_COVERAGE_FLOOR:-79.5}

# The analyzers are checked before the suite runs, not after, and their absence
# is a failure rather than a note.
#
# An absent analyzer skips its suite instead of failing it, so a run without
# one measures several points low for a reason that is not a regression. While
# this gate was a job of its own that only mattered to whoever read the log;
# now that it is the suite the `verify` job runs, a missing analyzer would
# report a floor breach that is really a toolchain nobody installed, and the
# reader would go looking for lost tests.
#
# KIVGRAPH_COVERAGE_ALLOW_PARTIAL measures anyway and reports without gating,
# which is what a workstation that has not installed all five wants.
# Each analyzer is checked the way that actually answers for it.
#
# rust-analyzer is run, because rustup ships a proxy under that name whether or
# not the component is installed: `command -v` answers yes on a machine where
# the analyzer cannot start, which is the one case worth catching -- the Rust
# suite would skip and the number would come out low with nothing saying why.
#
# pyright-langserver is looked up instead of run. It is a language server and
# has no --version: invoked without a transport it exits non-zero, so running
# it would report every installation as missing. Looking it up is also exactly
# what the suite does to decide whether to skip.
missing=()
command -v dart >/dev/null 2>&1 || missing+=(dart)
command -v pyright-langserver >/dev/null 2>&1 || missing+=(pyright-langserver)
rust-analyzer --version >/dev/null 2>&1 || missing+=(rust-analyzer)
if [[ ${#missing[@]} -gt 0 ]]; then
	if [[ -z ${KIVGRAPH_COVERAGE_ALLOW_PARTIAL:-} ]]; then
		echo "coverage gate: not on PATH: ${missing[*]}" >&2
		echo "the suites that need them would skip, and the number would be low for a reason that is not a regression." >&2
		echo "install them, or set KIVGRAPH_COVERAGE_ALLOW_PARTIAL=1 to measure without gating." >&2
		exit 1
	fi
	echo "note: not on PATH: ${missing[*]}; measuring without gating"
fi

profile=$(mktemp -t kivgraph-coverage.XXXXXX)
filtered=$(mktemp -t kivgraph-coverage-product.XXXXXX)
log=$(mktemp -t kivgraph-coverage-test.XXXXXX)
trap 'rm -f "$profile" "$filtered" "$log"' EXIT

# The suite's output is kept rather than discarded: this is the only run of the
# tests, so a failure has to be readable. Under GitHub Actions the failing
# lines are also emitted as annotations, so the reason shows on the run without
# opening the log.
if ! go test ./... -coverpkg=./... -covermode=atomic -coverprofile="$profile" 2>&1 | tee "$log"; then
	if [[ -n ${GITHUB_ACTIONS:-} ]]; then
		annotations=$(grep -E '^(--- )?FAIL|\.go:[0-9]+:' "$log" | sed -n '1,20p')
		while IFS= read -r line; do
			[[ -n $line ]] && printf '::error::%s\n' "$line"
		done <<<"$annotations"
	fi
	exit 1
fi

head -1 "$profile" >"$filtered"
grep -v -e 'kivgraph/benchmarks/' -e 'kivgraph/internal/testsupport' "$profile" |
	tail -n +2 >>"$filtered"

total=$(go tool cover -func="$filtered" | tail -1 | grep -oE '[0-9]+\.[0-9]+')

if [[ ${#missing[@]} -gt 0 ]]; then
	echo "coverage: ${total}% of statements, not gated: ${missing[*]} did not run"
	exit 0
fi

if awk -v total="$total" -v floor="$floor" 'BEGIN { exit !(total < floor) }'; then
	echo "coverage gate: ${total}% of statements is below the ${floor}% floor"
	echo ""
	echo "the functions furthest from covered:"
	# The report is built in one substitution rather than piped into head,
	# whose exit closes the pipe and would make this script report SIGPIPE
	# instead of the failure it is announcing.
	worst=$(go tool cover -func="$filtered" |
		grep -vE '100\.0%$|^total' |
		awk '{ printf "%8s  %s  %s\n", $NF, $2, $1 }' |
		sort -n)
	echo "$worst" | sed -n '1,15p'
	exit 1
fi

echo "coverage gate: ${total}% of statements, floor ${floor}%"
