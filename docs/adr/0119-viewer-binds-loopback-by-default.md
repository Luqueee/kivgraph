# ADR 0119: The viewer binds loopback by default and checks its Host

Status: accepted, 2026-11-15. Revises ADR 0032.

Changes the configuration: the default of `web.address` is `127.0.0.1:7777`,
not `0.0.0.0:7777`.

Changes the schema: no.

## Context

ADR 0032 made `kivgraph ui` listen on every interface by default, on the
argument that the graph is indexed on one machine and looked at from another,
so a loopback default "is not protecting anybody: it is in the way". The
guard became a warning.

The viewer has no authentication, and its responses carry repository and file
paths and symbol names and signatures. Two things made the warning a weak
guard:

- it is logged, and a viewer started by a script, a service or an agent's
  shell has nobody reading the log;
- the only request check was `Origin == Host` when `Origin` is present. A
  page on a hostname an attacker controls, resolved to `127.0.0.1` (DNS
  rebinding), reaches a loopback viewer with that hostname as both `Host` and
  `Origin`, and the check agrees with itself. Nothing validated `Host`.

The daemon already binds loopback by default, requires a token and refuses a
non-loopback bind without `--allow-remote` (ADR 0066, 0069). The viewer was the
one network surface of this program that did neither.

## Decision

1. **The default is `127.0.0.1:7777`.** Reaching the viewer from the network
   is a choice: `--addr` or `web.address`. Such a bind still logs what it
   exposes and how to close it.
2. **A loopback bind validates `Host`.** `webapi.Run` wraps the handler when
   the address it bound is loopback, and refuses with `403` a `Host` that is
   not `localhost`, a name under `.localhost`, or a loopback address, with or
   without a port. The attacker cannot choose a `Host` the viewer recognises.
3. **An exposed bind does not judge `Host`.** A viewer the operator chose to
   expose is reached by names this process cannot know; there the bind is the
   decision.

## Compatibility

A configuration written by an earlier `kivgraph init` carries
`web.address: 0.0.0.0:7777` explicitly, because `init` wrote every default. It
keeps working, keeps binding every interface and keeps warning. Only a
configuration that does not set the key, and one `init` writes from now on,
changes. Nothing that was valid becomes an error.

Anyone who looks at the viewer from another machine now sets `web.address` or
passes `--addr 0.0.0.0:7777` once.

## Not done

This adds no authentication. A viewer exposed on a network is still open to
whoever can reach it, and that is stated in the documentation.

## Verification

- `TestLoopbackViewerRefusesAForeignHostEvenWithAMatchingOrigin` runs the real
  `Run`: a foreign `Host` with a matching `Origin` gets `403` and is served
  with `200` on main before this change.
- `TestLoopbackHostNamesOnlyThisMachine` pins the accepted and refused forms,
  among them `localhost.evil.test`, `127.0.0.1.evil.test` and
  `127.0.0.1:7777@evil`.
- `TestExposedViewerDoesNotJudgeTheHost` pins the third decision.
- The default is asserted in `internal/config` and in `cmd/kivgraph`.
