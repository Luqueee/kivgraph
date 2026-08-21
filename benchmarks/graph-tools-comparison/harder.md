# The hard set: nine questions the seven never asked

The seven questions in `report.md` reached `7/7` for Kivgraph, and that is the
point at which they stop being useful: a set you answer completely cannot tell
you where you are wrong. This set exists to find that out, and it did -- three
of its nine are failures, two of them total.

Raw numbers in `results-hard.json`, every Kivgraph answer in `raw-hard/`, and
the run is `--set hard`. No verdict is emitted: this measures six tools on one
corpus in one state of it.

## Provenance

|fact|value|
|---|---|
|date|2026-08-21|
|commit|`6c7a789`|
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
|`H1_go_method`|a Go method name declared on `>= 3` receiver types, absent from every `interface` block, `>= 5` chars, in 4-10 files; first receiver alphabetically|`BotsHandler.GetAll`, 8 candidates|
|`H2_go_iface`|a method name declared inside an `interface` block with **exactly one** concrete implementation, in 4-10 files|`NotifierSubRepository.FindPendingGuilds`, 2 candidates|
|`H3_ts_type`|`export interface Name` declared exactly once, in 4-8 files|`ApiRuntimeState`, 200 candidates|
|`H4_ts_alias`|`export { X as Y } from` where `X` has one declaration and one alias|`CommandManager as SlashCommandManager`, 4 of 16|
|`H5_rs_trait`|a `fn` inside an `impl Trait for Type` block, unique among trait impls, in 4-10 files|`MemoryStateStore::delete_player`, 17 candidates|
|`A1`,`A2`,`A3`|a declaration no **other** file names, one per language|`BenchmarkDeserializeValueDate`, `addMockedSongsToQueue`, `build_all_image_sizes`|
|`O3_rs_outline`|a `.rs` file with 5-15 top-level declarations, first path alphabetically|`api-music-nodo/src/audio/range.rs`, 39 candidates|

### Where the rule had to look wider, and why it mattered

An occurrence window is a filter; an **absence is a truth claim**. Verifying one
over `.go`, `.ts` and `.rs` alone would have been wrong, because this corpus has
`1.159` `.tsx` files the narrow index never opened. Widening the verification to
`.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.json`, `.md`, `.yaml`, `.yml`, `.sql`
and `.toml` **discarded 219 of the 287 TypeScript candidates**, the first pick
among them: `addGiveawayEntry` is referenced from a `.tsx` file. Trusting the
narrow index would have published a failure that was the benchmark's own bug.

Go lost 1 candidate of 1.752 to the wider check and Rust 2 of 21.

## The ground truth

Every occurrence was read and attributed by hand before the run.

### `H1_go_method` -- `BotsHandler.GetAll`

Declared at `internal/application/handlers/bots_handler.go:47`. Six files hold
the name, and **one** is a reference to this declaration.

|file|line|what it is|
|---|---|---|
|`handlers/bots_handler.go`|`43`, `47`|doc comment and the declaration|
|`handlers/command_handler.go`|`30`, `33`|a **homonym** on `CommandHandler`, and its comment|
|`handlers/premium_handler.go`|`43`, `47`|a **homonym** on `PremiumHandler`, and its comment|
|`routers/bots_router.go`|`32`|`g.Get("/", h.GetAll)` where `h` is `*handlers.BotsHandler` -- **the answer**|
|`routers/command_router.go`|`24`|the same line on `*handlers.CommandHandler`|
|`routers/premium_router.go`|`30`|the same line on `*handlers.PremiumHandler`|

Truth: `services/api-db-go/internal/application/routers/bots_router.go`.

Five of the six files are wrong answers, and the reference is a **method value**
passed to a route, never a call. A name-matching tool scores `P=0.17`.

### `H2_go_iface` -- `NotifierSubRepository.FindPendingGuilds`

Declared at `internal/infrastructure/postgres/notifier_sub_repository.go:182`,
the only implementation of the method `pgrepo/repos.go:329` declares on an
interface.

|file|line|what it is|
|---|---|---|
|`postgres/notifier_sub_repository.go`|`171`, `182`|comment and the declaration|
|`handlers/guilds_handler.go`|`349`|`h.Notifier.FindPendingGuilds(...)` -- **the answer**|
|`pgrepo/repos.go`|`320`, `329`|a comment, and the **interface** method declaration|
|`dbnotifier/notifier_sub.go`|`11`|a comment|
|`postgres/migrations/...notifier-subs-schema.up.sql`|`27`|a SQL comment|
|`api-gateway/src/application/controllers/notifier-controller.ts`|`296`|a TypeScript comment -- the name crosses languages|

Truth: `services/api-db-go/internal/application/handlers/guilds_handler.go`.

The call site names the **interface**, so `go/types` resolves it there and not
to the implementation the question is about. The interface declaration is not an
answer: it declares, it does not call.

### `H3_ts_type` -- `ApiRuntimeState`

Declared at `libraries/library-shared/src/types/gateway-registry.ts:51`.

|file|what it is|
|---|---|
|`library-shared/src/types/gateway-registry.ts`|the declaration, and a use at `60` -- declaring file|
|`library-shared/src/redis/cache/gateway/registry/api-registry-cache.ts`|imports it and annotates six positions -- **an answer**|
|`gateway/src/grpc/manager/RegistryGrpcManager.ts`|imports it, annotates four positions, and re-exports it -- **an answer**, in another repository|
|`gateway/src/types/registry.ts`|`export type { ApiRuntimeState, ... }` and nothing else -- a barrel|

Truth: the two that use it.

### `H4_ts_alias` -- `CommandManager`

Declared at `modules/sdk-module-ts/src/sdk/managers/CommandManager.ts:8`.

