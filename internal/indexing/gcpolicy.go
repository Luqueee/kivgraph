package indexing

import (
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

var gcPolicy struct {
	mutex    sync.Mutex
	passes   int
	previous int
	lowered  bool
}

// collectSooner lowers the GC percent for a pass and returns what ends it.
// Passes of one process can overlap, so the first sets it and the last puts it
// back. A GOGC set in the environment is the operator's decision and wins, and
// so does a collector that is off or already below our percent.
func collectSooner() func() {
	if _, set := os.LookupEnv("GOGC"); set {
		return func() {}
	}
	gcPolicy.mutex.Lock()
	defer gcPolicy.mutex.Unlock()
	if gcPolicy.passes == 0 {
		gcPolicy.previous = debug.SetGCPercent(indexingGCPercent)
		gcPolicy.lowered = gcPolicy.previous > indexingGCPercent
		if !gcPolicy.lowered {
			debug.SetGCPercent(gcPolicy.previous)
		}
	}
	gcPolicy.passes++
	return func() {
		gcPolicy.mutex.Lock()
		defer gcPolicy.mutex.Unlock()
		gcPolicy.passes--
		if gcPolicy.passes == 0 && gcPolicy.lowered {
			debug.SetGCPercent(gcPolicy.previous)
		}
	}
}
