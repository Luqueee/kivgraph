# mcp-token-cost

Question: who calls this symbol, and what do those callers look like.

- Command: `go run ./benchmarks/mcp-token-cost --server /tmp/kivgraph-tagged`
- Commit: `f8a952d6112707e2d5a09882771e55ab6118729b`
- Generated: `2026-08-18T17:19:16Z`
- Server: `0.2.1`, MCP protocol `2025-06-18`
- Environment: `darwin/arm64`, `go1.26.4`
- Generation: `000001`, 13222 symbols, 305 files, 50103 edges, schema `2`, resolver `0.2.1`
- Corpus: `kivgraph` at `/Users/adria/Documents/programacion/projects/kivgraph`, indexed commit `f8a952d6`, tree unchanged
- Tokenizer: `cl100k_base`, question set version `2`

## Resident surface

11 tools, 10 annotated read-only.

| what | tokens |
| --- | ---: |
| route lines, Oh My Pi | 201 |
| descriptions, Oh My Pi | 444 |
| **resident total, Oh My Pi** | **645** |
| full schemas, deferred by both hosts | 1810 |

Neither host holds the schemas: Oh My Pi mounts each MCP tool as a device whose documentation is read on demand, and Claude Code defers them behind its tool search. The resident number is what a surface regression is measured against.

## Answering the question

What each side spends to say who calls the symbol. This is the part a graph server owns.

| symbol | class | refs | native | today | projected | today | projected |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `MergeAll` | rare_name | 3 | 750 | 228 | 228 | 3.29x | 3.29x |
| `CanonicalColumns` | rare_name | 2 | 1284 | 195 | 195 | 6.58x | 6.58x |
| `DiscoverGo` | rare_name | 3 | 1020 | 246 | 246 | 4.15x | 4.15x |
| `BuildPlan` | shared_name | 3 | 2547 | 271 | 271 | 9.40x | 9.40x |
| `NewServer` | common_name | 0 | 2092 | 175 | 175 | 11.95x | 11.95x |
| `Publish` | common_name | 4 | 3718 | 379 | 379 | 9.81x | 9.81x |
| **total** | | | **11411** | **1494** | **1494** | **7.64x** | **7.64x** |

## The whole session

The same answer plus the bodies the agent then opens. Both sides pay those identically, so this factor is bounded no matter how lean the answer gets.

| symbol | native | today | projected | today | projected |
| --- | ---: | ---: | ---: | ---: | ---: |
| `MergeAll` | 2350 | 1828 | 1441 | 1.29x | 1.63x |
| `CanonicalColumns` | 4271 | 3182 | 2529 | 1.34x | 1.69x |
| `DiscoverGo` | 3695 | 2921 | 2245 | 1.26x | 1.65x |
| `BuildPlan` | 7174 | 4898 | 3923 | 1.46x | 1.83x |
| `NewServer` | 2189 | 272 | 234 | 8.05x | 9.35x |
| `Publish` | 6835 | 3496 | 2818 | 1.96x | 2.43x |
| **total** | **26514** | **16597** | **13190** | **1.60x** | **2.01x** |

Of the session totals, 15103 tokens are source bodies. That is the floor: an answer that cost nothing at all would still land at **2.41x**, so no amount of payload work on this question class can go past it. What separates `today` from the served arm is no longer the answer -- a compact row carries its own line range, so the per-reference `get_symbol` cost 0 calls -- but who hands the bodies over: serving them with `get_source` instead of the host's range read is worth 3407 tokens net.

Publishing only one of the two factors is how this field arrives at its headline numbers. The answer factor flatters us and the session factor flatters the alternative; both are here.

## Arms

- **native**: the host's own captured answer for the same question, plus the bodies the agent then opens. Both captures are verbatim tool results committed under `native/`, never a reimplementation.
- **today**: the MCP calls a session needs against the published generation, plus the same host reads.
- **served**: the same calls plus one `get_source` that returns every body the answer named, measured against the real tool.

The two body figures are not the same number, and that is the point. A host range read prepends a snapshot header and a line number to every line, which measures 38 % on top of the bytes. Charging the raw slice to every arm, as this harness first did, discounted the alternative and made a source-serving tool look worthless.

## Limitations

- Both arms pay the same body cost, computed from the graph's exact line ranges. The native arm would in practice read wider, because grep reports a match line and not where the declaration ends, so the comparison is conservative in favour of the native path.
- The arms that open files are billed from captured host reads, so the line prefixes they carry are counted; the served arm is billed from the bytes alone. A missing capture fails the run instead of falling back to the raw slice.
- The served arm is measured against the real get_source, not projected.
- Adoption is not measured here. Whether an agent calls these tools at all is an observation over real sessions and belongs to LUQUE-1904.
- Neither money nor latency is measured. Prompt caching makes cost depend on the order the arms run in, so a token count is the only figure that survives a reordering.
- The generation holds a single repository, so no question exercises cross-repository resolution, which is where an exact graph has no native competitor at all.
