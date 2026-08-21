# The hard set: nine questions the seven never asked

The seven questions in `report.md` reached `7/7` for Kivgraph, and that is the
point at which they stop being useful: a set you answer completely cannot tell
you where you are wrong. This set exists to find that out, and it did -- three
of its nine were failures, two of them total.

One is now closed. `H2_go_iface` found that a Go method reached only through the
interface that declares it answered that **nothing** referenced it, in the same
words a real absence gets, and that `code-review-graph` answered it exactly. ADR
0054 fixed it and the row below is the measurement. Two remain: its Rust twin
`H5_rs_trait`, open by declared scope, and `H3_ts_type`.

Raw numbers in `results-hard.json`, every Kivgraph answer in `raw-hard/`, and
the run is `--set hard`. No verdict is emitted: this measures six tools on one
corpus in one state of it.

## Provenance

|fact|value|
|---|---|
|date|2026-08-21|
|commit|`8ba145a`, measured with the ADR 0054 bridge applied|
|corpus|`/Users/adria/Documents/programacion/projects/kena`, 37 git repositories|
|corpus files under the rule|`610` Go, `3.247` TypeScript, `85` Rust|
|tokenizer|`tiktoken` `o200k_base`|
|versions|kivgraph `0.3.6`, graft `0.10.1`, code-review-graph `2.3.7`, graphify `0.8.31`, codebase-memory-mcp `0.8.1`|

## The selection rules, fixed in advance

The set was chosen **before any tool was run against it**, by rule, over the
corpus excluding `node_modules`, `dist`, `build`, `target`, `.next`,
`graphify-out` and any `vendor` tree. Occurrence windows count distinct files
over `.go`, `.ts` and `.rs`, which is what the rule in `remeasure.md` counted.
Every rule takes the **alphabetically first** survivor, so no subject is a
choice.

|id|rule|selected|
|---|---|---|
|`H1_go_method`|**`1.00`/`1.00`**|`0.10`/`1.00`|`0.00`/`0.00`|`0.00`/`0.00`|`0.00`/`0.00`|`1.00`/`1.00`|
|`H2_go_iface`|**`1.00`/`1.00`**|`0.00`/`0.00`|`0.00`/`0.00`|`0.00`/`0.00`|`1.00`/`1.00`|`1.00`/`1.00`|
|`H3_ts_type`|**`1.00`/`0.50`**|`0.50`/`0.50`|`0.00`/`0.00`|`0.00`/`0.00`|`0.00`/`0.00`|`1.00`/`1.00`|
|`H4_ts_alias`|`1.00`/`1.00`|`0.25`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`0.00`/`0.00`|`1.00`/`1.00`|
|`H5_rs_trait`|**`0.00`/`0.00`**|`0.50`/`0.33`|`1.00`/`0.33`|`1.00`/`0.67`|`1.00`/`0.33`|`1.00`/`1.00`|
|`A1_go_absent`|`1.00`/`1.00`|`0.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|
|`A2_ts_absent`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|
|`A3_rs_absent`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|
|`O3_rs_outline`|`1.00`/`1.00`|`0.75`/`0.86`|`0.00`/`0.00`|`0.60`/`0.86`|`0.75`/`0.86`|`1.00`/`1.00`|
|**aggregate**|**`0.89`/`0.83`, `7/9`**|`0.46`/`0.74`, `2/9`|`0.56`/`0.48`, `4/9`|`0.62`/`0.61`, `4/9`|`0.64`/`0.58`, `4/9`|`1.00`/`1.00`, `9/9`|
|**tokens**|**`1,598`**|`4,425`|`5,562`|`16,449`|`8,669`|`74,783`|
|**calls**|`12`|`13`|`9`|`28`|`12`|`22`|

`7/9` against `7/7` on the easier set, and `grep` plus reading answers all nine
again. Kivgraph is the cheapest of the five at `1,598` tokens, `47x` under the
reading baseline, which is worth exactly as much as its accuracy on the two it
still gets wrong. The bridge that closed `H2` cost `43` tokens across the set
and moved nothing else: `H1`, the homonym question, is the one a careless bridge
would have polluted, and it holds at `1.00`/`1.00`.

The published seven were re-run on the same binary to check the same thing from
the other side, and stay at `7/7`, `1.00`/`1.00`, in `results-0.3.6.json`.

## What it found

### 1. A call through an interface reached nothing -- Go closed, Rust open

Both `H2` and `H5` answered `0.00`/`0.00`, with a sentence that is **false**:

```
"nothing references this symbol in the published graph; the edges are
 type-checked, so this is an absence rather than a miss"
