package indexing

import (
	"math"
	"os"
	"runtime/debug"
	"sync"
)

// indexingGCPercent is how far the heap may grow past what is live before a
// collection, for the length of a pass. The Go default of 100 lets it reach
// twice the live heap, and an index holds the syntax and the type information
// of every package it reads: on `prometheus` the live heap is 1.9 GB and the
// heap reached 3.7 GB before the collector ran, which is the memory the pass
// needs and does not use. Fifty costs about a tenth more time there (`25`
// costs three tenths) and takes a quarter of the peak footprint (ADR 0139).
const indexingGCPercent = 50

// cgroupHeadroom is the share of a container's memory limit the Go heap may
// use. The rest is for what Go does not count: the engine's own memory, the go
// command and the analyzers it starts. On `prometheus` in a 3 GB container the
// heap alone was never over its share and the pass was killed all the same
// (ADR 0140).
const cgroupHeadroom = 0.8

var gcPolicy struct {
	mutex         sync.Mutex
	passes        int
	previousGC    int
	loweredGC     bool
	previousLimit int64
	loweredLimit  bool
}

// collectSooner tunes the Go collector for a pass and returns what ends it.
// Passes of one process can overlap, so the first sets it and the last puts it
// back. Two things are set:
//
//   - the GC percent, unless GOGC is set (the operator's decision wins), the
//     collector is off or the percent is already lower;
//   - a soft memory limit, when the process runs under a memory limit of its own
//     and GOMEMLIMIT is not set. Without one the collector lets the heap grow to
//     what the percent allows and the kernel kills the process at the limit; with
//     one it works harder as the heap nears it.
func collectSooner() func() {
	return collectSoonerWithin(cgroupMemoryLimit)
}

func collectSoonerWithin(memoryLimit func() (int64, bool)) func() {
	gcPolicy.mutex.Lock()
	defer gcPolicy.mutex.Unlock()
	if gcPolicy.passes == 0 {
		gcPolicy.loweredGC, gcPolicy.loweredLimit = false, false
		if _, set := os.LookupEnv("GOGC"); !set {
			gcPolicy.previousGC = debug.SetGCPercent(indexingGCPercent)
			gcPolicy.loweredGC = gcPolicy.previousGC > indexingGCPercent
			if !gcPolicy.loweredGC {
				debug.SetGCPercent(gcPolicy.previousGC)
			}
		}
		if _, set := os.LookupEnv("GOMEMLIMIT"); !set {
			if limit, found := memoryLimit(); found {
				gcPolicy.previousLimit = debug.SetMemoryLimit(int64(float64(limit) * cgroupHeadroom))
				gcPolicy.loweredLimit = true
				if gcPolicy.previousLimit != math.MaxInt64 {
					// A limit set from code is somebody's decision too.
					debug.SetMemoryLimit(gcPolicy.previousLimit)
					gcPolicy.loweredLimit = false
				}
			}
		}
	}
	gcPolicy.passes++
	return func() {
		gcPolicy.mutex.Lock()
		defer gcPolicy.mutex.Unlock()
		gcPolicy.passes--
		if gcPolicy.passes != 0 {
			return
		}
		if gcPolicy.loweredGC {
			debug.SetGCPercent(gcPolicy.previousGC)
		}
		if gcPolicy.loweredLimit {
			debug.SetMemoryLimit(gcPolicy.previousLimit)
		}
	}
}
