# ADR 0090: The installer URL is an observed delivery

## Status

Accepted.

## Context

GitHub release counters measure assets, not installation attempts. They also
combine people, mirrors and the project's own release verification. The
website measures command copies, but a copied command may never run. Kivgraph
therefore had no source for the request that actually fetched an installer,
and no way to separate `curl`, PowerShell and automated clients.

The public installation command must keep using the release asset byte for
byte. Analytics must not become an availability dependency, and the request
arrives before `KIVGRAPH_TELEMETRY` exists in the downloaded shell.

## Decision

The public commands fetch `/install.sh` or `/install.ps1` from
`kivgraph.dev`. A narrowly routed Cloudflare Worker records an
`installer_fetched` event in D1 and answers with a `302` to the corresponding
`releases/latest/download` asset on GitHub.

The event stores the timestamp, endpoint channel, observed `User-Agent`
truncated to 512 bytes, parsed client family and version, a closed CI flag and
the two-letter country supplied by Cloudflare. It stores no address, cookie,
query string, inferred operating system or identifier. `HEAD` requests are not
events. Unknown paths are refused.

The write is best effort. Every write error still returns the redirect, so D1
cannot prevent an installation. The direct GitHub URL stays documented as the
way to avoid this pre-shell delivery measurement.

The same Worker runs after the repository's daily cumulative snapshot. It
loads only the latest snapshot, compares it with the most recent earlier
snapshot stored in D1, and writes the derived delta. A missed day is recovered
by the next cumulative reading instead of becoming a false zero.

The administration website remains a local Bun process. It reads D1 through a
server-only, read-only API token and never sends that token to the browser.

The same redirect boundary exposes `/github` for the landing's repository
links. It records `github_click` separately from installer delivery and then
redirects to the repository. Technical links to issues, source files, releases
or the direct installer fallback continue to target GitHub without measurement.

## Alternatives

- Serving a copied installer from the landing would create a second release
  artifact that could drift from GitHub.
- Calling D1's REST API from the installer would expose credentials and make
  analytics part of the client.
- Inferring installation attempts from GitHub counters would preserve the
  ambiguity this event exists to remove.
- Moving the administration website to Workers would change its hosting model
  without improving the measurement boundary.

## Consequences

Installer delivery, GitHub visits, verified installation and first run remain
separate facts. The public command gains one redirect and the endpoint becomes
a small deployed component with a D1 binding and a scheduled trigger. Raw
observed user agents require a retention bound; the scheduled job removes event
rows older than 90 days while daily aggregates remain.

The direct GitHub command is less convenient but preserves a real opt-out for
the delivery event. Documentation must name that distinction instead of
claiming that a shell variable can affect a request already made.

## Risks

The public endpoint can be called by anyone and its request count is not a
person count. Client-family parsing is descriptive, not authentication. A
Cloudflare route or Worker outage could affect the landing URL, so release
documentation retains the direct GitHub fallback and deployment smoke tests
must follow the redirect through to the official asset.
