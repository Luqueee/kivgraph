package tools

import (
	"context"
	"testing"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// include_derived changes which rows an answer holds, so a cursor minted with
// one value is an offset into a different set under the other. It has to be
// refused, the way a cursor is under any other changed filter: resuming would
// skip or repeat rows and say nothing.
func TestCursorIsBoundToIncludeDerived(t *testing.T) {
	type page struct {
		cursor *string
		err    error
	}
	tools := []struct {
		name string
		call func(store *hotsnapshot.SnapshotStore, derived bool, cursor string) page
		open func(t *testing.T) *hotsnapshot.SnapshotStore
	}{
		{
			name: "find_references",
			open: func(t *testing.T) *hotsnapshot.SnapshotStore { return referenceSnapshot(t, 201) },
			call: func(store *hotsnapshot.SnapshotStore, derived bool, cursor string) page {
				_, response, err := findReferences(context.Background(), nil, FindReferencesInput{
					StableKey: "symbol-target", Direction: FindReferencesDirectionIncoming,
					Limit: 1, IncludeDerived: derived, Cursor: cursor,
				}, store)
				return page{response.NextCursor, err}
			},
		},
		{
			name: "find_symbol",
			open: func(t *testing.T) *hotsnapshot.SnapshotStore { return symbolSnapshot(t, 202) },
			call: func(store *hotsnapshot.SnapshotStore, derived bool, cursor string) page {
				_, response, err := findSymbol(context.Background(), nil, FindSymbolInput{
					Name: "alp", Mode: FindSymbolModePrefix, Limit: 1, IncludeDerived: derived, Cursor: cursor,
				}, store)
				return page{response.NextCursor, err}
			},
		},
		{
			name: "get_blast_radius",
			open: func(t *testing.T) *hotsnapshot.SnapshotStore { return blastRadiusStore(t, 203) },
			call: func(store *hotsnapshot.SnapshotStore, derived bool, cursor string) page {
				_, response, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
					StableKey: "sym-core", Limit: 1, IncludeDerived: derived, Cursor: cursor,
				}, store)
				return page{response.NextCursor, err}
			},
		},
		{
			name: "trace_dependencies",
			open: func(t *testing.T) *hotsnapshot.SnapshotStore { return traceDependenciesStore(t, 204) },
			call: func(store *hotsnapshot.SnapshotStore, derived bool, cursor string) page {
				_, response, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
					StableKey: "sym-root", Limit: 1, IncludeDerived: derived, Cursor: cursor,
				}, store)
				return page{response.NextCursor, err}
			},
		},
	}
	for _, tool := range tools {
		t.Run(tool.name, func(t *testing.T) {
			store := tool.open(t)
			for _, derived := range []bool{false, true} {
				first := tool.call(store, derived, "")
				if first.err != nil || first.cursor == nil {
					t.Fatalf("first page (include_derived=%v): cursor = %v, err = %v, want a cursor", derived, first.cursor, first.err)
				}
				// Unchanged, the cursor still resumes.
				if same := tool.call(store, derived, *first.cursor); same.err != nil {
					t.Fatalf("cursor reused with the same include_derived=%v: %v", derived, same.err)
				}
				if flipped := tool.call(store, !derived, *first.cursor); ErrorCode(flipped.err) != CodeCursorInvalid {
					t.Fatalf("cursor minted with include_derived=%v reused with %v: error code = %q (err=%v), want %q",
						derived, !derived, ErrorCode(flipped.err), flipped.err, CodeCursorInvalid)
				}
			}
		})
	}
}
