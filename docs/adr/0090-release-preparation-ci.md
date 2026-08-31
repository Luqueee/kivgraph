# ADR 0090: Fast CI for release preparation commits

- **Status:** accepted
- **Date:** 2026-08-31
- **Changes the release protocol:** no
- **Changes the persistent schema:** no
- **Requires a rebuild:** no

## Context

A release-preparation commit updates version and installation metadata before a
tag is created. It does not change the product, but the ordinary CI workflow
previously scheduled every semantic, native, cross-platform, and bundle job.
For example, commit 00c1510b6e6e16d63123635a12ef39ae22114e74 changed five
metadata files in 28 lines while still selecting the complete workflow; the
classifier keeps that distinction explicit without making an unsupported timing
claim.

The tag workflow calls this workflow through `workflow_call`. A tag must
continue to receive the complete verification because it produces the published
bundles. The optimization therefore applies only to push and pull-request
events, never to the release workflow.

## Decision

The first job in `.github/workflows/ci.yml` classifies the complete revision
range. It selects the release-preparation path only when all of these conditions
hold:

- the event is a push to `main` or a pull request targeting `main`;
- the subject is `chore(release): prepare vX.Y.Z`, with an optional
  prerelease suffix and optional squash-merge issue number;
- every path changed by every commit in the range is in this allowlist:

  `README.md`
  `docs/installation.md`
  `internal/version/version.go`
  `landing/src/content/docs/install.md`
  the release note whose filename is the exact tag version,
  `landing/src/content/releases/vX.Y.Z.md` (including any prerelease suffix).

For a pull request the classifier uses the event's head SHA rather than
GitHub's synthetic merge commit, so the preparation subject is evaluated on the
actual contributor commit.

The classifier examines commit paths rather than only the final tree. A change
that is added and then reverted in the same push still selects complete CI.
Missing or invalid commit objects, an unsupported event, a subject mismatch, or
one path outside the allowlist all fail closed to the complete workflow.
The shell classifier has a self-test covering these negative cases and runs that
self-test in CI.

When the release-preparation path is selected, the workflow runs only the
metadata checks: the version package tests, compilation and vetting of the
version and CLI packages, the version command, the documentation ratchet, and
the landing checks. It does not build a distribution bundle or access
LadybugDB.

All other revisions retain the existing jobs unchanged. A final `CI result`
job is the single aggregate status: it requires the metadata job on the fast
path and every existing job on the complete path. Repository branch protection
should require this aggregate check rather than a skipped platform job.

## Consequences

Metadata-only release preparation is fast and does not consume native or
cross-platform runners. Tags remain protected by the complete CI because
the `workflow_call` path cannot select the shortcut.

The allowlist is a safety boundary, not a general change detector. A release
commit that touches source code, build scripts, lockfiles, workflow files, or
any other product input automatically receives the complete suite. Extending
the fast path requires an explicit change to the classifier, its self-tests,
this ADR, and the branch-protection check.
