# ADR 0157: `daemon install` refuses a home that is not the account's

## Status

Accepted. Changes the behaviour of one command and adds one flag to it,
`--allow-foreign-home`. Changes no tool, schema or stable key.

## Context

`kivgraph daemon install` writes the unit under `HOME` -- a launchd agent in
`~/Library/LaunchAgents`, a systemd unit under `~/.config/systemd/user` -- and
registers it with the session manager of the account that runs it. A shell
whose `HOME` points at a scratch directory therefore leaves a registration
whose files the directory takes with it: a supervised daemon with a state
directory that no longer exists, restarting for as long as the session lasts.

It happened: two hundred such agents accumulated on one workstation from tests
that reached the real supervisor (see `runConfiguredServe`), and the same state
is one `HOME=$(mktemp -d) kivgraph daemon install` away for anyone measuring or
testing with a temporary home. Nothing in the command noticed.

## Decision

`daemon install` compares `HOME` with the home directory the operating system
records for the account, by directory identity -- a symlink to the home is the
home -- and refuses when they differ, before anything is written:

```
daemon install: HOME is /tmp/x and this account's home is /Users/me: the unit
would be written under the first and registered with the second's session
manager, and would outlive it; run it from your own session, or pass
--allow-foreign-home if this is intended
```

`--allow-foreign-home` installs anyway, for the machines where the two differ
on purpose: a container, a service account, a CI image.

Two cases do not refuse, because the comparison cannot be made: the account's
home cannot be looked up or read, and `HOME` cannot be resolved. A guard that
refuses on its own failure turns a missing directory-service answer into an
outage.

`daemon status` and `daemon remove` are untouched. They read or take away what
is under `HOME`, and neither registers anything.

## Compatibility

An install from a shell with a different `HOME` was accepted and now needs the
flag. A script that did that on purpose adds it; nothing else about the command,
its output or the unit it writes changes. The message names the flag.
