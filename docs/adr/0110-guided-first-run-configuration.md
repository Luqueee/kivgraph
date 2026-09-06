# ADR 0110: guided first-run configuration

- **Status:** accepted
- **Date:** 2026-09-04
- **Changes the CLI surface:** yes -- adds `configure`
- **Changes the installer behavior:** yes -- offers configuration after install
- **Changes the graph or MCP protocol:** no

## Context

Installing Kivgraph currently leaves several compatible integrations to be
configured through separate commands. The client schemas are intentionally
different: MCP registrations, skills, hooks and project instructions do not
share a file or a common installation path. Requiring a person to repeat the
agent selection for each surface makes a first installation needlessly long and
makes it easy to enable only part of the integration.

The release installer also needs a safe first-run path. It may be executed from
a terminal or through `curl | bash`, so it must not block when standard input is
the downloaded script and it must not silently start a daemon or modify a
project in a non-interactive environment.

## Decision

Add the guided command:

```text
kivgraph configure
kivgraph configure --target codex --target claude-code
kivgraph configure --stdio
```

With no `--target`, the command opens the existing multi-select coding-agent
selector once. With one or more repeatable `--target` flags, it is
non-interactive. The aliases `claude` and `omp` are accepted for the same
targets as the instructions command. Duplicate targets and unknown targets are
rejected before any file is written.

The selected targets are routed according to the surface they support:

- MCP is installed for every selected MCP target.
- The Agent Skill is installed only for targets with a local skill directory;
  Claude Desktop is reported as skipped.
- The pre-tool-use hook is installed for every selected hook target.
- Project instructions are installed only for targets with a project context
  file, with shared destinations deduplicated.

MCP, skill and hook integrations use user scope. Project instructions use the
nearest Git root, or the current directory outside Git. The command initializes
the default Kivgraph configuration after a target selection when it is missing,
but it never registers repositories or runs an index. `--dry-run` skips that
initialization and all writes.

The MCP transport follows the existing `mcp install` contract. The daemon is
offered once after the target selection when no reachable supervised daemon is
available. `--daemon` is explicit consent and `--stdio` selects per-client
`serve` entries; passing both is rejected. The existing daemon provisioning
path is reused, so the supervisor is installed only after consent and only once
for the whole configure operation.

After a successful release installation, `scripts/install.sh` and
`scripts/install.ps1` ask whether to invoke `kivgraph configure`. The shell
installer reads the answer from `/dev/tty` so a `curl | bash` pipeline remains
usable. If no terminal is available it skips the wizard and prints the command
to run later. `KIVGRAPH_CONFIGURE=0` skips the offer and `KIVGRAPH_CONFIGURE=1`
accepts it when a terminal is available. A configuration failure does not roll
back an already installed bundle; the command is printed for retry.

## Alternatives

- **Keep the commands separate:** preserves the existing low-level surfaces but
  leaves first-run setup repetitive and easy to under-configure.
- **Install every supported target automatically:** would modify clients and a
  project without the operator selecting them.
- **Start the daemon during installation:** would make the installer mutate
  service state without the configure consent prompt and would fail when no
  Kivgraph configuration exists yet.
- **Prompt on standard input unconditionally:** blocks the documented pipeline
  where standard input contains the installer itself.

## Consequences

The normal first run is one command and one agent selection. Agents that do not
support a given surface remain usable and are named in the output instead of
turning a partial compatibility mapping into an error. Existing per-surface
commands remain available for project-scoped or targeted maintenance.

The installer is still safe to run in automation: without a terminal it does
not configure clients, install a supervisor or touch the current project. A
user can opt into a non-interactive target list by invoking `configure` with
repeatable `--target` flags.

## Verification

CLI tests cover transport conflicts, unknown and duplicate targets, dry-run
non-mutation, interactive selection, all supported surfaces and Claude Desktop
surface skips. The installer help and documentation name the opt-in controls
for both shell and PowerShell installers.
