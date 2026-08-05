# TypeScript engine benchmark: native TypeScript 7 vs TypeScript 5 Compiler API

- Command: `node run.mjs --sizes 250,1000,4000 --iterations 50 --warmup 5 --seed 42`
- Commit: `f312471ee156d6d068c918f45380408290d435e3-dirty`
- Generated at: `2026-08-05T10:34:23.392Z`
- Platform: `linux/x64`, `node 25.9.0`
- Engines: `typescript@7.0.2` (native) and `typescript@5.9.3` (JavaScript Compiler API)

## Project load and full type check

| Modules | Cold load TS7 ms | Cold load TS5 ms | Load speedup | Full check TS7 ms | Full check TS5 ms | Check speedup | RSS TS7 MB | RSS TS5 MB | RSS ratio |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 250 | 17.9 | 87.9 | 4.9x | 23.7 | 114.7 | 4.85x | 148.8 | 335.8 | 2.26x |
| 1000 | 19.8 | 87.5 | 4.43x | 91.3 | 223.9 | 2.45x | 194.7 | 423.9 | 2.18x |
| 4000 | 19.2 | 88.0 | 4.58x | 350.4 | 506.8 | 1.45x | 380.5 | 552.2 | 1.45x |

## Warm semantic operations, p50 microseconds

### 250 modules

| Operation | TS7 p50 us | TS5 p50 us | TS7 speedup | TS7 p95 us | TS5 p95 us | TS7 returned | TS5 returned |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| symbol_at_position | 69.2 | 10.2 | 0.15x | 124.1 | 74.2 | 1 | 1 |
| symbol_batch_per_file | 278.9 | 407.1 | 1.46x | 428.2 | 592.1 | 50 | 50 |
| alias_declarations | 136.0 | 3.4 | 0.03x | 203.7 | 10.1 | 1 | 1 |
| references_in_file | 147.9 | 3534.8 | 23.9x | 184.3 | 4377.1 | 2 | 2 |
| exports_of_barrel | 800.7 | 18.0 | 0.02x | 1059.5 | 79.2 | 501 | 501 |
| incremental_edit_and_resolve | 235.5 | 3809.5 | 16.18x | 261.9 | 5075.2 | 1 | 1 |

### 1000 modules

| Operation | TS7 p50 us | TS5 p50 us | TS7 speedup | TS7 p95 us | TS5 p95 us | TS7 returned | TS5 returned |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| symbol_at_position | 70.4 | 9.0 | 0.13x | 111.1 | 63.3 | 1 | 1 |
| symbol_batch_per_file | 285.6 | 383.2 | 1.34x | 411.2 | 503.7 | 50 | 50 |
| alias_declarations | 139.8 | 3.3 | 0.02x | 197.3 | 5.7 | 1 | 1 |
| references_in_file | 160.3 | 16330.0 | 101.89x | 181.7 | 19503.6 | 2 | 2 |
| exports_of_barrel | 3212.8 | 61.7 | 0.02x | 4818.4 | 359.2 | 2001 | 2001 |
| incremental_edit_and_resolve | 299.7 | 7782.4 | 25.97x | 370.4 | 10736.4 | 1 | 1 |

### 4000 modules

| Operation | TS7 p50 us | TS5 p50 us | TS7 speedup | TS7 p95 us | TS5 p95 us | TS7 returned | TS5 returned |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| symbol_at_position | 83.3 | 15.8 | 0.19x | 131.6 | 62.5 | 1 | 1 |
| symbol_batch_per_file | 282.0 | 380.9 | 1.35x | 375.0 | 485.0 | 50 | 50 |
| alias_declarations | 129.2 | 3.2 | 0.02x | 165.9 | 6.7 | 1 | 1 |
| references_in_file | 156.9 | 74618.4 | 475.52x | 179.6 | 77787.8 | 2 | 2 |
| exports_of_barrel | 15065.3 | 125.7 | 0.01x | 16318.6 | 231.4 | 8001 | 8001 |
| incremental_edit_and_resolve | 636.6 | 19086.1 | 29.98x | 1252.4 | 22971.7 | 1 | 1 |

## Method and asymmetries

- Both engines analyse the same generated corpus: one hub module imported by every module, seeded cross-module imports, and a barrel that re-exports everything.
- `cold_load` measures engine construction plus the first query that forces the program to exist. For TypeScript 7 it includes spawning the native `tsgo --api` server.
- `symbol_at_position` resolves one symbol per call and exposes the fixed round-trip cost of the native engine; `symbol_batch_per_file` resolves 50 positions of one file in a single call, which the TypeScript 7 checker accepts natively and the TypeScript 5 checker has to emulate with a loop.
- `references_in_file` is asymmetric by construction: TypeScript 7 exposes a file-scoped `getReferencesToSymbolInFile`, while the TypeScript 5 Compiler API only offers a project-wide search that is then filtered to the same file. Returned counts are compared to confirm both produce identical results.
- `rss_bytes` sums the resident memory of the harness process and every descendant, so the native server counts against TypeScript 7.
- The programs differ slightly in default library files because each compiler version ships its own `lib.*.d.ts` set; the corpus source files are identical.
- Every measurement runs in its own Node process, so no engine benefits from the other's warm heap.

## Consequences for Luque

- The native engine does not make every operation faster; it changes the cost model. Project-scale work becomes much cheaper, while each individual request pays a fixed inter-process round trip of roughly 70 to 140 microseconds.
- Operations that Luque runs once per project or per file change (load, full check, incremental re-resolve, file-scoped references) are decisively faster on the native engine, and the gap widens with corpus size.
- Operations that Luque would run once per symbol are slower on the native engine unless they are batched. The batched form is faster than the JavaScript Compiler API, so the worker protocol must be batch-oriented per file rather than chatty per symbol.
- Transferring large symbol sets is the worst case: reading every export of a barrel costs one round trip per symbol payload and degrades with the export count. Bulk extraction must avoid materialising whole module export sets when a narrower query exists.
- Resident memory is consistently lower for the native engine even after counting the spawned server process.
