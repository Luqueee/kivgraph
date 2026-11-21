#!/usr/bin/env bash
set -euo pipefail

# classify-docs-change.sh reads changed paths on stdin and prints `true` when
# every one of them is documentation that no build, test or bundle reads. It is
# an allowlist and deliberately narrow: a path it does not recognise prints
# `false`, and the caller runs the complete CI.
#
# Usage: git diff --name-only --no-renames A...B | scripts/classify-docs-change.sh
#        scripts/classify-docs-change.sh selftest

is_documentation() {
	case "$1" in
	# Read by something that is not a reader: the MCP surface test parses the
	# protocol specification, and fetch-ladybug.sh takes the pinned version and
	# digest from the dependency page, which the bundle also ships.
	docs/protocol/* | docs/dependencies/*) return 1 ;;
	docs/*) return 0 ;;
	README.md | TASKS.md) return 0 ;;
	AGENTS.md | */AGENTS.md | CLAUDE.md | */CLAUDE.md) return 0 ;;
	.claude/skills/*) return 0 ;;
	.github/*.md | .github/ISSUE_TEMPLATE/*) return 0 ;;
	*) return 1 ;;
	esac
}

classify() {
	local path seen=false
	while IFS= read -r path; do
		[[ -n "$path" ]] || continue
		seen=true
		is_documentation "$path" || {
			printf 'false\n'
			return 0
		}
	done
	# An empty change proves nothing about what it touched.
	printf '%s\n' "$seen"
}

expect() {
	local want=$1 got
	shift
	got=$(printf '%s\n' "$@" | classify)
	[[ "$got" == "$want" ]] || {
		printf 'selftest: %s -> %s, want %s\n' "$*" "$got" "$want" >&2
		exit 1
	}
}

selftest() {
	expect true README.md docs/adr/0001-x.md internal/mcp/AGENTS.md
	expect true .claude/skills/running-tests/SKILL.md .github/PULL_REQUEST_TEMPLATE.md
	expect false ''
	expect false README.md internal/mcp/server.go
	expect false docs/protocol/mcp-surface-v3.md
	expect false docs/dependencies/ladybugdb.md
	# Root Markdown the bundle installs, and Markdown the binary embeds.
	expect false THIRD_PARTY_NOTICES.md
	expect false internal/integrations/assets/kivgraph/SKILL.md
	expect false landing/src/content/releases/v1.0.0.md
	expect false .github/workflows/ci.yml
	printf 'selftest: ok\n'
}

if [[ "${1:-}" == selftest ]]; then
	selftest
	exit 0
fi
classify
