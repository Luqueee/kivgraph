# mcp-token-cost

Question: who calls this symbol, and what do those callers look like.

- Command: `go run ./benchmarks/mcp-token-cost --server /tmp/kv-main --dir benchmarks/mcp-token-cost/corpora/cobra --repository cobra`
- Commit: `de7a74e024b0963e902bb4ae086bba471d399615`
- Generated: `2026-11-24T10:20:49Z`
- Server: `0.10.3`, MCP protocol `2026-07-28`
- Environment: `darwin/arm64`, `go1.26.11`
- Generation: `000001`, 781 symbols, 35 files, 6113 edges, schema `5`, resolver `0.10.3`
- Corpus: `cobra` at `/private/tmp/kivgraph-corpora/cobra/src`, indexed commit `88b30ab8`, tree unchanged
- Tokenizer: `cl100k_base`, question set version `1`

## Resident surface

15 tools, 13 annotated read-only.

| what | tokens |
| --- | ---: |
| route lines, Oh My Pi | 279 |
| descriptions, Oh My Pi | 533 |
| **resident total, Oh My Pi** | **812** |
| full schemas, deferred by both hosts | 5289 |

Neither host holds the schemas: Oh My Pi mounts each MCP tool as a device whose documentation is read on demand, and Claude Code defers them behind its tool search. The resident number is what a surface regression is measured against.

## Answering the question

What each side spends to say who calls the symbol. This is the part a graph server owns.

| symbol | class | refs | native | today | projected | today | projected |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `ExactArgs` | rare_name | 17 | 511 | 382 | 382 | 1.34x | 1.34x |
| `CheckErr` | rare_name | 7 | 231 | 268 | 268 | 0.86x | 0.86x |
| `Traverse` | rare_name | 8 | 290 | 309 | 309 | 0.94x | 0.94x |
| `MarkFlagRequired` | shared_name | 10 | 375 | 443 | 443 | 0.85x | 0.85x |
| `AddCommand` | common_name | 50 | 924 | 923 | 923 | 1.00x | 1.00x |
| `Execute` | common_name | 14 | 597 | 371 | 371 | 1.61x | 1.61x |
| **total** | | | **2928** | **2696** | **2696** | **1.09x** | **1.09x** |

## The whole session

The same answer plus the bodies the agent then opens. Both sides pay those identically, so this factor is bounded no matter how lean the answer gets.

| symbol | native | today | projected | today | projected |
| --- | ---: | ---: | ---: | ---: | ---: |
| `ExactArgs` | 4667 | 4538 | 3195 | 1.03x | 1.46x |
| `CheckErr` | 2452 | 2489 | 1694 | 0.99x | 1.45x |
| `Traverse` | 7074 | 7093 | 5397 | 1.00x | 1.31x |
| `MarkFlagRequired` | 12087 | 12155 | 9465 | 0.99x | 1.28x |
| `AddCommand` | 29661 | 29660 | 21703 | 1.00x | 1.37x |
| `Execute` | 17739 | 17513 | 12973 | 1.01x | 1.37x |
| **total** | **73680** | **73448** | **54427** | **1.00x** | **1.35x** |

Of the session totals, 70752 tokens are source bodies. That is the floor: an answer that cost nothing at all would still land at **1.49x**, so no amount of payload work on this question class can go past it. What separates `today` from the served arm is no longer the answer -- a compact row carries its own line range, so the per-reference `get_symbol` cost 0 calls -- but who hands the bodies over: serving them with `get_source` instead of the host's range read is worth 19021 tokens net.

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
