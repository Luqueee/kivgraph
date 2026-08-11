package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

const (
	FindSymbolModeExact          = "exact"
	FindSymbolModeQualifiedExact = "qualified_exact"
	FindSymbolModePrefix         = "prefix"

	DefaultSymbolLimit = 50
	MaximumSymbolLimit = hotsnapshot.MaxExactResults
	findSymbolToolName = "find_symbol"
)

// FindSymbolInput contains the search mode and optional page controls for
// find_symbol. An empty mode is the exact unqualified-name search.
type FindSymbolInput struct {
	Name   string `json:"name"`
	Mode   string `json:"mode,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// SymbolSummary is the stable public result shape for symbol discovery.
type SymbolSummary struct {
	StableKey         string `json:"stable_key"`
	CanonicalIdentity string `json:"canonical_identity"`
	Name              string `json:"name"`
	QualifiedName     string `json:"qualified_name"`
	Kind              string `json:"kind"`
	Signature         string `json:"signature"`
}

type findSymbolQuery struct {
	Tool string `json:"tool"`
	Name string `json:"name"`
	Mode string `json:"mode"`
}

// RegisterFindSymbol adds the read-only symbol search tool without a graph
// source. Calls require a snapshot-backed registration to return data.
func RegisterFindSymbol(server *sdkmcp.Server) {
	RegisterFindSymbolWithObserverAndSnapshotStore(server, nil, nil)
}

// RegisterFindSymbolWithObserver adds find_symbol and optionally observes
// handler latency.
func RegisterFindSymbolWithObserver(server *sdkmcp.Server, observer Observer) {
	RegisterFindSymbolWithObserverAndSnapshotStore(server, observer, nil)
}

// RegisterFindSymbolWithSnapshotStore registers find_symbol over the
// immutable snapshot currently published by snapshotStore.
func RegisterFindSymbolWithSnapshotStore(server *sdkmcp.Server, snapshotStore *hotsnapshot.SnapshotStore) {
	RegisterFindSymbolWithObserverAndSnapshotStore(server, nil, snapshotStore)
}

// RegisterFindSymbolWithObserverAndSnapshotStore registers find_symbol over a
// snapshot store and optionally observes latency.
func RegisterFindSymbolWithObserverAndSnapshotStore(
	server *sdkmcp.Server,
	observer Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	callObservers ...CallObserver,
) {
	callObserver := firstCallObserver(callObservers)
	handler := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments FindSymbolInput,
	) (*sdkmcp.CallToolResult, Response[[]SymbolSummary], error) {
		return findSymbol(ctx, request, arguments, snapshotStore)
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments FindSymbolInput,
		) (*sdkmcp.CallToolResult, Response[[]SymbolSummary], error) {
			start := time.Now()
			result, symbols, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, findSymbolToolName, start, symbols, err)
			return result, symbols, err
		}
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        findSymbolToolName,
		Description: "Finds symbols by exact name, qualified name, or name prefix.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, handler)
}

func findSymbol(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments FindSymbolInput,
	snapshotStore *hotsnapshot.SnapshotStore,
) (*sdkmcp.CallToolResult, Response[[]SymbolSummary], error) {
	name, err := normalizeSymbolName(arguments.Name)
	if err != nil {
		return nil, Response[[]SymbolSummary]{}, err
	}
	mode, err := normalizeFindSymbolMode(arguments.Mode)
	if err != nil {
		return nil, Response[[]SymbolSummary]{}, err
	}
	limit, err := normalizeSymbolLimit(arguments.Limit)
	if err != nil {
		return nil, Response[[]SymbolSummary]{}, err
	}
	queryHash, err := HashQuery(findSymbolQuery{Tool: findSymbolToolName, Name: name, Mode: mode})
	if err != nil {
		return nil, Response[[]SymbolSummary]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[[]SymbolSummary]{}, ErrIndexNotReady()
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[[]SymbolSummary]{}, ErrIndexNotReady()
	}
	metadata := snapshot.Metadata()

	offset := 0
	if arguments.Cursor != "" {
		cursor, err := DecodeCursor(arguments.Cursor)
		if err != nil {
			return nil, Response[[]SymbolSummary]{}, err
		}
		if err := cursor.ValidateAgainst(metadata.ID, queryHash, SortingVersionStableKeyV1); err != nil {
			return nil, Response[[]SymbolSummary]{}, err
		}
		offset = cursor.Offset
	}

	page, err := searchSymbolPage(snapshot, name, mode, offset, limit)
	if err != nil {
		return nil, Response[[]SymbolSummary]{}, WrapToolError(CodeInvalidArgument, "symbol search pagination is invalid", err)
	}
	results := make([]SymbolSummary, 0, len(page.IDs))
	for _, id := range page.IDs {
		symbol, found := snapshot.Symbol(id)
		if !found {
			return nil, Response[[]SymbolSummary]{}, WrapToolError(
				CodeSnapshotUnavailable,
				"active snapshot symbol index is inconsistent",
				fmt.Errorf("symbol index %d is missing", id),
			)
		}
		summary, err := symbolSummary(snapshot, symbol)
		if err != nil {
			return nil, Response[[]SymbolSummary]{}, WrapToolError(
				CodeSnapshotUnavailable,
				"active snapshot contains invalid symbol metadata",
				err,
			)
		}
		results = append(results, summary)
	}

	var nextCursor *string
	if page.HasMore {
		nextOffset := page.Offset + len(page.IDs)
		cursor, err := NewCursor(metadata.ID, queryHash, nextOffset, SortingVersionStableKeyV1)
		if err != nil {
			return nil, Response[[]SymbolSummary]{}, err
		}
		encoded, err := cursor.Encode()
		if err != nil {
			return nil, Response[[]SymbolSummary]{}, err
		}
		nextCursor = &encoded
	}

	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[[]SymbolSummary]{
		SnapshotID:    &snapshotID,
		SnapshotAgeMS: &snapshotAgeMS,
		Total:         page.Total,
		Returned:      len(results),
		Truncated:     page.HasMore,
		NextCursor:    nextCursor,
		Coverage:      Coverage{},
		Results:       results,
	}, nil
}

func normalizeSymbolName(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return "", NewToolError(CodeInvalidArgument, "name must be a non-empty value without surrounding whitespace")
	}
	return value, nil
}

func normalizeFindSymbolMode(value string) (string, error) {
	if value == "" {
		return FindSymbolModeExact, nil
	}
	switch value {
	case FindSymbolModeExact, FindSymbolModeQualifiedExact, FindSymbolModePrefix:
		return value, nil
	default:
		return "", NewToolError(CodeInvalidArgument, fmt.Sprintf("mode %q is unsupported", value))
	}
}

func normalizeSymbolLimit(value int) (int, error) {
	if value == 0 {
		return DefaultSymbolLimit, nil
	}
	if value < 1 || value > MaximumSymbolLimit {
		return 0, NewToolError(CodeInvalidArgument, fmt.Sprintf("limit must be between 1 and %d", MaximumSymbolLimit))
	}
	return value, nil
}

func searchSymbolPage(snapshot *hotsnapshot.GraphSnapshot, name, mode string, offset, limit int) (hotsnapshot.SymbolPage, error) {
	nameID, found := snapshot.Strings().Lookup(name)
	switch mode {
	case FindSymbolModeExact:
		if !found {
			nameID = hotsnapshot.InvalidInternedString
		}
		return snapshot.SearchSymbolsByName(nameID, offset, limit)
	case FindSymbolModeQualifiedExact:
		if !found {
			nameID = hotsnapshot.InvalidInternedString
		}
		return snapshot.SearchSymbolsByQName(nameID, offset, limit)
	case FindSymbolModePrefix:
		return snapshot.SearchSymbolsByNamePrefix(name, offset, limit)
	default:
		return hotsnapshot.SymbolPage{}, fmt.Errorf("unsupported mode %q", mode)
	}
}

func symbolSummary(snapshot *hotsnapshot.GraphSnapshot, symbol hotsnapshot.SymbolRecord) (SymbolSummary, error) {
	table := snapshot.Strings()
	canonical, canonicalOK := table.String(symbol.CanonicalIdentity)
	name, nameOK := table.String(symbol.Name)
	qualifiedName, qualifiedNameOK := table.String(symbol.QualifiedName)
	kind, kindOK := table.String(symbol.Kind)
	signature, signatureOK := table.String(symbol.Signature)
	if !canonicalOK || !nameOK || !qualifiedNameOK || !kindOK || !signatureOK {
		return SymbolSummary{}, fmt.Errorf(
			"symbol metadata references invalid strings (canonical_ok=%t name_ok=%t qualified_name_ok=%t kind_ok=%t signature_ok=%t)",
			canonicalOK, nameOK, qualifiedNameOK, kindOK, signatureOK,
		)
	}
	return SymbolSummary{
		StableKey:         string(symbol.StableKey),
		CanonicalIdentity: canonical,
		Name:              name,
		QualifiedName:     qualifiedName,
		Kind:              kind,
		Signature:         signature,
	}, nil
}
