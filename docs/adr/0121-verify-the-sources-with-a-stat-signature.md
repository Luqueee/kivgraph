# ADR 0121: Verify the sources with a stat signature before reading them again

Status: accepted, 2026-11-16, implemented behind an opt-in. Narrows one rejected
alternative of ADR 0097.

Changes the configuration: a new key, `indexing.source_verification`, whose
default keeps today's behaviour.

Changes the schema: no. The manifest and its digest are untouched.

## Context

ADR 0097 binds a published generation to the sources that produced it. A pass
observes the source tree before analysis and observes it again inside the
validation of the candidate, immediately before `CURRENT` can move; a changed,
missing or unreadable input rejects the candidate. The digest that identifies an
observation hashes every analysed file below every repository, over-approximating
on purpose, and the verification repeats all of it.

`benchmarks/publication-profile` measured that second observation at
`1,25`-`1,39 s` of a `12`-`14 s` pass, `10`-`11 %` of it, on six repositories.
It is the cost that had no name inside `publish`, and it grows with the size of
the tree, not of the graph. `benchmarks/source-verification` splits it, warm, on
the same corpus:

- one observation costs `786 ms`: `613 ms` of digest and `173 ms` of git state;
- of the digest, `62 %` is traversal and `38 %` is hashing, and which dominates
  depends on the repository;
- a stat of every directory read and every analysed file, compared again
  without reading a directory or opening a file, verifies the same tree in
  `25 ms`. That leaves `198 ms`, a saving of `588 ms` or `75 %`.

ADR 0097 rejected "trust file timestamps": timestamps do not identify bytes,
and can stay unchanged across coarse filesystem clocks. That rejection is
right about identity and this decision keeps it. What it does not address is a
different question that the verification asks: not "what are these bytes?" but
"would reading them again give the digest I already have?". A cheap proof can
answer yes only when it is certain, and fall back to the read when it is not.

## Decision

1. **Identity does not change.** The manifest written into the generation, its
   schema and the content digest it carries are exactly as before. Nothing about
   what a generation records moves.
2. **The first observation also keeps a signature.** The walk that computes the
   digest already visits every directory and stats every analysed file. It
   records, in memory and for the length of the pass, the size, modification
   time, inode change time, inode and mode of each, and of each directory it
   entered. The signature is not persisted.
3. **Verification compares signatures first.** Inside the validation closure,
   the verifier stats each recorded directory and file again. If every stamp is
   equal, and no entry was racy, it accepts without reading any directory or
   file. Policy, analyzer fingerprint, composition and git state are still
   compared as today.
4. **Any doubt is today's behaviour.** A differing stamp, a missing entry, an
   entry modified within two seconds of the capture, a stat error, or a
   filesystem the verifier does not trust makes it run the full observation and
   compare digests. The answer is then the one the pass gives now. The signature
   can only let verification skip work; it never produces a verdict the full
   observation would not.
5. **The inode change time is part of the stamp, and is not optional.** A
   modification time can be set by a program; a change time cannot. The harness
   shows the scenario that edits a file without changing its size and restores its
   own modification time is missed without it, and caught with it.
