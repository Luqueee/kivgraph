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

## A question has to be priceable by name

The harness opens each caller with `get_source`, by repository, path and qualified
name, and `get_source` refuses a name that two symbols share. A TypeScript
constructor parameter property is both a parameter and a property under one
qualified name (`HTTPError.response`), and two parameters of one name in a file
share theirs too (`newDefaults` in `ky`). A symbol whose callers include one of
those cannot be priced, and the run stops with `AMBIGUOUS_SYMBOL`. `ky` asked
about `Options` and now asks about `Hooks` for that reason. Replacing a question
changes the question set, so it is done before the baseline is committed, never
after: the ratchet refuses to compare two sets.

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

The graph of a corpus is not identical on every platform. `ripgrep` indexes to
`4,978` symbols on `darwin/arm64` and `4,975` on `linux/amd64`, and the Linux
graph names one caller range (`crates/searcher/src/sink.rs:322-328`) the macOS
graph does not. A baseline recorded on one platform therefore carries the host
reads of the other too, captured at the same tree; a range the platform in use does
not ask for is simply unused. If CI stops with `no captured host read`, take
the listed range with the host's `read`, count it, and add it to `native/reads.json`.

## Reading the result

Both factors are published on purpose. The answer factor is the part a graph
server owns, and it flatters the graph. The session factor adds the bodies the
agent then opens, which both arms pay alike, and it flatters `grep`. The
ceiling is what the bodies alone allow: an answer that cost nothing would still
land there.

The baseline does not time either arm. The cross-repository question lives in
`cross-repo/` and is run separately.

### Direct versus lookup-first, including latency

For “who calls X?”, start with `find_references(name)`; pass the full selector
instead if the question already identifies a declaration. Narrow an ambiguous
name rather than choosing the first candidate. To open the bodies, use the
subject and caller selectors from the answer with `get_source`; this path needs
no end line for the subject. A host range read that needs its end line is a
different route and may still need the lookup.

Run after preparing each frozen corpus. `--routes` checks the original baseline
and writes nothing:

```bash
go build -o /tmp/kv-routes ./benchmarks/mcp-token-cost
for c in cobra ky ripgrep; do
  HOME="/tmp/kivgraph-corpora/$c/home" /tmp/kv-routes --routes \
    --server /tmp/kv-main --dir "benchmarks/mcp-token-cost/corpora/$c" \
    --repository "$c"
done
```

For new measurements, pass `--routes-raw /path/to/new-file.jsonl` to record
each measured pair before pooling: question, zero-based pair, arm, one-based
position, and answer/session durations in nanoseconds. Existing files are
never overwritten. Historical percentiles cannot reconstruct the raw samples.

Local stdio, darwin/arm64, server built at `de7a74e0`, 2026-11-24. Each
question gets one discarded pair, then 20 pairs alternating which goes first,
on the same warm MCP session. Percentiles pool 120 samples per corpus and arm.
An answer ends at receipt of the first references page. A served session ends
after fetching the subject and every caller **on that page**, in batches of up
to 20. The run rejects different pages (except opaque cursor values) or source
payloads. Tokens count response text once per question, not once per repeat.

| Corpus | Answer tokens: lookup → direct | Served session tokens: lookup → direct | Native session tokens |
| --- | ---: | ---: | ---: |
| cobra | 2,697 → 2,175 | 54,428 → 53,906 | 73,680 |
| ky | 2,235 → 1,235 | 40,874 → 39,874 | 57,564 |
| ripgrep | 4,429 → 3,929 | 216,831 → 216,331 | 228,736 |

| Corpus | Answer p50/p95 ms: lookup → direct | Served session p50/p95 ms: lookup → direct |
| --- | --- | --- |
| cobra | 0.471/1.259 → 0.275/0.947 | 1.739/6.875 → 1.536/5.783 |
| ky | 0.471/1.247 → 0.237/0.809 | 1.830/3.524 → 1.655/2.933 |
| ripgrep | 0.531/1.097 → 0.312/0.683 | 4.779/11.080 → 4.111/10.900 |

