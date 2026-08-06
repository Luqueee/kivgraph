package tools

import (
	"context"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

// TestGetUnresolvedReferencesReturnsEvidenceNotIdentity pins what a row means:
// the request the resolver made and where it failed, with the optional file and
// source symbol left empty when the failure is module level.
func TestGetUnresolvedReferencesReturnsEvidenceNotIdentity(t *testing.T) {
	store := unresolvedStore(t, 51)

	_, response, err := getUnresolvedReferences(context.Background(), nil, GetUnresolvedReferencesInput{}, store)
	if err != nil {
		t.Fatalf("getUnresolvedReferences() error = %v", err)
	}
	if response.Total != 3 || response.Returned != 3 || response.Truncated {
		t.Fatalf("pagination = %#v, want the three unresolved references", response)
	}
	if response.Coverage != (Coverage{UnresolvedRelated: 3}) {
		t.Fatalf("coverage = %#v, want every row counted as unresolved", response.Coverage)
	}
	wantKeys := []string{"unresolved-go-symbol", "unresolved-module", "unresolved-ts-package"}
	for index, summary := range response.Results {
		if summary.UnresolvedKey != wantKeys[index] {
			t.Fatalf("keys = %#v, want %v", response.Results, wantKeys)
		}
	}

	symbolLevel := response.Results[0]
	if symbolLevel.SourceSymbolKey != "sym-consumer" || symbolLevel.FilePath != "consumer.go" ||
		symbolLevel.RequestedPackage != "example.com/missing" || symbolLevel.RequestedSymbol != "Thing" ||
		symbolLevel.Reason != "package_not_found" || symbolLevel.StartLine != 12 {
		t.Fatalf("symbol-level row = %#v", symbolLevel)
	}
	moduleLevel := response.Results[1]
	if moduleLevel.SourceSymbolKey != "" || moduleLevel.FileKey != "" || moduleLevel.PackageKey != "" {
		t.Fatalf("module-level row = %#v, want no file or symbol identity", moduleLevel)
	}
	if moduleLevel.RepositoryKey != "repo-go" || moduleLevel.Language != "go" || moduleLevel.RequestedSymbol != "" {
		t.Fatalf("module-level row = %#v, want repository and language only", moduleLevel)
	}
}

// TestGetUnresolvedReferencesSeparatesObservedFromRequested is the reason the
// package filter is split in two: repo-ts consumes a package whose name matches
// a package that exists in the graph, and the two filters must not collide.
func TestGetUnresolvedReferencesSeparatesObservedFromRequested(t *testing.T) {
	store := unresolvedStore(t, 52)

	_, byPackage, err := getUnresolvedReferences(context.Background(), nil, GetUnresolvedReferencesInput{Package: "pkg-ts"}, store)
	if err != nil {
		t.Fatal(err)
	}
	if byPackage.Total != 1 || byPackage.Results[0].UnresolvedKey != "unresolved-ts-package" {
		t.Fatalf("package filter = %#v, want the failure observed inside pkg-ts", byPackage.Results)
	}

	_, byRequested, err := getUnresolvedReferences(context.Background(), nil, GetUnresolvedReferencesInput{
		RequestedPackage: "example.com/missing",
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if byRequested.Total != 2 {
		t.Fatalf("requested_package filter = %#v, want both requests for the missing package", byRequested.Results)
	}
	for _, summary := range byRequested.Results {
		if summary.RequestedPackage != "example.com/missing" {
			t.Fatalf("requested_package filter leaked %#v", summary)
		}
	}
}

func TestGetUnresolvedReferencesAppliesEveryFilter(t *testing.T) {
	store := unresolvedStore(t, 53)

	cases := map[string]struct {
		arguments GetUnresolvedReferencesInput
		wantTotal int
	}{
		"repo":             {arguments: GetUnresolvedReferencesInput{Repo: "repo-go"}, wantTotal: 2},
		"language":         {arguments: GetUnresolvedReferencesInput{Language: "ts"}, wantTotal: 1},
		"reason":           {arguments: GetUnresolvedReferencesInput{Reason: "package_not_found"}, wantTotal: 1},
		"requested symbol": {arguments: GetUnresolvedReferencesInput{RequestedSymbol: "Thing"}, wantTotal: 1},
		"unknown reason":   {arguments: GetUnresolvedReferencesInput{Reason: "no_such_reason"}, wantTotal: 0},
		"combined":         {arguments: GetUnresolvedReferencesInput{Repo: "repo-go", Language: "ts"}, wantTotal: 0},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			_, response, err := getUnresolvedReferences(context.Background(), nil, test.arguments, store)
			if err != nil {
				t.Fatal(err)
			}
			if response.Total != test.wantTotal {
				t.Fatalf("total = %d, want %d (%#v)", response.Total, test.wantTotal, response.Results)
			}
		})
	}
}

func TestGetUnresolvedReferencesPaginatesWithSnapshotCursor(t *testing.T) {
	store := unresolvedStore(t, 54)

	_, first, err := getUnresolvedReferences(context.Background(), nil, GetUnresolvedReferencesInput{Limit: 2}, store)
	if err != nil {
		t.Fatal(err)
	}
	if first.Returned != 2 || !first.Truncated || first.NextCursor == nil {
		t.Fatalf("first page = %#v, want two rows and a cursor", first)
	}
	_, second, err := getUnresolvedReferences(context.Background(), nil, GetUnresolvedReferencesInput{
		Limit: 2, Cursor: *first.NextCursor,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if second.Returned != 1 || second.Truncated || second.NextCursor != nil {
		t.Fatalf("second page = %#v, want the final row", second)
	}
	if second.Results[0].UnresolvedKey != "unresolved-ts-package" {
		t.Fatalf("second page row = %#v", second.Results[0])
	}
}

func TestGetUnresolvedReferencesClassifiesFailures(t *testing.T) {
	store := unresolvedStore(t, 55)
	cases := []struct {
		name      string
		arguments GetUnresolvedReferencesInput
		store     *hotsnapshot.SnapshotStore
		wantCode  string
	}{
		{name: "padded repo", arguments: GetUnresolvedReferencesInput{Repo: " repo-go"}, store: store, wantCode: CodeInvalidArgument},
		{name: "padded reason", arguments: GetUnresolvedReferencesInput{Reason: "package_not_found "}, store: store, wantCode: CodeInvalidArgument},
		{name: "limit above maximum", arguments: GetUnresolvedReferencesInput{Limit: MaximumUnresolvedLimit + 1}, store: store, wantCode: CodeInvalidArgument},
		{name: "invalid cursor", arguments: GetUnresolvedReferencesInput{Cursor: "not-a-cursor"}, store: store, wantCode: CodeCursorInvalid},
		{name: "unpublished snapshot", arguments: GetUnresolvedReferencesInput{}, store: hotsnapshot.NewSnapshotStore(nil), wantCode: CodeIndexNotReady},
		{name: "missing store", arguments: GetUnresolvedReferencesInput{}, wantCode: CodeIndexNotReady},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := getUnresolvedReferences(context.Background(), nil, test.arguments, test.store)
			if got := ErrorCode(err); got != test.wantCode {
				t.Fatalf("error code = %q, want %q (err=%v)", got, test.wantCode, err)
			}
		})
	}
}

func TestGetUnresolvedReferencesIsRegisteredReadOnly(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterGetUnresolvedReferences(server)
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
		if tool.Name == unresolvedReferencesToolName {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("get_unresolved_references annotations = %#v, want read-only", tool.Annotations)
			}
			return
		}
	}
	t.Fatal("get_unresolved_references is not registered")
}

