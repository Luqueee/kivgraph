package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

func TestGetFileOutlineDescribesOneFileAndOneDirectory(t *testing.T) {
	client := newFileOutlineToolClient(t, fileOutlineSnapshot(t, 41))

	single := callFileOutline(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts/facts.go",
	})
	if len(single.Results.Files) != 1 || single.Total != 2 || single.Returned != 2 {
		t.Fatalf("single file outline = %#v", single)
	}
	if single.Results.Files[0].Path != "internal/facts/facts.go" {
		t.Fatalf("group path = %#v", single.Results.Files[0])
	}
	first := single.Results.Files[0].Symbols[0]
	if first.Name != "Merge" || first.Kind != "method" || first.Signature != "func (Set) Merge(Set)" {
		t.Fatalf("first declaration = %#v", first)
	}
	if first.StartLine != 10 || first.EndLine != 20 {
		t.Fatalf("first location = %#v", first)
	}
	if !first.Exported {
		t.Fatalf("Merge must be reported as exported: %#v", first)
	}
	// The concise default withholds the identifiers a caller can rebuild from
	// the group path and the name beside it.
	if first.CanonicalIdentity != "" || first.StableKey != "" {
		t.Fatalf("concise row carries derived identifiers = %#v", first)
	}
	if len(single.Results.Languages) != 1 || single.Results.Languages[0] != "go" {
		t.Fatalf("languages = %#v", single.Results.Languages)
	}

	// The same argument answers the directory question: one path, two
	// granularities, one tool. The path is stated once per group, not per row.
	directory := callFileOutline(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts",
	})
	if len(directory.Results.Files) != 2 || directory.Total != 3 {
		t.Fatalf("directory outline = %#v", directory.Results)
	}
	for _, group := range directory.Results.Files {
		if group.Path == "" || len(group.Symbols) == 0 {
			t.Fatalf("directory group = %#v, want a path and its declarations", group)
		}
	}

	// A trailing slash is the same directory.
	slashed := callFileOutline(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts/",
	})
	if len(slashed.Results.Files) != len(directory.Results.Files) || slashed.Total != directory.Total {
		t.Fatalf("trailing slash outline = %#v, want the same as %#v", slashed.Results, directory.Results)
	}
}

// TestGetFileOutlineDirectoryPrefixNeedsTheSeparator guards the boundary that
// a naive prefix match gets wrong: `internal/facts` is not a prefix question
// about `internal/factsheet.go`, it is a directory.
func TestGetFileOutlineDirectoryPrefixNeedsTheSeparator(t *testing.T) {
	client := newFileOutlineToolClient(t, fileOutlineSnapshot(t, 42))
	outline := callFileOutline(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts",
	})
	for _, group := range outline.Results.Files {
		if group.Path == "internal/factsheet.go" {
			t.Fatalf("directory outline reached a sibling file: %#v", group)
		}
	}
}

func TestGetFileOutlineFiltersByKindAndRestoresDetail(t *testing.T) {
	client := newFileOutlineToolClient(t, fileOutlineSnapshot(t, 43))

	methods := callFileOutline(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts", "kind": "method",
	})
	for _, symbol := range outlineSymbols(methods.Results) {
		if symbol.Kind != "method" {
			t.Fatalf("kind filter kept %#v", symbol)
		}
	}
	if methods.Returned == 0 {
		t.Fatal("kind filter kept nothing, want the one method")
	}

	detailed := callFileOutline(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts/facts.go",
		"response_format": ResponseFormatDetailed,
	})
	detailedRows := outlineSymbols(detailed.Results)
	if detailedRows[0].CanonicalIdentity == "" || detailedRows[0].StableKey == "" {
		t.Fatalf("detailed row = %#v, want the derived identifiers back", detailedRows[0])
	}
}

