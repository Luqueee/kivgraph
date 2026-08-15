package rebuild

import "runtime/debug"

// ReturnBuildMemory hands what a snapshot build borrowed back to the operating
// system.
//
// BuildSnapshot materialises the graph three times before it can publish it
// once: the rows the canonical scan decodes, the snapshot's own row set, and
// the snapshot. Measured on a 189 MB graph -- 102 385 symbols, 259 556 edges --
// the Go heap peaks near 500 MB to produce 173 MB of live snapshot, and the
// runtime then keeps that arena: the heap goal alone is twice the live heap at
// the default GOGC, and a process that only answers queries never allocates
// enough afterwards to reuse the rest. Left alone, a server parks at three
// times what it holds, for as long as it runs.
//
// This is the one moment the transient is provably dead: the snapshot has been
// published, or was dropped because another publisher won, and either way
// nothing can reach its inputs. A server has nothing else to do until the next
// request, so the collection and scavenge cost it nothing a caller can observe.
//
// It is deliberately not a memory limit. GOMEMLIMIT would also bound the
// indexer, whose peak is the work itself, and bounding that trades memory for
// a GC that never stops running.
func ReturnBuildMemory() {
	debug.FreeOSMemory()
}
