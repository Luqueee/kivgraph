package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

func TestRustIdentifierUsesUnicodeXID(t *testing.T) {
	for _, s := range []string{"\u0301a", "a\u037a", "\u2e2f", "a-b", "_"} {
		if isRustIdentifier(s) {
			t.Errorf("accepted non-identifier %q", s)
		}
	}
	for _, s := range []string{"a\u0301", "_あ", "\u2118x", "\u2160", "x\u00b7y"} {
		if !isRustIdentifier(s) {
			t.Errorf("rejected Rust identifier %q", s)
		}
	}
}

func TestRustReceiverSelector(t *testing.T) {
	const first = "lines::impl::Cursor::Reader::next"
	const second = "lines::impl::Cursor<'a>::Iterator::next"
	for _, tc := range []struct {
		name              string
		methods, paired   int
		query, path, code string
		labels            []string
	}{
		{"same short owner in another module", 1, 1, "elsewhere::Cursor::next", "", CodeSymbolNotFound, nil},
		{"duplicate canonical name", 2, 2, "Cursor::next", "lines.rs", CodeAmbiguousSymbol, []string{"stable_key key-a", "stable_key key-b", "stable_key alone"}},
		{"duplicate mixed", 2, 1, "Cursor::next", "lines.rs", CodeAmbiguousSymbol, []string{"stable_key key-a", "stable_key key-b", "stable_key alone"}},
		{"two traits", 2, 2, "Cursor::next", "", CodeAmbiguousSymbol, []string{first, second}},
		{"mixed evidence", 2, 1, "Cursor::next", "", CodeAmbiguousSymbol, []string{first, second, "unproven"}},
		{"suggestion only", 2, 0, "Cursor::next", "", CodeSymbolNotFound, []string{first, second, "unproven"}},
		{"wrong receiver", 1, 1, "Other::next", "", CodeSymbolNotFound, nil},
		{"wrong file", 1, 1, "Cursor::next", "other.rs", CodeSymbolNotFound, nil},
		{"canonical despite ambiguity", 2, 2, first, "", "", nil},
		{"verified receiver", 1, 1, "Cursor::next", "lines.rs", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := hotsnapshot.LadybugSnapshotRows{
				Repositories: []hotsnapshot.RepositoryRow{{Key: "repo", Name: "crate", Languages: "rust"}},
				Packages:     []hotsnapshot.PackageRow{{Key: "pkg", RepositoryKey: "repo", Language: "rust", Name: "crate", ModulePath: "crate"}},
				Files:        []hotsnapshot.FileRow{{Key: "f", RepositoryKey: "repo", PackageKey: "pkg", Language: "rust", Path: "lines.rs"}},
				Symbols:      []hotsnapshot.SymbolRow{{StableKey: "owner", CanonicalIdentity: "rust:owner", FileKey: "f", Language: "rust", Kind: "struct", Name: "Cursor", QualifiedName: "lines::Cursor", StartLine: 1, EndLine: 1}},
			}
			for i, qn := range []string{first, second}[:tc.methods] {
				key := hotsnapshot.StableKey(qn)
				if strings.HasPrefix(tc.name, "duplicate") {
					qn, key = first, []hotsnapshot.StableKey{"key-a", "key-b"}[i]
				}
				rows.Symbols = append(rows.Symbols, hotsnapshot.SymbolRow{StableKey: key, CanonicalIdentity: "rust:" + string(key), FileKey: "f", Language: "rust", Kind: "method", Name: "next", QualifiedName: qn, StartLine: uint32(i + 2), EndLine: uint32(i + 2)})
				if i < tc.paired {
					rows.Edges = append(rows.Edges, hotsnapshot.EdgeRow{SourceKey: key, TargetKey: "owner", Kind: facts.CodeMethodOf, Confidence: facts.CodeStructuralCertain, Provenance: facts.CodeRustAnalyzerDefinition, EvidenceKind: "scip", EvidenceSourceFileKey: "f", EvidenceTargetFileKey: "f"})
				}
			}
			s, err := hotsnapshot.BuildGraphSnapshot(rows, 1, time.Unix(1, 0), 1)
			if err != nil {
				t.Fatal(err)
			}
			id, err := resolveSymbolSelector(s, symbolSelector{Repository: "crate", Path: tc.path, QualifiedName: tc.query})
			if tc.code != "" {
				if err == nil || ErrorCode(err) != tc.code {
					t.Fatalf("want %s; got %v", tc.code, err)
				}
				if tc.query == "elsewhere::Cursor::next" && strings.Contains(err.Error(), first) {
					t.Fatalf("suggested a method proven to belong to a different owner: %v", err)
				}
				for _, label := range tc.labels {
					if !strings.Contains(err.Error(), label) {
						t.Errorf("missing %q in %v", label, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			method, _ := s.Symbol(id)
			if got := symbolStableKey(s, method); got != first {
				t.Fatalf("resolved to %q instead of %q", got, first)
			}
		})
	}
}

func TestRustAmbiguityBoundsExamplesNotCount(t *testing.T) {
	var names []string
	for _, trait := range []string{"A", "B", "C", "D", "E", "F", "G"} {
		names = append(names, "lines::impl::Cursor::"+trait+"::next")
	}
	s := rustCursorSnapshot(t, 1, "crate", true, names...).Load()
	_, err := resolveSymbolSelector(s, symbolSelector{QualifiedName: "Cursor::next"})
	if ErrorCode(err) != CodeAmbiguousSymbol || !strings.Contains(err.Error(), "7 Rust methods") ||
		!strings.Contains(err.Error(), "(+2 more)") || strings.Count(err.Error(), "lines::impl::Cursor::") != 5 {
		t.Fatalf("expected five examples and the full count of seven: %v", err)
	}
}
