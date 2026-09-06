# ADR 0112: user-level agent instructions

- **Status:** accepted
- **Date:** 2026-09-04
- **Changes the CLI surface:** yes -- changes `instructions install` scope
- **Changes the installer behavior:** yes -- `configure` writes user files
- **Changes the graph or MCP protocol:** no

## Context

ADR 0109 put the Kivgraph navigation block in a repository instruction file.
That treats an agent integration as project-owned source and causes a setup
command to mutate the checkout from which it is invoked. It is also wrong for
the intended persistent behavior: each supported coding agent loads a distinct
user-level instruction file alongside its MCP, skill and hook configuration.

The previous scope made `configure` depend on a Git root even though all other
surfaces it configures are user scoped. It also made an installation started
outside a repository choose the current directory as an accidental target.

## Decision

Supersede the instruction-scope decisions in ADR 0109 and ADR 0110. The
managed Kivgraph block is user configuration and never modifies a repository.

`kivgraph instructions install` and `kivgraph configure` own a canonical
`KIVGRAPH.md` beside each client configuration and register it as follows:

| Target | User configuration |
| --- | --- |
| Codex | `~/.codex/KIVGRAPH.md`, referenced by `~/.codex/AGENTS.md` |
| Claude Code/Desktop | `CLAUDE.md` references the nearby canonical file |
| OpenCode | `opencode.json` names the nearby canonical file in `instructions` |
| Oh My Pi | `~/.omp/agent/KIVGRAPH.md`, referenced by `~/.omp/agent/AGENTS.md` |

Claude uses `~/.claude/CLAUDE.md` and `~/.claude/KIVGRAPH.md`. OpenCode uses
`~/.config/opencode/opencode.json` and `~/.config/opencode/KIVGRAPH.md`.

Codex, Claude, and Oh My Pi expand their small managed absolute reference to
`KIVGRAPH.md`. OpenCode does not interpret references in `AGENTS.md`, so it keeps
that file untouched and uses its native JSON `instructions` list instead.
`CODEX_HOME` and `PI_CODING_AGENT_DIR`, when present, replace the default Codex
and Oh My Pi configuration roots respectively.

The interactive selector and `configure` deduplicate shared destinations.
`instructions install --agent TARGET` is the explicit form for new scripts.
The legacy `--file AGENTS.md`, `--file CLAUDE.md`, and
`--file .omp/AGENTS.md` forms remain accepted. They select every matching
user-level target so a valid previous invocation does not become an error.

The existing block ownership, malformed-block rejection, `--force`, backup,
atomic write, permissions, and symlink protections remain unchanged. An exact
unmodified legacy full block is migrated to the reference automatically; an
edited reference or canonical prompt needs `--force`. Parent directories are
validated beneath the user home before a write. No command requires a Git
repository or derives an instruction destination from the current working
directory.

## Alternatives

- **Keep project instructions:** violates the boundary between a user agent
  integration and repository-owned instructions.
- **Copy the complete prompt into every user instruction file:** makes prompt
  upgrades duplicate a large block and differs from the reference pattern used
  by RTK for Codex.
- **Remove `--file` immediately:** turns an accepted v0.9.9 command into a
  failure instead of naming its migration.

## Consequences

Running `configure` or `instructions install` from any directory produces the
same selected user configuration. Existing repository `AGENTS.md`,
`CLAUDE.md`, and `.omp/AGENTS.md` files remain untouched. A user who previously
installed the managed project block can keep it or remove it deliberately; the
new command does not edit or remove it during upgrade.

## Verification

Integration tests assert every global destination, reject unsafe paths, and
prove that installing Codex instructions leaves project instructions unchanged.
CLI tests cover operation without a Git root, interactive multi-agent setup,
legacy selector compatibility, shared Claude destination deduplication, native
OpenCode registration, and `configure` coverage for every supported surface.
