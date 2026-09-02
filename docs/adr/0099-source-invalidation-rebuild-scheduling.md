# ADR 0099: schedule profile rebuilds from source invalidation state

- Status: Accepted
- Date: 2026-09-02
- Issue: #150

## Context

ADR 0098 records which published profiles depend on a mutable source and why
they are stale. A long-running server still needs to observe source changes,
fan them out to those profiles and rebuild without making readers wait for a
partially indexed graph.

The filesystem backend can drop notifications, several profiles can share one
worktree, and a source can change while a full pass is analyzing it. The
rebuild path therefore has to remain the existing complete, verified
publication path.

## Decision

Each enabled profile owns a source monitor. The monitor installs filesystem
watches before seeding its content-hash cache, filters events with the same
source policy as reconciliation, and periodically reconciles the complete
tree. Its initial observation is reported when the caller already has a
published manifest, closing the startup gap without making a cache-less
standalone monitor rebuild on every launch.

The installation owns one scheduler over the invalidation manager. It queues
each stale profile at most once, serializes rebuilds through the existing
profile indexer gate and starts a complete `index --full` child pass. The pass
captures a source manifest, verifies that manifest again before publication and
records the new generation only after all rebuild gates pass. A failed pass or
a source that changes during analysis leaves the previous generation active
and the profile stale. Failed stale rebuilds retry asynchronously with a
bounded per-event budget so one profile's backoff does not block others.

If generation publication succeeds but derived invalidation bookkeeping fails,
the pass remains successful. Human output reports the bookkeeping warning and
the `index --full --json` result carries optional `recording_error` data so a
caller can distinguish a published graph from a failed rebuild.

## Consequences

- Dirty edits, commit movement and provider changes reach every dependent
  profile without rebuilding the same profile repeatedly for one event batch.
- Dropped filesystem events are recovered by periodic reconciliation.
- A source change during analysis cannot publish a graph that describes older
  input bytes.
- Scheduler shutdown waits for both active rebuild work and delayed retries.
- A bookkeeping failure is visible without incorrectly claiming that the
  canonical generation failed.

## Rejected alternatives

- Rebuilding synchronously in the filesystem callback would stop event
  consumption during a full pass and increase the chance of missed changes.
- Scheduling one queue per profile would duplicate shared-source fanout and
  allow analyzer targets to contend outside the existing process gate.
- Treating a failed rebuild as a successful publication would clear stale
  state and make the next pass unable to explain what remains wrong.

## Verification

The watcher tests classify source and manifest events, close cleanly after
cancellation and cover the initial-cache contract. The indexing and command
tests cover source observation, stale-profile fanout and deduplicated rebuild
scheduling. Bazel and the Go test suites exercise the same direct dependency
graph used by the production targets.
