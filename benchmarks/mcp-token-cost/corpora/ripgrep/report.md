# mcp-token-cost

Question: who calls this symbol, and what do those callers look like.

- Command: `go run ./benchmarks/mcp-token-cost --server /tmp/kv-main --dir benchmarks/mcp-token-cost/corpora/ripgrep --repository ripgrep`
- Commit: `de7a74e024b0963e902bb4ae086bba471d399615`
- Generated: `2026-11-24T10:21:27Z`
- Server: `0.10.3`, MCP protocol `2026-07-28`
- Environment: `darwin/arm64`, `go1.26.11`
- Generation: `000001`, 4978 symbols, 98 files, 25844 edges, schema `5`, resolver `0.10.3`
- Corpus: `ripgrep` at `/private/tmp/kivgraph-corpora/ripgrep/src`, indexed commit `4649aa97`, tree unchanged
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
| `Replacer` | rare_name | 8 | 175 | 390 | 390 | 0.45x | 0.45x |
| `LineIter` | rare_name | 12 | 292 | 531 | 531 | 0.55x | 0.55x |
| `HiArgs` | rare_name | 13 | 379 | 485 | 485 | 0.78x | 0.78x |
| `Config` | shared_name | 6 | 915 | 429 | 429 | 2.13x | 2.13x |
| `Searcher` | common_name | 50 | 1876 | 1717 | 1717 | 1.09x | 1.09x |
| `SinkMatch` | common_name | 21 | 555 | 876 | 876 | 0.63x | 0.63x |
| **total** | | | **4192** | **4428** | **4428** | **0.95x** | **0.95x** |

## The whole session

The same answer plus the bodies the agent then opens. Both sides pay those identically, so this factor is bounded no matter how lean the answer gets.

| symbol | native | today | projected | today | projected |
| --- | ---: | ---: | ---: | ---: | ---: |
| `Replacer` | 23286 | 23501 | 40394 | 0.99x | 0.58x |
| `LineIter` | 25820 | 26059 | 21420 | 0.99x | 1.21x |
| `HiArgs` | 25333 | 25439 | 23416 | 1.00x | 1.08x |
| `Config` | 11688 | 11202 | 9049 | 1.04x | 1.29x |
| `Searcher` | 78478 | 78319 | 64040 | 1.00x | 1.23x |
| `SinkMatch` | 64131 | 64452 | 58511 | 1.00x | 1.10x |
| **total** | **228736** | **228972** | **216830** | **1.00x** | **1.05x** |

Of the session totals, 224544 tokens are source bodies. That is the floor: an answer that cost nothing at all would still land at **1.02x**, so no amount of payload work on this question class can go past it. What separates `today` from the served arm is no longer the answer -- a compact row carries its own line range, so the per-reference `get_symbol` cost 0 calls -- but who hands the bodies over: serving them with `get_source` instead of the host's range read is worth 12142 tokens net.

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
