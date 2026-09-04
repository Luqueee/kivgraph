# ADR 0094: preserve runtime identity and freshness during profile upgrade

## Context

The first profile migration copied the entire installation state directory.
A real `daemon.sock` made that copy fail before MCP initialization. Moving the
root would also replace live lock inodes. The profile artifact list omitted
`freshness/`, and the configured server's `profileProjectIndexer` did not expose
the freshness capability implemented by `indexing.Service`.

These are upgrade and adapter defects, not reasons to rebuild an unchanged graph
or weaken Atenea's freshness requirement. This decision refines the migration
section of ADR 0087; it does not change the MCP envelope or analyzer guarantees.

## Decision

Keep the installation root in place. Copy only graph artifacts into a private
staging directory: generations, CURRENT, BACKUP, backups, freshness, factcache,
synthetic go.work and go.work.sum, and the legacy graph.lbdb. Copy the repository
registry separately. Shared analyzer targets, logs, endpoints, PID files,
sockets, and lock files remain at installation scope and are never relocated.

A nonblocking migration lock outside the root serializes upgrades. Acquire the
existing analyzer-targets, resync, and publication locks before copying. Refuse
a reachable daemon and ask the operator to stop it. A stale Unix socket is
allowed but neither copied nor deleted. Other special top-level files, and
special files anywhere inside graph artifacts, fail closed. Permissions and
I/O errors are not interpreted as a stopped daemon or silently skipped.

Validate the registry and CURRENT target before publication. Publish a separate
`.pre-profiles` backup and then atomically rename the candidate into its profile
directory. The original graph remains untouched at installation scope as an
additional recovery source; removing it is an explicit maintenance decision,
not part of loading configuration. This temporarily costs additional disk.

If interrupted between the two renames, retry only when the backup and current
candidate have identical paths, modes and bytes. A differing backup is retained
and reported, never overwritten. Continue supporting recovery of a missing root
from the previous migration's backup. An existing profile must pass validation;
directory existence alone is not successful migration.

`ReadProfile` is a read-only counterpart to the migrating `LoadProfile` entry.
The configured indexer binds freshness to its actual default snapshot store and
startup storage path, rereads the current registry selection, and delegates to
`indexing.Service`. A changed storage location requires restart. A changed
configuration default cannot redirect evidence from a running server's default.
Another profile with the same generation number cannot borrow that attestation.
Aggregate queries continue to omit a global freshness claim.

Pin the status response's snapshot before invoking the inventory probe. The
probe can overlap publication: probing generation 76 and then loading generation
77 previously attached a stale attestation to a successful rebuild. If the probe
itself observes a newer generation than the pinned response, report `unverified`
and retain the probe's generation so clients can retry the read. Never relabel
an attestation with another generation or trigger a rebuild from that race.

## Validation and limits

Regression tests use real Unix sockets and writer locks, interrupted migration
fixtures, mismatched backups, partial destinations, and MCP calls through the
configured indexer. Source edits, additions, removals and missing repositories
must produce stale or unavailable states, independently of semantic coverage.

Deterministic publication-during-probe tests cover both old fresh and old stale
attestations, plus a probe that observes a newer generation. An isolated read of
the retained real generation 77 confirmed its inventory digest still matched;
this does not establish that no transient filesystem change occurred earlier.

The migration checks filesystem layout, not native graph integrity; opening the
published snapshot remains a separate startup gate. Installation must stop old
writers, retain the old bundle, verify native startup and MCP freshness, and
roll back on failure. An old binary must not resume writing the retained legacy
graph alongside a new profile-aware server. No automatic cleanup or installation
is performed by a freshness query.
