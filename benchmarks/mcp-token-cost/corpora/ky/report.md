# mcp-token-cost

Question: who calls this symbol, and what do those callers look like.

- Command: `go run ./benchmarks/mcp-token-cost --server /tmp/kv-main --dir benchmarks/mcp-token-cost/corpora/ky --repository ky`
- Commit: `de7a74e024b0963e902bb4ae086bba471d399615`
- Generated: `2026-11-24T10:22:25Z`
- Server: `0.10.3`, MCP protocol `2026-07-28`
- Environment: `darwin/arm64`, `go1.26.11`
- Generation: `000002`, 1134 symbols, 31 files, 2698 edges, schema `5`, resolver `0.10.3`
- Corpus: `ky` at `/private/tmp/kivgraph-corpora/ky/src`, indexed commit `35418888`, tree unchanged
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
| `mergeHeaders` | rare_name | 2 | 117 | 295 | 295 | 0.40x | 0.40x |
| `deepMerge` | rare_name | 2 | 166 | 296 | 296 | 0.56x | 0.56x |
| `isObject` | rare_name | 9 | 297 | 361 | 361 | 0.82x | 0.82x |
| `retry` | shared_name | 4 | 3925 | 336 | 336 | 11.68x | 11.68x |
| `Hooks` | common_name | 9 | 431 | 372 | 372 | 1.16x | 1.16x |
| `TimeoutError` | common_name | 17 | 2304 | 569 | 569 | 4.05x | 4.05x |
| **total** | | | **7240** | **2229** | **2229** | **3.25x** | **3.25x** |

## The whole session

The same answer plus the bodies the agent then opens. Both sides pay those identically, so this factor is bounded no matter how lean the answer gets.

| symbol | native | today | projected | today | projected |
| --- | ---: | ---: | ---: | ---: | ---: |
| `mergeHeaders` | 12504 | 12682 | 10084 | 0.99x | 1.24x |
| `deepMerge` | 818 | 948 | 724 | 0.86x | 1.13x |
| `isObject` | 9871 | 9935 | 7885 | 0.99x | 1.25x |
| `retry` | 16784 | 13195 | 10480 | 1.27x | 1.60x |
| `Hooks` | 5059 | 5000 | 3811 | 1.01x | 1.33x |
| `TimeoutError` | 12528 | 10793 | 7884 | 1.16x | 1.59x |
| **total** | **57564** | **52553** | **40868** | **1.10x** | **1.41x** |

Of the session totals, 50324 tokens are source bodies. That is the floor: an answer that cost nothing at all would still land at **1.53x**, so no amount of payload work on this question class can go past it. What separates `today` from the served arm is no longer the answer -- a compact row carries its own line range, so the per-reference `get_symbol` cost 0 calls -- but who hands the bodies over: serving them with `get_source` instead of the host's range read is worth 11685 tokens net.

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
