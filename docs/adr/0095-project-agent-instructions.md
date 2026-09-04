# ADR 0095: project agent instructions

- **Status:** accepted
- **Date:** 2026-09-04
- **Changes the CLI surface:** yes -- adds `instructions install`
- **Changes the graph or MCP protocol:** no

## Context

Codex, Oh My Pi and Claude Code load project context from Markdown files. A
project that registers the Kivgraph MCP server still needs a short instruction
that tells an agent when the semantic graph is useful and how to interpret its
answers. The existing `skill install` command serves client skill directories;
it does not add project context to a repository.

The command must preserve project-owned instructions. Replacing an existing
`AGENTS.md` or `CLAUDE.md` would discard build rules and local decisions, while
silently appending a second Kivgraph section would make upgrades accumulate
stale copies.

## Decision

Add:

```text
kivgraph instructions install
kivgraph instructions install --agent codex
kivgraph instructions install --agent claude
kivgraph instructions install --agent omp
```

The command resolves the nearest ancestor containing a `.git` marker, or uses
the current directory when no repository marker exists. Without `--agent` or
`--file`, an interactive selector allows one or more coding agents to be
selected. Codex and OpenCode select the root `AGENTS.md`; Claude Code selects
the root `CLAUDE.md`; Oh My Pi selects its native `.omp/AGENTS.md`. Agents that
share a destination are installed once.
The short aliases `claude` and `omp` are accepted alongside the canonical
target names. `--file` accepts `AGENTS.md`, `CLAUDE.md` or `.omp/AGENTS.md` as a
direct override, and cannot be combined with `--agent`. These files are
project-scoped.
Global agent configuration is not changed because these clients use different
user-level roots.

The command embeds one concise English instruction block and surrounds it with
Kivgraph markers. A missing file is created. An existing file receives the
block after its current content. An exact block is idempotent. An edited block
requires `--force`, which replaces only the marked block and preserves the
surrounding file. A malformed or ambiguous marker pair is rejected even with
`--force`, so the command never guesses which text it owns.

`--dry-run` computes the same plan without writing. Writes use the existing
atomic integration writer, reject symlink destinations, keep a
`.kivgraph.bak` copy when an existing file is changed, and use mode `0600`.
The repository convention `CLAUDE.md -> AGENTS.md` is the one accepted
symlink: the command writes the adjacent `AGENTS.md` destination directly.
Other symlinks are rejected.

The command only changes the selected project context file and its backup. It
does not initialize Kivgraph, index repositories, install an MCP registration,
install a skill or enable a hook. Those remain explicit commands.

## Alternatives

- **Overwrite the whole context file:** loses project-owned instructions and
  makes a tool-specific installer unsafe to run on an existing repository.
- **Create a second file and reference it from the context file:** imports are
  not the same across clients, and a reference adds another compatibility
  surface without helping agents that only load the nearest context file.
- **Always write both files:** duplicates the block for clients that load both
  names and lets the two copies drift.
- **Use `skill install`:** skills are client-discovered directories and are not
  the project instruction mechanism requested here.

## Consequences

The common project form needs one command and one managed section. Users select
one or more target agents and receive their native project paths. A project
using the existing `CLAUDE.md -> AGENTS.md` convention keeps one source of
instructions. Selecting clients that share a physical destination does not
repeat the write.
An upgrade does not replace an edited section; the user must pass `--force` to
accept the embedded version.

The command adds no global state and does not require an indexed graph. The
installed text therefore remains useful as setup guidance before the first
index, and it tells the agent to fall back to text search when Kivgraph is not
available.

## Verification

The integration tests cover invalid names, symlinks, the accepted Claude link,
malformed and edited blocks, dry runs, CRLF files, atomic write failures,
preservation, idempotence, forced replacement and both filenames. CLI tests
cover project-root resolution, interactive multi-agent selection and
cancellation, physical destination deduplication, help, explicit agent
selection and dry-run output.
