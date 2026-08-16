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
// It is deliberately not a memory limit, and since ADR 0042 the reason is
// narrower than it was: the pass runs as a child process, so what a limit here
// would still reach is only what the child inherits. That is the environment --
// `subprocess.go` never sets `command.Env` -- so `GOMEMLIMIT` in the
// environment would bound the indexer too, whose peak is the work itself, and
// bounding that trades memory for a GC that never stops running. A limit set
// programmatically in `serve` would not travel to the child; it would also have
// to clear this build's own peak, which is measured above at roughly three
// times the live snapshot, so it can only reclaim the distance between that
// peak and where a long-lived server parks. The peak itself is the lever.
func ReturnBuildMemory() {
	debug.FreeOSMemory()
}