|file|what it is|
|---|---|
|`src/sdk/client/ModuleActions.ts`|imports it, annotates a field, and calls `new CommandManager(grpc)` -- **the answer**|
|`src/index.ts`|`export { CommandManager as SlashCommandManager }` -- a renamed barrel|
|`src/sdk/managers/index.ts`|`export * from "./CommandManager.js"` -- a barrel|
|`AGENTS.md`|prose|

Truth: `modules/sdk-module-ts/src/sdk/client/ModuleActions.ts`.

### `H5_rs_trait` -- `MemoryStateStore::delete_player`

Declared at `services/kenalink-rs/src/state/memory.rs:84`, the single `impl
StateStore` in the crate. Every call goes through `Arc<dyn StateStore>`.

|file|what it is|
|---|---|
|`src/state/memory.rs`|the declaration -- declaring file|
|`src/api_rest/routes_players.rs`|`state.store.delete_player(&key)` at `436` -- **an answer**. Also declares a **homonym** free `pub async fn delete_player` at `393`, whose own callers are at `1463`, `1641`, `1688`|
|`src/api_ws/mod.rs`|`state.store.delete_player(&fresh.key)` at `420` -- **an answer**|
|`src/main.rs`|the same call at `218` and `345` -- **an answer**|
|`src/state/mod.rs`|the **trait** method declaration at `42`, plus comments|
|`src/api_rest/mod.rs`|`routes_players::delete_player` at `38` -- the homonym|
|`src/audio/songbird_engine.rs`|a comment|
|three `.md` audit files|prose|

Truth: `routes_players.rs`, `api_ws/mod.rs`, `main.rs`.

### The absences

Truth is the empty set for all three: no other file in the corpus names them.

Scoring one required stating a convention the harness only implied. Precision
and recall are both undefined against an empty truth, and leaving them at zero
marked the only correct answer -- claiming nothing -- as a total failure, which
is why the set had no absence question until now. `scoreAgainst` now says it:
claiming nothing against an empty truth is exact, and claiming anything against
it is precision zero with nothing left to miss.

### `O3_rs_outline`

The seven column-0 declarations of `src/audio/range.rs`: `RangeOutcome`,
`build_response`, `file_response`, `insert_header`, `parse_decimal`,
`parse_range`, `tests`. The `impl RangeOutcome` block is not a name the file
declares and is not in the truth; `mod tests` is, and it is `#[cfg(test)]`, so
this question also asks whether a tool reads test code.

## The result

|question|kivgraph|graphify|graft|codebase-memory|code-review-graph|`grep`|
|---|---|---|---|---|---|---|
|`H1_go_method`|**`1.00`/`1.00`**|`0.10`/`1.00`|`0.00`/`0.00`|`0.00`/`0.00`|`0.00`/`0.00`|`1.00`/`1.00`|
|`H2_go_iface`|**`0.00`/`0.00`**|`0.00`/`0.00`|`0.00`/`0.00`|`0.00`/`0.00`|**`1.00`/`1.00`**|`1.00`/`1.00`|
|`H3_ts_type`|**`1.00`/`0.50`**|`0.50`/`0.50`|`0.00`/`0.00`|`0.00`/`0.00`|`0.00`/`0.00`|`1.00`/`1.00`|
|`H4_ts_alias`|`1.00`/`1.00`|`0.25`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`0.00`/`0.00`|`1.00`/`1.00`|
|`H5_rs_trait`|**`0.00`/`0.00`**|`0.50`/`0.33`|`1.00`/`0.33`|`1.00`/`0.67`|`1.00`/`0.33`|`1.00`/`1.00`|
|`A1_go_absent`|`1.00`/`1.00`|`0.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|
|`A2_ts_absent`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|
|`A3_rs_absent`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|
|`O3_rs_outline`|`1.00`/`1.00`|`0.75`/`0.86`|`0.00`/`0.00`|`0.60`/`0.86`|`0.75`/`0.86`|`1.00`/`1.00`|
|**aggregate**|**`0.78`/`0.72`, `6/9`**|`0.46`/`0.74`, `2/9`|`0.56`/`0.48`, `4/9`|`0.62`/`0.61`, `4/9`|`0.64`/`0.58`, `4/9`|`1.00`/`1.00`, `9/9`|
|**tokens**|**`1,555`**|`4,435`|`5,562`|`16,528`|`8,669`|`74,783`|
|**calls**|`12`|`13`|`9`|`28`|`12`|`22`|

`6/9` against `7/7` on the easier set, and `grep` plus reading answers all nine
again. Kivgraph is the cheapest of the five at `1,555` tokens, `48x` under the
reading baseline, which is worth exactly as much as its accuracy on the three it
gets wrong.

## The three failures, and they are ours

### 1. A call through an interface reaches nothing (`H2`, `H5`)

Both are `0.00`/`0.00`, and both answer with a sentence that is **false**:

```
"nothing references this symbol in the published graph; the edges are
 type-checked, so this is an absence rather than a miss"
```

There is a caller. It calls through `Notifier` in Go and through
`Arc<dyn StateStore>` in Rust, and in both cases the implementation asked about
is the only one there is. This is the worst failure shape available -- a
confident absence -- and it contradicts the claim the front page makes, that an
empty reference list means nobody calls it. That claim holds for a static call
and does not hold for a dynamic one.

`code-review-graph` answers `H2` exactly. We lose this one outright.

The fix is a design decision rather than a bug, which is why nothing is changed
here: bridging `IMPLEMENTS` so a call on an interface counts as a call on its
implementations is correct when there is one implementation and destroys
precision when there are twenty, since every implementation would be named as a
caller of a call site that reaches one of them. The measurement says what the
options cost; it does not pick one.

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
