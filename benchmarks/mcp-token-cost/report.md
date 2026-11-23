# mcp-token-cost

Question: who calls this symbol, and what do those callers look like.

- Command: `go run ./benchmarks/mcp-token-cost --server /tmp/kv-measure`
- Commit: `b86dd900649359810d8872a74c312f094776feba`
- Generated: `2026-11-23T10:07:10Z`
- Server: `0.10.1`, MCP protocol `2025-06-18`
- Environment: `darwin/arm64`, `go1.26.11`
- Generation: `000001`, 25613 symbols, 946 files, 137136 edges, schema `5`, resolver `0.10.1`
- Corpus: `kivgraph` at `/Users/gtrave/Documents/kivgraph`, indexed commit `b86dd900`, tree unchanged
- Tokenizer: `cl100k_base`, question set version `2`

## Resident surface

15 tools, 13 annotated read-only.

| what | tokens |
| --- | ---: |
| route lines, Oh My Pi | 279 |
| descriptions, Oh My Pi | 533 |
| **resident total, Oh My Pi** | **812** |
| full schemas, deferred by both hosts | 5174 |

Neither host holds the schemas: Oh My Pi mounts each MCP tool as a device whose documentation is read on demand, and Claude Code defers them behind its tool search. The resident number is what a surface regression is measured against.

## Answering the question

What each side spends to say who calls the symbol. This is the part a graph server owns.

| symbol | class | refs | native | today | projected | today | projected |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `MergeAll` | rare_name | 5 | 429 | 1534 | 1534 | 0.28x | 0.28x |
| `CanonicalColumns` | rare_name | 7 | 442 | 1516 | 1516 | 0.29x | 0.29x |
| `DiscoverGo` | rare_name | 26 | 617 | 1948 | 1948 | 0.32x | 0.32x |
| `BuildPlan` | shared_name | 22 | 855 | 1890 | 1890 | 0.45x | 0.45x |
| `NewServer` | common_name | 4 | 1422 | 2365 | 2365 | 0.60x | 0.60x |
| `Publish` | common_name | 0 | 1226 | 1385 | 1385 | 0.89x | 0.89x |
| **total** | | | **4991** | **10638** | **10638** | **0.47x** | **0.47x** |

## The whole session

The same answer plus the bodies the agent then opens. Both sides pay those identically, so this factor is bounded no matter how lean the answer gets.

| symbol | native | today | projected | today | projected |
| --- | ---: | ---: | ---: | ---: | ---: |
| `MergeAll` | 2939 | 4044 | 3486 | 0.73x | 0.84x |
| `CanonicalColumns` | 5016 | 6090 | 5046 | 0.82x | 0.99x |
| `DiscoverGo` | 15738 | 17069 | 14223 | 0.92x | 1.11x |
| `BuildPlan` | 16536 | 17571 | 14407 | 0.94x | 1.15x |
| `NewServer` | 3014 | 3957 | 3621 | 0.76x | 0.83x |
| `Publish` | 1628 | 1787 | 1688 | 0.91x | 0.96x |
| **total** | **44871** | **50518** | **42471** | **0.89x** | **1.06x** |

Of the session totals, 39880 tokens are source bodies. That is the floor: an answer that cost nothing at all would still land at **1.50x**, so no amount of payload work on this question class can go past it. What separates `today` from the served arm is no longer the answer -- a compact row carries its own line range, so the per-reference `get_symbol` cost 0 calls -- but who hands the bodies over: serving them with `get_source` instead of the host's range read is worth 8047 tokens net.

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
