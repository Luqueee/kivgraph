package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestReferencePreviewKeepsOnlyVerifiedDistinctExactLocations(t *testing.T) {
	store, root := sourceSnapshot(t, 91)
	row := ReferenceSummary{Repository: "alpha", FilePath: "set.go", StartLine: 5, Confidence: "EXACT_TYPECHECKED"}
	candidate := row
	candidate.Confidence = "CANDIDATE"
	candidate.StartLine = 7
	rows := []ReferenceSummary{candidate, row, row}
	read := func() []ReferencePreview {
		t.Helper()
		previews, trimmed := referenceEvidence(t.Context(), rows, map[string]*evidenceSource{"": newEvidenceSource(store.Load())})
		if len(previews) != 1 || trimmed != 0 {
			t.Fatalf("previews = %+v, trimmed = %d", previews, trimmed)
		}
		return previews
	}
	got := read()[0]
	if got.StartLine != 5 || got.ShownStartLine != 4 || got.ShownEndLine != 6 || got.Code != "func Merge() {\n\treturn\n}" || got.Unavailable != "" {
		t.Fatalf("preview = %+v", got)
	}
	if err := os.WriteFile(filepath.Join(root, "set.go"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got = read()[0]
	if got.StartLine != 5 || got.Unavailable == "" || got.Code != "" || got.QualifiedName != "" || got.ShownStartLine != 0 || got.ShownEndLine != 0 {
		t.Fatalf("changed file served as verified: %+v", got)
	}
}

// Two previews from two files read one shared source and each shows its own
// expected declaration.
func TestReferencePreviewsShareOneSource(t *testing.T) {
	store, _ := sourceSnapshot(t, 92)
	merge := ReferenceSummary{Repository: "alpha", FilePath: "set.go", StartLine: 5, Confidence: "EXACT_TYPECHECKED"}
	other := ReferenceSummary{Repository: "alpha", FilePath: "other.go", StartLine: 3, Confidence: "EXACT_TYPECHECKED"}
	source := newEvidenceSource(store.Load())
	previews, trimmed := referenceEvidence(t.Context(), []ReferenceSummary{merge, other}, map[string]*evidenceSource{"": source})
	if trimmed != 0 || len(previews) != 2 {
		t.Fatalf("previews = %+v, trimmed = %d", previews, trimmed)
	}
	if previews[0].QualifiedName != "pkg.Merge" || previews[0].Code != "func Merge() {\n\treturn\n}" {
		t.Fatalf("first preview = %+v", previews[0])
	}
	if previews[1].QualifiedName != "other.Other" || previews[1].Code != "func Other() {\n}" || previews[1].Unavailable != "" {
		t.Fatalf("second preview = %+v", previews[1])
	}
}

func TestReferencePreviewWholeCallableByteBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra int
	}{{"at cap", 0}, {"one byte over", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			root := testsupport.TempDir(t)
			lines := []string{"func caller() {", "", "", "", "", "", "  target()", "", "", "", "", "", "}"}
			lines[1] = "// é" + strings.Repeat(" ", MaximumReferenceEvidenceBytes+tc.extra-len("// é")-len(strings.Join(lines, "\n")))
			body := strings.Join(lines, "\n")
			text := "package p\n" + body + "\n"
			if err := os.WriteFile(filepath.Join(root, "p.go"), []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			rows := hotsnapshot.LadybugSnapshotRows{
				Repositories: []hotsnapshot.RepositoryRow{{Key: "r", Name: "r", Path: root, Languages: "go"}},
				Packages:     []hotsnapshot.PackageRow{{Key: "p", RepositoryKey: "r", Name: "p", Language: "go"}},
				Files:        []hotsnapshot.FileRow{{Key: "f", RepositoryKey: "r", PackageKey: "p", Path: "p.go", Language: "go", ContentHash: digestOf(text)}},
				Symbols:      []hotsnapshot.SymbolRow{{StableKey: "s", CanonicalIdentity: "go:caller", FileKey: "f", Name: "caller", QualifiedName: "p.caller", Language: "go", Kind: "func", StartLine: 2, EndLine: 14}},
			}
			snap, err := hotsnapshot.BuildGraphSnapshot(rows, 1, time.Now(), 1)
			if err != nil {
				t.Fatal(err)
			}
			p, err := newEvidenceSource(snap).preview(t.Context(), ReferenceSummary{Repository: "r", FilePath: "p.go", StartLine: 8})
			if err != nil {
				t.Fatal(err)
			}
			if tc.extra == 0 {
				if p.Code != body || p.ShownStartLine != 2 || p.ShownEndLine != 14 || p.CallableStartLine != 0 || p.CallableEndLine != 0 {
					t.Fatalf("whole callable not served at byte cap: %+v", p)
				}
			} else if p.Code != strings.Join(lines[2:11], "\n") || p.ShownStartLine != 4 || p.ShownEndLine != 12 || p.CallableStartLine != 2 || p.CallableEndLine != 14 {
				t.Fatalf("oversized callable not identified as partial: %+v", p)
			}
			store := hotsnapshot.NewSnapshotStore(snap)
			readDefinition := func() *ReferencePreview {
				t.Helper()
				_, response, err := findReferences(t.Context(), nil, FindReferencesInput{Name: "caller", IncludeEvidence: true}, store)
				if err != nil {
					t.Fatal(err)
				}
				return response.Results.Definition
			}
			def := readDefinition()
			if tc.extra != 0 {
				if def != nil {
					t.Fatal("oversized definition returned")
				}
				return
			}
			if def == nil || def.Code != body || def.ShownStartLine != 2 || def.ShownEndLine != 14 || def.QualifiedName != "p.caller" {
				t.Fatalf("definition without incoming rows: %+v", def)
			}
			if err := os.WriteFile(filepath.Join(root, "p.go"), []byte("package p\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if readDefinition() != nil {
				t.Fatal("changed source was served as the indexed definition")
			}
		})
	}
}
