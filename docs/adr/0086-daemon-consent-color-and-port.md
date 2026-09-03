# ADR 0086: daemon consent, presentation, and port selection

- **Status:** proposed
- **Date:** 2026-08-30
- **Changes the MCP protocol:** no
- **Changes the persistent schema:** no
- **Requires a rebuild:** no
- **Changes the CLI surface:** yes
- **Relaxes a root contract:** no

## Context

`mcp install` writes a `url` entry by default, but installing a supervisor is
a separate side effect from editing a client file. The previous behavior could
attempt that installation without asking and, on macOS, a busy port could make
launchd restart the daemon without it publishing `daemon.json`.

In addition, `daemon status` and several maintenance reports used a linear
format that was difficult to scan in a terminal, even though a shared policy
already existed for colors and for keeping ANSI escapes out of pipes.

## Proposed decision

The proposed behavior is that when `mcp install` needs to provision an absent
or stale supervisor, an interactive invocation asks for consent. A negative
answer keeps the `stdio` entry; `--daemon` is explicit consent and does not
show the question. Without a terminal, the operation does not block waiting
for an answer and keeps `stdio`. If supervisor status cannot be inspected, it
reports that condition and also keeps `stdio`; an unknown ownership state is
not consent. Target selection happens before this decision, so cancelling
selection does not start anything.

The proposed presentation is for `daemon status`, `graph status`, `rollback`,
and `snapshot` to use a key/value table in a terminal. The same information
would remain in the existing line format when output is redirected. States and
results would use the existing ANSI layer, which is disabled by `NO_COLOR`,
`TERM=dumb`, or a non-terminal destination.

The proposed daemon behavior is to prefer `127.0.0.1:7788`. When the caller
does not pass `--addr` and that port is occupied, it would bind
`127.0.0.1:0`, publish the selected port, and store it in `daemon.port` with
`0600` permissions. A later start would reuse that port. An explicit `--addr`
would not fall back: if it is occupied, the command would fail and name the
address.

The proposed supervisor specification always uses the resolved configuration
path. This would mean `daemon status` without repeating `--config` describes
the same unit as `mcp install` instead of calling it stale because the path is
empty.

## Consequences

- Automatic installation is opt-in from a terminal and safe for scripts; a
  caller that needs to require it uses `--daemon`.
- A machine already using `7788` does not change ports. A new installation that
  finds the address occupied gets a stable port, but an existing client must
  rerun `mcp install` if an administrator manually changes or releases the
  assignment.
- `daemon.port` is derived operational state, not a graph generation. It is
  kept alongside `daemon.token` and replaced atomically.
- No rendering dependency is added: the table uses the output layer that
  already colors help, integrations, logs, and statistics.

## Verification

The implementation was checked from the repository root with:

```bash
go test ./cmd/kivgraph ./internal/daemon ./internal/supervisor
```

That command covers `TestDaemonProvisionNeedsConsentWhenNoUnitExists`,
`TestDaemonProvisionDoesNotAskForAnExistingUnit`,
`TestDefaultProvisionSkipsInstallWhenSupervisorStatusFails`,
`TestDefaultProvisionDeclinesWithoutAnInteractiveTerminal`,
`TestListenHTTPChoosesAndPersistsAPortWhenTheDefaultIsBusy`,
`TestListenHTTPReusesThePersistedPort`,
`TestListenHTTPRejectsACorruptPersistedPort`,
`TestRenderKeyValueTableAlignsHumanOutput`, and
`TestRenderKeyValueTableKeepsEmptyValuesVisible`. The tests use temporary
state directories and a local temporary TCP listener; they do not depend on a
checked-in or generated corpus. Redirected output remains covered by the
existing assertions.
