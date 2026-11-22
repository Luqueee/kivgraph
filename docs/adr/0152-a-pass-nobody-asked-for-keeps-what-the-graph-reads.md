# ADR 0152: A pass nobody asked for keeps what the graph reads

## Status

Accepted. Narrows ADR 0147 for one caller and closes the question ADR 0149
left open. Adds a flag to `index --full` and a field to its `--json` result;
changes no schema, stable key or tool.

## Context

ADR 0147 lets a pass publish without what it could not read: a module that
does not load is recorded as `MODULE_NOT_LOADED` and the rest is published.
That is right for a pass somebody ran. They read its report, and the report
names the module.

The watcher runs the same pass, and nobody reads that one. On a machine whose
`go` had moved ahead of the build (ADR 0149) every Go module failed to load,
and the daemon replaced a graph of `29339` Go definitions with one of `1293`.
It did so again on every change. Each answer went on saying `COMPLETE` about
what was left, because a module that is not in the graph bounds nothing a
query about another declaration looks at.

The same happens in the middle of ordinary work: an import of a package that
is not downloaded yet, saved, and the graph of that module is gone until the
next save that compiles.

## Decision

A pass the watcher starts is **unattended**. Before it publishes, it compares
what it read with the active generation, and publishes nothing when a file

- held a declaration in the active generation,
- is still a regular file on disk,
- holds no declaration in the candidate,
- belongs to a repository and a language the registry still names, and
- the candidate records a failure ending in `_NOT_LOADED` for that repository
  and language.

The active generation stays. The daemon logs the refusal once, with the first
file and the count, and the remedy; it does not retry on a timer, because the
same sources give the same refusal. The next change tries again.

Each clause keeps out something that is not a loss: a file that was deleted, a
repository or a language that was withdrawn, a file a configuration now
excludes or that no longer declares anything. In those the pass read all it
was asked to, and there is no `_NOT_LOADED` failure to say otherwise.

A pass somebody asked for -- `kivgraph index`, `index --full`,
`index_project`, `start_index_project` -- publishes as ADR 0147 says.

The child process is told with `index --full --unattended`. Its `--json`
result carries `lost`, at most twenty `repository:path` entries, so that the
refusal crosses the process boundary as a fact and not as the words of an
error.

## Rejected

- **Refusing whenever anything is not loaded.** A package that never loaded
  would stop every later publication. The comparison is with what the graph
  reads today.
- **Comparing counts per language.** The case that started this lost most of
  a language, not all of it, and a threshold is a guess.
- **Publishing and marking the generation degraded.** Every tool would have to
  read the mark, and the answers that were right an hour ago would still be
  gone.

## Consequences

- While a module does not load, the graph a daemon serves is the last one
  that read it, and it ages. `graph_status` reports the tree as changed.
- A generation whose snapshot cannot be read is not compared with: the pass
  publishes.
- Every change while the failure lasts costs a pass that publishes nothing.

## Verification

`lostFiles` is tested clause by clause; the refusal is tested across the
process boundary and in the scheduler. By hand, on a copy of one repository of
`35` Go files with an import nothing provides: `index --full --unattended`
exits `1`, names `35` files and leaves generation `000001` active;
`index --full` publishes `000002` with `not_loaded=1`.

Not run: a daemon left serving while a module breaks and recovers.
