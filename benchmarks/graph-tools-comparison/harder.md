# The hard set: nine questions the seven never asked

The seven questions in `report.md` reached `7/7` for Kivgraph, and that is the
point at which they stop being useful: a set you answer completely cannot tell
you where you are wrong. This set exists to find that out, and it did -- three
of its nine were failures, two of them total.

**All three are closed**, by ADR 0054 for the two dynamic-dispatch ones and ADR
0055 for the cross-package type. The set now reads `9/9` at `1.00`/`1.00`, which
means it has stopped being able to tell us where we are wrong: the next useful
contribution to this file is a question that fails.

Raw numbers in `results-hard.json`, every Kivgraph answer in `raw-hard/`, and
the run is `--set hard`. No verdict is emitted: this measures six tools on one
corpus in one state of it.

## Provenance

|fact|value|
|---|---|
|date|2026-08-21|
|commit|`6c7a789`|
|corpus|`/path/to/workspace`, 37 git repositories|
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
|`O3_rs_outline`|a `.rs` file with 5-15 top-level declarations, first path alphabetically|`rs-svc-a/src/audio/range.rs`, 39 candidates|

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

Truth: `services/go-svc-a/internal/application/routers/bots_router.go`.

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

Truth: `services/go-svc-a/internal/application/handlers/guilds_handler.go`.

The call site names the **interface**, so `go/types` resolves it there and not
to the implementation the question is about. The interface declaration is not an
answer: it declares, it does not call.

### `H3_ts_type` -- `ApiRuntimeState`

Declared at `libraries/library-shared/src/types/gateway-registry.ts:51`.

|file|what it is|
|---|---|
|`library-shared/src/types/gateway-registry.ts`|the declaration, and a use at `60` -- declaring file|
|`library-shared/src/redis/cache/gateway/registry/go-svc-d-cache.ts`|imports it and annotates six positions -- **an answer**|
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

Declared at `services/rs-svc-b/src/state/memory.rs:84`, the single `impl
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

Re-measured on `2026-08-22` with `kivgraph 0.5.0` (commit `c1bde5d`). The
previously published table is superseded, and two independent things moved --
they are separated below rather than folded into one number.

|question|kivgraph|graphify|graft|codebase-memory|code-review-graph|`grep`|
|---|---|---|---|---|---|---|
|`H1_go_method`|**`1.00`/`1.00`**|`0.10`/`1.00`|`0.00`/`0.00`|`0.00`/`0.00`|`0.00`/`0.00`|`1.00`/`1.00`|
|`H2_go_iface`|**`1.00`/`1.00`**|`0.00`/`0.00`|`0.00`/`0.00`|`0.00`/`0.00`|`1.00`/`1.00`|`1.00`/`1.00`|
|`H3_ts_type`|`0.50`/`1.00`|`0.50`/`0.50`|`0.00`/`0.00`|`0.00`/`0.00`|`0.00`/`0.00`|`1.00`/`1.00`|
|`H4_ts_alias`|**`1.00`/`1.00`**|`0.00`/`0.00`|`1.00`/`1.00`|`1.00`/`1.00`|`0.00`/`0.00`|`1.00`/`1.00`|
|`H5_rs_trait`|**`1.00`/`1.00`**|`0.00`/`0.00`|`1.00`/`0.33`|`1.00`/`0.67`|`0.00`/`0.00`|`1.00`/`1.00`|
|`A1_go_absent`|**`1.00`/`1.00`**|`0.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|
|`A2_ts_absent`|**`1.00`/`1.00`**|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|
|`A3_rs_absent`|**`1.00`/`1.00`**|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|`1.00`/`1.00`|
|`O3_rs_outline`|**`1.00`/`1.00`**|`0.00`/`0.00`|`0.00`/`0.00`|`0.60`/`0.86`|`0.00`/`0.00`|`1.00`/`1.00`|
|**exact**|**`8/9`**|`2/9`|`4/9`|`4/9`|`4/9`|`9/9`|
|**tokens**|**`1,835`**|`2,663`|`5,562`|`16,601`|`3,511`|`42,698`|
|**calls**|`12`|`11`|`9`|`28`|`11`|`17`|