```

There is a caller. It calls through `Notifier` in Go and through
`Arc<dyn StateStore>` in Rust, and in both cases the implementation asked about
is the only one there is. This is the worst failure shape available -- a
confident absence -- and it contradicted the claim the front page makes, that an
empty reference list means nobody calls it. That claim held for a static call
and did not hold for a dynamic one. `code-review-graph` answered `H2` exactly:
it was not a boundary of the problem, it was ours.

The cause was not in the query. `IMPLEMENTS` related a type to an interface,
which is what `types.Implements` decides, and **nothing related the concrete
method to the interface method** -- the only fact that says which declaration a
call arrives at.

ADR 0054 closed the Go half. The loader now pairs each interface method with
the method `types.LookupFieldOrMethod` selects, which is the checker's own
answer and not a name match, and `find_references` crosses that pairing **only
where the subject is the one implementation**. With two, a call reaches one of
them and naming both would trade a false absence for a false presence, so it is
refused. Every bridged row carries `via` and the page declares
`dispatch_through`, so nothing is passed off as a direct call:

```json
"edge_kind":"CALLS_DIRECT","provenance":"GO_AST_CALL",
"via":"NotifierSubRepo.FindPendingGuilds",
"dispatch_through":["NotifierSubRepo.FindPendingGuilds"]
```

`H5` is unchanged at `0.00`/`0.00` and open by declared scope: Rust's
`IMPLEMENTS` comes from `impl Trait for Type` and is still type-to-trait, so
there is no pairing to cross. The query contract is written and tested; what is
missing is the Rust loader doing what the Go one now does.

### 2. A cross-package type-only import is invisible (`H3`)

`P=1.00`, `R=0.50`. The answer holds seven `TYPE_USES` rows and the whole page
hoists `repository: library-shared`: there is not one cross-repository type use
in it. `RegistryGrpcManager.ts` in `gateway` does
`import type { ApiRuntimeState } from "@kena/shared"` and annotates four
positions with it, and we do not have it. A function crossing the same boundary
resolves -- that is `R1` in the other set -- so this is the type-level half of
a bridge whose value half already works.

### 3. Two failures that were the benchmark's, not the tool's

Both were found by this run and both are fixed, because publishing them as
Kivgraph failures would have been wrong:

- **The outline comparison read the spelling, not the answer.** It dropped any
  label containing `.` as nested, which is right for Go and TypeScript, where a
  top-level name is already bare, and scored every Rust answer at zero, because
  a Rust label carries its module path: `audio::range::parse_range` never
  equalled `parse_range`. The scope prefix is now derived -- the label that is a
  `::` prefix of all the others is the file's own module -- and stripped, after
  which depth one is top level. `O3` went from `0.00`/`0.00` to `1.00`/`1.00`
  with no change to the answer.
- **The outline arm read one page.** It ignored `next_cursor`, so recall was
  capped at the page size for any file bigger than one page.

And one Kivgraph bug they uncovered on the way: `get_file_outline` reported
`total: 24` for a file holding `12` declarations, with `truncated` false and no
cursor, because the member-kind filter ran per row **after** the count. Every
enum variant and struct field was counted and then dropped. The gate now runs on
the walk that builds the page, so `total`, the rows and `coverage` describe one
set. There was no test for `include_members` at all, which is why it shipped.

## What this set does not measure

- **Impact.** The family has one question, `I1_go_depth2`, in the other set.
  Establishing transitive truth at depth two by hand across TypeScript was
  judged more likely to produce a wrong truth than a useful question, so it is
  absent rather than guessed.
- **`find_cross_repo_consumers`.** No question asks it.
- **Python and Dart.** Not in this corpus.
- **Two of these questions share a convention with a shipped decision.** `H3`
  and `H4` treat a pure re-export barrel as not a use, which is the same
  convention ADR 0053 implemented. They therefore cannot be independent evidence
  for that decision, and are not offered as such.

## Reproduce

```bash
go run ./benchmarks/graph-tools-comparison --set hard \
  --dir /tmp/bench-hard --state-root /tmp/5way-hard \
  --kivgraph-home /tmp/kivhome-hard
```

Every arm indexes the repositories the set names. Running `--skip-indexing`
against a state root built for the other set gives `code-review-graph` and
`graphify` no graph for the new repositories and scores them zero for the
harness's reason rather than their own; that happened on the first pass here and
is why the numbers above come from a full run.