No native latency is inferred from token captures. These timings exclude
startup, indexing, model inference and token counting; they are not a CI SLO.

### Which bodies did an agent actually open?

Pilot on the same frozen trees and server build as `--routes`. We took the
first symbol in each question set and asked one concrete maintenance question:
show how `ExactArgs` rejects a bad count and cite a test; identify the exact
Ky user-supplied Content-Type predicate and its header inputs; compare the two
Standard sink constructors and the initial state of their `Replacer`.
These are targeted questions, not a request to explain every caller.

Nine fresh OMP scout sessions (`claude-haiku-5-5`), one per task and policy.
Each started with the same actual references page for its task. The only
source reader accepted named bodies from that page and called `get_source`.
The eager reader opened the subject and every row on the first request.
The free reader opened the agent's selection. The narrow instruction also
asked for the smallest sufficient declarations first and explicitly described
the reader's scope. Pages were not changed or pruned between policies.

| Corpus | Full model tokens: eager / free / narrow | Wall seconds: eager / free / narrow |
| --- | ---: | ---: |
| cobra | 295,199 / 153,137 / 114,014 | 23.1 / 15.4 / 15.3 |
| ky | 255,022 / 92,682 / 188,932 | 24.1 / 14.0 / 30.0 |
| ripgrep | 140,416 / 77,230 / 115,416 | 16.2 / 11.2 / 14.8 |

Model tokens sum provider usage over all turns, including cached input,
repeated context and output; they are not unique tokens or dollar cost. Wall
time is from delivery of the question to submission of the answer. There was
one run per cell, with parallel agents and no control for prompt cache warmth.

| Corpus | Eager rows incl. subject | Free / narrow bodies opened | Source evidence sufficient for the question |
| --- | ---: | ---: | --- |
| cobra | 18 | 3 / 3 | `ExactArgs` and `TestExactArgs_WithInvalidCount` |
| ky | 3 | 3 / 3 | `Ky.userProvidedContentType` alone |
| ripgrep | 9 (7 distinct) | 4 / 3 | `sink`, `sink_with_path`, `Replacer::new` |

All nine answers contained the required facts when checked against the pinned
source, not necessarily correct incidental commentary. For instance, the narrow
Ky answer got the predicate right but incorrectly described its position before
options initialization. Both selective Ky sessions read the entire class.
The narrow instruction caused a second batch, not a saving. The free Rust
session skipped both enclosing modules; removing literal duplicates alone
reduced the raw direct MCP session only from 40,331 to 35,393 tokens, against
881 for the free selection. The host truncated the eager Rust response, so its
raw MCP volume is not what the model saw.

The reader could not fetch names outside the page. Failed lookups and extra
turns are included in the full-session figures. This is evidence for allowing
selective reads, not a ranking of prompts, a general latency factor, or a
comparison against a native agent allowed to read selectively too.

### Selective grep versus selective graph, starting from the question

Two independent runs per question and arm, twelve fresh scouts in total.
Each received the same question and starting symbol, not a references page
or an answer key. The text arm chose regexes, paths, context and ranges with
`rg -n` (15.2.0) and the host reader. The graph arm could repeat or batch
lookups with `find_symbol`, `find_references` and `get_source`, including
symbols outside the first page. Both could stop when they had enough evidence.
[Session records](selective-grep.json) include the prompts, frozen revisions,
queries, selected reads, provider usage and summaries.
See the [agent run record](agent-runs.md) for launch, seed and provenance
details, including which historical adapters cannot be replayed exactly.

| Corpus | Full tokens: grep / graph | Sum of session seconds: grep / graph |
| --- | ---: | ---: |
| cobra | 421,763 / 422,406 | 38.7 / 46.3 |
| ky | 804,050 / 566,326 | 62.2 / 68.2 |
| ripgrep | 359,969 / 349,460 | 44.5 / 56.3 |

