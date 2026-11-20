# ADR 0143: An indexer's process tree does not outlive it

## Status

Accepted. No persistent format changes.

## Context

Java and C# are indexed by building them. `scip-dotnet` runs `dotnet restore`,
a Roslyn build host and MSBuild worker nodes; `scip-java` runs Maven or Gradle
and their JVMs. Both were started with `exec.CommandContext`, which knows one
process. Three things followed, each observed:

- **A time limit killed the launcher and left the rest.** The children also
  kept the output pipe open, so the caller went on reading it.
- **A kivgraph that was itself killed left all of it.** Killed `0.2 s` into a
  C# index, the indexer was still running three seconds later with no parent;
  on a large solution it runs to the end for nobody. `dotnet restore`
  processes, build hosts and indexers hours old were found on one workstation.
- **A successful index left a build daemon.** One MSBuild worker node per pass,
  `117 MB`, idling for the fifteen minutes MSBuild keeps it for reuse.

The Dart analysis server had the second problem alone and got a guard of its
own in the previous change.

## Decision

`internal/proctree` runs a tool as a tree. Its `CombinedOutput`:

1. starts the tool as the leader of a process group of its own;
2. kills the group, not the process, when the context is cancelled, and bounds
   the wait on the pipes after that;
3. guards the group against this process dying: a watcher blocks on a pipe only
   kivgraph writes to and kills the group when the kernel closes it;
4. kills what is left of the group when the tool returns, whatever it returned.

The Java and C# loaders call it. The Dart guard moves there and keeps guarding
one process, because the analysis server is one.

The fourth point is the one with a cost. A build daemon the tool started is
killed, so the next index starts another: on `GuardClauses` an index took
`11.4 s` against `9.7 s`, one run each. A daemon the user already had running is
not in the group and is not touched; the build goes on using it.

## Consequences

- Nothing a Java or C# index starts is running after the index returns, is
  cancelled or is killed. Measured with `SIGKILL` at several points of a C# and
  a Java index: no process left three seconds later.
- The tools no longer receive the terminal's interrupt directly, since they are
  in another group. `kivgraph` cancels its context on `SIGINT` and `SIGTERM`,
  which now kills the group.
- Unix only. Elsewhere the wait is bounded and the rest is a declared
  limitation; the equivalent is a job object.
- The watcher is `/bin/sh` with one `read`. Without a shell the tool runs
  unguarded, as it did before.
- Not changed: `rust-analyzer` and the Python worker are still started as one
  process each.
