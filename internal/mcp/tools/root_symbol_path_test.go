package tools

import (
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// A selector's path names a file, or a directory to look under. It was matched
// as a string prefix, so the exact path of one file also matched every file
// whose name merely starts the same way -- and a path cut short in the middle
// of a name resolved as if it were whole.
func TestSymbolSelectorPathIsAFileOrADirectoryNotAStringPrefix(t *testing.T) {
	file := func(key, path string) hotsnapshot.FileRow {
		return hotsnapshot.FileRow{Key: key, RepositoryKey: "repo", PackageKey: "pkg", Path: path, Language: "ts"}
	}
	symbol := func(key, fileKey string) hotsnapshot.SymbolRow {
		return hotsnapshot.SymbolRow{
			StableKey: hotsnapshot.StableKey(key), CanonicalIdentity: "ts:" + key, FileKey: fileKey, Language: "ts",
			Name: "format", QualifiedName: "util.format", Kind: "function", StartLine: 1, EndLine: 2,
		}
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repo", Name: "web", Languages: "ts"}},
		Packages:     []hotsnapshot.PackageRow{{Key: "pkg", RepositoryKey: "repo", Language: "ts", Name: "web", ModulePath: "web"}},
		Files: []hotsnapshot.FileRow{
			file("file-ts", "src/util.ts"),
			file("file-tsx", "src/util.tsx"),
			file("file-test", "src/util.ts.snap/format.ts"),
			file("file-other", "srcgen/util.ts"),
		},
		Symbols: []hotsnapshot.SymbolRow{
			symbol("sym-ts", "file-ts"), symbol("sym-tsx", "file-tsx"),
			symbol("sym-snap", "file-test"), symbol("sym-gen", "file-other"),
		},
	}, 61, time.Unix(1_700_000_061, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	resolve := func(path string) (string, string) {
		t.Helper()
		selector, err := normalizeSymbolSelector("", "web", path, "util.format")
		if err != nil {
			t.Fatalf("normalizeSymbolSelector(%q) error = %v", path, err)
		}
		id, err := resolveSymbolSelector(snapshot, selector)
		if err != nil {
			return "", ErrorCode(err)
		}
		record, _ := snapshot.Symbol(id)
		return symbolStableKey(snapshot, record), ""
	}

	cases := []struct {
		path, wantKey, wantCode string
	}{
		// The exact file, which three other paths start with.
		{path: "src/util.ts", wantKey: "sym-ts"},
		{path: "src/util.tsx", wantKey: "sym-tsx"},
		{path: "srcgen/util.ts", wantKey: "sym-gen"},
		// A directory holds what is under it and nothing beside it.
		{path: "srcgen", wantKey: "sym-gen"},
		{path: "src/util.ts.snap", wantKey: "sym-snap"},
		{path: "src", wantCode: CodeAmbiguousSymbol},
		// Not a file and not a directory: a name cut short.
		{path: "src/ut", wantCode: CodeSymbolNotFound},
		{path: "srcg", wantCode: CodeSymbolNotFound},
	}
	for _, test := range cases {
		key, code := resolve(test.path)
		if key != test.wantKey || code != test.wantCode {
			t.Errorf("path %q resolved to key %q, error code %q; want key %q, error code %q",
				test.path, key, code, test.wantKey, test.wantCode)
		}
	}
}
