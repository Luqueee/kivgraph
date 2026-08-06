# MCP runtime profile analysis

**Task:** LUQUE-1306  
**Code commit:** `20a198d9b0f007ff37357f1857bf17a675cedd58`  
**Profile run:** 32 concurrent clients, 10,000 measured calls, 100 warm-up calls per operation and client, seed 42, 100,000 symbols, 1,000,000 edges.  
**Environment:** Linux amd64, Go 1.24.4, AMD Ryzen 7 9700X 8-Core Processor.

## Reproduction

```text
go build -o /tmp/luque-mcp-client-profile ./benchmarks/mcp-client
/tmp/luque-mcp-client-profile \
  --clients 32 \
  --calls 10000 \
  --warmup 100 \
  --symbols 100000 \
  --edges 1000000 \
  --seed 42 \
  --output /tmp/luque-profile-results \
  --profile-dir benchmarks/mcp-client-32/profiles
```

CPU and runtime trace capture cover the measured concurrent-call phase. Heap,
allocation, mutex and block profiles are written after that phase. Mutex
sampling uses fraction 1 and block sampling uses rate 1. The profiling run is
not a performance gate because CPU and trace instrumentation add overhead.
Its observed control values were p50 0.594801 ms, p95 4.084686 ms, p99
9.283704 ms, 24,102.4 calls/s, 0 errors and no continuous RSS growth.

## Artifacts

| Profile | Size | Analysis command |
| --- | ---: | --- |
| `cpu.pprof` | 28,375 bytes | `go tool pprof -top /tmp/luque-mcp-client-profile cpu.pprof` |
| `heap.pprof` | 24,577 bytes | `go tool pprof -top -sample_index=inuse_space /tmp/luque-mcp-client-profile heap.pprof` |
| `allocs.pprof` | 24,844 bytes | `go tool pprof -top -sample_index=alloc_space /tmp/luque-mcp-client-profile allocs.pprof` |
| `mutex.pprof` | 3,697 bytes | `go tool pprof -top -sample_index=delay /tmp/luque-mcp-client-profile mutex.pprof` |
| `block.pprof` | 5,008 bytes | `go tool pprof -top -sample_index=delay /tmp/luque-mcp-client-profile block.pprof` |
| `trace.out` | 3,476,470 bytes | `go tool trace -pprof=sync trace.out` / `-pprof=sched` |

## Findings

### CPU

The profile contains 5,650 ms of samples over 601.46 ms of wall time
(939.39% aggregate CPU). The dominant hot path is MCP JSON-RPC payload
processing, not graph traversal:

- `encoding/json.stateInString`: 960 ms flat, 16.99%.
- `encoding/json.checkValid`: 420 ms flat, 7.43%.
- `encoding/json.(*decodeState).skip`: 410 ms flat, 7.26%.
- `encoding/json.appendCompact`: 320 ms flat, 5.66%.
- `jsonschema.(*state).validate`: 980 ms cumulative, 17.35%.
- Runtime allocation and write-barrier work is also visible.

This identifies serialization and schema validation as the first CPU-level
bottleneck in this workload.

### Heap

The live heap profile reports 128.91 MB. The largest retained areas are:

- `hotsnapshot.NewGraphSnapshot`: 57.33 MB cumulative, 48.03 MB flat.
- `StringInterner.Intern`: 14.11 MB flat.
- `main.buildCorpus`: 9.00 MB flat.
- `cloneSymbolLists`: 6.74 MB flat.
- `GraphSnapshot.Traverse`: 5.01 MB flat.
- JSON decode/encode and schema validation together account for additional
  retained memory.

The heap profile includes the synthetic snapshot, workload, SDK state and
profiling runtime; it is not an isolated per-request heap measurement.

### Allocations

The cumulative allocation profile reports 6.05 GB and 76,336,687 allocated
objects. The largest allocation sources are:

- `GraphSnapshot.Traverse`: 1.36 GB flat, 22.42%.
- `encoding/json.Marshal`: 0.76 GB flat.
- `encoding/json.(*decodeState).objectInterface`: 0.62 GB flat.
- `jsonschema.(*state).validate`: 0.56 GB flat, 1.13 GB cumulative.
- `json.RawMessage.UnmarshalJSON`: 0.47 GB flat.
- `reflect.copyVal`: 24,379,739 objects, 31.94%.
- `jsonschema.(*state).validate`: 15,246,275 objects flat and 41,527,979
  cumulative.
- `encoding/json.unquote`: 10,081,770 objects.

Allocations therefore point to JSON/schema conversion and reflection before
any index or traversal optimization is attempted.

### Mutex

The mutex delay profile reports 1.91 s aggregate delay:

- `sync.(*Mutex).Unlock`: 1.75 s, 91.78%.
- `runtime._LostContendedRuntimeLock`: 0.16 s, 8.22%.
- The cumulative path is dominated by
  `jsonrpc2.Connection.processResult` -> `Connection.write` -> `mcp.ioConn.Write`.

This is evidence of synchronization pressure in the shared in-memory
JSON-RPC path. It is not a direct measurement of a LadybugDB or graph-store
mutex.

### Block

The block profile reports 51.61 s aggregate goroutine blocking time; this is
summed across goroutines and is not wall-clock latency:

- `runtime.selectgo`: 47.53 s, 92.09%.
- `runtime.chanrecv1`: 1.90 s, 3.67%.
- `sync.(*Mutex).Lock`: 1.76 s, 3.42%.
- `sync.(*WaitGroup).Wait`: 0.41 s.
- JSON-RPC reads account for 22.44 s cumulative through JSON decoding and
  21.98 s through `Connection.readIncoming` / `mcp.ioConn.Read`.

### Trace

The synchronization profile derived from `trace.out` reports 51,714 ms
aggregate delay, with `runtime.selectgo` at 47,517.64 ms (91.89%) and
`mcp.ioConn.Write` at 1,733.90 ms (3.35%). The scheduler profile reports
15,270.81 ms aggregate delay, led by:

- `runtime.chansend1`: 7,941.22 ms, 52.00%.
- `jsonrpc2.Connection.handleAsync`: 3,140.64 ms cumulative, 20.57%.
- `runtime.selectgo`: 2,174.16 ms, 14.24%.
- GC mark/start work is visible but not dominant.

The trace corroborates channel and JSON-RPC coordination as the source of
concurrency pressure.

## Decision and limitations

LUQUE-1306 changes profiling only; it does not optimize production code. The
first actionable category for LUQUE-1307 is **allocations/serialization**:
JSON encoding/decoding, schema validation and reflection dominate CPU and
allocation profiles, while traversal is the largest graph-side allocator.

The run uses `NewInMemoryTransports`, a synthetic corpus and one hardware
configuration. Profiles include setup state and, for CPU/trace, profiler
instrumentation overhead. Aggregate mutex/block/trace delays cannot be
converted directly to per-request latency, and no direct global-contention
counter exists yet. STDIO, sockets, network and a real repository checkout
were not profiled.

**Result:** PASS with the limitations above.  
**Next task:** LUQUE-1307.
