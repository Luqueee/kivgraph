package tools

import (
	"context"
	"fmt"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

const (
	DefaultUnresolvedLimit       = 50
	MaximumUnresolvedLimit       = hotsnapshot.MaxExactResults
	SortingVersionUnresolvedV1   = "unresolved-v1"
	unresolvedReferencesToolName = "get_unresolved_references"
)

// GetUnresolvedReferencesInput filters the references that could not be
// resolved to an exact symbol.
//
// Package is deliberately split in two. Repo, Package, Language and the
// position describe where the failure was observed; RequestedPackage and
// RequestedSymbol describe what the resolver asked for and did not find.
// Collapsing both sides into one "package" filter would make the answer
// ambiguous the moment a repository consumes a package with its own name.
type GetUnresolvedReferencesInput struct {
	Repo             string `json:"repo,omitempty"`
	Package          string `json:"package,omitempty"`
	RequestedPackage string `json:"requested_package,omitempty"`
	RequestedSymbol  string `json:"requested_symbol,omitempty"`
	Reason           string `json:"reason,omitempty"`
	Language         string `json:"language,omitempty"`
	Limit            int    `json:"limit,omitempty"`
	Cursor           string `json:"cursor,omitempty"`
}

// UnresolvedReferenceSummary is one recorded resolution failure. It is
// evidence about a request, never an inferred symbol identity: the requested
// package and symbol are the strings the resolver used, not graph keys.
type UnresolvedReferenceSummary struct {
	UnresolvedKey string `json:"unresolved_key"`

	RepositoryKey   string `json:"repository_key"`
	PackageKey      string `json:"package_key,omitempty"`
	PackageName     string `json:"package_name,omitempty"`
	FileKey         string `json:"file_key,omitempty"`
	FilePath        string `json:"file_path,omitempty"`
	SourceSymbolKey string `json:"source_symbol_key,omitempty"`
	Language        string `json:"language,omitempty"`

	RequestedPackage string `json:"requested_package"`
	RequestedSymbol  string `json:"requested_symbol,omitempty"`
	Reason           string `json:"reason"`
	Detail           string `json:"detail,omitempty"`

	StartLine   uint32 `json:"start_line,omitempty"`
	StartColumn uint32 `json:"start_column,omitempty"`
	StartOffset uint32 `json:"start_offset,omitempty"`
}

type unresolvedReferencesOptions struct {
	Repo             string
	Package          string
	RequestedPackage string
	RequestedSymbol  string
	Reason           string
	Language         string
	Limit            int
}

type unresolvedReferencesQuery struct {
	Tool             string `json:"tool"`
	Repo             string `json:"repo,omitempty"`
	Package          string `json:"package,omitempty"`
	RequestedPackage string `json:"requested_package,omitempty"`
	RequestedSymbol  string `json:"requested_symbol,omitempty"`
	Reason           string `json:"reason,omitempty"`
	Language         string `json:"language,omitempty"`
}

// RegisterGetUnresolvedReferences adds the read-only unresolved lookup without
// a graph source. Calls require a snapshot-backed registration to return data.
func RegisterGetUnresolvedReferences(server *sdkmcp.Server) {
	RegisterGetUnresolvedReferencesWithObserverAndSnapshotStore(server, nil, nil)
}

// RegisterGetUnresolvedReferencesWithObserver adds the tool and observes its
// handler latency when observer is non-nil.
func RegisterGetUnresolvedReferencesWithObserver(server *sdkmcp.Server, observer Observer) {
	RegisterGetUnresolvedReferencesWithObserverAndSnapshotStore(server, observer, nil)
}

// RegisterGetUnresolvedReferencesWithSnapshotStore registers the tool over the
// immutable snapshot currently published by snapshotStore.
func RegisterGetUnresolvedReferencesWithSnapshotStore(server *sdkmcp.Server, snapshotStore *hotsnapshot.SnapshotStore) {
	RegisterGetUnresolvedReferencesWithObserverAndSnapshotStore(server, nil, snapshotStore)
}

// RegisterGetUnresolvedReferencesWithObserverAndSnapshotStore registers the
// tool over an immutable snapshot and optionally observes latency.
func RegisterGetUnresolvedReferencesWithObserverAndSnapshotStore(
	server *sdkmcp.Server,
	observer Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	callObservers ...CallObserver,
) {
	callObserver := firstCallObserver(callObservers)
	handler := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments GetUnresolvedReferencesInput,
	) (*sdkmcp.CallToolResult, Response[[]UnresolvedReferenceSummary], error) {
		return getUnresolvedReferences(ctx, request, arguments, snapshotStore)
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments GetUnresolvedReferencesInput,
		) (*sdkmcp.CallToolResult, Response[[]UnresolvedReferenceSummary], error) {
			start := time.Now()
			result, response, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, unresolvedReferencesToolName, request, start, response, err)
			return result, response, err
		}
	}
	addQueryTool(server, &sdkmcp.Tool{
		Name:        unresolvedReferencesToolName,
		Description: "Lists references that could not be resolved to an exact symbol.",
		Annotations: readOnlyClosedWorld(),
	}, handler)
}

