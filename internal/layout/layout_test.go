package layout_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/layout"
)

func TestBuildIsDeterministicAndPreservesContainment(t *testing.T) {
	snapshot := layoutSnapshot(t)
	config := layout.DefaultConfig()

	first, err := layout.Build(context.Background(), snapshot, config)
	if err != nil {
		t.Fatalf("first Build() error = %v", err)
	}
	second, err := layout.Build(context.Background(), snapshot, config)
	if err != nil {
		t.Fatalf("second Build() error = %v", err)
	}
	firstBytes, err := first.MarshalBinary()
	if err != nil {
		t.Fatalf("first MarshalBinary() error = %v", err)
	}
	secondBytes, err := second.MarshalBinary()
	if err != nil {
		t.Fatalf("second MarshalBinary() error = %v", err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("layout bytes differ between identical builds")
	}

	nodes := first.Nodes()
	if len(nodes) != 14 {
		t.Fatalf("node count = %d, want 14", len(nodes))
	}
	if nodes[0].Kind != layout.NodeRepository || nodes[1].Kind != layout.NodeRepository {
		t.Fatalf("first nodes = %#v, want repositories", nodes[:2])
	}
	for index := 1; index < len(nodes); index++ {
		if nodes[index-1].Kind > nodes[index].Kind {
			t.Fatalf("nodes are not grouped by hierarchy level: %#v then %#v", nodes[index-1], nodes[index])
		}
	}
	for _, node := range nodes {
		if !node.Bounds.Valid() {
			t.Fatalf("node %v has invalid bounds %#v", node.Kind, node.Bounds)
		}
		if node.Kind == layout.NodeRepository {
			continue
		}
		parent := findNode(nodes, node.Parent)
		if parent.Kind == layout.NodeNone || !contains(parent.Bounds, node.Bounds) {
			t.Fatalf("node %#v is outside parent %#v", node, parent)
		}
	}

	stats := first.GridStats()
	if stats.Cells == 0 || stats.IndexedEntries == 0 {
		t.Fatalf("grid stats = %#v, want indexed cells", stats)
	}
}

func TestQueryViewportHonorsLODBoundsAndLimit(t *testing.T) {
	snapshot := layoutSnapshot(t)
	built, err := layout.Build(context.Background(), snapshot, layout.DefaultConfig())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	symbol := findNode(built.Nodes(), layout.NodeRef{Kind: layout.NodeSymbol, ID: 0})
	queryBounds := symbol.Bounds

	filesOnly, err := built.QueryViewport(layout.ViewportQuery{Bounds: queryBounds, MaxLevel: layout.LODFiles})
	if err != nil {
		t.Fatalf("files-only QueryViewport() error = %v", err)
	}
	if len(filesOnly.Nodes) == 0 {
		t.Fatal("files-only query returned no containing nodes")
	}
	for _, node := range filesOnly.Nodes {
		if node.Kind == layout.NodeSymbol {
			t.Fatalf("files-only query returned symbol %#v", node)
		}
		if !node.Bounds.Intersects(queryBounds) {
			t.Fatalf("query returned a node outside viewport: %#v", node)
		}
	}

	allLevels, err := built.QueryViewport(layout.ViewportQuery{Bounds: queryBounds, MaxLevel: layout.LODSymbols})
	if err != nil {
		t.Fatalf("all-level QueryViewport() error = %v", err)
	}
	if !containsNode(allLevels.Nodes, layout.NodeRef{Kind: layout.NodeSymbol, ID: 0}) {
		t.Fatalf("all-level query omitted target symbol: %#v", allLevels.Nodes)
	}
	for _, node := range allLevels.Nodes {
		if !node.Bounds.Intersects(queryBounds) {
			t.Fatalf("all-level query returned a node outside viewport: %#v", node)
		}
	}

	outside := layout.Rect{
		MinX: built.Bounds().MaxX + 1,
		MinY: built.Bounds().MaxY + 1,
		MaxX: built.Bounds().MaxX + 100,
		MaxY: built.Bounds().MaxY + 100,
	}
	empty, err := built.QueryViewport(layout.ViewportQuery{Bounds: outside, MaxLevel: layout.LODSymbols})
	if err != nil {
		t.Fatalf("outside QueryViewport() error = %v", err)
	}
	if len(empty.Nodes) != 0 || empty.Truncated {
		t.Fatalf("outside query = %#v, want empty", empty)
	}

	limited, err := built.QueryViewport(layout.ViewportQuery{Bounds: built.Bounds(), MaxLevel: layout.LODRepositories, MaxNodes: 1})
	if err != nil {
		t.Fatalf("limited QueryViewport() error = %v", err)
	}
	if len(limited.Nodes) != 1 || !limited.Truncated {
		t.Fatalf("limited query = %#v, want one node and truncation", limited)
	}
}

func TestQueryViewportRejectsInvalidLimits(t *testing.T) {
	built, err := layout.Build(context.Background(), layoutSnapshot(t), layout.DefaultConfig())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	cases := []struct {
		name  string
		query layout.ViewportQuery
		want  error
	}{
		{
			name:  "empty bounds",
			query: layout.ViewportQuery{Bounds: layout.Rect{MaxX: 1, MaxY: 0}},
			want:  layout.ErrInvalidViewport,
		},
		{
			name:  "invalid level",
			query: layout.ViewportQuery{Bounds: layout.Rect{MaxX: 1, MaxY: 1}, MaxLevel: layout.LOD(99)},
			want:  layout.ErrInvalidLOD,
		},
		{
			name:  "negative limit",
			query: layout.ViewportQuery{Bounds: layout.Rect{MaxX: 1, MaxY: 1}, MaxNodes: -1},
			want:  layout.ErrViewportNodeLimit,
		},
		{
			name: "too many cells",
			query: layout.ViewportQuery{
				Bounds:   layout.Rect{MinX: -(1 << 40), MinY: -(1 << 40), MaxX: 1 << 40, MaxY: 1 << 40},
				MaxLevel: layout.LODRepositories,
			},
			want: layout.ErrViewportTooLarge,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := built.QueryViewport(testCase.query)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("QueryViewport() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestBuildHandlesEmptySnapshotAndCancellation(t *testing.T) {
	empty := emptySnapshot(t)
	built, err := layout.Build(context.Background(), empty, layout.DefaultConfig())
	if err != nil {
		t.Fatalf("empty Build() error = %v", err)
	}
	if nodes := built.Nodes(); len(nodes) != 0 {
		t.Fatalf("empty layout nodes = %d, want 0", len(nodes))
	}
	if built.Bounds() != (layout.Rect{}) {
		t.Fatalf("empty layout bounds = %#v, want zero bounds", built.Bounds())
	}
	result, err := built.QueryViewport(layout.ViewportQuery{
		Bounds:   layout.Rect{MaxX: 1, MaxY: 1},
		MaxLevel: layout.LODSymbols,
	})
	if err != nil {
		t.Fatalf("empty QueryViewport() error = %v", err)
	}
	if len(result.Nodes) != 0 || result.Truncated {
		t.Fatalf("empty QueryViewport() = %#v, want empty result", result)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = layout.Build(ctx, empty, layout.DefaultConfig())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Build() error = %v, want context canceled", err)
	}
}

func emptySnapshot(t *testing.T) *hotsnapshot.GraphSnapshot {
	t.Helper()
	interner := hotsnapshot.NewStringInterner()
	snapshot, err := hotsnapshot.NewGraphSnapshot(hotsnapshot.GraphSnapshotInput{
		ID:                2,
		CreatedAt:         time.Unix(2, 0).UTC(),
		Version:           1,
		Strings:           interner.Freeze(),
		ForwardOffsets:    []uint32{0},
		ReverseOffsets:    []uint32{0},
		SymbolByStableKey: map[hotsnapshot.StableKey]hotsnapshot.SymbolID{},
		SymbolsByName:     map[hotsnapshot.InternedString][]hotsnapshot.SymbolID{},
		SymbolsByQName:    map[hotsnapshot.InternedString][]hotsnapshot.SymbolID{},
		FileByRepoPath:    map[hotsnapshot.RepoPathKey]hotsnapshot.FileID{},
	})
	if err != nil {
		t.Fatalf("NewGraphSnapshot() error = %v", err)
	}
	return snapshot
}

func TestBuildRejectsInvalidContainment(t *testing.T) {
	interner := hotsnapshot.NewStringInterner()
	strings := interner.Freeze()
	snapshot, err := hotsnapshot.NewGraphSnapshot(hotsnapshot.GraphSnapshotInput{
		ID:                1,
		CreatedAt:         time.Unix(1, 0).UTC(),
		Version:           1,
		Strings:           strings,
		Repositories:      []hotsnapshot.RepositoryRecord{{}},
		Packages:          []hotsnapshot.PackageRecord{{Repository: hotsnapshot.InvalidRepositoryID}},
		ForwardOffsets:    []uint32{0},
		ReverseOffsets:    []uint32{0},
		SymbolByStableKey: map[hotsnapshot.StableKey]hotsnapshot.SymbolID{},
		SymbolsByName:     map[hotsnapshot.InternedString][]hotsnapshot.SymbolID{},
		SymbolsByQName:    map[hotsnapshot.InternedString][]hotsnapshot.SymbolID{},
		FileByRepoPath:    map[hotsnapshot.RepoPathKey]hotsnapshot.FileID{},
	})
	if err != nil {
		t.Fatalf("NewGraphSnapshot() error = %v", err)
	}
	_, err = layout.Build(context.Background(), snapshot, layout.DefaultConfig())
	if !errors.Is(err, layout.ErrInvalidContainment) {
		t.Fatalf("Build() error = %v, want invalid containment", err)
	}
}

func layoutSnapshot(t *testing.T) *hotsnapshot.GraphSnapshot {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-a", Name: "repo-a", Path: "/repo-a"},
			{Key: "repo-b", Name: "repo-b", Path: "/repo-b"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "pkg-a1", RepositoryKey: "repo-a", Name: "pkg-a1"},
			{Key: "pkg-a2", RepositoryKey: "repo-a", Name: "pkg-a2"},
			{Key: "pkg-b1", RepositoryKey: "repo-b", Name: "pkg-b1"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-a1", RepositoryKey: "repo-a", PackageKey: "pkg-a1", Path: "a1.go"},
			{Key: "file-a2", RepositoryKey: "repo-a", PackageKey: "pkg-a2", Path: "a2.go"},
			{Key: "file-b1", RepositoryKey: "repo-b", PackageKey: "pkg-b1", Path: "b1.go"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "symbol-a1-1", CanonicalIdentity: "go:a1.1", FileKey: "file-a1", Name: "A1", QualifiedName: "a1.A1", Kind: "func"},
			{StableKey: "symbol-a1-2", CanonicalIdentity: "go:a1.2", FileKey: "file-a1", Name: "A2", QualifiedName: "a1.A2", Kind: "func"},
			{StableKey: "symbol-a1-3", CanonicalIdentity: "go:a1.3", FileKey: "file-a1", Name: "A3", QualifiedName: "a1.A3", Kind: "func"},
			{StableKey: "symbol-a2-1", CanonicalIdentity: "go:a2.1", FileKey: "file-a2", Name: "B1", QualifiedName: "a2.B1", Kind: "func"},
			{StableKey: "symbol-b1-1", CanonicalIdentity: "go:b1.1", FileKey: "file-b1", Name: "C1", QualifiedName: "b1.C1", Kind: "func"},
			{StableKey: "symbol-b1-2", CanonicalIdentity: "go:b1.2", FileKey: "file-b1", Name: "C2", QualifiedName: "b1.C2", Kind: "func"},
		},
	}, 77, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return snapshot
}

func findNode(nodes []layout.Node, reference layout.NodeRef) layout.Node {
	for _, node := range nodes {
		if node.Kind == reference.Kind && node.ID == reference.ID {
			return node
		}
	}
	panic("layout node not found")
}

func containsNode(nodes []layout.Node, reference layout.NodeRef) bool {
	for _, node := range nodes {
		if node.Kind == reference.Kind && node.ID == reference.ID {
			return true
		}
	}
	return false
}

func contains(parent, child layout.Rect) bool {
	return parent.MinX <= child.MinX && child.MaxX <= parent.MaxX &&
		parent.MinY <= child.MinY && child.MaxY <= parent.MaxY
}
