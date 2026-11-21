# ADR 0131: a source symlink is recorded, not fatal

## Status

Accepted. Refines the symlink policy of the source inventory; it does not relax
the rule that symlinks are never followed.

## Context

`freshness.Capture` hashes every source file of a registered repository before
and after a pass, to prove the sources did not move while indexing. It never
followed symlinks, and it enforced that by returning an error:

```text
index --full: capture source inventory: inventory etcd:
inventory ".../client/v3/concurrency/example_election_test.go": refuses symlink
```

One symlink with a source extension therefore aborted the whole pass. `etcd`
links its integration examples into the package that documents them, so a
repository of that size could not be indexed at all, whatever its other files
were. Refusing to follow is the policy; failing the index is only how it was
implemented.

## Decision

A symlink found while walking a repository is not followed and is not an error.
The inventory digest covers the link's text instead of its content:
`<path> NUL symlink NUL <sha-256 of the link target text> NUL`.

- Retargeting, adding or removing the link changes the digest, so a generation
  built before the change is still reported stale.
- The content of the file the link points at does not enter the digest through
  the link. If it is inside the repository it is hashed where it lives.
- A symlink named as an explicit `manifests` entry is still refused. That path
  is a choice the user wrote down, and a link there is worth an error.

A repository with no source symlinks produces the same digest as before.

## Consequences

- `etcd` no longer stops at the inventory. It then stops at another limit: its
  modules require Go 1.27 and a build type-checks only up to the Go version of
  the toolchain that compiled it. That is reported as such and is not changed
  here.
- Whether the Go, TypeScript or Rust loaders read a file through a link is
  decided by each loader and is not changed by this ADR.