All twelve answers gave the required facts against the pinned sources; this
does not certify every incidental claim or exact line citation. The graph won
both token comparisons on ky; the winner flipped across runs on each of the
other two. Both arms took 33 model turns overall. Provider-reported cost was
$0.06989 for text and $0.07817 for graph, with uncontrolled cache warmth.

Three setup blocks were discarded before these two: the native path guard
initially rejected the macOS temporary-directory symlink, and concurrent graph
calls could not safely share the prototype stdio transport. The recorded graph
arm opens a fresh stdio connection and server process per call against an
already indexed corpus. The text arm starts `rg` per search. Times include the
model and tool adapters but not indexing; parallel load and only two runs per
cell preclude a stable latency estimate.

### Persistent MCP and typed-reference questions

Two fresh scouts per arm per question, 24 sessions. Each graph agent had
one initialized stdio connection for its whole session: 158 MCP calls on 12
connections, checked by process ID and request sequence. Text agents chose
their own `rg` queries and host range reads as before. No arm received a
references page up front. [Records and prompts](persistent-selective-grep.json)
include calls, token usage, timing, answers and transport measurements.

| Question set | Full tokens: text / graph | Seconds: text / graph | Model turns: text / graph |
| --- | ---: | ---: | ---: |
| Three original controls | 1,445,935 / 1,918,346 | 125.0 / 154.1 | 30 / 46 |
| Go: package function vs same-named method | 320,614 / 640,533 | 43.8 / 63.7 | 8 / 16 |
| TS: property vs retry helpers and hooks | 1,021,886 / 2,131,156 | 70.0 / 181.4 | 15 / 44 |
| Rust: fields storing the module-private Config | 352,196 / 552,913 | 39.1 / 54.2 | 8 / 14 |

All 24 answers supplied the requested facts, not necessarily accurate incidental
claims or individual line citations. The graph adapter exposed three operations
through a single JSON-argument tool, not the native per-operation schemas. The
two TS property sessions alone received 17 validation, ambiguity, not-found or
invalid-argument responses. This measures the adapter and model as used; it
does not establish that type resolution itself is slower. The new questions
also admitted cheap text searches: for example, both Rust fields are in the
same file, and the question identifies the module.

A separate fixed-query replay isolated the transport: six selectors, six
alternating pairs each, identical result rows. Median/p95 were 27.46/46.31 ms
starting and stopping a server per call versus 0.51/1.37 ms reusing one. The
agent times exclude the initial connection and indexing and include inference;
the replay includes neither inference nor agent decisions. Session totals are
sums, not concurrent makespan. Comparing the old and new agent totals is not
a controlled estimate of the effect of persistence.

### Native MCP schemas for KyOptions.retry

Four fresh headless OMP sessions asked the same TS property question, without
its path or any preloaded results. The native mount forwarded the server's
`tools/list` descriptors unchanged for `find_symbol`, `find_references` and
`get_source`. The control exposed the same operations through one MCP tool
taking an operation and JSON argument string. Each session used one persistent
server. The order was native, generic, generic, native; the model was
`claude-haiku-5-5` with low thinking. [Calls, usage and answers](ky-native-tools.json).

| Arm (two sessions) | Rejected / attempted calls | Model turns | Full model tokens | Seconds to final token | Reported USD |
| --- | ---: | ---: | ---: | ---: | ---: |
| Generic | 1 / 10 | 11 | 532,273 | 39.559 | 0.02096 |
| Native schemas | 0 / 10 | 8 | 379,497 | 33.884 | 0.01800 |