`grep` plus reading now gets `9/9` for `42,698` tokens against `1,835` --
**`23.3x`**, where this table used to publish `41x`. That drop is a correction to
the harness, not a change in the product, and it is the larger of the two moves.

### The `grep` column was overcharged

Five of these nine subjects have exactly **one** declaration, and the native arm
was charged for reading that declaring file whole. The only justification this
harness ever gave for the charge is in the `Declarations` field's own doc: the
reads are "the minimum a reader needs to tell homonyms apart". With one
declaration there is nothing to separate, and `familyLocate` already made
exactly that call -- it charges for the search alone because "the `grep` line
itself shows `func withRetry(`".

So the read is now charged only when a references question has more than one
declaration site. The five that stop paying it:

Measured both ways on the same corpus and the same generation, changing only the
rule. The old rule reproduces this table's published `74,783` exactly, which is
what makes the pair comparable:

|question|before|after|
|---|---|---|
|`H3_ts_type`|`736`|`455`|
|`H4_ts_alias`|`533`|`138`|
|`A1_go_absent`|`558`|`29`|
|`A2_ts_absent`|`6,365`|`42`|
|`A3_rs_absent`|`24,625`|`68`|
|**sum**|**`32,817`**|**`732`**|

`A3_rs_absent` was billed `24,625` tokens, and every one of them was a whole-file
read of a Rust source that could not change the answer: the question is whether
anything references `build_all_image_sizes`, the search returns the declaration
and nothing else, and that **is** the answer. Two thirds of what this set
charged `grep` came from five questions where a reader would never have opened a
file.

The rule is scoped to references on purpose. `familyImpact` shares that code
path and its reads are not about homonyms at all: a transitive answer is built
by finding the declaration that **encloses** each hit and searching again, so the
file must be opened regardless. An unscoped version of this fix undercharged a
depth-3 question to `94` tokens -- for an answer no `grep` produces -- which is
the same dishonesty pointing the other way.

### `H3` found a defect, and closing it restored the row

`H3_ts_type` came back `0.50`/`1.00` on the first re-measurement, with two extra
rows -- both `.d.ts` files under `libraries/library-shared/dist`. Part of that is
corpus: the published run states "TypeScript packages NOT built (no `dist/`)",
that `dist/` exists now, and `generated_files` accepts only `include`, so a built
package is indexed. But the shape of the two rows was ours:

```
gateway:../../libraries/library-shared/dist/.../go-svc-d-cache.d.ts
sdk-module-ts:../../libraries/library-shared/dist/.../go-svc-d-cache.d.ts
```

One file, attributed to **two repositories, neither of which contains it**, by a
path escaping its own repository with `../..`. A row is meant to be addressable
-- repository, repository-relative path, qualified name, range -- and neither of
those can be fed back to any tool. Same class as
`fix(indexing): refuse Go facts whose file is outside the repository`, which
closed it for Go; TypeScript still did it. That was `LUQUE-2011`, and it is
closed: a consumer now retires a fact whose file leaves its own tree, counts it,
and retains the gap with its reason instead of publishing an unaddressable row.

Re-attributing the file to `library-shared` was considered and rejected on
evidence. A `File` belongs to a `Package`, and a consumer payload never names the
provider's package -- so the row would be package-less. Worse, `MergeAll` keeps
the first row for a key and drops later ones **without comparing**, so a
package-less row could silently beat a complete one: the shape of `LUQUE-2002`.

What matters for anyone reading this table: the workspace relation does not
depend on those rows and did not move. An import binding's target identity is
built from the **provider's** own repository and package, byte identical to the
key the provider assigns its own declaration, so `find_cross_repo_consumers` on
`ApiRuntimeState` still answers `25` consumers at `EXACT_TYPECHECKED`. What was
retired are uses whose *source* file is the provider's build output -- facts about
the provider, for the provider's own pass to report.

Measured after the fix: `H3` is `1.00`/`1.00` at `280` tokens, and **no row in any
of the 29 questions has a path that escapes its repository**. Across the corpus
the retirement drops `193` files, `542` symbols and `1,275` edges, and adds `173`
retained gaps.

