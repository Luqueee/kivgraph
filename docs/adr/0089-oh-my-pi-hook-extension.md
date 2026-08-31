# ADR 0089: install the gate as an Oh My Pi extension

- **Status:** accepted
- **Date:** 2026-08-31
- **Changes the MCP protocol:** no
- **Changes the persistent schema:** no
- **Requires a rebuild:** no
- **Changes the CLI surface:** yes -- `oh-my-pi` is added to `hook` targets

## Context

The pre-tool-use gate already has one canonical entry point:
`kivgraph hook run`. Claude Code, Claude Desktop, and Codex invoke it from
shell-hook configuration. OpenCode uses a generated plugin because its native
pre-tool callback is a JavaScript module rather than a shell-hook document.

Oh My Pi uses the same extension model. Its current runtime discovers native
extensions from the user agent root and from the project `.omp` directory; it
does not need a package manifest or a settings-file registration for a direct
extension module. The relevant upstream contracts are documented in the
[Oh My Pi repository](https://github.com/can1357/oh-my-pi), in its
extension-loading and configuration guides.

## Decision

`kivgraph hook install --target oh-my-pi` writes a generated JavaScript
extension with the following paths:

| Scope | Path |
| --- | --- |
| user | `~/.omp/agent/extensions/kivgraph.js` |
| project | `<project>/.omp/extensions/kivgraph.js` |

The extension exports a default factory and registers an Oh My Pi
`tool_call` handler. For each call it builds the existing `PreToolUse` payload
from the event's tool name, input, and working directory, then runs the
installed executable with `hook run`. A denial becomes the native
`{ block: true, reason }` result. An allow, an invalid response, a missing
executable, or a timeout gives the host no opinion and lets the tool proceed.

The executable path is embedded as a JavaScript string during installation.
This keeps desktop-launched Oh My Pi processes independent of the shell `PATH`
and uses the same atomic write, backup, idempotency, conflict, `--force`,
`status`, and `remove` lifecycle as the OpenCode module.

## Compatibility

The target name is `oh-my-pi`, matching the existing MCP and skill integration
vocabulary. `HookTargets` now includes it, so help, completion, interactive
detection, and the CLI all expose the same accepted target set. The old
legacy-hook exclusion in ADR 0077 is superseded for this target only; the
closed-world gate rule and the fail-open behavior remain unchanged.

The extension does not provide a session identifier. Consequently, the
once-per-session advisory briefing is not emitted for Oh My Pi, just as it is
not emitted for the existing OpenCode module.

## Consequences

Oh My Pi can use the same graph-backed refusal logic as the other supported
clients without a second hook protocol or a second classifier. A generated
module is also easy to inspect and remove, and an unrelated extension is never
replaced without `--force`.

The integration depends on Oh My Pi's native extension discovery and event
shape. If that host contract changes, the generated asset and its fixture
tests must change together; the canonical `hook run` protocol does not need to
change.
