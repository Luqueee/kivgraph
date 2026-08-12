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
	FindSymbolModeSubstring      = "substring"

	DefaultSymbolLimit = 50
	MaximumSymbolLimit = hotsnapshot.MaxExactResults
	findSymbolToolName = "find_symbol"
)

// FindSymbolInput contains the search mode, the filters and the page controls
// for find_symbol. An empty mode is the exact unqualified-name search.
//
// Kind, Repo and PathPrefix narrow the page without changing its cost class:
// prefix and substring already walk every symbol name in the snapshot, so
// filtering while walking is free.
type FindSymbolInput struct {
	Name           string `json:"name"`
	Mode           string `json:"mode,omitempty"`
	Kind           string `json:"kind,omitempty"`
	Repo           string `json:"repo,omitempty"`
	PathPrefix     string `json:"path_prefix,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	Cursor         string `json:"cursor,omitempty"`
}

// SymbolSummary is the stable public result shape for symbol discovery. It
// carries where the symbol is, because a search result the agent cannot open
// costs a second call to become useful.
//
// CanonicalIdentity is omitted unless the caller asks for the detailed
// format: it is the concatenation of language, repository, package, qualified
// name, kind and discriminator, every one of which is already a field here or
// is the signature itself.
type SymbolSummary struct {
	StableKey         string `json:"stable_key"`
	Name              string `json:"name"`
	QualifiedName     string `json:"qualified_name"`
	Kind              string `json:"kind"`
	Signature         string `json:"signature"`
	Exported          bool   `json:"exported"`
	RepositoryName    string `json:"repository_name"`
	FilePath          string `json:"file_path"`
	StartLine         uint32 `json:"start_line"`
	EndLine           uint32 `json:"end_line"`
	CanonicalIdentity string `json:"canonical_identity,omitempty"`
}

type findSymbolQuery struct {
	Tool       string `json:"tool"`
	Name       string `json:"name"`
	Mode       string `json:"mode"`
	Kind       string `json:"kind,omitempty"`
	Repo       string `json:"repo,omitempty"`
	PathPrefix string `json:"path_prefix,omitempty"`
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
		Name:         findSymbolToolName,
		Description:  "Finds symbols by exact name, qualified name, prefix or substring, and returns where each one is. Narrow with kind, repo and path_prefix.",
		OutputSchema: ConciseOutputSchema(),
		Annotations:  &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
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
	format, err := normalizeResponseFormat(arguments.ResponseFormat)
	if err != nil {
		return nil, Response[[]SymbolSummary]{}, err
	}
	filter := hotsnapshot.SymbolFilter{
		Kind:           arguments.Kind,
		RepositoryName: arguments.Repo,
		PathPrefix:     arguments.PathPrefix,
	}
	// The cursor is bound to the whole query, filters included: a page taken
	// with one filter is not a page of another.
	queryHash, err := HashQuery(findSymbolQuery{
		Tool: findSymbolToolName, Name: name, Mode: mode,
		Kind: filter.Kind, Repo: filter.RepositoryName, PathPrefix: filter.PathPrefix,
	})
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

	page, err := searchSymbolPage(snapshot, name, mode, filter, offset, limit)
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
		summary, err := symbolSummary(snapshot, symbol, format)
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

	// A search that found nothing and reports no uncertainty is claiming the
	// name does not exist. It may only mean its provider was never indexed,
	// and the index recorded exactly that, with a file and a line.
	_, unresolvedRelated := snapshot.UnresolvedNamingSymbol(name, 0)

	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[[]SymbolSummary]{
		SnapshotID:    &snapshotID,
		SnapshotAgeMS: &snapshotAgeMS,
		Total:         page.Total,
		Returned:      len(results),
		Truncated:     page.HasMore,
		NextCursor:    nextCursor,
		Coverage:      Coverage{Exact: len(results), UnresolvedRelated: unresolvedRelated},
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
	case FindSymbolModeExact, FindSymbolModeQualifiedExact, FindSymbolModePrefix, FindSymbolModeSubstring:
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

func searchSymbolPage(
	snapshot *hotsnapshot.GraphSnapshot,
	name, mode string,
	filter hotsnapshot.SymbolFilter,
	offset, limit int,
) (hotsnapshot.SymbolPage, error) {
	nameID, found := snapshot.Strings().Lookup(name)
	switch mode {
	case FindSymbolModeExact:
		if !found {
			nameID = hotsnapshot.InvalidInternedString
		}
		return snapshot.SearchSymbolsByName(nameID, filter, offset, limit)
	case FindSymbolModeQualifiedExact:
		if !found {
			nameID = hotsnapshot.InvalidInternedString
		}
		return snapshot.SearchSymbolsByQName(nameID, filter, offset, limit)
	case FindSymbolModePrefix:
		return snapshot.SearchSymbolsByNamePrefix(name, filter, offset, limit)
	case FindSymbolModeSubstring:
		return snapshot.SearchSymbolsByNameSubstring(name, filter, offset, limit)
	default:
		return hotsnapshot.SymbolPage{}, fmt.Errorf("unsupported mode %q", mode)
	}
}

// symbolSummary builds one result row. The location is not optional: a search
// result the agent cannot open is a result it has to ask about again.
func symbolSummary(
	snapshot *hotsnapshot.GraphSnapshot,
	symbol hotsnapshot.SymbolRecord,
	format string,
) (SymbolSummary, error) {
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
	location, err := resolveSymbolLocation(snapshot, symbol)
	if err != nil {
		return SymbolSummary{}, err
	}
	summary := SymbolSummary{
		StableKey:      string(symbol.StableKey),
		Name:           name,
		QualifiedName:  qualifiedName,
		Kind:           kind,
		Signature:      signature,
		Exported:       symbol.Exported,
		RepositoryName: location.RepositoryName,
		FilePath:       location.FilePath,
		StartLine:      symbol.StartLine,
		EndLine:        symbol.EndLine,
	}
	if format == ResponseFormatDetailed {
		summary.CanonicalIdentity = canonical
	}
	return summary, nil
}
