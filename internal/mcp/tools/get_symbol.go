package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

const getSymbolToolName = "get_symbol"

// GetSymbolInput identifies one symbol by its durable stable key.
type GetSymbolInput struct {
	StableKey string `json:"stable_key"`
}

// SymbolDetails is the public detail shape returned for one symbol.
type SymbolDetails struct {
	StableKey         string `json:"stable_key"`
	CanonicalIdentity string `json:"canonical_identity"`
	RepositoryKey     string `json:"repository_key"`
	RepositoryName    string `json:"repository_name"`
	RepositoryPath    string `json:"repository_path"`
	PackageName       string `json:"package_name"`
	ModulePath        string `json:"module_path"`
	FilePath          string `json:"file_path"`
	Name              string `json:"name"`
	QualifiedName     string `json:"qualified_name"`
	Kind              string `json:"kind"`
	Signature         string `json:"signature"`
	StartLine         uint32 `json:"start_line"`
	EndLine           uint32 `json:"end_line"`
}

// RegisterGetSymbol adds the read-only symbol lookup tool without a graph
// source. Calls require a snapshot-backed registration to return data.
func RegisterGetSymbol(server *sdkmcp.Server) {
	RegisterGetSymbolWithObserverAndSnapshotStore(server, nil, nil)
}

// RegisterGetSymbolWithObserver adds get_symbol and optionally observes
// handler latency.
func RegisterGetSymbolWithObserver(server *sdkmcp.Server, observer Observer) {
	RegisterGetSymbolWithObserverAndSnapshotStore(server, observer, nil)
}

// RegisterGetSymbolWithSnapshotStore registers get_symbol over the immutable
// snapshot currently published by snapshotStore.
func RegisterGetSymbolWithSnapshotStore(server *sdkmcp.Server, snapshotStore *hotsnapshot.SnapshotStore) {
	RegisterGetSymbolWithObserverAndSnapshotStore(server, nil, snapshotStore)
}

// RegisterGetSymbolWithObserverAndSnapshotStore registers get_symbol over a
// snapshot store and optionally observes latency.
func RegisterGetSymbolWithObserverAndSnapshotStore(
	server *sdkmcp.Server,
	observer Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	callObservers ...CallObserver,
) {
	callObserver := firstCallObserver(callObservers)
	handler := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments GetSymbolInput,
	) (*sdkmcp.CallToolResult, Response[SymbolDetails], error) {
		return getSymbol(ctx, request, arguments, snapshotStore)
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments GetSymbolInput,
		) (*sdkmcp.CallToolResult, Response[SymbolDetails], error) {
			start := time.Now()
			result, symbol, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, getSymbolToolName, start, symbol, err)
			return result, symbol, err
		}
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        getSymbolToolName,
		Description: "Returns one symbol by its stable key.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, handler)
}

func getSymbol(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments GetSymbolInput,
	snapshotStore *hotsnapshot.SnapshotStore,
) (*sdkmcp.CallToolResult, Response[SymbolDetails], error) {
	stableKey, err := normalizeSymbolStableKey(arguments.StableKey)
	if err != nil {
		return nil, Response[SymbolDetails]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[SymbolDetails]{}, NewToolError(CodeIndexNotReady, "no HotSnapshot is published")
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[SymbolDetails]{}, NewToolError(CodeIndexNotReady, "no HotSnapshot is published")
	}

	symbolID, found := snapshot.SymbolByStableKey(hotsnapshot.StableKey(stableKey))
	if !found {
		return nil, Response[SymbolDetails]{}, NewToolError(CodeSymbolNotFound, fmt.Sprintf("symbol %q was not found", stableKey))
	}
	symbol, found := snapshot.Symbol(symbolID)
	if !found {
		return nil, Response[SymbolDetails]{}, WrapToolError(
			CodeSnapshotUnavailable,
			"active snapshot symbol index is inconsistent",
			fmt.Errorf("symbol index %d is missing", symbolID),
		)
	}
	details, err := symbolDetails(snapshot, symbol)
	if err != nil {
		return nil, Response[SymbolDetails]{}, WrapToolError(
			CodeSnapshotUnavailable,
			"active snapshot contains invalid symbol metadata",
			err,
		)
	}

	metadata := snapshot.Metadata()
	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[SymbolDetails]{
		SnapshotID:    &snapshotID,
		SnapshotAgeMS: &snapshotAgeMS,
		Total:         1,
		Returned:      1,
		Results:       details,
	}, nil
}

func normalizeSymbolStableKey(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return "", NewToolError(CodeInvalidArgument, "stable_key must be a non-empty value without surrounding whitespace")
	}
	return value, nil
}

func symbolDetails(snapshot *hotsnapshot.GraphSnapshot, symbol hotsnapshot.SymbolRecord) (SymbolDetails, error) {
	summary, err := symbolSummary(snapshot, symbol)
	if err != nil {
		return SymbolDetails{}, err
	}
	file, fileOK := snapshot.File(symbol.File)
	if !fileOK {
		return SymbolDetails{}, fmt.Errorf("symbol %q references missing file %d", symbol.StableKey, symbol.File)
	}
	pkg, packageOK := snapshot.Package(file.Package)
	if !packageOK {
		return SymbolDetails{}, fmt.Errorf("symbol %q references missing package %d", symbol.StableKey, file.Package)
	}
	repository, repositoryOK := snapshot.Repository(file.Repository)
	if !repositoryOK {
		return SymbolDetails{}, fmt.Errorf("symbol %q references missing repository %d", symbol.StableKey, file.Repository)
	}

	table := snapshot.Strings()
	repositoryKey, repositoryKeyOK := table.String(repository.Key)
	repositoryName, repositoryNameOK := table.String(repository.Name)
	repositoryPath, repositoryPathOK := table.String(repository.Path)
	packageName, packageNameOK := table.String(pkg.Name)
	modulePath, modulePathOK := table.String(pkg.ModulePath)
	filePath, filePathOK := table.String(file.Path)
	if !repositoryKeyOK || !repositoryNameOK || !repositoryPathOK || !packageNameOK || !modulePathOK || !filePathOK {
		return SymbolDetails{}, fmt.Errorf(
			"symbol %q references invalid location strings (repository_key_ok=%t repository_name_ok=%t repository_path_ok=%t package_name_ok=%t module_path_ok=%t file_path_ok=%t)",
			symbol.StableKey,
			repositoryKeyOK,
			repositoryNameOK,
			repositoryPathOK,
			packageNameOK,
			modulePathOK,
			filePathOK,
		)
	}

	return SymbolDetails{
		StableKey:         summary.StableKey,
		CanonicalIdentity: summary.CanonicalIdentity,
		RepositoryKey:     repositoryKey,
		RepositoryName:    repositoryName,
		RepositoryPath:    repositoryPath,
		PackageName:       packageName,
		ModulePath:        modulePath,
		FilePath:          filePath,
		Name:              summary.Name,
		QualifiedName:     summary.QualifiedName,
		Kind:              summary.Kind,
		Signature:         summary.Signature,
		StartLine:         symbol.StartLine,
		EndLine:           symbol.EndLine,
	}, nil
}
