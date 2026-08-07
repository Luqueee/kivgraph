package hotsnapshot

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	hotBenchmarkRowsOnce sync.Once
	hotBenchmarkRows     LadybugSnapshotRows
	hotBenchmarkSnapshot *GraphSnapshot
)

func BenchmarkHotSnapshotBuild(b *testing.B) {
	rows := hotBenchmarkRowsFixture()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		snapshot, err := BuildGraphSnapshot(rows, 1, time.Unix(1, 0).UTC(), 1)
		if err != nil {
			b.Fatal(err)
		}
		hotBenchmarkSnapshot = snapshot
	}
	if rss := hotBenchmarkRSSBytes(); rss > 0 {
		b.ReportMetric(float64(rss), "rss-bytes")
	}
}

func BenchmarkHotSnapshotBuildPublish(b *testing.B) {
	rows := hotBenchmarkRowsFixture()
	b.ReportAllocs()
	var snapshotID uint64
	b.ResetTimer()
	for b.Loop() {
		snapshotID++
		snapshot, err := BuildGraphSnapshot(rows, snapshotID, time.Unix(int64(snapshotID+1), 0).UTC(), 1)
		if err != nil {
			b.Fatal(err)
		}
		store := NewSnapshotStore(nil)
		if err := store.Publish(snapshot); err != nil {
			b.Fatal(err)
		}
		hotBenchmarkSnapshot = store.Load()
	}
}

func BenchmarkHotSnapshotFindExact(b *testing.B) {
	snapshot := hotBenchmarkSnapshotFixture(b)
	key := StableKey("s-50000")
	b.ReportAllocs()
	for b.Loop() {
		if _, found := snapshot.SymbolByStableKey(key); !found {
			b.Fatal("stable key not found")
		}
	}
}

func BenchmarkHotSnapshotReferences(b *testing.B) {
	snapshot := hotBenchmarkSnapshotFixture(b)
	id, found := snapshot.SymbolByStableKey("s-50000")
	if !found {
		b.Fatal("stable key not found")
	}
	b.ReportAllocs()
	for b.Loop() {
		if len(snapshot.Outgoing(id)) == 0 {
			b.Fatal("outgoing references unexpectedly empty")
		}
	}
}
func BenchmarkHotSnapshotVisitSymbols(b *testing.B) {
	snapshot := hotBenchmarkSnapshotFixture(b)
	ctx := context.Background()
	end := SymbolID(len(snapshot.symbols))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := snapshot.VisitSymbols(ctx, 0, end, benchmarkConsumeSymbol); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(end), "symbols/op")
	if hotBenchmarkVisitSink == 0 {
		b.Fatal("symbol visitor was not called")
	}
}

func BenchmarkHotSnapshotVisitCSR(b *testing.B) {
	snapshot := hotBenchmarkSnapshotFixture(b)
	ctx := context.Background()
	end := EdgeID(len(snapshot.forwardEdges))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := snapshot.VisitEdges(ctx, TraversalOutgoing, 0, end, benchmarkConsumeEdge); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(end), "edges/op")
	if hotBenchmarkVisitSink == 0 {
		b.Fatal("edge visitor was not called")
	}
}
func BenchmarkHotSnapshotOutgoingAllSymbols(b *testing.B) {
	snapshot := hotBenchmarkSnapshotFixture(b)
	symbolCount := SymbolID(len(snapshot.symbols))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for symbol := SymbolID(0); symbol < symbolCount; symbol++ {
			for _, edge := range snapshot.Outgoing(symbol) {
				hotBenchmarkVisitSink += uint64(edge.Target)
			}
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(symbolCount), "symbols/op")
	if hotBenchmarkVisitSink == 0 {
		b.Fatal("outgoing getter was not called")
	}
}

func BenchmarkHotSnapshotVisitCSRBySymbol(b *testing.B) {
	snapshot := hotBenchmarkSnapshotFixture(b)
	ctx := context.Background()
	symbolCount := SymbolID(len(snapshot.symbols))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for symbol := SymbolID(0); symbol < symbolCount; symbol++ {
			start, end, ok := snapshot.CSRRange(TraversalOutgoing, symbol)
			if !ok {
				b.Fatal("outgoing CSR range was not found")
			}
			if err := snapshot.VisitEdges(ctx, TraversalOutgoing, start, end, benchmarkConsumeEdge); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(symbolCount), "symbols/op")
	if hotBenchmarkVisitSink == 0 {
		b.Fatal("outgoing iterator was not called")
	}
}

var hotBenchmarkVisitSink uint64

func benchmarkConsumeSymbol(id SymbolID, symbol SymbolRecord) error {
	hotBenchmarkVisitSink += uint64(id) + uint64(symbol.File)
	return nil
}