All four answers identified the clone, recursive array copy and option-key
registry. The one rejection was `SYMBOL_NOT_FOUND` for `Ky.cloneInitHookOptions`
instead of the file-level function; neither arm had a schema error or an
ambiguity refusal. Native agents batched lookups in both sessions. Token totals
include repeated and cached context, not just the tool payloads; times exclude
startup and indexing. The previous 17 rejections were in scout sessions, not
this headless client. They did not recur in its generic control either, so the
historical difference cannot be attributed to schemas alone.

### All six questions: native MCP versus selective host grep

Twenty-four fresh headless OMP sessions, the same six questions, frozen
repositories and low-thinking Haiku model as above. Native then grep, then
grep before native in the second pair; corpora ran in parallel. Each native
agent used only the three unchanged MCP tool schemas over its own persistent
connection. Each text agent chose its searches and ranges with the host's
`grep` and `read`. No references or source bodies were supplied. Wall time ends
at the last answer token, excluding setup and indexing. [Session evidence,
calls and answers](six-native-vs-grep.json).

| Question | Full tokens: grep / MCP | Seconds: grep / MCP |
| --- | ---: | ---: |
| `ExactArgs` | 517,880 / 587,370 | 18.8 / 24.2 |
| `mergeHeaders` | 375,396 / 495,721 | 21.9 / 30.2 |
| `Replacer` | 350,887 / 316,676 | 19.8 / 18.8 |
| `MarkFlagRequired` | 300,194 / 413,387 | 20.5 / 29.9 |
| `KyOptions.retry` | 599,222 / 487,033 | 36.2 / 35.4 |
| `line_buffer::Config` | 305,830 / 565,246 | 22.6 / 32.4 |

The required facts were present in all 24 answers; this does not score every
incidental claim or inferred line number. Text took 54 model turns and MCP 62.
Of 70 MCP calls, 11 were refused: six invalid arguments, three symbols not
found, two ambiguous symbols. Two of 66 text calls failed on a missing path.
Summed reported costs were $0.06149 and $0.08310 for text and MCP. The earlier
zero refusals on the retry question did not repeat. Two repetitions per cell
and uncontrolled prompt-cache state do not establish a general cost ranking.

### Making the selector choice visible

`selector-schema-ab.json` records the same six questions, twice per variant,
using native MCP tools. Only the descriptions changed, not validation.
The baseline and field-only descriptions alternated order; the final variant,
which also names the alternatives in the tool description, ran afterwards.

| Metric, summed over 12 sessions | Baseline | Fields only | Final |
| --- | ---: | ---: | ---: |
| Invalid arguments / tool calls | 6 / 70 | 5 / 55 | 1 / 58 |
| All rejected calls | 11 | 6 | 6 |
| Answers meeting the rubric | 12 | 11 | 12 |
| Model turns | 57 | 52 | 53 |
| Full provider tokens | 2,663,662 | 2,361,705 | 2,457,800 |
| Seconds through final answer | 153.2 | 150.9 | 132.2 |
| Provider cost (USD) | 0.05714 | 0.05874 | 0.07812 |

One field-only answer cited both Go callers but denied one was a caller.
The final Rust run still mixed two selectors once. Three ambiguities and two
missing symbols account for its other refusals. Historical grep used
2,449,409 tokens: essentially level, not a demonstrated graph advantage.
Cache state and order were uncontrolled across the baseline and final variant.

### Opening the containing function in Ky

[`ky-get-content-length-position-ab-ten.json`](ky-get-content-length-position-ab-ten.json)
records ten fresh pairs on one frozen question, alternating which MCP binary
ran first. The prompt was identical and did not teach the position selector.
The old and new servers took 277.7 and 193.5 summed seconds respectively.
The new selector was used in all ten sessions. Every first position request
omitted `snapshot_id`; two retries also encountered a changed generation.

Required facts without a contradictory caller count: 10/10 old, 9/10 new.
The failed answer counted a repeated context line as another call site. Remove
both pairs with off-protocol tools and both arms scored 8/8: 214.4 versus
152.6 seconds. No fresh grep arm or cross-language speed claim.

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