6. **It is opt-in.** `indexing.source_verification` accepts `full` (the default,
   today's behaviour) and `signature`. The default moves only by a later ADR with
   evidence from filesystems this one did not measure.

## Evidence

`benchmarks/source-verification/results.json`, commit `af6dec2`, six
repositories, medians of seven warm samples:

|proof|per observation|
|---|---|
|digest + git state, today|`786 ms`|
|signature verification + git state|`198 ms`|

A change matrix of fourteen modifications on a fresh synthetic tree, applied
one at a time:

- **zero were missed**: every modification that changes the digest changes the
  signature, including a rename over a file, an added file, a deleted file and a
  same-size edit with the file's own modification time restored;
- **four are false positives**: adding or touching a file that is not analysed,
  `chmod`, and temporary files in an ignored build-output directory. Each makes
  the verifier fall back to the full observation, which costs what it costs now.
  A repository being built during the pass keeps the saving only when its build
  does not write into a directory the walk enters.

## Consequences

- With `signature` on, the verification inside a pass measured `1,40 s` with
  `full` and `0,71 s` with `signature`, `-49 %`, and the pass `0,7 s` shorter,
  about `6 %`, in the two undisturbed rounds of an interleaved run (six
  repositories, five of them not read). The first figure of this ADR, `75 %` of an
  observation, was the digest and the git state; a pass's observation also
  computes the analyzer fingerprint, `148`-`483 ms` in a temporary trace, so
  the saving is about half of the step, not three quarters of it.
- A pass whose trees move while it runs costs what it costs today, plus the
  signature comparison, which is `25 ms` on the measured corpus.
- The git state, `173 ms` and almost all of it process start, is unaffected. It
  is a separate cost.
- A change that an entry's stat does not show is a change the verifier would
  accept. The decision does not claim that cannot happen; it makes it require a
  filesystem that misreports `ctime` or a clock that moves backwards, and keeps
  the default off until that is measured where it matters.

## Not decided here

- **Which filesystems are trusted.** The matrix ran on a local APFS volume with
  nanosecond timestamps. On a filesystem with coarse timestamps (FAT, two
  seconds) or stale client attribute caches (NFS, SMB) the signature has no
  guarantee. The implementation has to either detect those and fall back, or the
  option has to say so; both are cheaper to decide with a filesystem to test on.
- **Whether to hash only the files whose stamp changed.** It would shrink the cost
  of a false positive from a full observation to a few reads. It needs per-file
  digests kept for the length of the pass, and is a second step, not this one.
- **The git state.** Spawning git once per repository is `173 ms`; batching it
  is an unrelated change.

## Rejected alternatives

### Trust modification times alone

Rejected, again, for the reason in ADR 0097, and now with a measurement: the
matrix scenario that restores a file's modification time is missed without the
inode change time.

### Verify nothing, or only the commit

The check exists because a source can change between analysis and
publication, including uncommitted bytes that never move `HEAD`.

### Replace the second observation with a file-watcher event stream

A watcher can lose events (queue overflow, a process that was not running), and
its absence cannot be told from "nothing changed". The signature fails towards a
full read; a lost event fails towards publishing.

### Make `signature` the default now

The saving is `10`-`11 %` of a pass and the residual risk is not zero. That is
not a trade to make on one filesystem.

## Implementation

- `sourceobservation.TreeDigestSigned` records the signature during the walk the
  digest already makes; `TreeDigest` is unchanged. The stamp of a directory is
  taken before it is listed and the stamp of a file before it is opened.
- `CaptureWithSignatures` is the first observation of a pass and
  `CaptureReusing` the one inside the validation. `CaptureReusing` returns the
  manifest `Capture` would return: only the content digest of a tree comes from
  the signature, and the git state, the policy and the composition are observed
  again as before.
- **The fallback is per repository, not per pass.** Decision 4 says a doubt makes
  the verifier run the full observation. Implemented, a repository whose
  signature does not hold is read and a repository whose signature holds beside
  it is not. The answer for each repository is the one a full observation gives
  it, which is what decision 4 protects.
- A signature is also refused for a different root than the one it was taken
  from, and never vouches for a tree with a symlinked source, one that had an entry
  vanish while it was captured, or a platform whose stat carries no change time
  (everything that is not Linux or macOS).
- `indexing.source_verification` is `full` by default. A configuration written
  before the key exists loads and verifies as it always did.
- `kivgraph index --full` prints `index.source_verification` with how many trees
  the verification observed and how many it did not read, only when the option is
  on.

## Verification

Done:

- the change matrix of the harness, as unit tests of the verifier, with eleven
  modifications and the `ctime` ablation kept as the test that shows why the
  change time is compared. Five mutations of the implementation (no `ctime`, no
  racy rule, no directory stamps, no root check, a symlink that does not distrust)
  each fail a test;
- tests that every doubtful outcome (differing stamp, missing entry, racy entry,
  stat error, symlinked source) is not reported unchanged, and that the manifest
  the signature path returns equals a full capture's after every modification;
- a test that a quiet tree is not read at all (the walk's per-entry checks drop
  from one per entry to a handful);
- a pass-level measurement with the option on and off, interleaved in one window:
  see `benchmarks/source-verification/report.md`. A repository edited within two
  seconds of the capture is read as before, so the saving is in the repositories
  that were not.

Not done, and why it stays opt-in:

- a run on a network filesystem or a coarse-timestamp one, to decide the trusted
  set;
- the fingerprint and the git state, which the signature does not touch.