// TestGetFileOutlineNamesWhatItCouldNotFind is the difference between "there
// is nothing declared here" and "I do not know this path". An empty page
// reads as the first and would be a lie.
func TestGetFileOutlineNamesWhatItCouldNotFind(t *testing.T) {
	store := fileOutlineSnapshot(t, 44)
	cases := []struct {
		name      string
		arguments GetFileOutlineInput
		wantCode  string
	}{
		{
			name:      "unknown repository",
			arguments: GetFileOutlineInput{Repository: "ghost", Path: "internal/facts"},
			wantCode:  CodeRepositoryNotFound,
		},
		{
			name:      "unknown path",
			arguments: GetFileOutlineInput{Repository: "alpha-repo", Path: "internal/ghost"},
			wantCode:  CodeSymbolNotFound,
		},
		{
			name:      "absolute path",
			arguments: GetFileOutlineInput{Repository: "alpha-repo", Path: "/internal/facts"},
			wantCode:  CodeInvalidArgument,
		},
		{
			name:      "traversal",
			arguments: GetFileOutlineInput{Repository: "alpha-repo", Path: "internal/../../etc"},
			wantCode:  CodeInvalidArgument,
		},
		{
			name:      "empty path",
			arguments: GetFileOutlineInput{Repository: "alpha-repo"},
			wantCode:  CodeInvalidArgument,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := getFileOutline(context.Background(), nil, test.arguments, store)
			if ErrorCode(err) != test.wantCode {
				t.Fatalf("error code = %q (%v), want %q", ErrorCode(err), err, test.wantCode)
			}
		})
	}

	if _, _, err := getFileOutline(context.Background(), nil, GetFileOutlineInput{
		Repository: "alpha-repo", Path: "internal/facts",
	}, hotsnapshot.NewSnapshotStore(nil)); ErrorCode(err) != CodeIndexNotReady {
		t.Fatalf("unpublished snapshot error code = %q, want %q", ErrorCode(err), CodeIndexNotReady)
	}
}

func TestGetFileOutlineIsRegisteredReadOnly(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterGetFileOutline(server)
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	listed, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == fileOutlineToolName {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("get_file_outline annotations = %#v, want read-only", tool.Annotations)
			}
			return
		}
	}
	t.Fatal("get_file_outline is not registered")
}

// callFileOutline asks for the row-per-declaration shape unless the test names
// a view: the tests that use it are about which declarations are reported, and
// only the two payload tests are about how they are spelled.
func callFileOutline(t *testing.T, client *sdkmcp.ClientSession, arguments map[string]any) Response[FileOutline] {
	t.Helper()
	if _, named := arguments["view"]; !named {
		arguments["view"] = ViewFull
	}
	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: fileOutlineToolName, Arguments: arguments,
	})
	if err != nil {
		t.Fatalf("get_file_outline CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("get_file_outline CallTool() returned an error: %#v", result.Content)
	}
	response := decodeResponse[FileOutline](t, result)
	return response
}

// fileOutlineText is the payload itself, which is what a view changes.
func fileOutlineText(t *testing.T, client *sdkmcp.ClientSession, arguments map[string]any) string {
	t.Helper()
	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: fileOutlineToolName, Arguments: arguments,
	})
	if err != nil {
		t.Fatalf("get_file_outline CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("get_file_outline CallTool() returned an error: %#v", result.Content)
	}
	if len(result.Content) != 1 {
		t.Fatalf("get_file_outline returned %d content blocks, want exactly one", len(result.Content))
	}
	text, ok := result.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("get_file_outline content block is %T, want text", result.Content[0])
	}
	return text.Text
}

func newFileOutlineToolClient(t *testing.T, store *hotsnapshot.SnapshotStore) *sdkmcp.ClientSession {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterGetFileOutlineWithSnapshotStore(server, store)
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return clientSession
}