func getUnresolvedReferences(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments GetUnresolvedReferencesInput,
	snapshotStore *hotsnapshot.SnapshotStore,
) (*sdkmcp.CallToolResult, Response[[]UnresolvedReferenceSummary], error) {
	options, err := normalizeUnresolvedReferencesInput(arguments)
	if err != nil {
		return nil, Response[[]UnresolvedReferenceSummary]{}, err
	}
	queryHash, err := HashQuery(unresolvedReferencesQuery{
		Tool: unresolvedReferencesToolName, Repo: options.Repo, Package: options.Package,
		RequestedPackage: options.RequestedPackage, RequestedSymbol: options.RequestedSymbol,
		Reason: options.Reason, Language: options.Language,
	})
	if err != nil {
		return nil, Response[[]UnresolvedReferenceSummary]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[[]UnresolvedReferenceSummary]{}, ErrIndexNotReady()
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[[]UnresolvedReferenceSummary]{}, ErrIndexNotReady()
	}

	metadata := snapshot.Metadata()
	offset := 0
	if arguments.Cursor != "" {
		cursor, err := DecodeCursor(arguments.Cursor)
		if err != nil {
			return nil, Response[[]UnresolvedReferenceSummary]{}, err
		}
		if err := cursor.ValidateAgainst(metadata.ID, queryHash, SortingVersionUnresolvedV1); err != nil {
			return nil, Response[[]UnresolvedReferenceSummary]{}, err
		}
		offset = cursor.Offset
	}

	results := make([]UnresolvedReferenceSummary, 0, options.Limit)
	coverage := Coverage{}
	total := 0
	for _, reference := range snapshot.UnresolvedReferences() {
		summary, err := unresolvedReferenceSummary(snapshot, reference)
		if err != nil {
			return nil, Response[[]UnresolvedReferenceSummary]{}, WrapToolError(
				CodeSnapshotUnavailable,
				"active snapshot contains invalid unresolved reference metadata",
				err,
			)
		}
		if !unresolvedReferenceMatches(summary, options) {
			continue
		}
		coverage.UnresolvedRelated++
		if total >= offset && len(results) < options.Limit {
			results = append(results, summary)
		}
		total++
	}

	hasMore := offset <= total && total-offset > len(results)
	var nextCursor *string
	if hasMore {
		cursor, err := NewCursor(metadata.ID, queryHash, offset+len(results), SortingVersionUnresolvedV1)
		if err != nil {
			return nil, Response[[]UnresolvedReferenceSummary]{}, err
		}
		encoded, err := cursor.Encode()
		if err != nil {
			return nil, Response[[]UnresolvedReferenceSummary]{}, err
		}
		nextCursor = &encoded
	}

	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[[]UnresolvedReferenceSummary]{
		SnapshotID: &snapshotID, SnapshotAgeMS: &snapshotAgeMS,
		Total: total, Returned: len(results), Truncated: hasMore, NextCursor: nextCursor,
		Coverage: coverage, Results: results,
	}, nil
}

