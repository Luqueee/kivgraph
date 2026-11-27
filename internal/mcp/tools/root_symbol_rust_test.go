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

func TestRustRawIdentifierRules(t *testing.T) {
	for _, s := range []string{"r#type", "r#match", "r#async", "r#Café"} {
		if !isRustIdentifier(s) {
			t.Errorf("rejected raw identifier %q", s)
		}
	}
	for _, s := range []string{"r#", "r#_", "r#self", "r#super", "r#crate", "r#Self", "r#1x", "r#a-b", "r#r#x"} {
		if isRustIdentifier(s) {
			t.Errorf("accepted prohibited raw identifier %q", s)
		}
	}
}

// rustSpellingSnapshot is one Rust struct named owner, with one method per
// [name, canonical name] pair. Each method has the analyzer's METHOD_OF edge,
// so the receiver is proven and only its spelling is under test.
func rustSpellingSnapshot(t *testing.T, owner string, methods ...[2]string) *hotsnapshot.SnapshotStore {
	t.Helper()
	return rustSpellingSnapshotOfKind(t, "method", owner, methods...)
}

// rustSpellingSnapshotOfKind is rustSpellingSnapshot with each member declared
// as kind: `method`, or `static_method` for an associated function without self.
func rustSpellingSnapshotOfKind(t *testing.T, kind, owner string, methods ...[2]string) *hotsnapshot.SnapshotStore {
	t.Helper()
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repo", Name: "crate", Path: "/crate", Languages: "rust"}},
		Packages:     []hotsnapshot.PackageRow{{Key: "pkg", RepositoryKey: "repo", Language: "rust", Name: "crate", ModulePath: "crate"}},
		Files:        []hotsnapshot.FileRow{{Key: "f", RepositoryKey: "repo", PackageKey: "pkg", Language: "rust", Path: "lines.rs"}},
		Symbols:      []hotsnapshot.SymbolRow{{StableKey: "owner", CanonicalIdentity: "rust:owner", FileKey: "f", Language: "rust", Kind: "struct", Name: owner, QualifiedName: "lines::" + owner, StartLine: 1, EndLine: 1}},
	}
	for i, m := range methods {
		key := hotsnapshot.StableKey(m[1])
		rows.Symbols = append(rows.Symbols, hotsnapshot.SymbolRow{StableKey: key, CanonicalIdentity: "rust:" + m[1], FileKey: "f", Language: "rust", Kind: kind, Name: m[0], QualifiedName: m[1], StartLine: uint32(i + 2), EndLine: uint32(i + 2)})
		rows.Edges = append(rows.Edges, hotsnapshot.EdgeRow{SourceKey: key, TargetKey: "owner", Kind: facts.CodeMethodOf, Confidence: facts.CodeStructuralCertain, Provenance: facts.CodeRustAnalyzerDefinition, EvidenceKind: "scip", EvidenceSourceFileKey: "f", EvidenceTargetFileKey: "f"})
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, 1, time.Unix(1, 0), 1)
	if err != nil {
		t.Fatal(err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

func TestRustSelectorRawIdentifiers(t *testing.T) {
	const method = "lines::impl::r#type::r#match"
	s := rustSpellingSnapshot(t, "r#type", [2]string{"r#match", method}).Load()
	for _, query := range []string{"type::match", "r#type::r#match", "type::r#match", "r#type::match"} {
		id, err := resolveSymbolSelector(s, symbolSelector{Repository: "crate", QualifiedName: query})
		if err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		resolved, _ := s.Symbol(id)
		if got := symbolStableKey(s, resolved); got != method {
			t.Fatalf("%q resolved to %q instead of %q", query, got, method)
		}
	}
	for _, query := range []string{"r#self::match", "r#super::match", "r#crate::match", "r#Self::match", "r#_::match", "r#::match", "type::r#"} {
		if _, err := resolveSymbolSelector(s, symbolSelector{Repository: "crate", QualifiedName: query}); ErrorCode(err) != CodeSymbolNotFound {
			t.Fatalf("%q: want %s; got %v", query, CodeSymbolNotFound, err)
		}
	}
}

func TestRustSelectorNormalizesNFC(t *testing.T) {
	const method = "lines::impl::Cafe\u0301::na\u00efve"
	s := rustSpellingSnapshot(t, "Cafe\u0301", [2]string{"na\u00efve", method}).Load()
	for _, query := range []string{"Caf\u00e9::na\u00efve", "Cafe\u0301::nai\u0308ve", "Caf\u00e9::nai\u0308ve", "Cafe\u0301::na\u00efve"} {
		id, err := resolveSymbolSelector(s, symbolSelector{Repository: "crate", QualifiedName: query})
		if err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		resolved, _ := s.Symbol(id)
		if got := symbolStableKey(s, resolved); got != method {
			t.Fatalf("%q resolved to %q instead of %q", query, got, method)
		}
	}
}

func TestRustSelectorNormalizedCollisionIsAmbiguous(t *testing.T) {
	s := rustSpellingSnapshot(
		t, "Caf\u00e9",
		[2]string{"na\u00efve", "lines::impl::Caf\u00e9::na\u00efve"},
		[2]string{"nai\u0308ve", "lines::impl::Cafe\u0301::nai\u0308ve"},
	).Load()
	_, err := resolveSymbolSelector(s, symbolSelector{Repository: "crate", QualifiedName: "Caf\u00e9::na\u00efve"})
	if ErrorCode(err) != CodeAmbiguousSymbol {
		t.Fatalf("want %s for two methods equal after NFC; got %v", CodeAmbiguousSymbol, err)
	}
}

func TestRustReceiverResolvesStaticMethod(t *testing.T) {
	// The loader publishes an associated function without self as
	// static_method, and the analyzer still pairs it with its type by METHOD_OF.
	const fn = "lines::impl::Cursor::new"
	s := rustSpellingSnapshotOfKind(t, "static_method", "Cursor", [2]string{"new", fn}).Load()
	id, err := resolveSymbolSelector(s, symbolSelector{Repository: "crate", QualifiedName: "Cursor::new"})
	if err != nil {
		t.Fatal(err)
	}
	resolved, _ := s.Symbol(id)
	if got := symbolStableKey(s, resolved); got != fn {
		t.Fatalf("resolved to %q instead of %q", got, fn)
	}
}
