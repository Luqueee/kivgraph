# ADR 0092: first-run telemetry joins the delivery dataset

- **Status:** accepted and implemented
- **Date:** 2026-08-31
- **Supersedes:** the collector and storage choice in ADR 0083

## Context

ADR 0083 separated downloads, verified installs and binary first runs because
they are different facts. Its emitters shipped, but their data did not meet
again: installer delivery and GitHub deltas were stored in D1, while the
versioned completion and first-run events went to a dedicated Umami property.
The internal dashboard queried only D1.

That split made a real `v0.9.5` installation invisible. D1 had the request for
`/install.sh`, but that request necessarily has no release version because the
script has not run yet. The versioned installer event reached the landing
collector, but the dashboard could not query it. The newest GitHub snapshot
also predated the release, so no other row introduced `0.9.5` into the release
table.

This is not eventual consistency. Two stores with no join never converge.

## Decision

The exact public route `POST /api/telemetry/first-run` is owned by the same
Cloudflare Worker that owns `/install.sh`, `/install.ps1` and `/github`. The
payload emitted by released clients does not change: `emitter`, `version`,
`platform`, `channel` and `transport` on binary rows.

The Worker keeps the validation contract from ADR 0083:

- unknown fields and values are refused;
- versions at or below the last release without emitters are refused;
- bodies above `1024` bytes are refused;
- every accepted or refused request answers `204`;
- an analytics failure never changes that response.

The address is observed only at the Cloudflare edge. Before a write, the
Worker hashes a tuple of secret salt, UTC day, address, version and emitter
with SHA-256. D1 stores that digest and the country code supplied by
Cloudflare; it stores no address, city, coordinates, cookie or query string.
The secret is a Worker secret and is never part of the client or repository.

A partial unique index over event, version and daily visitor hash makes one
address contribute at most one row per emitter, version and UTC day. An
`AFTER INSERT` trigger updates `daily_metrics` in the same D1 transaction, so
the raw fact and the dashboard aggregate cannot diverge. The public emitters
map to separate internal facts:

| emitter | D1 event |
| --- | --- |
| `installer` | `install_succeeded` |
| `binary` | `binary_first_run` |
| `supervisor` | `supervisor_registered` |

Only `binary_first_run.unique_count` is an activation count. Installer
successes, binary first runs and supervisor registrations are never added
together.

The landing's original handler remains an origin-only fallback while deployed
clients continue to use the same URL. The public Cloudflare route takes
precedence, so production writes have one owner and one dataset.

## Consequences

- A genuine versioned event appears in the internal dashboard immediately;
  it does not wait for the next GitHub snapshot.
- Installer delivery remains versionless. Assigning the later release version
  to the earlier HTTP request would be inference, not observation.
- The dashboard can compare GitHub deltas, installer delivery, verified
  installs and first runs without joining a second analytics product.
- Umami remains the website and crawler analytics system. It no longer owns
  the production first-run route.
- A corporate NAT is one daily visitor. That bias is explicit and resets each
  UTC day.
- A missing D1 migration or Worker hash secret makes release telemetry fail
  closed. The release procedure therefore verifies both before considering a
  publication complete.

## Rejected alternatives

**Teach the dashboard to query Umami.** This keeps two stores, adds an
authenticated API dependency to every dashboard load and still needs a join
between incompatible notions of a visitor. It fixes the screen while
preserving the split that broke it.

**Attach a version to `/install.sh`.** The edge has not observed the selected
release or a successful installation at that point. Guessing `latest` would
mislabel cached scripts, failed installs and concurrent releases.

**Store the address and deduplicate later.** It would answer no additional
product question. The daily digest is sufficient for the declared metric and
removes data the project has no reason to retain.
