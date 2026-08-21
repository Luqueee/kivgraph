# Impact: the question this tool is sold on, measured twice

`report.md` and `harder.md` between them ask sixteen questions and **one** of
them is about impact -- "what breaks if I change this" -- which is the question
the front page leads with. Twelve are references and three are outlines. This set
closes that gap by two, and it is the first entry in the list of what the other
two sets declared they did not measure.

Raw numbers in `results-impact.json`, the Kivgraph answers in `raw-impact/`, and
the run is `--set impact`. No verdict: two questions on one corpus.

## Provenance

|fact|value|
|---|---|
|date|2026-08-21|
|commit|`5295901`|
|corpus|`/Users/adria/Documents/programacion/projects/kena`, 37 git repositories|
|tokenizer|`tiktoken` `o200k_base`|
|versions|kivgraph `0.3.6`, graft `0.10.1`, code-review-graph `2.3.7`, graphify `0.8.31`, codebase-memory-mcp `0.8.1`|

## The selection rule, and why it only produced Go

Fixed in advance, over the same corpus and exclusions the other sets use:

1. A Go top-level declaration named exactly once in the corpus, exported, at
   least five characters.
2. Its bare name occurs in **1 to 3** files besides the one declaring it, so the
   second hop can be read by hand.
3. The enclosing declaration of every one of those occurrences -- in Go, the last
   `func` at column zero above the line -- must itself be named exactly once, or
   the second hop has no knowable truth.
4. The union of the first and second hop is between 2 and 6 files.
5. The **alphabetically first** survivor.

The rule left 129 candidates and selected `GuildsHandler.AutomationScheduledGuildIDs`.

**TypeScript produced nothing, and the reason is worth recording.** Of 675
candidates that passed the caller window, **670** had the use sitting outside any
top-level exported function -- in a class method, an arrow function, or a
top-level statement. That is the ordinary shape of this corpus, and it is the
same shape the module-symbol work in ADR 0052 was about. Worse, the heuristic
that reads an enclosing declaration in Go is **unsound** in TypeScript: taking
the last `export function` above a line attributes a use inside a later class to
whatever function happened to be declared before it. The four candidates that
survived did so with a second hop resolved by name matching -- one of them
expanded through `clamped`, which is a local variable in five unrelated packages.

So a TypeScript impact truth is not derivable this way, and inventing one would
have been worse than the gap. It stays open, now with a measured reason rather
than an estimate.

## The two questions

Both ask about the same subject and differ only in the hop count, so what they
isolate is the depth itself.

`GuildsHandler.AutomationScheduledGuildIDs` is declared at
`internal/application/handlers/guilds_handler.go:301`.

**Hop 1.** `routers/guilds_router.go:42` --
`g.Get("/automation-scheduled", h.AutomationScheduledGuildIDs)`. A method value
handed to a route, never a call, inside `RegisterGuilds`.

**Hop 2.** Every mention of `RegisterGuilds` outside its own file:

|file|line|what it is|
|---|---|---|
|`cmd/server/main.go`|`315`|`routers.RegisterGuilds(app, guildsHandler)` -- a call|
|`handlers/guilds_mock_test.go`|`27`|a call|
|`routers/routers_test.go`|`164`, `196`|two calls|
|`routers/module_notifier_router.go`|`22`|**a comment**: "vive en `guilds_router.go` (`RegisterGuilds`)"|

The comment is why this truth was read rather than computed: the mechanical
expansion claimed five files and one of them is prose.

Truth at two hops, four files:

- `services/api-db-go/cmd/server/main.go`
- `services/api-db-go/internal/application/handlers/guilds_mock_test.go`
- `services/api-db-go/internal/application/routers/guilds_router.go`
- `services/api-db-go/internal/application/routers/routers_test.go`

**Truth at three hops is the same four**, and that is the question. The frontier
at hop two is `main`, `TestRegisterGuilds_RoutesMounted`,
`TestRegisterGuilds_LiteralsNotShadowed` and `newGuildsMockApp`: an entry point,
two test functions the framework invokes, and a helper used only in its own file.
Nothing calls any of them -- checked, including `main`, which is declared eight
times in this repository and named in fifty places without a single call. An
answer that grows at hop three is inventing reach.

## The result

|question|kivgraph|graphify|graft|codebase-memory|code-review-graph|`grep`|
|---|---|---|---|---|---|---|
|`I2_go_depth2`|**`1.00`/`1.00`**|`0.40`/`1.00`|`0.00`/`0.00`|`0.00`/`0.00`|`0.03`/`0.50`|`1.00`/`1.00`|
|`I3_go_depth3`|**`1.00`/`1.00`**|`0.40`/`1.00`|`0.00`/`0.00`|`0.00`/`0.00`|`0.03`/`0.50`|`1.00`/`1.00`|
|**aggregate**|**`1.00`/`1.00`, `2/2`**|`0.40`/`1.00`, `0/2`|`0.00`/`0.00`, `0/2`|`0.00`/`0.00`, `0/2`|`0.03`/`0.50`, `0/2`|`1.00`/`1.00`, `2/2`|
|**tokens**|`5,137`|**`3,154`**|`396`|`1,304`|`452,864`|`7,152`|

Two things in that table are not what the reference questions say.

**The cheapness mostly goes away.** `5,137` tokens against `grep`'s `7,152` is
`1.4x`, not the `41x` the hard set shows. An impact answer is a set of files and
a reader has to be given it; there is far less to save than on a question whose
honest answer is four rows. Reporting the reference ratio as the tool's ratio
would be the dishonest summary.

**Precision is what separates the five here, and it separates them by a lot.**
`code-review-graph` claims `67` false files out of `69`, at `452,864` tokens --
`88x` what we spend and `63x` what reading costs. Its note says why, and it is
not a defect: its `impact` takes changed *files*, so naming a declaration hands
it a whole file's blast radius. graphify answers `--depth 2` with six false
files out of ten. graft and codebase-memory answer nothing at all: graft has no
impact command of this shape and codebase-memory's `trace_path` returned no
callers.

Neither answer grew from two hops to three, for us or for them. Ours is exact at
both.

## Limitations

- **Two questions, one subject, one language.** The depth dimension is isolated,
  which is what makes the pair worth having; nothing here says anything about
  impact in TypeScript or Rust, and the rule above says why the TypeScript half
  is missing rather than late.
- **A frontier of entry points is one shape of impact, not the shape.** A
  subject whose second hop is ordinary library code would grow at hop three, and
  no question here has that shape. It is the obvious next one to write.
- **`field` and `variable` are excluded by default** from our answer, which the
  payload declares in `kinds_default_excluded`; the note repeats it rather than
  absorbing it into the score.

## Reproduce

```bash
go run ./benchmarks/graph-tools-comparison --set impact \
  --dir /tmp/bench-impact --state-root /tmp/5way-impact \
  --kivgraph-home /tmp/kivhome-impact
```
