package tools

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

const (
	DefaultDependencyDepth       = 3
	MaximumDependencyDepth       = hotsnapshot.MaxTraversalDepth
	DefaultDependencyLimit       = 50
	MaximumDependencyLimit       = hotsnapshot.MaxExactResults
	DefaultDependencyMaxNodes    = 5_000
	MaximumDependencyMaxNodes    = hotsnapshot.MaxTraversalNodes
	SortingVersionDependenciesV1 = "dependencies-v1"
	traceDependenciesToolName    = "trace_dependencies"
)

// TraceDependenciesInput bounds one dependency traversal. edge_kinds and
// confidence gate which edges the traversal may follow, so they change what is
// reachable; repo and language only select which reached symbols are returned.
type TraceDependenciesInput struct {
	StableKey  string   `json:"stable_key"`
	Depth      int      `json:"depth,omitempty"`
	MaxNodes   int      `json:"max_nodes,omitempty"`
	Repo       string   `json:"repo,omitempty"`
	Language   string   `json:"language,omitempty"`
	EdgeKinds  []string `json:"edge_kinds,omitempty"`
	Confidence string   `json:"confidence,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	Cursor     string   `json:"cursor,omitempty"`
}

// DependencyTrace is the traversal itself: the root, what the bounds did to
// it, and one page of reached symbols. The envelope's total, returned and
// next_cursor page over Nodes.
type DependencyTrace struct {
	RootKey            string          `json:"root_key"`
	RootRepositoryKey  string          `json:"root_repository_key"`
	Depth              int             `json:"depth"`
	MaxNodes           int             `json:"max_nodes"`
	Reached            int             `json:"reached"`
	DeepestDepth       int             `json:"deepest_depth"`
	TraversalTruncated bool            `json:"traversal_truncated"`
	Nodes              []ReachedSymbol `json:"nodes"`
}

// ReachedSymbol is one symbol a bounded traversal reached, together with the
// edge it first arrived by. That edge is a fact of this traversal, not the only
// route: a breadth-first frontier records the shortest one it found. Both
// trace_dependencies and get_blast_radius return this shape.
type ReachedSymbol struct {
	StableKey     string `json:"stable_key"`
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name"`
	Kind          string `json:"kind"`
	Depth         int    `json:"depth"`
	RepositoryKey string `json:"repository_key"`
	Language      string `json:"language"`
	FileKey       string `json:"file_key"`
	FilePath      string `json:"file_path"`

	// ReachedFromKey is the already-reached symbol this one was discovered
	// from, which is the edge's target when the traversal runs incoming. The
	// edge's own orientation is therefore not implied by this field.
	ReachedFromKey string `json:"reached_from_key"`
	ViaKind        string `json:"via_kind"`
	ViaConfidence  string `json:"via_confidence"`
	ViaProvenance  string `json:"via_provenance"`
}

type traceDependenciesOptions struct {
	StableKey  string
	Depth      int
	MaxNodes   int
	Repo       string
	Language   string
	EdgeKinds  []string
	Confidence string
	Limit      int
}

type traceDependenciesQuery struct {
	Tool       string   `json:"tool"`
	StableKey  string   `json:"stable_key"`
	Depth      int      `json:"depth"`
	MaxNodes   int      `json:"max_nodes"`
	Repo       string   `json:"repo,omitempty"`
	Language   string   `json:"language,omitempty"`
	EdgeKinds  []string `json:"edge_kinds,omitempty"`
	Confidence string   `json:"confidence,omitempty"`
}

// RegisterTraceDependencies adds the read-only dependency traversal without a
// graph source. Calls require a snapshot-backed registration to return data.
func RegisterTraceDependencies(server *sdkmcp.Server) {
	RegisterTraceDependenciesWithObserverAndSnapshotStore(server, nil, nil)
}

// RegisterTraceDependenciesWithObserver adds the tool and observes its handler
// latency when observer is non-nil.
func RegisterTraceDependenciesWithObserver(server *sdkmcp.Server, observer Observer) {
	RegisterTraceDependenciesWithObserverAndSnapshotStore(server, observer, nil)
}

