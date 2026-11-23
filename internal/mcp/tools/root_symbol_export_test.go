package tools

import (
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// A TypeScript `export const x` publishes two symbols that share a qualified
// name: the declaration and the export row that mentions it. A qualified name
// is a question about the declaration, so it resolves to it; the export row is
// still reachable by its stable key. It was answered AMBIGUOUS_SYMBOL, which
// told the caller to narrow by repository and path -- already given.
func TestSymbolSelectorPrefersTheDeclarationOverItsMention(t *testing.T) {
	symbol := func(key, fileKey, qualifiedName, kind string) hotsnapshot.SymbolRow {
		return hotsnapshot.SymbolRow{
			StableKey: hotsnapshot.StableKey(key), CanonicalIdentity: "ts:" + key, FileKey: fileKey, Language: "ts",
			Name: "merge", QualifiedName: qualifiedName, Kind: kind, StartLine: 1, EndLine: 2,
		}
	}
	file := func(key, path string) hotsnapshot.FileRow {
		return hotsnapshot.FileRow{Key: key, RepositoryKey: "repo", PackageKey: "pkg", Path: path, Language: "ts"}
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repo", Name: "web", Languages: "ts"}},
		Packages:     []hotsnapshot.PackageRow{{Key: "pkg", RepositoryKey: "repo", Language: "ts", Name: "web", ModulePath: "web"}},
		Files:        []hotsnapshot.FileRow{file("file-a", "src/a.ts"), file("file-b", "src/b.ts"), file("file-c", "src/c.ts")},
		Symbols: []hotsnapshot.SymbolRow{
			// One declaration and its export row.
			symbol("a-declaration", "file-a", "a.merge", "variable"),
			symbol("a-export", "file-a", "a.merge", "export"),
			// Two declarations and an export row: still ambiguous, between the two.
			symbol("b-declaration", "file-b", "shared.merge", "function"),
			symbol("c-declaration", "file-c", "shared.merge", "function"),
			symbol("b-export", "file-b", "shared.merge", "export"),
			// Only an export row: it is all there is to answer with.
			symbol("only-export", "file-c", "c.only", "export"),
		},
	}, 62, time.Unix(1_700_000_062, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	resolve := func(qualifiedName string) (string, string, string) {
		t.Helper()
		selector, err := normalizeSymbolSelector("", "web", "", qualifiedName)
		if err != nil {
			t.Fatalf("normalizeSymbolSelector(%q) error = %v", qualifiedName, err)
		}
		id, err := resolveSymbolSelector(snapshot, selector)
		if err != nil {
			return "", ErrorCode(err), err.Error()
		}
		record, _ := snapshot.Symbol(id)
		return symbolStableKey(snapshot, record), "", ""
	}

	if key, code, _ := resolve("a.merge"); key != "a-declaration" || code != "" {
		t.Errorf("a.merge resolved to %q (%q), want a-declaration", key, code)
	}
	if key, code, _ := resolve("c.only"); key != "only-export" || code != "" {
		t.Errorf("c.only resolved to %q (%q), want only-export", key, code)
	}
	key, code, message := resolve("shared.merge")
	if key != "" || code != CodeAmbiguousSymbol {
		t.Fatalf("shared.merge resolved to %q (%q), want %s", key, code, CodeAmbiguousSymbol)
	}
	// The export row is not offered as a third candidate, and the count says two.
	if want := `AMBIGUOUS_SYMBOL: qualified name "shared.merge" names 2 symbols; narrow with repository and path: web src/b.ts:1-2, web src/c.ts:1-2`; message != want {
		t.Errorf("message = %q, want %q", message, want)
	}

	selector, err := normalizeSymbolSelector("a-export", "", "", "")
	if err != nil {
		t.Fatalf("normalizeSymbolSelector(stable key) error = %v", err)
	}
	if id, err := resolveSymbolSelector(snapshot, selector); err != nil {
		t.Errorf("the export row by its stable key: %v", err)
	} else if record, _ := snapshot.Symbol(id); symbolStableKey(snapshot, record) != "a-export" {
		t.Errorf("the stable key a-export resolved to another symbol")
	}
}