// fileOutlineSnapshot holds three files: two under internal/facts and one
// sibling whose path shares the prefix but not the directory.
func fileOutlineSnapshot(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(
		hotsnapshot.LadybugSnapshotRows{
			Repositories: []hotsnapshot.RepositoryRow{
				{Key: "repo-a", Name: "alpha-repo", Path: "/repo-a", Languages: "go"},
			},
			Packages: []hotsnapshot.PackageRow{
				{Key: "package-a", RepositoryKey: "repo-a", Language: "go", Name: "facts", ModulePath: "example.com/pkg"},
			},
			Files: []hotsnapshot.FileRow{
				{Key: "file-facts", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "internal/facts/facts.go", Language: "go"},
				{Key: "file-delta", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "internal/facts/delta.go", Language: "go"},
				{Key: "file-sheet", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "internal/factsheet.go", Language: "go"},
			},
			Symbols: []hotsnapshot.SymbolRow{
				{
					StableKey: "symbol-diff", CanonicalIdentity: "go:facts.Diff", FileKey: "file-delta",
					Language: "go", Name: "Diff", QualifiedName: "facts.Diff", Kind: "func",
					Signature: "func Diff(Set, Set) Delta", Exported: true, StartLine: 5, EndLine: 40,
				},
				{
					StableKey: "symbol-merge", CanonicalIdentity: "go:facts.Set.Merge", FileKey: "file-facts",
					Language: "go", Name: "Merge", QualifiedName: "facts.Set.Merge", Kind: "method",
					Signature: "func (Set) Merge(Set)", Exported: true, StartLine: 10, EndLine: 20,
				},
				{
					StableKey: "symbol-sheet", CanonicalIdentity: "go:facts.Sheet", FileKey: "file-sheet",
					Language: "go", Name: "Sheet", QualifiedName: "facts.Sheet", Kind: "type",
					Signature: "type Sheet struct{}", Exported: true, StartLine: 1, EndLine: 3,
				},
				{
					StableKey: "symbol-merged", CanonicalIdentity: "go:facts.merged", FileKey: "file-facts",
					Language: "go", Name: "merged", QualifiedName: "facts.merged", Kind: "func",
					Signature: "func merged()", StartLine: 30, EndLine: 35,
				},
			},
		},
		id,
		time.Unix(1_700_000_000+int64(id), 0).UTC(),
		1,
	)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

// manyDeclarationsSnapshot has six declarations under one directory: four
// exported functions across two files, and two unexported variables in a
// third. Every declaration shares `repository` and `package`, so kind and
// visibility are the only columns left for compact to hoist or group.
func manyDeclarationsSnapshot(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(
		hotsnapshot.LadybugSnapshotRows{
			Repositories: []hotsnapshot.RepositoryRow{
				{Key: "repo-a", Name: "alpha-repo", Path: "/repo-a", Languages: "go"},
			},
			Packages: []hotsnapshot.PackageRow{
				{Key: "package-a", RepositoryKey: "repo-a", Language: "go", Name: "handlers", ModulePath: "example.com/pkg"},
			},
			Files: []hotsnapshot.FileRow{
				{Key: "file-a", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "handlers/a.go", Language: "go"},
				{Key: "file-b", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "handlers/b.go", Language: "go"},
				{Key: "file-c", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "handlers/c.go", Language: "go"},
			},
			Symbols: []hotsnapshot.SymbolRow{
				{StableKey: "symbol-1", CanonicalIdentity: "go:1", FileKey: "file-a", Language: "go", Name: "HandleOne", QualifiedName: "handlers.HandleOne", Kind: "func", Signature: "func HandleOne()", Exported: true, StartLine: 10, EndLine: 10},
				{StableKey: "symbol-2", CanonicalIdentity: "go:2", FileKey: "file-a", Language: "go", Name: "HandleTwo", QualifiedName: "handlers.HandleTwo", Kind: "func", Signature: "func HandleTwo()", Exported: true, StartLine: 20, EndLine: 20},
				{StableKey: "symbol-3", CanonicalIdentity: "go:3", FileKey: "file-b", Language: "go", Name: "HandleThree", QualifiedName: "handlers.HandleThree", Kind: "func", Signature: "func HandleThree()", Exported: true, StartLine: 30, EndLine: 30},
				{StableKey: "symbol-4", CanonicalIdentity: "go:4", FileKey: "file-b", Language: "go", Name: "HandleFour", QualifiedName: "handlers.HandleFour", Kind: "func", Signature: "func HandleFour()", Exported: true, StartLine: 40, EndLine: 40},
				{StableKey: "symbol-5", CanonicalIdentity: "go:5", FileKey: "file-c", Language: "go", Name: "handleFive", QualifiedName: "handlers.handleFive", Kind: "variable", Signature: "func()", Exported: false, StartLine: 50, EndLine: 50},
				{StableKey: "symbol-6", CanonicalIdentity: "go:6", FileKey: "file-c", Language: "go", Name: "handleSix", QualifiedName: "handlers.handleSix", Kind: "variable", Signature: "func()", Exported: false, StartLine: 60, EndLine: 60},
			},
		},
		id,
		time.Unix(1_700_000_000+int64(id), 0).UTC(),
		1,
	)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

// TestGetFileOutlineCompactGroupsTheMajorityKindOnce is the regression guard
// for the real page that motivated this: a 197-declaration directory outline
// over `kena` had 7 distinct kinds, one covering 132 of them, and repeating
// `kind` on every row cost more than reading the source would have. Here four
// declarations share one (kind, visibility) pair across two files and two
// share another in a third, which is enough to force grouping over the flat
// fallback.
func TestGetFileOutlineCompactGroupsTheMajorityKindOnce(t *testing.T) {
	client := newFileOutlineToolClient(t, manyDeclarationsSnapshot(t, 51))

	wire := fileOutlineText(t, client, map[string]any{"repository": "alpha-repo", "path": "handlers"})
	var payload struct {
		Results struct {
			Repository string `json:"repository"`
			Package    string `json:"package"`
			Files      []any  `json:"files"`
			Groups     []struct {
				Kind     string `json:"kind"`
				Exported *bool  `json:"exported"`
				Files    []struct {
					File string `json:"file"`
					At   []any  `json:"at"`
				} `json:"files"`
			} `json:"groups"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(wire), &payload); err != nil {
		t.Fatalf("unmarshal compact page: %v (%s)", err, wire)
	}
	if payload.Results.Repository != "alpha-repo" || payload.Results.Package != "handlers" {
		t.Fatalf("header = %#v, want repository and package hoisted", payload.Results)
	}
	if payload.Results.Files != nil {
		t.Fatalf("page stayed flat instead of grouping two real kinds: %#v", payload.Results.Files)
	}
	if len(payload.Results.Groups) != 2 {
		t.Fatalf("groups = %#v, want exactly two: func/exported and variable/unexported", payload.Results.Groups)
	}
	rowsIn := func(group int) int {
		total := 0
		for _, file := range payload.Results.Groups[group].Files {
			total += len(file.At)
		}
		return total
	}
	var funcRows, varRows int
	for index, group := range payload.Results.Groups {
		switch group.Kind {
		case "func":
			if group.Exported == nil || !*group.Exported {
				t.Fatalf("func group exported = %v, want true", group.Exported)
			}
			funcRows = rowsIn(index)
			if len(group.Files) != 2 {
				t.Fatalf("func group files = %#v, want two", group.Files)
			}
		case "variable":
			if group.Exported == nil || *group.Exported {
				t.Fatalf("variable group exported = %v, want false", group.Exported)
			}
			varRows = rowsIn(index)
			if len(group.Files) != 1 {
				t.Fatalf("variable group files = %#v, want one", group.Files)
			}
		default:
			t.Fatalf("unexpected group kind %q", group.Kind)
		}
	}
	if funcRows != 4 || varRows != 2 {
		t.Fatalf("func rows = %d, variable rows = %d, want 4 and 2", funcRows, varRows)
	}
	// Every entry inside a group is a bare label: kind and visibility are
	// entirely accounted for by the group, and every name here is implied by
	// its qualified name, so nothing is left for the row to carry.
	for _, group := range payload.Results.Groups {
		for _, file := range group.Files {
			for _, entry := range file.At {
				if _, isArray := entry.([]any); isArray {
					t.Fatalf("group %+v entry = %#v, want a bare label", group, entry)
				}
			}
		}
	}
}

// outlineSymbols flattens the grouped result for assertions that do not care
// which file a declaration came from.
func outlineSymbols(outline FileOutline) []OutlineSymbol {
	rows := make([]OutlineSymbol, 0, len(outline.Files))
	for _, group := range outline.Files {
		rows = append(rows, group.Symbols...)
	}
	return rows
}

// TestGetFileOutlineFullViewKeepsTodaysPayload pins the shape a client that
// asks for `view: "full"` still gets: every envelope field present, and every
// column on every declaration.
func TestGetFileOutlineFullViewKeepsTodaysPayload(t *testing.T) {
	client := newFileOutlineToolClient(t, fileOutlineSnapshot(t, 45))
	text := fileOutlineText(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts/facts.go", "view": ViewFull,
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	// The age is the one field a snapshot cannot state twice in a row.
	if _, ok := payload["snapshot_age_ms"].(float64); !ok {
		t.Fatalf("snapshot_age_ms = %#v, want a number", payload["snapshot_age_ms"])
	}
	payload["snapshot_age_ms"] = "measured"
	want := map[string]any{
		"snapshot_id": float64(45), "snapshot_age_ms": "measured",
		"total": float64(2), "returned": float64(2),
		"truncated": false, "next_cursor": nil,
		"coverage": map[string]any{
			"exact": float64(2), "candidate": float64(0),
			"unresolved_related": float64(0), "package_level": float64(0),
		},
		"results": map[string]any{
			"repository": "alpha-repo", "path": "internal/facts/facts.go",
			"packages": []any{"facts"}, "languages": []any{"go"},
			"files": []any{map[string]any{
				"path": "internal/facts/facts.go",
				"symbols": []any{
					map[string]any{
						"name": "Merge", "kind": "method", "signature": "func (Set) Merge(Set)",
						"exported": true, "start_line": float64(10), "end_line": float64(20),
						"qualified_name": "facts.Set.Merge",
					},
					map[string]any{
						"name": "merged", "kind": "func", "signature": "func merged()",
						"exported": false, "start_line": float64(30), "end_line": float64(35),
						"qualified_name": "facts.merged",
					},
				},
			}},
		},
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("full payload = %s", text)
	}
}

// TestGetFileOutlineCompactViewHoistsAndDropsSignatures is the default answer:
// the same declarations, on the same lines, without the signature that is the
// largest field of a row and without repeating the package on each of them.
func TestGetFileOutlineCompactViewHoistsAndDropsSignatures(t *testing.T) {
	client := newFileOutlineToolClient(t, fileOutlineSnapshot(t, 46))

	compact := fileOutlineText(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts/facts.go",
	})
	wantCompact := `{"snapshot_id":46,"total":2,"returned":2,"coverage":{"exact":2},` +
		`"results":{"repository":"alpha-repo","path":"internal/facts/facts.go","package":"facts",` +
		`"files":[{"file":"internal/facts/facts.go","at":[` +
		`["facts.Set.Merge@10-20","method","exported"],` +
		`["facts.merged@30-35","func","unexported"]]}]}}`
	if compact != wantCompact {
		t.Fatalf("compact payload =\n%s\nwant\n%s", compact, wantCompact)
	}

	// A page whose declarations agree on their kind states it once, and its
	// total counts what it returned: `total` and the rows describe one set, so
	// a filtered page cannot report a file as larger than what it can show.
	methods := fileOutlineText(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts", "kind": "method",
	})
	wantMethods := `{"snapshot_id":46,"total":1,"returned":1,"coverage":{"exact":1},` +
		`"results":{"repository":"alpha-repo","path":"internal/facts","package":"facts",` +
		`"kind":"method","exported":true,` +
		`"files":[{"file":"internal/facts/facts.go","at":["facts.Set.Merge@10-20"]}]}}`
	if methods != wantMethods {
		t.Fatalf("compact kind-filtered payload =\n%s\nwant\n%s", methods, wantMethods)
	}

	// The signature and the identifiers are one argument away, in this view too.
	detailed := fileOutlineText(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts/facts.go",
		"response_format": ResponseFormatDetailed,
	})
	wantDetailed := `{"snapshot_id":46,"total":2,"returned":2,"coverage":{"exact":2},` +
		`"results":{"repository":"alpha-repo","path":"internal/facts/facts.go","package":"facts",` +
		`"files":[{"file":"internal/facts/facts.go","at":[` +
		`["facts.Set.Merge@10-20","method","exported","func (Set) Merge(Set)","symbol-merge","go:facts.Set.Merge"],` +
		`["facts.merged@30-35","func","unexported","func merged()","symbol-merged","go:facts.merged"]]}]}}`
	if detailed != wantDetailed {
		t.Fatalf("compact detailed payload =\n%s\nwant\n%s", detailed, wantDetailed)
	}

	// Same declarations, fewer bytes: that is the whole point of the view.
	verbose := fileOutlineText(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts/facts.go", "view": ViewFull,
	})
	t.Logf("one page of two declarations: full %d bytes, compact %d bytes", len(verbose), len(compact))
	if len(compact) >= len(verbose) {
		t.Fatalf("compact payload is %d bytes and full is %d", len(compact), len(verbose))
	}
}

// TestGetFileOutlineFilesViewAnswersWhereWithoutWhat is the cheapest question
// the tool answers: which files hold the declarations, and how many each.
func TestGetFileOutlineFilesViewAnswersWhereWithoutWhat(t *testing.T) {
	client := newFileOutlineToolClient(t, fileOutlineSnapshot(t, 47))
	files := fileOutlineText(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts", "view": ViewFiles,
	})
	want := `{"snapshot_id":47,"total":3,"returned":3,"coverage":{"exact":3},` +
		`"results":{"repository":"alpha-repo","path":"internal/facts","files":[` +
		`{"file":"internal/facts/delta.go","declarations":1},` +
		`{"file":"internal/facts/facts.go","declarations":2}]}}`
	if files != want {
		t.Fatalf("files payload =\n%s\nwant\n%s", files, want)
	}

	if _, _, err := getFileOutline(context.Background(), nil, GetFileOutlineInput{
		Repository: "alpha-repo", Path: "internal/facts", View: "brief",
	}, fileOutlineSnapshot(t, 48)); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("unsupported view error code = %q, want %q", ErrorCode(err), CodeInvalidArgument)
	}
}

// TestGetFileOutlineCountsOnlyWhatItReturns defends the count against the shape
// that broke it: a Rust enum's variants are member kinds an outline leaves out
// by default, and they were counted before they were dropped, so a file holding
// twelve declarations answered `total: 24` with `truncated` false and no cursor.
// A reader who trusts the count then goes looking for a half that never was.
func TestGetFileOutlineCountsOnlyWhatItReturns(t *testing.T) {
	store := memberKindSnapshot(t, 61)
	client := newFileOutlineToolClient(t, store)

	byDefault := fileOutlineText(t, client, map[string]any{
		"repository": "alpha-repo", "path": "src/range.rs",
	})
	wantDefault := `{"snapshot_id":61,"total":1,"returned":1,"coverage":{"exact":1},` +
		`"results":{"repository":"alpha-repo","path":"src/range.rs","package":"audio",` +
		`"kind":"enum","exported":true,"files":[{"file":"src/range.rs","at":["RangeOutcome@1-9"]}]}}`
	if byDefault != wantDefault {
		t.Fatalf("default payload =\n%s\nwant\n%s", byDefault, wantDefault)
	}

	withMembers := fileOutlineText(t, client, map[string]any{
		"repository": "alpha-repo", "path": "src/range.rs", "include_members": true,
	})
	wantMembers := `{"snapshot_id":61,"total":3,"returned":3,"coverage":{"exact":3},` +
		`"results":{"repository":"alpha-repo","path":"src/range.rs","package":"audio",` +
		`"exported":true,"files":[{"file":"src/range.rs","at":[` +
		`["RangeOutcome@1-9","enum"],["Range@3","variant"],["SendFile@5","variant"]]}]}}`
	if withMembers != wantMembers {
		t.Fatalf("include_members payload =\n%s\nwant\n%s", withMembers, wantMembers)
	}
}

// memberKindSnapshot is one enum and the two variants that belong to it.
func memberKindSnapshot(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(
		hotsnapshot.LadybugSnapshotRows{
			Repositories: []hotsnapshot.RepositoryRow{
				{Key: "repo-a", Name: "alpha-repo", Path: "/repo-a", Languages: "rust"},
			},
			Packages: []hotsnapshot.PackageRow{
				{Key: "package-a", RepositoryKey: "repo-a", Language: "rust", Name: "audio", ModulePath: "audio"},
			},
			Files: []hotsnapshot.FileRow{
				{Key: "file-range", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "src/range.rs", Language: "rust"},
			},
			Symbols: []hotsnapshot.SymbolRow{
				{
					StableKey: "symbol-outcome", CanonicalIdentity: "rust:RangeOutcome", FileKey: "file-range",
					Language: "rust", Name: "RangeOutcome", QualifiedName: "RangeOutcome", Kind: "enum",
					Signature: "enum RangeOutcome", Exported: true, StartLine: 1, EndLine: 9,
				},
				{
					StableKey: "symbol-variant-range", CanonicalIdentity: "rust:RangeOutcome::Range", FileKey: "file-range",
					Language: "rust", Name: "Range", QualifiedName: "Range", Kind: "variant",
					Signature: "Range(u64)", Exported: true, StartLine: 3, EndLine: 3,
				},
				{
					StableKey: "symbol-variant-send", CanonicalIdentity: "rust:RangeOutcome::SendFile", FileKey: "file-range",
					Language: "rust", Name: "SendFile", QualifiedName: "SendFile", Kind: "variant",
					Signature: "SendFile", Exported: true, StartLine: 5, EndLine: 5,
				},
			},
		},
		id,
		time.Unix(1_700_000_000+int64(id), 0).UTC(),
		1,
	)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}
