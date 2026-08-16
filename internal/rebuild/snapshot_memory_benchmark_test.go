//go:build ladybug && cgo

package rebuild

import (
	"context"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
)

// BenchmarkBuildSnapshotMemory measures what one snapshot build costs a
// serving process, which is a different question from what the build
// allocates. A server keeps whatever the runtime does not hand back: the
// numbers that decide its resident size are the arena after the build and the
// arena after the scavenge ReturnBuildMemory performs, not B/op.
//
// It is gated on a real database because the shape of the graph is the whole
// point. A synthetic corpus with one thousand distinct strings repeated a
// hundred times measures the interner, not the string volume a real corpus
// decodes:
//
//	KIVGRAPH_SNAPSHOT_BUILD_DB=~/.local/state/kivgraph/generations/000085/graph.db \
//	  make test-ladybug PKGS=./internal/rebuild \
//	  ARGS='-run ^$ -bench ^BenchmarkBuildSnapshotMemory$ -benchtime=1x'
//
// The reported metrics are megabytes, because the differences that matter here
// are hundreds of megabytes and B/op reads as noise at that scale:
//
//	alloc_MB     everything the build allocated, freed or not
//	live_MB      heap in use once the snapshot is the only thing reachable
//	arena_MB     heap the runtime holds after the build
//	parked_MB    heap the runtime holds after ReturnBuildMemory, which is
//	             what a server parks at until its next request
func BenchmarkBuildSnapshotMemory(b *testing.B) {
	databasePath := os.Getenv("KIVGRAPH_SNAPSHOT_BUILD_DB")
	if databasePath == "" {
		b.Skip("set KIVGRAPH_SNAPSHOT_BUILD_DB to a populated canonical graph")
	}
	if _, err := os.Stat(databasePath); err != nil {
		b.Fatalf("stat %s: %v", databasePath, err)
	}

	// This measures what a server does, which is return the native heap along
	// with the Go arena. Setting KIVGRAPH_SNAPSHOT_BUILD_UNBOUNDED_HEAP
	// reproduces the code path before that existed -- the Go scavenge alone --
	// so the two numbers can be compared on one machine with one warm page
	// cache, which is the only way to tell an allocator effect from a cold
	// read of a 189 MB file.
	returnBuildMemory := ReturnBuildMemory
	if os.Getenv("KIVGRAPH_SNAPSHOT_BUILD_UNBOUNDED_HEAP") != "" {
		returnBuildMemory = debug.FreeOSMemory
	}

	ctx := context.Background()
	for b.Loop() {
		// A previous iteration's arena would be read as this one's, so the
		// measurement starts from the same place the first one did.
		debug.FreeOSMemory()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		snapshot, report, err := BuildSnapshot(ctx, BuildSnapshotOptions{
			DatabasePath: databasePath,
			SnapshotID:   1,
		})
		if err != nil {
			b.Fatalf("build snapshot: %v", err)
		}
		if !report.Passed {
			b.Fatalf("build snapshot did not pass")
		}

		var built runtime.MemStats
		runtime.ReadMemStats(&built)
		// The snapshot has to stay reachable across the collection below:
		// measuring the live set with the only reference already dead would
		// report an empty heap and call it a saving.
		runtime.GC()
		var live runtime.MemStats
		runtime.ReadMemStats(&live)
		returnBuildMemory()
		var parked runtime.MemStats
		runtime.ReadMemStats(&parked)
		runtime.KeepAlive(snapshot)

		const mb = 1 << 20
		b.ReportMetric(float64(built.TotalAlloc-before.TotalAlloc)/mb, "alloc_MB")
		b.ReportMetric(float64(live.HeapAlloc)/mb, "live_MB")
		b.ReportMetric(float64(built.HeapSys-built.HeapReleased)/mb, "arena_MB")
		b.ReportMetric(float64(parked.HeapSys-parked.HeapReleased)/mb, "parked_MB")
		// The Go heap is not what a server is measured by. The engine's buffer
		// pool is a native allocation proportional to the graph, so a scavenge
		// of the Go arena cannot return it and MemStats cannot see it: only
		// resident size can. A platform without /proc reports nothing here
		// rather than a zero that would read as an answer.
		if resident, ok := residentBytes(); ok {
			b.ReportMetric(float64(resident)/mb, "rss_MB")
		}
		b.ReportMetric(float64(report.Stats.Symbols), "symbols")
	}
}

// residentBytes answers this process's resident size, and whether it could be
// read at all.
func residentBytes() (uint64, bool) {
	statm, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(statm))
	if len(fields) < 2 {
		return 0, false
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return pages * uint64(os.Getpagesize()), true
}