func benchmarkConsumeEdge(id EdgeID, edge PackedEdge) error {
	hotBenchmarkVisitSink += uint64(id) + uint64(edge.Target)
	return nil
}

func BenchmarkHotSnapshotDepth3(b *testing.B) {
	snapshot := hotBenchmarkSnapshotFixture(b)
	b.ReportAllocs()
	for b.Loop() {
		result, err := snapshot.Traverse(0, TraversalOptions{Direction: TraversalOutgoing, MaxDepth: 3, MaxNodes: MaxTraversalNodes})
		if err != nil || len(result.Visits) == 0 {
			b.Fatalf("Traverse(depth=3) = %d, %v", len(result.Visits), err)
		}
	}
}

func BenchmarkHotSnapshotDepth5(b *testing.B) {
	snapshot := hotBenchmarkSnapshotFixture(b)
	b.ReportAllocs()
	for b.Loop() {
		result, err := snapshot.Traverse(0, TraversalOptions{Direction: TraversalOutgoing, MaxDepth: 5, MaxNodes: MaxTraversalNodes})
		if err != nil || len(result.Visits) == 0 {
			b.Fatalf("Traverse(depth=5) = %d, %v", len(result.Visits), err)
		}
	}
}

func BenchmarkHotSnapshotConcurrentFind(b *testing.B) {
	snapshot := hotBenchmarkSnapshotFixture(b)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, found := snapshot.SymbolByStableKey("s-50000"); !found {
				b.Fatal("stable key not found")
			}
		}
	})
}

func hotBenchmarkRowsFixture() LadybugSnapshotRows {
	hotBenchmarkRowsOnce.Do(func() {
		const (
			symbolCount     = 100_000
			edgeCount       = 1_000_000
			fileCount       = 1_000
			packageCount    = 100
			repositoryCount = 10
		)
		rows := LadybugSnapshotRows{
			Repositories: make([]RepositoryRow, repositoryCount),
			Packages:     make([]PackageRow, packageCount),
			Files:        make([]FileRow, fileCount),
			Symbols:      make([]SymbolRow, symbolCount),
			Edges:        make([]EdgeRow, edgeCount),
		}
		for index := range rows.Repositories {
			key := "repo-" + strconv.Itoa(index)
			rows.Repositories[index] = RepositoryRow{Key: key, Name: key, Commit: "commit-" + strconv.Itoa(index)}
		}
		for index := range rows.Packages {
			rows.Packages[index] = PackageRow{Key: "pkg-" + strconv.Itoa(index), RepositoryKey: "repo-" + strconv.Itoa(index%repositoryCount), Name: "package-" + strconv.Itoa(index), ModulePath: "example.com/module-" + strconv.Itoa(index)}
		}
		for index := range rows.Files {
			rows.Files[index] = FileRow{Key: "file-" + strconv.Itoa(index), RepositoryKey: "repo-" + strconv.Itoa(index%repositoryCount), PackageKey: "pkg-" + strconv.Itoa(index%packageCount), Path: "src/file-" + strconv.Itoa(index) + ".ts"}
		}
		for index := range rows.Symbols {
			key := "s-" + strconv.Itoa(index)
			rows.Symbols[index] = SymbolRow{StableKey: StableKey(key), CanonicalIdentity: "identity-" + strconv.Itoa(index), FileKey: "file-" + strconv.Itoa(index%fileCount), Name: "name-" + strconv.Itoa(index), QualifiedName: "module." + key, Kind: "function", Signature: "(): void"}
		}
		for index := range rows.Edges {
			source := index % symbolCount

			target := (source + 1 + (index / symbolCount)) % symbolCount
			rows.Edges[index] = EdgeRow{SourceKey: rows.Symbols[source].StableKey, TargetKey: rows.Symbols[target].StableKey, Kind: 1, Confidence: 1, Provenance: 1, EvidenceKind: "checker", EvidenceSourceFileKey: rows.Symbols[source].FileKey, EvidenceTargetFileKey: rows.Symbols[target].FileKey}
		}
		hotBenchmarkRows = rows
	})
	return hotBenchmarkRows
}

func hotBenchmarkSnapshotFixture(b *testing.B) *GraphSnapshot {
	b.Helper()
	if hotBenchmarkSnapshot == nil {
		snapshot, err := BuildGraphSnapshot(hotBenchmarkRowsFixture(), 1, time.Unix(1, 0).UTC(), 1)
		if err != nil {
			b.Fatal(err)
		}
		hotBenchmarkSnapshot = snapshot
	}
	return hotBenchmarkSnapshot
}
func hotBenchmarkRSSBytes() int64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmHWM:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kilobytes, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kilobytes * 1024
	}
	return 0
}
