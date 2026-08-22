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
// string bytes can be mapped, while the three lookup indexes and the interner's
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
		keys := snapshot.StableKeys().Stats()
		volume := uint64(strings.Bytes) +
			stableKeyTableBytes(keys) +
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
		// The heap is no longer the whole snapshot: since the string arena is
		// read out of the mapped file, heap_live excludes it and volume is the
		// arithmetic size of everything a file could be read in place -- which is
		// now larger than the heap rather than a fraction of it. What the two
		// together say is how much of the snapshot stopped being per-process.
		b.ReportMetric(float64(live)/mb, "heap_live_MB")
		b.ReportMetric(float64(volume)/mb, "volume_MB")
		b.ReportMetric(float64(uint64(strings.Bytes))/mb, "mapped_arena_MB")
		b.ReportMetric(float64(stableKeyTableBytes(keys))/mb, "mapped_keys_MB")
		// The unmappable part is worth splitting, because two very different
		// changes attack it. The first counterfactual is what the string table no
		// longer pays: sixteen bytes of Go string header per interned value, which
		// an arena replaced with four. The second is the hash tables, and only one
		// of the two this used to rebuild is still a hash table -- the keys became
		// the ordered arena counted in the volume above, so measuring a map for
		// them would price a structure no reader builds.
		b.ReportMetric(float64(uint64(strings.Entries)*16)/mb, "headers_avoided_MB")
		var beforeMaps runtime.MemStats
		runtime.ReadMemStats(&beforeMaps)
		byName := make(map[hotsnapshot.InternedString][]hotsnapshot.SymbolID)
		for id := range hotsnapshot.SymbolID(counts.Symbols) {
			record, _ := snapshot.Symbol(id)
			byName[record.Name] = append(byName[record.Name], id)
		}
		runtime.GC()
		var afterMaps runtime.MemStats
		runtime.ReadMemStats(&afterMaps)
		b.ReportMetric(float64(afterMaps.HeapAlloc-beforeMaps.HeapAlloc)/mb, "name_map_MB")
		runtime.KeepAlive(byName)
		// The string table's own lookup map is the last candidate for the
		// remainder, and the biggest single structure a snapshot cannot map: one
		// entry per interned value, against seven megabytes of headers for the
		// same values. It is rebuilt here over exactly those values.
		var beforeIndex runtime.MemStats
		runtime.ReadMemStats(&beforeIndex)
		lookup := make(map[string]hotsnapshot.InternedString, strings.Entries)
		for id := range hotsnapshot.InternedString(strings.Entries) {
			value, _ := snapshot.Strings().String(id)
			lookup[value] = id
		}
		runtime.GC()
		var afterIndex runtime.MemStats
		runtime.ReadMemStats(&afterIndex)
		b.ReportMetric(float64(afterIndex.HeapAlloc-beforeIndex.HeapAlloc)/mb, "interner_map_avoided_MB")
		b.ReportMetric(float64(strings.Entries), "interned")
		runtime.KeepAlive(lookup)
		b.ReportMetric(float64(info.Size())/mb, "file_MB")
		b.ReportMetric(float64(counts.Symbols), "symbols")
		runtime.KeepAlive(snapshot)
	}
}

// stableKeyTableBytes is what the key table occupies: its arena plus one offset
// per entry and the terminator that bounds the last key.
func stableKeyTableBytes(stats hotsnapshot.StableKeyTableStats) uint64 {
	return stats.Bytes + 4*(uint64(stats.Entries)+1)
}
