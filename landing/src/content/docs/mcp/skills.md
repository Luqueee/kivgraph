---
title: Agent Skill
description: What Kivgraph's Agent Skill tells a coding agent, how to install it per client and scope, and how to inspect or remove it.
---

## What a skill is here

An Agent Skill is a Markdown instruction file a coding agent loads alongside its
tools. Kivgraph ships one to route a question to the right tool before the agent
reaches for grep or starts opening files, and to request visible notices of
tool use. It is not required in order to use the MCP server. Install it to
change which tool the agent picks; skip it and the server keeps its normal
surface: three indexing controls before publication and fourteen tools after.

## Install

```bash
kivgraph skill install
```

With no `--target`, the command detects the supported agents present in the
requested scope, opens a selector with the detected ones pre-checked, and
installs into every entry you confirm. Arrows or `j`/`k` move, space toggles,
`a` selects all, `n` selects none, Enter confirms, `q` or Esc cancels.
Confirming with nothing selected is refused.

For scripted use, name the client:

```bash
kivgraph skill install --target claude-code --scope user
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--target` | empty | Client to write: `claude-code`, `claude-desktop`, `codex`, `opencode`, `oh-my-pi`. Omit it on `install` to open the selector. Required for `status` and `remove`. |
| `--scope` | `user` | `user` or `project`. `project` resolves against the current working directory. Any other value is rejected. |
| `--dry-run` | off | Report the plan as `would-install` and write nothing. Not accepted by `skill status`. |
| `--force` | off | Replace a file at the skill path that is not the canonical skill. Without it, that case is an error. |

Without a terminal and without `--target`, the selector cannot run and the
command fails with `interactive selection requires a terminal; pass --target`.
Client integrations are supported on Linux and macOS only.

`skill install` copies one file. It does not initialise Kivgraph, does not
register the MCP server, and does not index anything. Register the server
separately: see [MCP clients](/mcp/clients/). Build a graph with
[indexing](/guides/indexing/).

## Where it lands

The installed file is always named `SKILL.md` and always sits in a
`skills/kivgraph/` directory. `<project>` is the current working directory.

| Client | `--target` value | Scope | Path |
| --- | --- | --- | --- |
| Claude Code | `claude-code` | `user` | `~/.claude/skills/kivgraph/SKILL.md` |
| Claude Code | `claude-code` | `project` | `<project>/.claude/skills/kivgraph/SKILL.md` |
| Codex | `codex` | `user` | `~/.agents/skills/kivgraph/SKILL.md` |
| Codex | `codex` | `project` | `<project>/.agents/skills/kivgraph/SKILL.md` |
| OpenCode | `opencode` | `user` | `~/.config/opencode/skills/kivgraph/SKILL.md` |
| OpenCode | `opencode` | `project` | `<project>/.opencode/skills/kivgraph/SKILL.md` |
| Oh My Pi | `oh-my-pi` | `user` | `~/.omp/agent/skills/kivgraph/SKILL.md` |
| Oh My Pi | `oh-my-pi` | `project` | `<project>/.omp/skills/kivgraph/SKILL.md` |

Claude Desktop has no local skill target. It is a supported MCP client, but it
never appears in the skill selector, and `--target claude-desktop` fails with
`target "claude-desktop" does not support local skill installation`. Register
the MCP server for it and it uses the tools without the skill.

## What the skill says

The skill's routing contract is to reach for the graph when the question is about
callers, references, impact or cross-repository consumers, and reach for the
files only after the graph has named them.

### Visible tool use

Before every Kivgraph MCP call, the skill asks the agent to send a short chat
notice naming the exact tool, its target (symbol, file, repository or scope),
and the question it will answer, in the conversation's language. For example:

`Kivgraph · find_references — NewServer: check who calls it.`

Repeated calls each get a notice. Parallel calls may share a preamble, with a
separate line for each call. The notice states intent, not success, and does not
replace user approval for either indexing mutation. There is no additional
mandatory completion message, setting or change to tool results.

The MCP server sends the same rule in its connection instructions, including
when no graph exists. This is **best effort**: the client decides whether to
give the instructions to its model and display the model's preamble. Claude
Desktop has no local skill and relies on the MCP instructions alone. This is
not a server-generated chat event or an audit log.

After upgrading the running server, reconnect the client (or start a new chat)
so it receives the new instructions. Existing skills, including local edits,
are not overwritten automatically; installs without an existing skill receive
the new skill.
To adopt the skill notice without replacing your customizations, copy its
"Visible tool use" section from the shipped skill into your existing one.
User-scope links share `~/.config/kivgraph/skills/kivgraph/SKILL.md`;
project-scoped skills are separate copies. CLI commands such as `kivgraph ui`
are outside this chat-notice contract.

### Routing

| Question | Tool |
| --- | --- |
| Is a graph published, how old is it, what does it cover | [`graph_status`](/docs/tools/graph-status/) |
| Which repositories are indexed, and in which language | [`list_repositories`](/docs/tools/list-repositories/) |
| I can describe the behavior but do not know the symbol or file | [`find_by_intent`](/docs/tools/find-by-intent/) |
| Where is this name or qualified name declared | [`find_symbol`](/docs/tools/find-symbol/) |
| Everything about one symbol already identified | [`get_symbol`](/docs/tools/get-symbol/) |
| The code behind rows a tool returned | [`get_source`](/docs/tools/get-source/) |
| What a package or directory declares | [`get_file_outline`](/docs/tools/get-file-outline/) |
| Who references this, or what it reaches, one hop out | [`find_references`](/docs/tools/find-references/) |
| Bounded dependency paths out of a symbol | [`trace_dependencies`](/docs/tools/trace-dependencies/) |
| Consumers in another repository | [`find_cross_repo_consumers`](/docs/tools/find-cross-repo-consumers/) |
| Bounded incoming impact, grouped | [`get_blast_radius`](/docs/tools/get-blast-radius/) |
| Register projects and rebuild the graph | [`index_project`](/docs/tools/index-project/) |
| Start a rebuild without holding one call open | [`start_index_project`](/docs/tools/start-index-project/) |
| Poll an asynchronous rebuild | [`get_index_status`](/docs/tools/get-index-status/) |

