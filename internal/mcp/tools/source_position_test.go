package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestPositionRequiresUnchangedSnapshotAndFile(t *testing.T) {
	store, root := sourceSnapshot(t, 91)
	line := uint32(5)
	pos := SourceRequest{Repository: "alpha", Path: "set.go", Line: &line, Publication: publicationOf(store)}
	call := func(q SourceRequest) (Response[SourceResult], error) {
		_, r, err := getSource(t.Context(), nil, GetSourceInput{Symbols: []SourceRequest{{StableKey: "sym-other"}, q}}, store)
		return r, err
	}
	stale, _ := sourceSnapshot(t, 90)
	replaced := store.Load().Metadata()
	replaced.CreatedAt = replaced.CreatedAt.Add(-time.Hour)
	for _, tc := range []struct {
		name, code string
		change     func(*SourceRequest)
	}{
		{"missing publication", CodeInvalidArgument, func(q *SourceRequest) { q.Publication = "" }},
		{"missing file", CodeInvalidArgument, func(q *SourceRequest) { q.Path = "" }},
		{"mixed selectors", CodeInvalidArgument, func(q *SourceRequest) { q.StableKey = "sym-helper" }},
		{"bad path", CodeInvalidArgument, func(q *SourceRequest) { q.Path = "../set.go" }},
		{"zero line", CodeInvalidArgument, func(q *SourceRequest) { q.Line = new(uint32) }},
		{"old snapshot", CodeSnapshotUnavailable, func(q *SourceRequest) { q.Publication = publicationOf(stale) }},
		{"reused generation", CodeSnapshotUnavailable, func(q *SourceRequest) { q.Publication = publicationToken(replaced) }},
		{"malformed publication", CodeSnapshotUnavailable, func(q *SourceRequest) { q.Publication = "3.1dk8sq0g9x5k" }},
		{"outside function", CodeSymbolNotFound, func(q *SourceRequest) { q.Line = new(uint32(7)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := pos
			tc.change(&q)
			r, err := call(q)
			if err == nil || ErrorCode(err) != tc.code || len(r.Results.Bodies) != 0 {
				t.Fatalf("partial or incorrect refusal: %+v, %v", r, err)
			}
		})
	}
	r, err := call(pos)
	if err != nil || len(r.Results.Bodies) != 2 || r.Results.Bodies[1].Code != "func Merge() {\n\treturn\n}" {
		t.Fatalf("mixed batch: %+v, %v", r, err)
	}
	if err := os.WriteFile(filepath.Join(root, "set.go"), []byte("package pkg\n\nfunc Merge() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = call(pos); err == nil || ErrorCode(err) != CodeSnapshotUnavailable {
		t.Fatalf("changed file: %v", err)
	}
}

func TestPositionRefusesRangeEndingOnTerminalNewlineSentinel(t *testing.T) {
	store := rangeSnapshot(t, "func Eof() {\n}\n", 1, 3)
	_, response, err := getSource(t.Context(), nil, GetSourceInput{Symbols: []SourceRequest{{
		Repository: "alpha", Path: "eof.go", Line: new(uint32(1)), Publication: publicationOf(store),
	}}}, store)
	if ErrorCode(err) != CodeSnapshotUnavailable || len(response.Results.Bodies) != 0 {
		t.Fatalf("served invalid callable range: %+v, %v", response.Results, err)
	}
}

func TestPositionInsideRustAssociatedFunction(t *testing.T) {
	root := testsupport.TempDir(t)
	text := "struct Cursor;\nimpl Cursor {\n fn new() -> Self {\n  Self\n }\n}\n"
	if err := os.WriteFile(filepath.Join(root, "lib.rs"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "r", Name: "cursor", Path: root, Languages: "rust"}},
		Packages:     []hotsnapshot.PackageRow{{Key: "p", RepositoryKey: "r", Language: "rust", Name: "cursor"}},
		Files:        []hotsnapshot.FileRow{{Key: "f", RepositoryKey: "r", PackageKey: "p", Path: "lib.rs", Language: "rust", ContentHash: digestOf(text)}},
		Symbols:      []hotsnapshot.SymbolRow{{StableKey: "new", CanonicalIdentity: "rust:Cursor:new", FileKey: "f", Language: "rust", Name: "new", QualifiedName: "impl::Cursor::new", Kind: "static_method", StartLine: 3, EndLine: 5}},
	}
	snap, err := hotsnapshot.BuildGraphSnapshot(rows, 1, time.Now(), 1)
	if err != nil {
		t.Fatal(err)
	}
	q := SourceRequest{Repository: "cursor", Path: "lib.rs", Line: new(uint32(4)), Publication: publicationToken(snap.Metadata())}
	_, r, err := getSource(t.Context(), nil, GetSourceInput{Symbols: []SourceRequest{q}}, hotsnapshot.NewSnapshotStore(snap))
	if err != nil || len(r.Results.Bodies) != 1 || r.Results.Bodies[0].QualifiedName != "impl::Cursor::new" {
		t.Fatalf("Rust associated function: %+v, %v", r, err)
	}
}

func TestPublicationTokenTracksPublicationNotProcessIdentity(t *testing.T) {
	published := time.Unix(1_700_000_000, 0).UTC()
	first := hotsnapshot.SnapshotMetadata{ID: 1, CreatedAt: published}
	// A rebuild reuses the numeric generation; only its publication time tells them apart.
	rebuilt := hotsnapshot.SnapshotMetadata{ID: 1, CreatedAt: published.Add(time.Minute)}
	if publicationToken(first) == publicationToken(rebuilt) {
		t.Fatal("rebuilt snapshot with a reused id kept the old publication token")
	}
	// A remap reads the same header back, so the token must not move.
	remapped := hotsnapshot.SnapshotMetadata{ID: 1, CreatedAt: published}
	if publicationToken(first) != publicationToken(remapped) {
		t.Fatal("remapped publication changed its token")
	}
}

func TestPositionSelectsInnermostArrowNotCallbackArgument(t *testing.T) {
	root := testsupport.TempDir(t)
	text := `export const outer = () => {
 const inner = <() => number>((() => {
  const value = <() => number>((use(() => 1))! as (() => number));
  return value();
 }))! satisfies (() => number) as (() => number);
 return inner();
};
const a = () => 1, b = () => 2;
`
	if err := os.WriteFile(filepath.Join(root, "body.ts"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	rows := hotsnapshot.LadybugSnapshotRows{Repositories: []hotsnapshot.RepositoryRow{{Key: "r", Name: "ky", Path: root, Languages: "typescript"}}, Packages: []hotsnapshot.PackageRow{{Key: "p", RepositoryKey: "r", Language: "typescript", Name: "ky"}}, Files: []hotsnapshot.FileRow{{Key: "f", RepositoryKey: "r", PackageKey: "p", Path: "body.ts", Language: "typescript", ContentHash: digestOf(text)}}}
	for _, s := range []struct {
		name       string
		start, end uint32
	}{{"outer", 1, 7}, {"inner", 2, 5}, {"value", 3, 3}, {"a", 8, 8}, {"b", 8, 8}} {
		rows.Symbols = append(rows.Symbols, hotsnapshot.SymbolRow{
			StableKey: hotsnapshot.StableKey(s.name), CanonicalIdentity: "ts:" + s.name,
			FileKey: "f", Language: "typescript", Name: s.name, QualifiedName: s.name,
			Kind: "variable", StartLine: s.start, EndLine: s.end,
		})
	}
	snap, err := hotsnapshot.BuildGraphSnapshot(rows, 1, time.Now(), 1)
	if err != nil {
		t.Fatal(err)
	}
	store := hotsnapshot.NewSnapshotStore(snap)
	for _, tc := range []struct {
		line uint32
		want string
	}{{3, "inner"}, {6, "outer"}, {8, ""}} {
		_, r, err := getSource(t.Context(), nil, GetSourceInput{Symbols: []SourceRequest{{Repository: "ky", Path: "body.ts", Line: &tc.line, Publication: publicationOf(store)}}}, store)
		if tc.want == "" {
			if err == nil || ErrorCode(err) != CodeAmbiguousSymbol {
				t.Fatalf("same-line bindings: %v", err)
			}
			continue
		}
		if err != nil || len(r.Results.Bodies) != 1 || r.Results.Bodies[0].QualifiedName != tc.want {
			t.Fatalf("line %d: %+v, %v", tc.line, r, err)
		}
	}
}

func TestPositionRefusesImplicitProfileEvenWhenIDsCoincide(t *testing.T) {
	a, _ := sourceSnapshot(t, 91)
	b, _ := sourceSnapshot(t, 91)
	aggregate, err := hotsnapshot.NewProfileSnapshotStore("first", map[string]*hotsnapshot.SnapshotStore{"first": a, "second": b})
	if err != nil {
		t.Fatal(err)
	}
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "position-test", Version: "1"}, nil)
	RegisterGetSourceWithSnapshotStore(srv, aggregate)
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "client", Version: "1"}, nil)
	ct, st := sdkmcp.NewInMemoryTransports()
	ss, err := srv.Connect(t.Context(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := client.Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, p := range [][]string{nil, {"*"}, {"first", "second"}, {"second"}} {
		r, err := cs.CallTool(t.Context(), &sdkmcp.CallToolParams{Name: "get_source", Arguments: GetSourceInput{Profile: p, Symbols: []SourceRequest{{Repository: "alpha", Path: "set.go", Line: new(uint32(5)), Publication: publicationToken(b.Load().Metadata())}}}})
		if err != nil {
			t.Fatalf("profiles %v: %v", p, err)
		}
		if r == nil || len(r.Content) != 1 {
			t.Fatalf("profiles %v: expected one response block, got %+v", p, r)
		}
		block, ok := r.Content[0].(*sdkmcp.TextContent)
		if !ok || block == nil {
			t.Fatalf("profiles %v: expected text, got %T", p, r.Content[0])
		}
		text := block.Text
		if len(p) == 1 && p[0] == "second" {
			if r.IsError || !strings.Contains(text, "func Merge()") {
				t.Fatalf("profiles %v: %s", p, text)
			}
		} else if !r.IsError || !strings.Contains(text, "one named profile") {
			t.Fatalf("profiles %v: %s", p, text)
		}
	}
}