The three earlier fixes cost `272` tokens across the set and `H1` remains the
evidence they stayed where they were aimed: three receiver types share one method
name, it is the question a careless bridge pollutes first, and it holds at
`1.00`/`1.00`.

**Nine questions on one corpus is a small set.** Being right on all of them is
not evidence of being right in general; it is the absence of a known miss on
nine questions, three of which were written blind. The rules are above and every
subject is mechanical, so anyone can add a tenth -- and that is worth more than
this table.

## What it found

### 1. A call through an interface reached nothing, in both languages

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

`code-review-graph` answered `H2` exactly: this was not a boundary of the
problem, it was ours.

The cause was not in the query. `IMPLEMENTS` related a type to an interface,
which is what `types.Implements` and `impl Trait for Type` decide, and **nothing
related the concrete method to the interface method** -- the only fact that says
which declaration a call arrives at. Rust had the pairing already but published
it as `OVERRIDES`, which is a different relation from the one Go reports under
that kind: there, a method that *hides* a promoted one, which a call never
reaches. One canonical kind cannot mean both.

ADR 0054 closed both. Go pairs each interface method with the method
`types.LookupFieldOrMethod` selects, which is the checker's own answer and not a
name match; Rust reports its member pairing as `IMPLEMENTS`, where it belongs.
`find_references` crosses the pairing **only where the subject is the one
implementation**, which is where a call through the interface can reach nothing
else. With two it refuses: a call reaches one of them and naming both would
trade a false absence for a false presence. Every bridged row carries `via` and
the page declares `dispatch_through`, so nothing is passed off as a direct call:

```json
"edge_kind":"CALLS_DIRECT","provenance":"GO_AST_CALL",
"via":"NotifierSubRepo.FindPendingGuilds",
"dispatch_through":["NotifierSubRepo.FindPendingGuilds"]
```

`H5` closed at `1.00`/`1.00` with `via: state::StateStore::delete_player`, and
its precision is the part worth reading: `routes_players.rs` also declares a
free `delete_player` of its own, and the callers of *that* stayed out.

### 2. A cross-package type reached through a local barrel was invisible (`H3`)

`P=1.00`, `R=0.50`: the answer held seven `TYPE_USES` rows, the whole page
hoisted `repository: library-shared`, and there was not one cross-repository
type use in it. `RegistryGrpcManager.ts` in `gateway` annotates four positions
with `ApiRuntimeState` and was absent.

The diagnosis was not the one the symptom suggested. It is not about types --
the same file's `import type { RedisAdapter } from "@private/shared"` resolves
fine. It is about the **path**: the manager imports the type from
`"../../types/registry.js"`, a local barrel whose entire content is
`export type { ApiRuntimeState, ... } from "@private/shared"`. Bindings were made
only for imports that named a package, so this file bound nothing, and its four
uses were dropped whole for having no target. Measured in the payload:

|name|imports|references|
|---|---|---|
|`RedisAdapter`, package import|`2`|`3`|
|`ApiRuntimeState`, relative import|`0`|**`0`**|

ADR 0055 closed it. A relative import that lands on a declaration some local
barrel already resolved inherits that barrel's provider, so the consumer carries
its own `IMPORTS_SYMBOL` and is one hop away -- the same shape `R1`'s five
consumers have. The match is on the identity of the declaration the checker
resolved to, never on a name.

It also cost a precision regression on the way, which the set caught: widening
the walk to every file of the program pulled in the `.d.ts` of every dependency,
and binding those named the installed copy of `@private/shared` as a consumer of a
type `@private/shared` declares -- in two repositories at once. `H3` read
`0.50`/`1.00` for one pass. Files under a `node_modules` are the provider's own
source and are not walked.

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

- **Impact.** Two questions were added after this set, in `impact.md`, which
  takes the family from one to three. TypeScript impact is still absent and now
  has a measured reason rather than an estimate: of 675 candidates, 670 use the
  symbol outside any top-level exported function, and the heuristic that reads an
  enclosing declaration in Go is unsound in TypeScript.
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