// RegisterTraceDependenciesWithSnapshotStore registers the tool over the
// immutable snapshot currently published by snapshotStore.
func RegisterTraceDependenciesWithSnapshotStore(server *sdkmcp.Server, snapshotStore *hotsnapshot.SnapshotStore) {
	RegisterTraceDependenciesWithObserverAndSnapshotStore(server, nil, snapshotStore)
}

// RegisterTraceDependenciesWithObserverAndSnapshotStore registers the tool over
// an immutable snapshot and optionally observes latency.
func RegisterTraceDependenciesWithObserverAndSnapshotStore(
	server *sdkmcp.Server,
	observer Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	callObservers ...CallObserver,
) {
	callObserver := firstCallObserver(callObservers)
	handler := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments TraceDependenciesInput,
	) (*sdkmcp.CallToolResult, Response[DependencyTrace], error) {
		return traceDependencies(ctx, request, arguments, snapshotStore)
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments TraceDependenciesInput,
		) (*sdkmcp.CallToolResult, Response[DependencyTrace], error) {
			start := time.Now()
			result, response, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, traceDependenciesToolName, start, response, err)
			return result, response, err
		}
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        traceDependenciesToolName,
		Description: "Traces the bounded outgoing dependency graph of a symbol.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, handler)
}

