package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

func TestGetFileOutlineDescribesOneFileAndOneDirectory(t *testing.T) {
	client := newFileOutlineToolClient(t, fileOutlineSnapshot(t, 41))

	single := callFileOutline(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts/facts.go",
	})
	if single.Results.Files != 1 || single.Total != 2 || single.Returned != 2 {
		t.Fatalf("single file outline = %#v", single)
	}
	first := single.Results.Symbols[0]
	if first.Name != "Merge" || first.Kind != "method" || first.Signature != "func (Set) Merge(Set)" {
		t.Fatalf("first declaration = %#v", first)
	}
	// A one-file outline does not repeat the path the caller just asked for.
	if first.FilePath != "" {
		t.Fatalf("single-file row repeats the path = %#v", first)
	}
	if first.StartLine != 10 || first.EndLine != 20 {
		t.Fatalf("first location = %#v", first)
	}
	if !first.Exported {
		t.Fatalf("Merge must be reported as exported: %#v", first)
	}
	// The concise default omits the canonical identity here too.
	if first.CanonicalIdentity != "" {
		t.Fatalf("concise row carries the canonical identity = %#v", first)
	}
	if len(single.Results.Languages) != 1 || single.Results.Languages[0] != "go" {
		t.Fatalf("languages = %#v", single.Results.Languages)
	}

	// The same argument answers the directory question: one path, two
	// granularities, one tool.
	directory := callFileOutline(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts",
	})
	for _, symbol := range directory.Results.Symbols {
		if symbol.FilePath == "" {
			t.Fatalf("a directory outline must say which file each row is in: %#v", symbol)
		}
	}
	if directory.Results.Files != 2 || directory.Total != 3 {
		t.Fatalf("directory outline = %#v", directory.Results)
	}

	// A trailing slash is the same directory.
	slashed := callFileOutline(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts/",
	})
	if slashed.Results.Files != directory.Results.Files || slashed.Total != directory.Total {
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
	for _, symbol := range outline.Results.Symbols {
		if symbol.FilePath == "internal/factsheet.go" {
			t.Fatalf("directory outline reached a sibling file: %#v", symbol)
		}
	}
}

func TestGetFileOutlineFiltersByKindAndRestoresDetail(t *testing.T) {
	client := newFileOutlineToolClient(t, fileOutlineSnapshot(t, 43))

	methods := callFileOutline(t, client, map[string]any{
		"repository": "alpha-repo", "path": "internal/facts", "kind": "method",
	})
	for _, symbol := range methods.Results.Symbols {
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
	if detailed.Results.Symbols[0].CanonicalIdentity == "" {
		t.Fatalf("detailed row = %#v, want the canonical identity back", detailed.Results.Symbols[0])
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

func callFileOutline(t *testing.T, client *sdkmcp.ClientSession, arguments map[string]any) Response[FileOutline] {
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
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal structured content: %v", err)
	}
	var response Response[FileOutline]
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("Unmarshal structured content: %v", err)
	}
	return response
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
