# mcp-token-cost

Question: who calls this symbol, and what do those callers look like.

- Command: `go run ./benchmarks/mcp-token-cost --server /tmp/lgfinal`
- Commit: `e2162d570b8849d3b14bb28288b3e6b343dcd4b8`
- Generated: `2026-08-14T13:22:51Z`
- Server: `0.5.1`, MCP protocol `2025-06-18`
- Environment: `darwin/arm64`, `go1.26.4`
- Generation: `000026`, 14424 symbols, 363 files, 55289 edges, schema `2`, resolver `0.5.1`
- Corpus: `ladygraph` at `/Users/adria/Documents/programacion/projects/ladygraph`, indexed commit `e2162d57`, tree unchanged
- Tokenizer: `cl100k_base`, question set version `2`

## Resident surface

11 tools, 10 annotated read-only.

| what | tokens |
| --- | ---: |
| route lines, Oh My Pi | 201 |
| descriptions, Oh My Pi | 444 |
| **resident total, Oh My Pi** | **645** |
| full schemas, deferred by both hosts | 1755 |

Neither host holds the schemas: Oh My Pi mounts each MCP tool as a device whose documentation is read on demand, and Claude Code defers them behind its tool search. The resident number is what a surface regression is measured against.

## Answering the question

What each side spends to say who calls the symbol. This is the part a graph server owns.

| symbol | class | refs | native | today | projected | today | projected |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `MergeAll` | rare_name | 3 | 621 | 516 | 516 | 1.20x | 1.20x |
| `CanonicalColumns` | rare_name | 3 | 1156 | 517 | 517 | 2.24x | 2.24x |
| `DiscoverGo` | rare_name | 3 | 903 | 519 | 519 | 1.74x | 1.74x |
| `BuildPlan` | shared_name | 3 | 2411 | 554 | 554 | 4.35x | 4.35x |
| `NewServer` | common_name | 0 | 2095 | 349 | 349 | 6.00x | 6.00x |
| `Publish` | common_name | 4 | 3480 | 630 | 630 | 5.52x | 5.52x |
| **total** | | | **10666** | **3085** | **3085** | **3.46x** | **3.46x** |

## The whole session

The same answer plus the bodies the agent then opens. Both sides pay those identically, so this factor is bounded no matter how lean the answer gets.

| symbol | native | today | projected | today | projected |
| --- | ---: | ---: | ---: | ---: | ---: |
| `MergeAll` | 2207 | 2102 | 1725 | 1.05x | 1.28x |
| `CanonicalColumns` | 4598 | 3959 | 3215 | 1.16x | 1.43x |
| `DiscoverGo` | 3561 | 3177 | 2514 | 1.12x | 1.42x |
| `BuildPlan` | 7020 | 5163 | 4202 | 1.36x | 1.67x |
| `NewServer` | 2188 | 442 | 407 | 4.95x | 5.38x |
| `Publish` | 6194 | 3344 | 2728 | 1.85x | 2.27x |
| **total** | **25768** | **18187** | **14791** | **1.42x** | **1.74x** |

Of the session totals, 15102 tokens are source bodies. That is the floor: an answer that cost nothing at all would still land at **2.34x**, so no amount of payload work on this question class can go past it. Removing the per-reference `get_symbol` round trip, and paying for `end_line` on every row instead, is worth 3396 tokens net.

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
- The generation holds 2 repositories but no question in the set has consumers outside its own, so the cross-repository case is unmeasured.
