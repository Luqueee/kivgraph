---
title: "Kivgraph FAQ: Code Graphs, MCP and Workspaces"
description: Answers about Kivgraph, cross-repository code graphs, MCP clients, token-efficient context and architectural relationships.
---

## What is Kivgraph?

Kivgraph is a local **cross-repository code intelligence MCP server** for AI coding agents. It indexes Go, TypeScript, Rust, Python and Dart repositories into a canonical semantic code graph and serves that graph to a client over stdio.

## Which languages are supported, and are they equal?

Go, TypeScript, Rust, Python and Dart — and no, they are not equal. Go, TypeScript and Rust edges are type-checked; Dart edges are resolved by Dart Analysis Server; Python uses exact semantic facts when a configured analyzer provides them and `CANDIDATE` facts in its bundled AST fallback. The response carries that distinction, so a Python candidate is never presented as a proven call. `--languages` accepts ten tokens: the five names plus the aliases `javascript`, `ts`, `js`, `rs` and `py`.

## Does Kivgraph replace `grep`?

No, and the benchmark names where it loses. On the current 29-question run `grep` costs fewer tokens on five questions — `A1_go_absent`, `A2_ts_absent`, `I1_go_depth2`, `A3_rs_absent` and `T1_go_trivial` — with both arms at recall 1.00; looking up a Go constructor that occurs twice in the whole corpus cost Kivgraph 123 tokens against `grep`'s 65. Kivgraph earns its cost on common names, transitive impact, cross-repository consumers and on proving an absence, which is where the run's widest margins are. See the [comparison](/comparison/).

## Does Kivgraph save tokens?

On the current run it does: 35,961 tokens over 36 tool calls against 267,980 tokens over 101 calls for `grep` plus reading, at precision 1.00 on both sides. That is 7.45x on totals, but the median per-question ratio is 5.95x — the total is pulled up by a few wide questions, the widest at 86.8x. Because the spread is that large, Kivgraph publishes the whole run rather than one headline percentage; see [token-efficient code understanding](/token-efficient-code-understanding/).

## Does my code leave my machine?

No. The indexer, the canonical store, the published snapshot and the MCP server all run locally, and the server speaks stdio to a client on the same machine. One caveat deserves to be stated plainly: `kivgraph ui` is a local HTTP listener that binds `0.0.0.0:7777` by default and carries **no authentication**, so anything that can reach that port reads your repository paths, file paths, symbol names and signatures. Bind it to loopback with `--addr 127.0.0.1:7777`, or set `web.address`, unless you intend it to be reachable — see [limits](/limits/) and the [viewer guide](/guides/viewer/).

## Does Kivgraph require an LLM or an API key?

No. Indexing is done by language analyzers and a query is a lookup in a published snapshot, so there is no model call anywhere in the pipeline and no key in the configuration. The only language model involved is whichever agent sits on the other end of the MCP connection, and even that is optional: `kivgraph index`, `kivgraph doctor` and `kivgraph ui` work with no client attached.

## How is it different from a vector database or embedding search?

An embedding index answers "what looks similar to this"; Kivgraph answers "what is provably connected to this". Its edges come from language analyzers or are explicitly marked `CANDIDATE` or `UNRESOLVED` — never from name matching or from a similarity score above a threshold. The practical difference is that an empty answer is evidence here and is not there: a nearest-neighbour search always returns neighbours, while a type-checked reference query returning nothing means nobody calls the symbol.

## How does cross-repository analysis work?

Each repository is registered with an explicit name and path, and that identity travels inside every fact the graph stores, so a consumer in another repository is never confused with a local caller. [`find_cross_repo_consumers`](/docs/tools/find-cross-repo-consumers/) reports symbol uses from outside the declaring repository and keeps package-level dependencies in a separate count, because depending on a package does not prove using a symbol. On the benchmark question about a shared `StatusCode` enum it returned all three external consumers in one call for 530 tokens, reporting 22 package-level rows separately; `grep` spent 12,200 tokens over seven calls and missed one consumer, which never spells the symbol. The benchmark corpus is private, so the repository, file and symbol names in these answers are substituted; the counts and the token figures are the measured ones.

## What happens when Kivgraph finds no references?

An empty list is a claim, not a failure — but it is only as strong as the evidence behind it, so read the confidence and completeness the response reports before treating it as proof of absence. Facts the analyzers could not attribute are not discarded: they are published as `UNRESOLVED` carrying their reason, repository and language, which is what separates "nobody calls this" from "the index could not load that part". A bare name that several declarations share is refused rather than guessed: asking for `withBackoff` in a corpus that declares it seven times returns `AMBIGUOUS_SYMBOL` with all seven candidates, for 129 tokens — a figure measured against the real identifiers, which the substituted name here stands in for.

## How much does indexing cost, and how often must I re-index?

A rebuild is always a full pass over every registered repository; there is no incremental mode. In the cold-index measurement of the five-way comparison, a 37-repository corpus took 37.6 s, produced 1,423 MB of on-disk state and published 96,482 symbols, and required the Go module cache per indexed module and `cargo` per Rust workspace. Re-index when the code you are asking about has moved: [`graph_status`](/docs/tools/graph-status/) reports per-repository freshness against the commit each repository was indexed at, so a stale answer is visible rather than silent.

## What happens if I query before publishing a generation?

There is no graph-query surface. A server with no published generation
registers the three indexing controls and puts the rebuild path in its MCP
`instructions`, because publishing graph tools that all answer
`INDEX_NOT_READY` would teach the agent that the tools do not work. It does not
exit either: a client launches the process itself, so exiting reads as a crash.
Run `kivgraph index --full`, or let the agent call `start_index_project` and
poll `get_index_status`; after either route publishes the first generation,
restart the client's MCP connection before querying the graph. See the
[Quickstart](/quickstart/).

## Which MCP clients does Kivgraph support?

`kivgraph mcp install` has five targets: `claude-code`, `claude-desktop`, `codex`, `opencode` and `oh-my-pi`. It takes `--target`, `--scope user|project` (default `user`), `--dry-run` and `--force`. Claude Desktop is the exception twice over — it is user-scope only, and it is the one target that installs no local skill. See the [client registration guide](/mcp/clients/), [Claude Code integration](/mcp/claude-code/), [Codex integration](/mcp/codex/) and [Oh My Pi integration](/mcp/oh-my-pi/).

## Is Kivgraph a multi-repository code graph?

Yes. Kivgraph registers repositories explicitly, preserves repository identity and can report supported symbol and dependency relationships across repository boundaries. See [cross-repository code graphs](/cross-repository-code-graph/).

## Is Kivgraph an architecture graph for microservices?

Not primarily. Kivgraph focuses on semantic code relationships: symbols, declarations, references, calls, packages and analyzer-backed dependencies. It does not claim automatic discovery of every HTTP, gRPC, Kafka or database runtime relationship.

## Is the project called Kivgraph or KiroGraph?

The project name is **Kivgraph**. It is a distinct project and is not KiroGraph.
