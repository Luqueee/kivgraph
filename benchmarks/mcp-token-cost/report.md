# mcp-token-cost

Question: who calls this symbol, and what do those callers look like.

- Command: `go run ./benchmarks/mcp-token-cost --server /tmp/kv-final`
- Commit: `1a7d02665751f3bf5dfd453c8bb8361880e0533d`
- Generated: `2026-08-26T17:36:26Z`
- Server: `0.8.0`, MCP protocol `2025-06-18`
- Environment: `darwin/arm64`, `go1.26.4`
- Generation: `000206`, 32441 symbols, 759 files, 111044 edges, schema `4`, resolver `0.7.0`
- Corpus: `kivgraph` at `/Users/adria/Documents/programacion/projects/kivgraph`, indexed commit `1a7d0266`, tree unchanged
- Tokenizer: `cl100k_base`, question set version `2`

## Resident surface

12 tools, 11 annotated read-only.

| what | tokens |
| --- | ---: |
| route lines, Oh My Pi | 220 |
| descriptions, Oh My Pi | 496 |
| **resident total, Oh My Pi** | **716** |
| full schemas, deferred by both hosts | 2104 |

Neither host holds the schemas: Oh My Pi mounts each MCP tool as a device whose documentation is read on demand, and Claude Code defers them behind its tool search. The resident number is what a surface regression is measured against.

## Answering the question

What each side spends to say who calls the symbol. This is the part a graph server owns.

| symbol | class | refs | native | today | projected | today | projected |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `MergeAll` | rare_name | 3 | 1082 | 1278 | 1278 | 0.85x | 0.85x |
| `CanonicalColumns` | rare_name | 3 | 1151 | 1270 | 1270 | 0.91x | 0.91x |
| `DiscoverGo` | rare_name | 4 | 1088 | 1314 | 1314 | 0.83x | 0.83x |
| `BuildPlan` | shared_name | 3 | 2539 | 1320 | 1320 | 1.92x | 1.92x |
| `NewServer` | common_name | 0 | 2269 | 1349 | 1349 | 1.68x | 1.68x |
| `Publish` | common_name | 3 | 4686 | 730 | 730 | 6.42x | 6.42x |
| **total** | | | **12815** | **7261** | **7261** | **1.76x** | **1.76x** |

## The whole session

The same answer plus the bodies the agent then opens. Both sides pay those identically, so this factor is bounded no matter how lean the answer gets.

| symbol | native | today | projected | today | projected |
| --- | ---: | ---: | ---: | ---: | ---: |
| `MergeAll` | 2708 | 2904 | 2492 | 0.93x | 1.09x |
| `CanonicalColumns` | 4627 | 4746 | 3972 | 0.97x | 1.16x |
| `DiscoverGo` | 4515 | 4741 | 3891 | 0.95x | 1.16x |
| `BuildPlan` | 7831 | 6612 | 5525 | 1.18x | 1.42x |
| `NewServer` | 2366 | 1446 | 1408 | 1.64x | 1.68x |
| `Publish` | 6601 | 2645 | 2181 | 2.50x | 3.03x |
| **total** | **28648** | **23094** | **19469** | **1.24x** | **1.47x** |

Of the session totals, 15833 tokens are source bodies. That is the floor: an answer that cost nothing at all would still land at **2.50x**, so no amount of payload work on this question class can go past it. What separates `today` from the served arm is no longer the answer -- a compact row carries its own line range, so the per-reference `get_symbol` cost 0 calls -- but who hands the bodies over: serving them with `get_source` instead of the host's range read is worth 3625 tokens net.

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
- The generation holds 3 repositories but no question in the set has consumers outside its own, so the cross-repository case is unmeasured.
