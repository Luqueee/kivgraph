# ADR 0115: preserve runtime identity during profile upgrade

## Context

The first profile migration copied the entire installation state directory.
A real `daemon.sock` made that copy fail before MCP initialization. Moving the
root would also replace live lock inodes. The profile artifact list omitted the
published freshness attestations.

These are upgrade defects, not reasons to rebuild an unchanged graph or weaken
freshness guarantees. This decision refines the migration
section of ADR 0087; it does not change the MCP envelope or analyzer guarantees.

## Decision

Keep the installation root in place. Copy only graph artifacts into a private
staging directory: generations, `CURRENT`, `BACKUP`, backups, freshness,
synthetic `go.work` and `go.work.sum`, and the legacy `graph.lbdb`. Copy the
repository registry separately. The content-addressed fact cache, analyzer
targets, logs, endpoints, PID files, sockets, and lock files remain at
installation scope and are never relocated.

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

## Validation and limits

Regression tests use real Unix sockets and writer locks, interrupted migration
fixtures, mismatched backups, and partial destinations.

The migration checks filesystem layout, not native graph integrity; opening the
published snapshot remains a separate startup gate. Installation must stop old
writers, retain the old bundle, verify native startup, and roll back on failure.
An old binary must not resume writing the retained legacy graph alongside a new
profile-aware server.