Without a published generation, the skill starts with `start_index_project`,
polls `get_index_status` to a terminal result, and reconnects after publication.
With a published graph, its first moves are `graph_status` to confirm its age
and freshness, then `list_repositories` to pick the repository and language
before narrowing. Repository names are case sensitive; two names differing
only in case are two repositories.

### Why an empty answer is an answer

Edges are resolved by `go/types`, the TypeScript checker and `rust-analyzer`,
not by matching names. A reference list is therefore complete for those
languages, and an empty one means nobody calls the symbol. Grep cannot tell you
that.

### Rows are addressable

Every row carries a repository, a repository-relative path, a qualified name
and a line range, and every tool accepts that triple in place of a stable key.
The next call is built from the answer just received, so stable keys need never
enter the conversation.

### Evidence rules the skill imposes

- Keep `EXACT`, `CANDIDATE` and `UNRESOLVED` apart. Never upgrade a candidate or
  an unresolved result because a name, path, alias or text matches.
- Read the `coverage` of `get_blast_radius`: `exact` and `candidate` count
  consumers of the symbol asked for, while `package_level` counts dependencies
  on the provider package and proves nothing about that symbol.
- Respect result limits and cursors. Narrow the query instead of treating a
  truncated response as complete.
- Treat the snapshot as a projection of the canonical graph, not as licence to
  invent facts. If it is missing or stale for the question, say so and ask for a
  re-index.
- In a `serve` process, `graph_status` answers `not_applicable` for `storage`
  and `worker`, with the reason: the server responds from the published
  snapshot and never opens the database or runs the TypeScript worker. That is
  not a misconfiguration.
- Unresolved references are facts about the workspace, not defects. Report the
  reason instead of concluding that coverage is broken. See
  [resolution](/docs/resolution/).

### Indexing

`index_project` and `start_index_project` are the two mutating tools. The skill
requires explicit user approval before calling either one, all projects passed
in one call through `projects`, and no claim of success until a new generation
and snapshot are published. It prefers the asynchronous start and polls
`get_index_status` so a client timeout cannot interrupt the call. A
rebuild resolves cross-repository edges over the complete fact set, so it costs
the whole corpus whatever was added: eleven separate calls build eleven graphs
and keep the last one. A full rebuild can outlive the client's per-call
timeout; the work still completes, and `graph_status` showing an advanced
`snapshot_id` is the check, not a retry.

Clients with form elicitation receive the server prompt. Codex uses its native
tool-approval prompt and then sends `confirmed: true`; URL-only clients use the
same fallback.

### Where it loses

Stated in the skill's own terms, and not softened: a rare name in a single
small repository is cheaper to grep, and one small file is cheaper to read than
to outline. Kivgraph wins on common names, on transitive impact, on
cross-repository consumers and on proving an absence. It is the wrong tool for
a one-off literal string search.

## Inspect and remove

```bash
kivgraph skill status --target claude-code --scope user
kivgraph skill remove --target claude-code --scope user
```

Both require `--target`; neither opens the selector. `skill status` reads the
path and reports one of three states:

| Status | Meaning |
| --- | --- |
| `absent` | Nothing at the path. |
| `managed` | The file is byte-identical to the canonical Kivgraph skill. |
| `incompatible` | A file exists at the path and is not the canonical skill. |

`skill remove` deletes only a file that is byte-identical to the canonical
skill. Anything else at that path is left alone and reported as an error unless
you pass `--force`. Nothing else in the client's skills directory is touched:
removal withdraws Kivgraph's own skill and nothing more. `--dry-run` reports
`would-remove` and deletes nothing.

## Safety

- The destination is inspected with a symlink-aware stat. A symlink at the skill
  path is refused (`refusing symlink integration path`), and so is anything that
  is not a regular file.
- Missing parent directories are created with mode `0700`.
- Writes are atomic: the content goes to a temporary file in the destination
  directory, is set to mode `0600`, is synced, is renamed over the destination,
  and the directory itself is synced afterwards. Removal uses the same
  rename-then-delete path.
- Before an existing file is overwritten or removed, its previous content is
  copied to `<path>.kivgraph.bak`. An existing backup is kept as it is and
  never overwritten, so the first backup survives later runs. A backup path that
  is a symlink or not a regular file aborts the operation.
- Replacing a file that is not the canonical skill requires `--force`. Without
  it the command fails with `integration path "<path>" contains an incompatible
  Kivgraph entry; use --force to replace or remove it`.
- An install whose destination already matches the canonical skill reports
  `managed` and writes nothing. No backup is created and no timestamp changes.

## In a release bundle

The canonical skill ships inside the release bundle at
`skills/kivgraph/SKILL.md` and is listed in the bundle's `SHA256SUMS`, so it is
verified with the same checksum pass as the rest of the payload. See
[install](/install/).
