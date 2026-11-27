package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPositionBatchReturnsBothDeclarations(t *testing.T) {
	store, _ := sourceSnapshot(t, 97)
	pub := publicationToken(store.Load().Metadata())
	merge, other := uint32(5), uint32(3)
	requests := []SourceRequest{
		{Repository: "alpha", Path: "set.go", Line: &merge, Publication: pub},
		{Repository: "alpha", Path: "other.go", Line: &other, Publication: pub},
	}
	_, response, err := getSource(t.Context(), nil, GetSourceInput{Symbols: requests}, store)
	if err != nil {
		t.Fatal(err)
	}
	bodies := response.Results.Bodies
	if len(bodies) != 2 || bodies[0].QualifiedName != "pkg.Merge" || bodies[1].QualifiedName != "other.Other" {
		t.Fatalf("unexpected declarations for set.go:5 and other.go:3: %+v", bodies)
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