func traceDependencies(
	ctx context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments TraceDependenciesInput,
	snapshotStore *hotsnapshot.SnapshotStore,
) (*sdkmcp.CallToolResult, Response[DependencyTrace], error) {
	options, err := normalizeTraceDependenciesInput(arguments)
	if err != nil {
		return nil, Response[DependencyTrace]{}, err
	}
	queryHash, err := HashQuery(traceDependenciesQuery{
		Tool: traceDependenciesToolName, StableKey: options.StableKey, Depth: options.Depth,
		MaxNodes: options.MaxNodes, Repo: options.Repo, Language: options.Language,
		EdgeKinds: options.EdgeKinds, Confidence: options.Confidence,
	})
	if err != nil {
		return nil, Response[DependencyTrace]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[DependencyTrace]{}, NewToolError(CodeIndexNotReady, "no HotSnapshot is published")
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[DependencyTrace]{}, NewToolError(CodeIndexNotReady, "no HotSnapshot is published")
	}
	rootID, found := snapshot.SymbolByStableKey(hotsnapshot.StableKey(options.StableKey))
	if !found {
		return nil, Response[DependencyTrace]{}, NewToolError(CodeSymbolNotFound, fmt.Sprintf("symbol %q was not found", options.StableKey))
	}
	root, _, rootRepository, _, err := symbolReferenceLocation(snapshot, rootID)
	if err != nil {
		return nil, Response[DependencyTrace]{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid root metadata", err)
	}

	metadata := snapshot.Metadata()
	offset := 0
	if arguments.Cursor != "" {
		cursor, err := DecodeCursor(arguments.Cursor)
		if err != nil {
			return nil, Response[DependencyTrace]{}, err
		}
		if err := cursor.ValidateAgainst(metadata.ID, queryHash, SortingVersionDependenciesV1); err != nil {
			return nil, Response[DependencyTrace]{}, err
		}
		offset = cursor.Offset
	}

	traversalOptions, err := dependencyTraversalOptions(ctx, options)
	if err != nil {
		return nil, Response[DependencyTrace]{}, err
	}
	traversal, err := snapshot.Traverse(rootID, traversalOptions)
	if err != nil {
		return nil, Response[DependencyTrace]{}, classifyTraversalError(err)
	}

	nodes, coverage, deepest, err := dependencyNodes(snapshot, traversal.Visits, options)
	if err != nil {
		return nil, Response[DependencyTrace]{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid dependency metadata", err)
	}
	total := len(nodes)
	if offset > total {
		offset = total
	}
	end := offset + options.Limit
	if end > total {
		end = total
	}
	page := append([]ReachedSymbol(nil), nodes[offset:end]...)
	hasMore := end < total
	var nextCursor *string
	if hasMore {
		cursor, err := NewCursor(metadata.ID, queryHash, end, SortingVersionDependenciesV1)
		if err != nil {
			return nil, Response[DependencyTrace]{}, err
		}
		encoded, err := cursor.Encode()
		if err != nil {
			return nil, Response[DependencyTrace]{}, err
		}
		nextCursor = &encoded
	}

	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[DependencyTrace]{
		SnapshotID: &snapshotID, SnapshotAgeMS: &snapshotAgeMS,
		Total: total, Returned: len(page), Truncated: hasMore, NextCursor: nextCursor,
		Coverage: coverage,
		Results: DependencyTrace{
			RootKey: string(root.StableKey), RootRepositoryKey: rootRepository,
			Depth: options.Depth, MaxNodes: options.MaxNodes,
			Reached: len(traversal.Visits) - 1, DeepestDepth: deepest,
			TraversalTruncated: traversal.Truncated, Nodes: page,
		},
	}, nil
}

func normalizeTraceDependenciesInput(arguments TraceDependenciesInput) (traceDependenciesOptions, error) {
	stableKey, err := normalizeSymbolStableKey(arguments.StableKey)
	if err != nil {
		return traceDependenciesOptions{}, err
	}
	depth := arguments.Depth
	if depth == 0 {
		depth = DefaultDependencyDepth
	}
	if depth < 1 || depth > MaximumDependencyDepth {
		return traceDependenciesOptions{}, NewToolError(CodeInvalidArgument, fmt.Sprintf("depth must be between 1 and %d", MaximumDependencyDepth))
	}
	maxNodes := arguments.MaxNodes
	if maxNodes == 0 {
		maxNodes = DefaultDependencyMaxNodes
	}
	if maxNodes < 1 || maxNodes > MaximumDependencyMaxNodes {
		return traceDependenciesOptions{}, NewToolError(CodeInvalidArgument, fmt.Sprintf("max_nodes must be between 1 and %d", MaximumDependencyMaxNodes))
	}
	repo, err := normalizeReferenceFilter(arguments.Repo, "repo")
	if err != nil {
		return traceDependenciesOptions{}, err
	}
	language, err := normalizeReferenceFilter(arguments.Language, "language")
	if err != nil {
		return traceDependenciesOptions{}, err
	}
	edgeKinds, err := normalizeReferenceEdgeKinds(arguments.EdgeKinds)
	if err != nil {
		return traceDependenciesOptions{}, err
	}
	confidence, err := normalizeReferenceConfidence(arguments.Confidence)
	if err != nil {
		return traceDependenciesOptions{}, err
	}
	limit := arguments.Limit
	if limit == 0 {
		limit = DefaultDependencyLimit
	}
	if limit < 1 || limit > MaximumDependencyLimit {
		return traceDependenciesOptions{}, NewToolError(CodeInvalidArgument, fmt.Sprintf("limit must be between 1 and %d", MaximumDependencyLimit))
	}
	return traceDependenciesOptions{
		StableKey: stableKey, Depth: depth, MaxNodes: maxNodes, Repo: repo,
		Language: language, EdgeKinds: edgeKinds, Confidence: confidence, Limit: limit,
	}, nil
}

// dependencyTraversalOptions translates the validated request into traversal
// bounds. The request's own deadline, when the client set one, becomes the
// traversal's logical timeout.
func dependencyTraversalOptions(ctx context.Context, options traceDependenciesOptions) (hotsnapshot.TraversalOptions, error) {
	traversal := hotsnapshot.TraversalOptions{
		Direction: hotsnapshot.TraversalOutgoing,
		MaxDepth:  options.Depth,
		MaxNodes:  options.MaxNodes,
	}
	if ctx != nil {
		if deadline, ok := ctx.Deadline(); ok {
			traversal.Deadline = deadline
		}
	}
	if len(options.EdgeKinds) > 0 {
		traversal.EdgeKinds = make([]uint8, 0, len(options.EdgeKinds))
		for _, kind := range options.EdgeKinds {
			code, err := facts.EdgeKind(kind).Code()
			if err != nil {
				return hotsnapshot.TraversalOptions{}, NewToolError(CodeInvalidArgument, fmt.Sprintf("edge kind %q is unsupported", kind))
			}
			traversal.EdgeKinds = append(traversal.EdgeKinds, code)
		}
	}
	if options.Confidence != "" {
		code, err := facts.Confidence(options.Confidence).Code()
		if err != nil {
			return hotsnapshot.TraversalOptions{}, NewToolError(CodeInvalidArgument, fmt.Sprintf("confidence %q is unsupported", options.Confidence))
		}
		traversal.Confidences = []uint8{code}
	}
	return traversal, nil
}

// dependencyNodes converts the frontier into wire rows. The start symbol is
// reported as the root, never as its own dependency. Repository and language
// filters select rows here, after reachability: a dependency found through a
// symbol in another repository is still reported.
func dependencyNodes(
	snapshot *hotsnapshot.GraphSnapshot,
	visits []hotsnapshot.TraversalVisit,
	options traceDependenciesOptions,
) ([]ReachedSymbol, Coverage, int, error) {
	nodes := make([]ReachedSymbol, 0, len(visits))
	coverage := Coverage{}
	deepest := 0
	for _, visit := range visits {
		if visit.Source == hotsnapshot.InvalidSymbolID {
			continue
		}
		if int(visit.Depth) > deepest {
			deepest = int(visit.Depth)
		}
		symbol, file, repositoryKey, languages, err := symbolReferenceLocation(snapshot, visit.ID)
		if err != nil {
			return nil, Coverage{}, 0, err
		}
		if options.Repo != "" && repositoryKey != options.Repo {
			continue
		}
		if options.Language != "" && !containsString(languages, options.Language) {
			continue
		}
		source, found := snapshot.Symbol(visit.Source)
		if !found {
			return nil, Coverage{}, 0, fmt.Errorf("symbol index %d is missing", visit.Source)
		}
		decoded, isReference, err := decodeReferenceEdge(visit.Edge)
		if err != nil {
			return nil, Coverage{}, 0, err
		}
		if !isReference {
			// The symbol CSR only carries symbol-to-symbol relations, so a
			// containment or package kind here means the snapshot disagrees
			// with the vocabulary it was built from.
			return nil, Coverage{}, 0, fmt.Errorf("symbol edge %d->%d has non-reference kind %d", visit.Source, visit.ID, visit.Edge.Kind)
		}
		table := snapshot.Strings()
		name, nameOK := table.String(symbol.Name)
		qualifiedName, qualifiedOK := table.String(symbol.QualifiedName)
		kind, kindOK := table.String(symbol.Kind)
		if !nameOK || !qualifiedOK || !kindOK {
			return nil, Coverage{}, 0, fmt.Errorf("symbol %q has invalid display strings", symbol.StableKey)
		}
		addReferenceCoverage(&coverage, decoded.Confidence)
		nodes = append(nodes, ReachedSymbol{
			StableKey: string(symbol.StableKey), Name: name, QualifiedName: qualifiedName, Kind: kind,
			Depth: int(visit.Depth), RepositoryKey: repositoryKey, Language: firstString(languages),
			FileKey: file.key, FilePath: file.path,
			ReachedFromKey: string(source.StableKey), ViaKind: string(decoded.Kind),
			ViaConfidence: string(decoded.Confidence), ViaProvenance: string(decoded.Provenance),
		})
	}
	return nodes, coverage, deepest, nil
}

func classifyTraversalError(err error) error {
	switch {
	case errors.Is(err, hotsnapshot.ErrTraversalTimeout):
		return WrapToolError(CodeTraversalLimitReached, "dependency traversal exceeded its deadline", err)
	case errors.Is(err, hotsnapshot.ErrInvalidTraversal):
		return WrapToolError(CodeInvalidArgument, "dependency traversal bounds are invalid for this snapshot", err)
	default:
		return WrapToolError(CodeSnapshotUnavailable, "dependency traversal could not run", err)
	}
}