func normalizeUnresolvedReferencesInput(arguments GetUnresolvedReferencesInput) (unresolvedReferencesOptions, error) {
	repo, err := normalizeReferenceFilter(arguments.Repo, "repo")
	if err != nil {
		return unresolvedReferencesOptions{}, err
	}
	pkg, err := normalizeReferenceFilter(arguments.Package, "package")
	if err != nil {
		return unresolvedReferencesOptions{}, err
	}
	requestedPackage, err := normalizeReferenceFilter(arguments.RequestedPackage, "requested_package")
	if err != nil {
		return unresolvedReferencesOptions{}, err
	}
	requestedSymbol, err := normalizeReferenceFilter(arguments.RequestedSymbol, "requested_symbol")
	if err != nil {
		return unresolvedReferencesOptions{}, err
	}
	reason, err := normalizeUnresolvedReason(arguments.Reason)
	if err != nil {
		return unresolvedReferencesOptions{}, err
	}
	language, err := normalizeReferenceFilter(arguments.Language, "language")
	if err != nil {
		return unresolvedReferencesOptions{}, err
	}
	limit := arguments.Limit
	if limit == 0 {
		limit = DefaultUnresolvedLimit
	}
	if limit < 1 || limit > MaximumUnresolvedLimit {
		return unresolvedReferencesOptions{}, NewToolError(CodeInvalidArgument, fmt.Sprintf("limit must be between 1 and %d", MaximumUnresolvedLimit))
	}
	return unresolvedReferencesOptions{
		Repo: repo, Package: pkg, RequestedPackage: requestedPackage,
		RequestedSymbol: requestedSymbol, Reason: reason, Language: language, Limit: limit,
	}, nil
}

// normalizeUnresolvedReason accepts any non-padded value. There is no
// cross-language reason vocabulary today: the Go loader emits
// goloader.UnresolvedReason values such as "package_not_found", while the
// TypeScript worker emits its own, like "PACKAGE_PROVIDER_NOT_FOUND".
// Validating against an invented list here would reject real facts.
func normalizeUnresolvedReason(value string) (string, error) {
	return normalizeReferenceFilter(value, "reason")
}
func unresolvedReferenceMatches(summary UnresolvedReferenceSummary, options unresolvedReferencesOptions) bool {
	switch {
	case options.Repo != "" && summary.RepositoryKey != options.Repo:
		return false
	case options.Package != "" && summary.PackageKey != options.Package:
		return false
	case options.RequestedPackage != "" && summary.RequestedPackage != options.RequestedPackage:
		return false
	case options.RequestedSymbol != "" && summary.RequestedSymbol != options.RequestedSymbol:
		return false
	case options.Reason != "" && summary.Reason != options.Reason:
		return false
	case options.Language != "" && summary.Language != options.Language:
		return false
	}
	return true
}

func unresolvedReferenceSummary(
	snapshot *hotsnapshot.GraphSnapshot,
	reference hotsnapshot.UnresolvedReferenceRecord,
) (UnresolvedReferenceSummary, error) {
	location, err := crossRepoUnresolvedLocation(snapshot, reference)
	if err != nil {
		return UnresolvedReferenceSummary{}, err
	}
	table := snapshot.Strings()
	key, keyOK := table.String(reference.Key)
	requestedPackage, packageOK := table.String(reference.RequestedPackage)
	requestedSymbol, symbolOK := table.String(reference.RequestedSymbol)
	reason, reasonOK := table.String(reference.Reason)
	detail, detailOK := table.String(reference.Detail)
	if !keyOK || !packageOK || !symbolOK || !reasonOK || !detailOK {
		return UnresolvedReferenceSummary{}, fmt.Errorf("unresolved reference contains invalid strings")
	}
	return UnresolvedReferenceSummary{
		UnresolvedKey: key,
		RepositoryKey: location.RepositoryKey, PackageKey: location.PackageKey, PackageName: location.PackageName,
		FileKey: location.FileKey, FilePath: location.FilePath, SourceSymbolKey: location.SymbolKey,
		Language:         firstString(location.Languages),
		RequestedPackage: requestedPackage, RequestedSymbol: requestedSymbol, Reason: reason, Detail: detail,
		StartLine: reference.StartLine, StartColumn: reference.StartColumn, StartOffset: reference.StartOffset,
	}, nil
}
