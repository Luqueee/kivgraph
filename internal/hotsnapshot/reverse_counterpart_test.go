package hotsnapshot

import (
	"errors"
	"testing"
	"time"
)

// twoEdgeCounterpartInput carries two edges out of one symbol so that a reverse
// row can be made wrong in exactly one dimension while everything around it
// still lines up. The shared fixture holds a single edge, which cannot express
// that: breaking its reverse row moves the source, and the walk then fails for
// having nowhere to look rather than for the field under test.
func twoEdgeCounterpartInput() GraphSnapshotInput {
	interner := NewStringInterner()
	for _, value := range []string{"repository", "commit", "module", "src/parser.ts", "parse", "Parser.parse", "method", "signature", "evidence", "checker"} {
		if _, err := interner.Intern(value); err != nil {
			panic(err)
		}
	}
	keys, err := NewStableKeyTable([]StableKey{"symbol-a", "symbol-b", "symbol-c"})
	if err != nil {
		panic(err)
	}
	symbol := SymbolRecord{CanonicalIdentity: 4, File: 0, Name: 4, QualifiedName: 5, Kind: 6, Signature: 7}
	return GraphSnapshotInput{
		ID:           42,
		CreatedAt:    time.Unix(1_700_000_000, 0).UTC(),
		Version:      1,
		Strings:      interner.Freeze(),
		Repositories: []RepositoryRecord{{Name: 0, Commit: 1}},
		Packages:     []PackageRecord{{Repository: 0, Name: 2, ModulePath: 2}},
		Files:        []FileRecord{{Repository: 0, Package: 0, Path: 3}},
		Symbols: []SymbolRecord{
			func() SymbolRecord { record := symbol; record.StableKey = 0; return record }(),
			func() SymbolRecord { record := symbol; record.StableKey = 1; return record }(),
			func() SymbolRecord { record := symbol; record.StableKey = 2; return record }(),
		},
		Evidence: []EvidenceRecord{
			{SourceFile: 0, TargetFile: 0, Kind: 8, Provenance: 9},
			{SourceFile: 0, TargetFile: 0, Kind: 8, Provenance: 9},
		},
		// Symbol 0 reaches 1 and 2, and the two edges differ in every byte a
		// counterpart has to agree on.
		ForwardOffsets: []uint32{0, 2, 2, 2},
		ForwardEdges: []PackedEdge{
			{Target: 1, Evidence: 0, Kind: 1, Confidence: 1, Provenance: 1, Flags: 0},
			{Target: 2, Evidence: 1, Kind: 2, Confidence: 2, Provenance: 2, Flags: 1},
		},
		ReverseOffsets: []uint32{0, 0, 1, 2},
		ReverseEdges: []PackedEdge{
			{Target: 0, Evidence: 0, Kind: 1, Confidence: 1, Provenance: 1, Flags: 0},
			{Target: 0, Evidence: 1, Kind: 2, Confidence: 2, Provenance: 2, Flags: 1},
		},
		StableKeys:     keys,
		SymbolsByName:  map[InternedString][]SymbolID{4: {0, 1, 2}},
		SymbolsByQName: map[InternedString][]SymbolID{5: {0, 1, 2}},
		FileByRepoPath: map[RepoPathKey]FileID{{Repository: 0, Path: 3}: 0},
	}
}

// TestReverseCounterpartComparesEveryEdgeField pins each field the walk has to
// agree on. The check used to key a map by all seven at once, so they could not
// rot apart; comparing them one at a time means each needs its own reason to
// exist, and every case here is a reverse CSR that differs from a valid one in
// exactly one of them.
func TestReverseCounterpartComparesEveryEdgeField(t *testing.T) {
	if _, err := NewGraphSnapshot(twoEdgeCounterpartInput()); err != nil {
		t.Fatalf("the fixture has to be accepted before a mutation of it means anything: %v", err)
	}

	// Swapping a field between the two reverse rows keeps both CSRs the same
	// length and every target in place, so the only thing left that can reject
	// it is the comparison of that field.
	swap := func(mutate func(first, second *PackedEdge)) func(*GraphSnapshotInput) {
		return func(input *GraphSnapshotInput) {
			mutate(&input.ReverseEdges[0], &input.ReverseEdges[1])
		}
	}
	cases := map[string]func(*GraphSnapshotInput){
		"target": func(input *GraphSnapshotInput) {
			// Both forward edges now land on symbol 1, so the row filed under
			// symbol 2 has no counterpart -- unless the target is ignored.
			input.ForwardEdges[1].Target = 1
		},
		"evidence": swap(func(first, second *PackedEdge) {
			first.Evidence, second.Evidence = second.Evidence, first.Evidence
		}),
		"kind": swap(func(first, second *PackedEdge) {
			first.Kind, second.Kind = second.Kind, first.Kind
		}),
		"confidence": swap(func(first, second *PackedEdge) {
			first.Confidence, second.Confidence = second.Confidence, first.Confidence
		}),
		"provenance": swap(func(first, second *PackedEdge) {
			first.Provenance, second.Provenance = second.Provenance, first.Provenance
		}),
		"flags": swap(func(first, second *PackedEdge) {
			first.Flags, second.Flags = second.Flags, first.Flags
		}),
		"one forward edge claimed twice": func(input *GraphSnapshotInput) {
			// Two rows under symbol 1 naming the same edge, and nothing under
			// symbol 2. The count still matches, so what rejects it is that a
			// claimed edge cannot be claimed again.
			input.ReverseOffsets = []uint32{0, 0, 2, 2}
			input.ReverseEdges[1] = input.ReverseEdges[0]
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			input := twoEdgeCounterpartInput()
			mutate(&input)
			if _, err := NewGraphSnapshot(input); !errors.Is(err, ErrInvalidGraphSnapshot) {
				t.Fatalf("a reverse CSR wrong only in %q was accepted: err = %v", name, err)
			}
		})
	}
}
