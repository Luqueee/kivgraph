package rebuild

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"
	"unsafe"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// BenchmarkLoadPublishedSnapshot measures what loading a published snapshot
// costs, and what the result is made of.
//
// The composition is the point. Phase 2 of ADR 0045 would map the file's volume
// and share it between processes, and what that is worth depends entirely on how
// much of a loaded snapshot is the volume: the tables, the two CSRs and the
// string bytes can be mapped, while the four lookup indexes and the interner's
// own map cannot, because they are hash tables. If the indexes dominate, mapping
// the rest saves little and buys a lifetime hazard -- a mapped string handed to a
// caller outlives nothing.
//
//	KIVGRAPH_SNAPSHOT_LOAD_DIR=~/.local/state/kivgraph/generations/000090 \
//	  go test ./internal/rebuild -run NONE -bench BenchmarkLoadPublishedSnapshot -benchtime=1x
func BenchmarkLoadPublishedSnapshot(b *testing.B) {
	directory := os.Getenv("KIVGRAPH_SNAPSHOT_LOAD_DIR")
	if directory == "" {
		b.Skip("set KIVGRAPH_SNAPSHOT_LOAD_DIR to a generation directory that carries a published snapshot")
	}
	info, err := os.Stat(filepath.Join(directory, PublishedSnapshotFileName))
	if err != nil {
		b.Skipf("no published snapshot: %v", err)
	}

	const mb = 1 << 20
	for b.Loop() {
		debug.FreeOSMemory()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		snapshot, err := loadPublishedSnapshot(directory)
		if err != nil {
			b.Fatalf("load: %v", err)
		}
		runtime.GC()
		var loaded runtime.MemStats
		runtime.ReadMemStats(&loaded)

		// What the volume weighs is arithmetic over what the snapshot says it
		// holds, not an estimate: every one of these is a fixed width table.
		counts := snapshot.Metadata().Counts
		strings := snapshot.Strings().Stats()
		volume := uint64(strings.Bytes) +
			uint64(counts.Repositories)*uint64(unsafe.Sizeof(hotsnapshot.RepositoryRecord{})) +
			uint64(counts.Packages)*uint64(unsafe.Sizeof(hotsnapshot.PackageRecord{})) +
			uint64(counts.Files)*uint64(unsafe.Sizeof(hotsnapshot.FileRecord{})) +
			uint64(counts.Symbols)*uint64(unsafe.Sizeof(hotsnapshot.SymbolRecord{})) +
			uint64(counts.Evidence)*uint64(unsafe.Sizeof(hotsnapshot.EvidenceRecord{})) +
			uint64(counts.PackageEdges)*uint64(unsafe.Sizeof(hotsnapshot.PackageDependencyRecord{})) +
			uint64(counts.Unresolved)*uint64(unsafe.Sizeof(hotsnapshot.UnresolvedReferenceRecord{})) +
			2*uint64(counts.Edges)*uint64(unsafe.Sizeof(hotsnapshot.PackedEdge{}))

		live := loaded.HeapAlloc
		b.ReportMetric(float64(loaded.TotalAlloc-before.TotalAlloc)/mb, "alloc_MB")
		b.ReportMetric(float64(live)/mb, "live_MB")
		b.ReportMetric(float64(volume)/mb, "volume_MB")
		// Whatever the volume does not explain is the part phase 2 cannot map:
		// the four lookup indexes, the interner's own map, and the string
		// headers a Go string carries beside its bytes.
		if live > volume {
			b.ReportMetric(float64(live-volume)/mb, "unmappable_MB")
		}
		// The unmappable part is worth splitting, because two very different
		// changes attack it. The string headers are arithmetic: a Go string is
		// sixteen bytes beside its own bytes, one per interned value. The hash
		// tables are measured by rebuilding the two heaviest of them, which is
		// what a sorted array and a binary search would replace.
		b.ReportMetric(float64(uint64(strings.Entries)*16)/mb, "headers_MB")
		var beforeMaps runtime.MemStats
		runtime.ReadMemStats(&beforeMaps)
		byStableKey := make(map[hotsnapshot.StableKey]hotsnapshot.SymbolID, counts.Symbols)
		byName := make(map[hotsnapshot.InternedString][]hotsnapshot.SymbolID)
		for id := range hotsnapshot.SymbolID(counts.Symbols) {
			record, _ := snapshot.Symbol(id)
			byStableKey[record.StableKey] = id
			byName[record.Name] = append(byName[record.Name], id)
		}
		runtime.GC()
		var afterMaps runtime.MemStats
		runtime.ReadMemStats(&afterMaps)
		b.ReportMetric(float64(afterMaps.HeapAlloc-beforeMaps.HeapAlloc)/mb, "two_maps_MB")
		runtime.KeepAlive(byStableKey)
		runtime.KeepAlive(byName)
		b.ReportMetric(float64(info.Size())/mb, "file_MB")
		b.ReportMetric(float64(counts.Symbols), "symbols")
		runtime.KeepAlive(snapshot)
	}
}
