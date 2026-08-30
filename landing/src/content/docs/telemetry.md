---
title: Telemetry
description: What Kivgraph reports, what it never reports, and the one variable that turns it off.
---

**As of `v0.9.3` -- the current release -- Kivgraph sends one ping.** `serve`
and the daemon report it the first time each version runs on a machine, the
installers report a second, independent one when they finish, and
`kivgraph daemon install` reports a third when it registers a supervisor entry
for the daemon. This page went up a release before the first two did, so that
the opt-out was documented before there was anything to opt out of, and so
that nobody has to read the source to find out what a version reports.

## The one thing it reports

A single event: **this version arrived here, and what it just did**. One
machine can produce **up to three** of them for one version -- one when an
installer finishes, one when the binary starts for the first time, one when
`daemon install` registers it with the platform -- and they are never added
together: a bundle can be installed and never launched, and a daemon can be
run once without ever being installed as a service.

It carries five fields and nothing else:

| field | values | what it says |
| --- | --- | --- |
| `emitter` | `installer`, `binary`, `supervisor` | which of the three just happened |
| `version` | `MAJOR.MINOR.PATCH`, as in `0.9.3` | which version |
| `platform` | `linux-amd64`, `darwin-arm64`, `windows-amd64` | which build |
| `channel` | `installer`, `mcpb`, `archive` | how it got there |
| `transport` | `stdio`, `daemon` | which arrangement served, on `binary` rows only |

That is the entire payload. There is no field for anything else, so there is
no version of it that carries more.

## What is never reported

- **Nothing about your code.** No repository name, no path, no file name, no
  symbol, no query, no result. The graph never leaves your machine, and this
  ping is not a channel that could carry part of it.
- **No identifier minted by us.** Kivgraph does not generate a machine id, an
  install id or a user id, and does not store one.
- **No hostname, no user name, no environment.**
- **No address stored by us.** The request has one, as every HTTP request
  does. It reaches the analytics collector, which turns it into a
  daily-rotating hash together with a few other values, and keeps the hash
  rather than the address. The practical consequence is that everyone behind
  one NAT counts as one machine, and we would rather say that than let you
  find it out.
- **A rough location, and that one is stored.** The collector derives a
  country from the address before discarding it, and a city whenever the
  address resolves to one -- a home connection usually does, a datacentre one
  usually does not. So the dataset can know that a first run happened in
  Sabadell rather than merely in Spain. No street, no coordinates and no
  address kept. This page claimed the city was always empty until 2026-08-30:
  that was measured with datacentre addresses, and the first real ping had a
  city in it.

## Why the ping exists at all

Download counts cannot answer it. GitHub counts a download when a URL is
fetched, and some of every release's downloads are our own: the release job
publishes three bundles to a package registry, which fetches each one back to
verify the checksum it was given. A directory mirroring a
registry looks exactly like a person installing, and a bundle can be
downloaded and never run.

*How many machines ran it* is a different question from *how many files were
fetched*, and only the binary can answer it.

## What never reports at all

A binary that is not sitting in the layout a release unpacks into does not
report -- it looks for that layout around itself, and the output of `go build`
or `go install` is not in one. That is a check on the surroundings and not on
who compiled the binary: your own build, dropped into an unpacked release,
would report. It also keeps our own continuous integration, which builds and
runs Kivgraph on every push, out of a number that is supposed to be about you.

The binary reports only when it starts serving: `kivgraph serve` and the
daemon. Running `kivgraph index` in a terminal sends nothing, and neither does
`kivgraph daemon status` or `kivgraph daemon remove` -- only `install`
registers the entry that the third fact reports.

## Turning it off

```sh
export KIVGRAPH_TELEMETRY=0
```

Set anywhere the process can see it -- your shell, the MCP client's `env`
block, the systemd unit -- and nothing is sent. There is no second switch on
your side and no partial mode.

The installers take the same variable. It has to reach the **shell**, not the
download:

```sh
curl -fsSL https://kivgraph.dev/install.sh | KIVGRAPH_TELEMETRY=0 sh
```

Turning it off costs you nothing: no feature checks it, and no behaviour
changes.

## Where it goes

To a self-hosted [Umami](https://umami.is/) instance run by this project, into
a dataset of its own, separate from the one that records visits to this
website. Not to a third-party analytics vendor, not to an ad network, and not
anywhere it is joined with anything else.

## When it changes

This page changes in the same commit as the code, or the change does not ship.
If a field is ever added, it is described here first.
