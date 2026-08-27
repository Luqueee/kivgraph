#!/usr/bin/env bash
# verify-coverage.sh measures statement coverage over the product packages and
# fails when it falls below the floor.
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

floor=${KIVGRAPH_COVERAGE_FLOOR:-79.0}
profile=$(mktemp -t kivgraph-coverage.XXXXXX)
filtered=$(mktemp -t kivgraph-coverage-product.XXXXXX)
trap 'rm -f "$profile" "$filtered"' EXIT

go test ./... -coverpkg=./... -covermode=atomic -coverprofile="$profile" >/dev/null

head -1 "$profile" >"$filtered"
grep -v -e 'kivgraph/benchmarks/' -e 'kivgraph/internal/testsupport' "$profile" |
	tail -n +2 >>"$filtered"

total=$(go tool cover -func="$filtered" | tail -1 | grep -oE '[0-9]+\.[0-9]+')

# A toolchain that is absent skips a whole suite rather than failing it, so a
# runner missing one measures lower for a reason that is not a regression.
# Naming them keeps a failure from being read as lost tests.
for tool in dart pyright-langserver rust-analyzer; do
	command -v "$tool" >/dev/null || echo "note: $tool is not on PATH; the suites that need it skipped"
done

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
