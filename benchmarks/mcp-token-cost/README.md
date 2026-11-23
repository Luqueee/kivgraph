# mcp-token-cost

What a coding session costs with Kivgraph and what the same session costs
with the host's own `grep` and file reads, for one question: who calls this
symbol, and what do those callers look like. Tokens are `cl100k_base`.

The numbers are in `report.md` and `results.json`. This file is how to
produce them again, because one part of the harness cannot be run by a
program alone.

## Two kinds of corpus

- **The repository itself** (this directory: `questions.json`, `native/`,
  `report.md`). It moves with every commit, so its captures go stale and a rerun
  has to retake them. It is what `--smoke` reads symbol names from, and it
  stays the source of the figures the documentation quotes until they are
  replaced.
- **Frozen corpora** (`corpora/<name>/`). A third-party repository at a pinned
  commit: nothing about it moves, so its captures are taken once and a number
  that changes is the server's. `cobra` (Go, `v1.10.2`), `ky` (TypeScript,
  `v2.2.0`) and `ripgrep` (Rust, `14.1.1`).

A frozen corpus is a directory:

| file | what it is |
| --- | --- |
| `corpus.json` | repository, tag, the **full commit** it must be at (a tag can move; an annotated tag's own hash is not a commit), languages, build tags, exclusions, and the commands that make its dependencies readable offline |
| `questions.json` | six symbols: three rare, one shared (declared more than once), two common |
| `native/` | the host's captured `grep` per question and `reads.json`, the token cost of each range read |
| `results.json`, `report.md` | the committed baseline and its report |

## What cannot be generated

The native arm is **the host's own tool output, captured verbatim**: a
`grep` of the symbol, and the range reads the agent then makes. A native arm
written by us would be an imitation, and an imitation is always worse than
the tool it stands in for, which biases the comparison our way. So
`native/*.grep.txt` and `native/reads.json` are produced by an agent running
in the host being measured, with that host's tools. They are tied to the
tree: a range that moved is a range that was not captured, and the run
stops and names every one of them. A frozen corpus is what stops that from
happening on every commit.

## Prepare a frozen corpus

```bash
# a binary built with the ladybug tag (see the root AGENTS.md)
go run ./benchmarks/mcp-token-cost/prepare \
  --corpus benchmarks/mcp-token-cost/corpora/cobra \
  --server /path/to/kivgraph
```

It clones the commit into `/tmp/kivgraph-corpora/<name>/src` (fixed on purpose:
a captured search carries the path it searched), refuses a checkout that is not
at the pinned commit or is not clean, runs the `prepare` commands, registers the
repository in a scratch `HOME`, and publishes a generation. Your own
configuration is never touched. `--then measure` and `--then check` run the
harness straight after.

`ky` needs `node` and `npm` on the `PATH`, and `ripgrep` needs `cargo` and a
`rust-analyzer` (the one this repository pins is found under `.tooling/`).

## Run

```bash
go run ./benchmarks/mcp-token-cost --server /path/to/kivgraph \
  --dir benchmarks/mcp-token-cost/corpora/cobra --repository cobra
```

with `HOME` the scratch one `prepare` printed. Without `--dir` it measures this
repository.

The first run on a tree stops with the list of ranges it needs, one
`path:start-end` per line. That list is the whole recapture.

## Recapture

In Oh My Pi, **in a session started with `KIVGRAPH_DISABLE_HOOK=1`** -- the search
gate refuses the host's `grep` of a name the graph knows, which is the very
capture this needs -- one cell does all of it:

```python
%load benchmarks/mcp-token-cost/recapture.py
await recapture("cobra", server="/path/to/kivgraph")
```

It retakes every search with `tool.grep`, then reads each range the harness asks
for with `tool.read`, counts the texts with the harness's own encoding, and runs
the harness again until it stops asking. Retake **every** range and not only
the missing ones: a file that kept its range can still have changed its lines.

By hand, the same thing: a `grep` of each question's pattern into the `capture`
file it names; each listed range read with the host's `read`, keeping the text
exactly as returned; the texts counted with

```bash
go run ./benchmarks/mcp-token-cost/count < texts.json   # {"path:a-b": "text"}
```

and each added to `native/reads.json` as `"path:a-b": {"file": "path", "tokens": N}`.

## The ratchet

```bash
go run ./benchmarks/mcp-token-cost --server /path/to/kivgraph \
  --dir benchmarks/mcp-token-cost/corpora/cobra --repository cobra --check
```

measures, compares with the committed `results.json` of the corpus, and writes
nothing. It fails when an answer costs more than the baseline by more than
`--tolerance` (`0.05`), when the total does, when the served session does, or
when a question finds **fewer references** than the baseline did -- the last is
what a recall regression looks like here. A figure that improves does not fail;
rerun without `--check` to keep it.

It refuses, instead of passing, whatever is not comparable: a corpus that is not
the tree that was indexed or not the commit of the baseline, another question
set, or another native capture.

CI runs it on every corpus that has a `results.json`, in the `smoke` job, with
the bundled binary. A corpus with no baseline yet says so and is skipped.

## Reading the result

Both factors are published on purpose. The answer factor is the part a graph
server owns, and it flatters the graph. The session factor adds the bodies the
agent then opens, which both arms pay alike, and it flatters `grep`. The
ceiling is what the bodies alone allow: an answer that cost nothing would still
land there.

Neither latency nor money is measured. The cross-repository question lives in
`cross-repo/` and is run separately.

## The repository's own corpus

Index it as its own documentation says, and not with the defaults, because the
defaults make its own fixtures unreadable scopes and the `completeness` block of
every answer says so (ADR 0156 measures what that costs): the `ladybug` build
tag, and `testdata/` left out.

```bash
export HOME=$(mktemp -d)   # never your own: this is an index of a scratch copy
kivgraph init --repository kivgraph=/path/to/kivgraph --languages go,typescript
# config.yaml:       go.build_tags: [ladybug]
# repositories.yaml: exclusions: [testdata]   (under the kivgraph repository)
kivgraph index --full
```

Use a binary built with the `ladybug` tag. Do not run `kivgraph daemon install`
from that `HOME`: it refuses, and for a reason.

The harness used to read only the top-level `files` of a `find_references`
answer. An answer whose callers are of more than one declaration kind comes
grouped by kind, with the files one level down, and every row of it was dropped
without an error: a question could report `0` references and price its bodies at
zero. It reads both shapes now and refuses an answer that returned rows and
yielded none. The figures in `report.md` of this directory predate that fix; the
session factors in it are not comparable with a rerun until its captures are
retaken.