// unresolvedStore records three failures: one bound to a symbol, one bound only
// to a repository, and one observed in a TypeScript package. Reasons follow the
// per-language vocabularies the loaders actually emit.
func unresolvedStore(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-go", Name: "go-consumer", Languages: "go"},
			{Key: "repo-ts", Name: "ts-consumer", Languages: "ts"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "pkg-go", RepositoryKey: "repo-go", Language: "go", Name: "consumer", ModulePath: "example.com/consumer"},
			{Key: "pkg-ts", RepositoryKey: "repo-ts", Language: "ts", Name: "app", ModulePath: "@acme/app"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-go", RepositoryKey: "repo-go", PackageKey: "pkg-go", Path: "consumer.go", Language: "go"},
			{Key: "file-ts", RepositoryKey: "repo-ts", PackageKey: "pkg-ts", Path: "index.ts", Language: "ts"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "sym-consumer", CanonicalIdentity: "go:consumer.Consumer", FileKey: "file-go", Language: "go", Name: "Consumer", QualifiedName: "consumer.Consumer", Kind: "func"},
		},
		Unresolved: []hotsnapshot.UnresolvedReferenceRow{
			{
				Key: "unresolved-go-symbol", RepositoryKey: "repo-go", FileKey: "file-go", SourceKey: "sym-consumer",
				Language: "go", RequestedPackage: "example.com/missing", RequestedSymbol: "Thing",
				Reason: "package_not_found", Detail: "no registered repository provides the module",
				StartLine: 12, StartColumn: 4, StartOffset: 180,
			},
			{
				Key: "unresolved-module", RepositoryKey: "repo-go", Language: "go",
				RequestedPackage: "example.com/missing", Reason: "module_provider_not_found",
				Detail: "module level failure with no file", StartLine: 1,
			},
			{
				Key: "unresolved-ts-package", RepositoryKey: "repo-ts", FileKey: "file-ts",
				Language: "ts", RequestedPackage: "@acme/other", RequestedSymbol: "Base",
				Reason: "PACKAGE_PROVIDER_NOT_FOUND", Detail: "no provider for the specifier",
				StartLine: 3, StartColumn: 1, StartOffset: 40,
			},
		},
	}, id, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}
