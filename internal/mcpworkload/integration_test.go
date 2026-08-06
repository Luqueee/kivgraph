package mcpworkload

import (
	"context"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	ladygraphmcp "github.com/Luqueee/ladygraph/internal/mcp"
)

func TestGeneratedRequestsAreAcceptedByMCPServer(t *testing.T) {
	snapshot, err := hotsnapshot.BuildGraphSnapshot(
		hotsnapshot.LadybugSnapshotRows{
			Repositories: []hotsnapshot.RepositoryRow{
				{Key: "repo-a", Name: "repo-a", Path: "/repo-a", Languages: "go"},
				{Key: "repo-b", Name: "repo-b", Path: "/repo-b", Languages: "go"},
			},
			Packages: []hotsnapshot.PackageRow{
				{Key: "package-a", RepositoryKey: "repo-a", Name: "pkg", ModulePath: "example.com/pkg"},
				{Key: "package-b", RepositoryKey: "repo-b", Name: "consumer", ModulePath: "example.com/consumer"},
			},
			Files: []hotsnapshot.FileRow{
				{Key: "file-a", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "symbol.go"},
				{Key: "file-b", RepositoryKey: "repo-b", PackageKey: "package-b", Path: "consumer.go"},
			},
			Symbols: []hotsnapshot.SymbolRow{
				{
					StableKey: "symbol-00000000", CanonicalIdentity: "go:symbol-00000000", FileKey: "file-a",
					Name: "symbol_00000000", QualifiedName: "pkg.symbol_00000000", Kind: "function",
					Signature: "func symbol_00000000()",
				},
				{
					StableKey: "symbol-consumer", CanonicalIdentity: "go:symbol-consumer", FileKey: "file-b",
					Name: "consumer", QualifiedName: "consumer.consumer", Kind: "function",
					Signature: "func consumer()",
				},
			},
			Edges: []hotsnapshot.EdgeRow{{
				SourceKey: "symbol-consumer", TargetKey: "symbol-00000000",
				Kind: facts.CodeReferences, Confidence: facts.CodeExactTypechecked,
				Provenance: facts.CodeGoTypesUse, EvidenceKind: "types",
				EvidenceSourceFileKey: "file-b", EvidenceTargetFileKey: "file-a",
			}},
		},
		1,
		time.Unix(1_700_000_001, 0).UTC(),
		1,
	)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}

	server := ladygraphmcp.NewServerWithSnapshotStore(hotsnapshot.NewSnapshotStore(snapshot))
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "mcp-workload-test", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	workload, err := Generate(context.Background(), Config{Calls: 20, Seed: 42, Corpus: Corpus{Probes: []Probe{{
		Name: "symbol_00000000", StableKey: "symbol-00000000",
	}}}})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	for _, request := range workload.Requests {
		result, err := clientSession.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name: string(request.Operation), Arguments: request.Arguments,
		})
		if err != nil {
			t.Fatalf("request %d (%s) error = %v", request.Sequence, request.Operation, err)
		}
		if result == nil || result.IsError {
			t.Fatalf("request %d (%s) result = %#v", request.Sequence, request.Operation, result)
		}
	}
}
