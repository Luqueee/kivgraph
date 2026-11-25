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
	line, gen := uint32(5), uint64(91)
	pos := SourceRequest{Repository: "alpha", Path: "set.go", Line: &line, SnapshotID: &gen}
	call := func(q SourceRequest) (Response[SourceResult], error) {
		_, r, err := getSource(t.Context(), nil, GetSourceInput{Symbols: []SourceRequest{{StableKey: "sym-other"}, q}}, store)
		return r, err
	}
	for _, tc := range []struct {
		name, code string
		change     func(*SourceRequest)
	}{
		{"missing snapshot", CodeInvalidArgument, func(q *SourceRequest) { q.SnapshotID = nil }},
		{"missing file", CodeInvalidArgument, func(q *SourceRequest) { q.Path = "" }},
		{"mixed selectors", CodeInvalidArgument, func(q *SourceRequest) { q.StableKey = "sym-helper" }},
		{"bad path", CodeInvalidArgument, func(q *SourceRequest) { q.Path = "../set.go" }},
		{"zero line", CodeInvalidArgument, func(q *SourceRequest) { q.Line = new(uint32) }},
		{"old snapshot", CodeSnapshotUnavailable, func(q *SourceRequest) { q.SnapshotID = new(uint64(90)) }},
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

func TestPositionSelectsInnermostArrowNotCallbackArgument(t *testing.T) {
	root := testsupport.TempDir(t)
	text := "export const outer = () => {\n const inner = () => {\n  const value = use(() => 1);\n  return value;\n };\n return inner();\n};\nconst a = () => 1, b = () => 2;\n"
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
		_, r, err := getSource(t.Context(), nil, GetSourceInput{Symbols: []SourceRequest{{Repository: "ky", Path: "body.ts", Line: &tc.line, SnapshotID: new(uint64(1))}}}, store)
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
		r, err := cs.CallTool(t.Context(), &sdkmcp.CallToolParams{Name: "get_source", Arguments: GetSourceInput{Profile: p, Symbols: []SourceRequest{{Repository: "alpha", Path: "set.go", Line: new(uint32(5)), SnapshotID: new(uint64(91))}}}})
		if err != nil {
			t.Fatal(err)
		}
		text := r.Content[0].(*sdkmcp.TextContent).Text
		if len(p) == 1 && p[0] == "second" {
			if r.IsError || !strings.Contains(text, "func Merge()") {
				t.Fatal(text)
			}
		} else if !r.IsError || !strings.Contains(text, "one named profile") {
			t.Fatal(text)
		}
	}
}
