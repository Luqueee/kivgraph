package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// One batch naming two files scans the symbol table once, and each position
// still resolves to the symbol a one-file resolution gives.
func TestPositionBatchScansSymbolTableOnce(t *testing.T) {
	store, _ := sourceSnapshot(t, 97)
	snapshot := store.Load()
	pub := publicationToken(snapshot.Metadata())
	merge, other := uint32(5), uint32(3)
	requests := []SourceRequest{
		{Repository: "alpha", Path: "set.go", Line: &merge, Publication: pub},
		{Repository: "alpha", Path: "other.go", Line: &other, Publication: pub},
	}
	selectors := []symbolSelector{{Repository: "alpha", Path: "set.go"}, {Repository: "alpha", Path: "other.go"}}
	batch := positionFiles{requests: requests, selectors: selectors}
	cache := map[string]*sourceFile{}
	for i, request := range requests {
		got, err := batch.resolve(t.Context(), snapshot, request, selectors[i].Path, cache)
		if err != nil {
			t.Fatalf("batch position %d: %v", i, err)
		}
		if i == 0 && len(batch.candidates) != len(requests) {
			t.Fatalf("first traversal indexed %d files, want both requested files", len(batch.candidates))
		}
		want, err := (&positionFiles{}).resolve(t.Context(), snapshot, request, selectors[i].Path, map[string]*sourceFile{})
		if err != nil || got != want {
			t.Fatalf("batch position %d resolved %d, one-file resolution %d, %v", i, got, want, err)
		}
	}
}

// A file changed after publication fails the batch with CodeSnapshotUnavailable.
func TestPositionBatchKeepsLazyFreshnessError(t *testing.T) {
	store, root := sourceSnapshot(t, 98)
	pub := publicationToken(store.Load().Metadata())
	if err := os.WriteFile(filepath.Join(root, "other.go"), []byte("package other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	merge, other := uint32(5), uint32(3)
	_, _, err := getSource(t.Context(), nil, GetSourceInput{Symbols: []SourceRequest{
		{Repository: "alpha", Path: "set.go", Line: &merge, Publication: pub},
		{Repository: "alpha", Path: "other.go", Line: &other, Publication: pub},
	}}, store)
	if err == nil || ErrorCode(err) != CodeSnapshotUnavailable {
		t.Fatalf("stale second file: %v", err)
	}
}

// BenchmarkPositionBatch measures one batch of positions over two files, the
// case the single symbol-table scan exists for.
func BenchmarkPositionBatch(b *testing.B) {
	store, _ := sourceSnapshot(b, 99)
	pub := publicationToken(store.Load().Metadata())
	merge, other := uint32(5), uint32(3)
	input := GetSourceInput{Symbols: []SourceRequest{
		{Repository: "alpha", Path: "set.go", Line: &merge, Publication: pub},
		{Repository: "alpha", Path: "other.go", Line: &other, Publication: pub},
	}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, err := getSource(b.Context(), nil, input, store); err != nil {
			b.Fatal(err)
		}
	}
}
