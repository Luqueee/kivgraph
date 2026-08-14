# mcp-token-cost

Question: who consumes this symbol from another repository.

- Command: `go run ./benchmarks/mcp-token-cost --server /tmp/lg1906 --config /private/tmp/lg-crossrepo-config/config.json --dir benchmarks/mcp-token-cost/cross-repo --repository shared-library`
- Commit: `a0372d4e566a8633f6222ffed7fc12ebefb74b2e`
- Generated: `2026-08-13T18:47:43Z`
- Server: `0.5.1`, MCP protocol `2025-06-18`
- Environment: `darwin/arm64`, `go1.26.4`
- Generation: `000003`, 8 symbols, 3 files, 12 edges, schema `2`, resolver `0.5.1`
- Corpus: `shared-library` at `/private/tmp/lg-crossrepo/shared-library`, indexed commit `9fe29054`, tree unchanged
- Tokenizer: `cl100k_base`, question set version `1`

## Resident surface

11 tools, 10 annotated read-only.

| what | tokens |
| --- | ---: |
| route lines, Oh My Pi | 201 |
| descriptions, Oh My Pi | 444 |
| **resident total, Oh My Pi** | **645** |
| full schemas, deferred by both hosts | 1723 |

Neither host holds the schemas: Oh My Pi mounts each MCP tool as a device whose documentation is read on demand, and Claude Code defers them behind its tool search. The resident number is what a surface regression is measured against.

## Answering the question

What each side spends to say who calls the symbol. This is the part a graph server owns.

| symbol | class | refs | native | today | projected | today | projected |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `Compute` | cross_repository | 2 | 211 | 399 | 399 | 0.53x | 0.53x |
| **total** | | | **211** | **399** | **399** | **0.53x** | **0.53x** |

## The whole session

The same answer plus the bodies the agent then opens. Both sides pay those identically, so this factor is bounded no matter how lean the answer gets.

| symbol | native | today | projected | today | projected |
| --- | ---: | ---: | ---: | ---: | ---: |
| `Compute` | 434 | 622 | 582 | 0.70x | 0.75x |
| **total** | **434** | **622** | **582** | **0.70x** | **0.75x** |

Of the session totals, 223 tokens are source bodies. That is the floor: an answer that cost nothing at all would still land at **4.17x**, so no amount of payload work on this question class can go past it. Removing the per-reference `get_symbol` round trip, and paying for `end_line` on every row instead, is worth 40 tokens net.

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
- The cross-repository question is measured on "Compute", and its native column is a floor rather than a ceiling: a grep finds the name but cannot tell whether the hit is the same symbol, and says nothing about a consumer that depends on the provider package without using the symbol.
