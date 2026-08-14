# mcp-token-cost

Question: who calls this symbol, and what do those callers look like.

- Command: `go run ./benchmarks/mcp-token-cost --server /tmp/lgnow`
- Commit: `a0372d4e566a8633f6222ffed7fc12ebefb74b2e`
- Generated: `2026-08-14T12:07:13Z`
- Server: `0.5.1`, MCP protocol `2025-06-18`
- Environment: `darwin/arm64`, `go1.26.4`
- Generation: `000024`, 10501 symbols, 301 files, 38546 edges, schema `2`, resolver `0.5.0`
- Corpus: `ladygraph` at `/Users/adria/Documents/programacion/projects/ladygraph`, indexed commit `a0372d4e`, tree unchanged
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
| `MergeAll` | rare_name | 2 | 481 | 446 | 446 | 1.08x | 1.08x |
| `CanonicalColumns` | rare_name | 3 | 1156 | 518 | 518 | 2.23x | 2.23x |
| `DiscoverGo` | rare_name | 3 | 903 | 520 | 520 | 1.74x | 1.74x |
| `BuildPlan` | shared_name | 3 | 2411 | 555 | 555 | 4.34x | 4.34x |
| `NewServer` | common_name | 0 | 2308 | 350 | 350 | 6.59x | 6.59x |
| `Publish` | common_name | 4 | 3480 | 631 | 631 | 5.52x | 5.52x |
| **total** | | | **10739** | **3020** | **3020** | **3.56x** | **3.56x** |

## The whole session

The same answer plus the bodies the agent then opens. Both sides pay those identically, so this factor is bounded no matter how lean the answer gets.

| symbol | native | today | projected | today | projected |
| --- | ---: | ---: | ---: | ---: | ---: |
| `MergeAll` | 1438 | 1403 | 1176 | 1.02x | 1.22x |
| `CanonicalColumns` | 4598 | 3960 | 3216 | 1.16x | 1.43x |
| `DiscoverGo` | 3561 | 3178 | 2515 | 1.12x | 1.42x |
| `BuildPlan` | 6952 | 5096 | 4142 | 1.36x | 1.68x |
| `NewServer` | 2401 | 443 | 408 | 5.42x | 5.88x |
| `Publish` | 6194 | 3345 | 2729 | 1.85x | 2.27x |
| **total** | **25144** | **17425** | **14186** | **1.44x** | **1.77x** |

Of the session totals, 14405 tokens are source bodies. That is the floor: an answer that cost nothing at all would still land at **2.40x**, so no amount of payload work on this question class can go past it. Removing the per-reference `get_symbol` round trip, and paying for `end_line` on every row instead, is worth 3239 tokens net.

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
