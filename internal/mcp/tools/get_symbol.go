package tools

import (
	"context"
	"fmt"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

const getSymbolToolName = "get_symbol"

// GetSymbolInput identifies one symbol, either by its durable stable key or by
// the repository, path and qualified name every row of this surface carries.
type GetSymbolInput struct {
	StableKey      string `json:"stable_key,omitempty"`
	QualifiedName  string `json:"qualified_name,omitempty"`
	Repository     string `json:"repository,omitempty"`
	Path           string `json:"path,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

// SymbolDetails is the public detail shape returned for one symbol. The
// derived identifiers -- the canonical identity and the repository key, whose
// value is already spelled out by the name and the path beside it -- are
// returned only for the detailed format.
type SymbolDetails struct {
	StableKey      string `json:"stable_key"`
	Repository     string `json:"repository"`
	RepositoryPath string `json:"repository_path"`
	PackageName    string `json:"package_name"`
	ModulePath     string `json:"module_path"`
	FilePath       string `json:"file_path"`
	Name           string `json:"name"`
	QualifiedName  string `json:"qualified_name"`
	Kind           string `json:"kind"`
	Signature      string `json:"signature"`
	Exported       bool   `json:"exported"`
	StartLine      uint32 `json:"start_line"`
	EndLine        uint32 `json:"end_line"`

	CanonicalIdentity string `json:"canonical_identity,omitempty"`
	RepositoryKey     string `json:"repository_key,omitempty"`
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
	addQueryTool(server, &sdkmcp.Tool{
		Name:        getSymbolToolName,
		Description: "One symbol's package, signature, visibility and line range, by stable key or by repository, path and qualified name.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, handler)
}

func getSymbol(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments GetSymbolInput,
	snapshotStore *hotsnapshot.SnapshotStore,
) (*sdkmcp.CallToolResult, Response[SymbolDetails], error) {
	selector, err := normalizeSymbolSelector(arguments.StableKey, arguments.Repository, arguments.Path, arguments.QualifiedName)
	if err != nil {
		return nil, Response[SymbolDetails]{}, err
	}
	format, err := normalizeResponseFormat(arguments.ResponseFormat)
	if err != nil {
		return nil, Response[SymbolDetails]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[SymbolDetails]{}, ErrIndexNotReady()
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[SymbolDetails]{}, ErrIndexNotReady()
	}

	symbolID, err := resolveSymbolSelector(snapshot, selector)
	if err != nil {
		return nil, Response[SymbolDetails]{}, err
	}
	symbol, found := snapshot.Symbol(symbolID)
	if !found {
		return nil, Response[SymbolDetails]{}, WrapToolError(
			CodeSnapshotUnavailable,
			"active snapshot symbol index is inconsistent",
			fmt.Errorf("symbol index %d is missing", symbolID),
		)
	}
	details, err := symbolDetails(snapshot, symbol, format)
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

func symbolDetails(
	snapshot *hotsnapshot.GraphSnapshot,
	symbol hotsnapshot.SymbolRecord,
	format string,
) (SymbolDetails, error) {
	summary, err := symbolSummary(snapshot, symbol, format)
	if err != nil {
		return SymbolDetails{}, err
	}
	location, err := resolveSymbolLocation(snapshot, symbol)
	if err != nil {
		return SymbolDetails{}, err
	}
	details := SymbolDetails{
		StableKey:      summary.StableKey,
		Repository:     location.RepositoryName,
		RepositoryPath: location.RepositoryPath,
		PackageName:    location.PackageName,
		ModulePath:     location.ModulePath,
		FilePath:       location.FilePath,
		Name:           summary.Name,
		QualifiedName:  summary.QualifiedName,
		Kind:           summary.Kind,
		Signature:      summary.Signature,
		Exported:       summary.Exported,
		StartLine:      symbol.StartLine,
		EndLine:        symbol.EndLine,
	}
	if format == ResponseFormatDetailed {
		details.CanonicalIdentity = summary.CanonicalIdentity
		details.RepositoryKey = location.RepositoryKey
	}
	return details, nil
}
