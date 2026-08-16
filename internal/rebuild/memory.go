package rebuild

import (
	"runtime/debug"

	"github.com/Luqueee/kivgraph/internal/nativeheap"
)

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
// It is deliberately not a memory limit, and the reason is now narrower than it
// was. Since ADR 0042 the pass runs as a child process, so what a limit set
// here still reaches is only what the child inherits -- the environment, since
// `subprocess.go` never sets `command.Env` -- and `GOMEMLIMIT` there would
// bound the indexer, whose peak is the work itself.
//
// It would also buy almost nothing, which is the part measurement had to
// settle. On a 189 MB graph the Go heap parks 2.4-4 MB above the live snapshot
// after every single build: there is no Go-side gap for a ceiling to close.
// What made a long-lived server park at three times what it holds was the
// allocator underneath the engine, and that is what the second call below
// returns. See ADR 0044.
func ReturnBuildMemory() {
	debug.FreeOSMemory()
	// The Go arena is the smaller half. Most of a build's bytes are the
	// engine's, allocated through libc, and measured on a 189 MB graph the Go
	// side parks 2.4 MB above the live snapshot while resident size climbs
	// 80 MB per build. That memory is free as far as the allocator is
	// concerned and still resident as far as the kernel is concerned, which
	// is the difference this closes.
	nativeheap.Return()
}
