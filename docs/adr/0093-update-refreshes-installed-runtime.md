# ADR 0093: update refreshes installed runtime integrations

- **Status:** accepted and implemented
- **Date:** 2026-09-01
- **Revises:** ADR 0068, ADR 0069 and ADR 0078
- **Changes the CLI surface:** yes -- a successful bundle update also refreshes
  installed daemon, hook, skill and MCP integration state
- **Changes the MCP protocol:** no
- **Changes the persistent graph schema:** no
- **Requires a reindex:** no

## Context

`kivgraph update` replaces the release bundle in place. The old implementation
stopped after that replacement and only offered to stop client-owned `serve`
and `ui` processes. A supervised daemon could therefore keep serving the old
binary, while client hooks, skills and MCP registrations were left to a later
manual install.

There is an additional Unix detail: after the directory swap, the updater can
resolve its own process through the `.previous` directory. Using that path to
describe the supervisor or an integration writes a path that disappears when
the update finishes.

## Decision

The update command captures the executable path before installing the release
and uses that path for every post-install operation.

After the bundle is installed, it performs these operations in order:

1. If the default configuration has an installed supervisor, refresh that
   daemon with the captured executable path. A missing supervisor is not
   provisioned as a side effect of update. A unit whose main definition exactly
   matches a Kivgraph-rendered legacy format is explicit ownership evidence:
   update rewrites it, reloads it and restarts the daemon. A hand-edited or
   foreign definition remains untouched and makes the update exit non-zero.
   On Linux, any user unit drop-in is treated as operator-managed configuration
   and has the same preservation rule; update does not merge or overwrite
   drop-ins.
2. Inspect user-scoped hooks and skills for every supported client. Existing
   Kivgraph-managed or broken entries are reinstalled; absent and incompatible
   entries are left alone. Project-scoped files are not changed by a
   user-level update.
3. Inspect existing user-scoped MCP entries. Existing `stdio` registrations
   remain `stdio`; existing daemon registrations remain daemon registrations
   and follow the currently published endpoint when it is available. A stale
   endpoint is replaceable without `--force` only when it exactly matches the
   endpoint persisted by the daemon before the update. A URL and bearer header
   alone are not ownership evidence. A missing endpoint never converts a
   daemon entry into `stdio`, and missing or foreign entries are not created or
   overwritten.

The normal integration ownership rules still apply. In particular, an edited
canonical skill is preserved as defined by ADR 0078, and the update does not
pass `--force` on behalf of the user.

The bundle replacement is not rolled back when a post-install operation fails.
The command reports the installed release and exits non-zero so automation can
detect that runtime refresh is incomplete. It continues inspecting the other
runtime surfaces and reports each failure.

## Consequences

Open chats using the supervised daemon see the new daemon after an update, and
managed hooks, skills and MCP entries are brought in line in the same command.
An update remains safe for clients and repositories it does not own: it does
not install new integrations, mutate project files or overwrite foreign or
user-edited content.

An operator may still need `kivgraph daemon install` when the supervisor is
foreign, hand-edited or absent, and a user-edited skill still requires the
explicit `--force` choice to replace it. Those are reported limitations rather
than silent data loss. If a managed daemon refresh fails, update keeps its
published process out of stale-process cleanup, so the normal recovery prompt
cannot stop the only supervised daemon.
