package tools

import (
	"context"
	"fmt"
	"sort"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

const (
	DefaultBlastRadiusDepth     = 3
	MaximumBlastRadiusDepth     = hotsnapshot.MaxTraversalDepth
	DefaultBlastRadiusLimit     = 50
	MaximumBlastRadiusLimit     = hotsnapshot.MaxExactResults
	DefaultBlastRadiusMaxNodes  = 5_000
	MaximumBlastRadiusMaxNodes  = hotsnapshot.MaxTraversalNodes
	SortingVersionBlastRadiusV1 = "blast-radius-v1"
	blastRadiusToolName         = "get_blast_radius"
)

// GetBlastRadiusInput bounds the impact traversal. edge_kinds and confidence
// gate which incoming edges may be followed, so they change what counts as
// affected; the tool reports aggregates, never a symbol listing.
type GetBlastRadiusInput struct {
	StableKey  string   `json:"stable_key"`
	Depth      int      `json:"depth,omitempty"`
	MaxNodes   int      `json:"max_nodes,omitempty"`
	EdgeKinds  []string `json:"edge_kinds,omitempty"`
	Confidence string   `json:"confidence,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	Cursor     string   `json:"cursor,omitempty"`
}

// BlastRadius is the impact of changing one symbol, grouped along the four
// axes a reviewer acts on. Counts are symbols reached by the incoming
// traversal, excluding the root itself.
//
// The envelope pages over ByPackage, the only axis that grows with the corpus;
// ByRepository, ByDepth and ByKind are complete in every page.
type BlastRadius struct {
	RootKey            string                    `json:"root_key"`
	RootRepositoryKey  string                    `json:"root_repository_key"`
	Depth              int                       `json:"depth"`
	MaxNodes           int                       `json:"max_nodes"`
	Affected           int                       `json:"affected"`
	DeepestDepth       int                       `json:"deepest_depth"`
	TraversalTruncated bool                      `json:"traversal_truncated"`
	ByRepository       []BlastRadiusGroup        `json:"by_repository"`
	ByDepth            []BlastRadiusDepthGroup   `json:"by_depth"`
	ByKind             []BlastRadiusGroup        `json:"by_kind"`
	ByPackage          []BlastRadiusPackageGroup `json:"by_package"`
}

// BlastRadiusGroup counts affected symbols under one repository key or one
// relation kind.
type BlastRadiusGroup struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type BlastRadiusDepthGroup struct {
	Depth int `json:"depth"`
	Count int `json:"count"`
}

type BlastRadiusPackageGroup struct {
	PackageKey    string `json:"package_key"`
	PackageName   string `json:"package_name"`
	RepositoryKey string `json:"repository_key"`
	Count         int    `json:"count"`
}

type blastRadiusOptions struct {
	StableKey  string
	Depth      int
	MaxNodes   int
	EdgeKinds  []string
	Confidence string
	Limit      int
}

type blastRadiusQuery struct {
	Tool       string   `json:"tool"`
	StableKey  string   `json:"stable_key"`
	Depth      int      `json:"depth"`
	MaxNodes   int      `json:"max_nodes"`
	EdgeKinds  []string `json:"edge_kinds,omitempty"`
	Confidence string   `json:"confidence,omitempty"`
}

// RegisterGetBlastRadius adds the read-only impact query without a graph
// source. Calls require a snapshot-backed registration to return data.
func RegisterGetBlastRadius(server *sdkmcp.Server) {
	RegisterGetBlastRadiusWithObserverAndSnapshotStore(server, nil, nil)
}

// RegisterGetBlastRadiusWithObserver adds the tool and observes its handler
// latency when observer is non-nil.
func RegisterGetBlastRadiusWithObserver(server *sdkmcp.Server, observer Observer) {
	RegisterGetBlastRadiusWithObserverAndSnapshotStore(server, observer, nil)
}

// RegisterGetBlastRadiusWithSnapshotStore registers the tool over the
// immutable snapshot currently published by snapshotStore.
func RegisterGetBlastRadiusWithSnapshotStore(server *sdkmcp.Server, snapshotStore *hotsnapshot.SnapshotStore) {
	RegisterGetBlastRadiusWithObserverAndSnapshotStore(server, nil, snapshotStore)
}

// RegisterGetBlastRadiusWithObserverAndSnapshotStore registers the tool over an
// immutable snapshot and optionally observes latency.
func RegisterGetBlastRadiusWithObserverAndSnapshotStore(
	server *sdkmcp.Server,
	observer Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
) {
	handler := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments GetBlastRadiusInput,
	) (*sdkmcp.CallToolResult, Response[BlastRadius], error) {
		return getBlastRadius(ctx, request, arguments, snapshotStore)
	}
	if observer != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments GetBlastRadiusInput,
		) (*sdkmcp.CallToolResult, Response[BlastRadius], error) {
			start := time.Now()
			result, response, err := underlying(ctx, request, arguments)
			observer(blastRadiusToolName, time.Since(start))
			return result, response, err
		}
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        blastRadiusToolName,
		Description: "Groups the bounded incoming impact of a symbol by repository, package, depth, and relation kind.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, handler)
}

func getBlastRadius(
	ctx context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments GetBlastRadiusInput,
	snapshotStore *hotsnapshot.SnapshotStore,
) (*sdkmcp.CallToolResult, Response[BlastRadius], error) {
	options, err := normalizeBlastRadiusInput(arguments)
	if err != nil {
		return nil, Response[BlastRadius]{}, err
	}
	queryHash, err := HashQuery(blastRadiusQuery{
		Tool: blastRadiusToolName, StableKey: options.StableKey, Depth: options.Depth,
		MaxNodes: options.MaxNodes, EdgeKinds: options.EdgeKinds, Confidence: options.Confidence,
	})
	if err != nil {
		return nil, Response[BlastRadius]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[BlastRadius]{}, NewToolError(CodeIndexNotReady, "no HotSnapshot is published")
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[BlastRadius]{}, NewToolError(CodeIndexNotReady, "no HotSnapshot is published")
	}
	rootID, found := snapshot.SymbolByStableKey(hotsnapshot.StableKey(options.StableKey))
	if !found {
		return nil, Response[BlastRadius]{}, NewToolError(CodeSymbolNotFound, fmt.Sprintf("symbol %q was not found", options.StableKey))
	}
	root, _, rootRepository, _, err := symbolReferenceLocation(snapshot, rootID)
	if err != nil {
		return nil, Response[BlastRadius]{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid root metadata", err)
	}

	metadata := snapshot.Metadata()
	offset := 0
	if arguments.Cursor != "" {
		cursor, err := DecodeCursor(arguments.Cursor)
		if err != nil {
			return nil, Response[BlastRadius]{}, err
		}
		if err := cursor.ValidateAgainst(metadata.ID, queryHash, SortingVersionBlastRadiusV1); err != nil {
			return nil, Response[BlastRadius]{}, err
		}
		offset = cursor.Offset
	}

	traversalOptions, err := blastRadiusTraversalOptions(ctx, options)
	if err != nil {
		return nil, Response[BlastRadius]{}, err
	}
	traversal, err := snapshot.Traverse(rootID, traversalOptions)
	if err != nil {
		return nil, Response[BlastRadius]{}, classifyTraversalError(err)
	}

	radius, coverage, err := blastRadiusGroups(snapshot, traversal.Visits)
	if err != nil {
		return nil, Response[BlastRadius]{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid impact metadata", err)
	}
	radius.RootKey = string(root.StableKey)
	radius.RootRepositoryKey = rootRepository
	radius.Depth = options.Depth
	radius.MaxNodes = options.MaxNodes
	radius.TraversalTruncated = traversal.Truncated

	total := len(radius.ByPackage)
	if offset > total {
		offset = total
	}
	end := offset + options.Limit
	if end > total {
		end = total
	}
	radius.ByPackage = append([]BlastRadiusPackageGroup(nil), radius.ByPackage[offset:end]...)
	hasMore := end < total
	var nextCursor *string
	if hasMore {
		cursor, err := NewCursor(metadata.ID, queryHash, end, SortingVersionBlastRadiusV1)
		if err != nil {
			return nil, Response[BlastRadius]{}, err
		}
		encoded, err := cursor.Encode()
		if err != nil {
			return nil, Response[BlastRadius]{}, err
		}
		nextCursor = &encoded
	}

	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[BlastRadius]{
		SnapshotID: &snapshotID, SnapshotAgeMS: &snapshotAgeMS,
		Total: total, Returned: len(radius.ByPackage), Truncated: hasMore, NextCursor: nextCursor,
		Coverage: coverage, Results: radius,
	}, nil
}

func normalizeBlastRadiusInput(arguments GetBlastRadiusInput) (blastRadiusOptions, error) {
	stableKey, err := normalizeSymbolStableKey(arguments.StableKey)
	if err != nil {
		return blastRadiusOptions{}, err
	}
	depth := arguments.Depth
	if depth == 0 {
		depth = DefaultBlastRadiusDepth
	}
	if depth < 1 || depth > MaximumBlastRadiusDepth {
		return blastRadiusOptions{}, NewToolError(CodeInvalidArgument, fmt.Sprintf("depth must be between 1 and %d", MaximumBlastRadiusDepth))
	}
	maxNodes := arguments.MaxNodes
	if maxNodes == 0 {
		maxNodes = DefaultBlastRadiusMaxNodes
	}
	if maxNodes < 1 || maxNodes > MaximumBlastRadiusMaxNodes {
		return blastRadiusOptions{}, NewToolError(CodeInvalidArgument, fmt.Sprintf("max_nodes must be between 1 and %d", MaximumBlastRadiusMaxNodes))
	}
	edgeKinds, err := normalizeReferenceEdgeKinds(arguments.EdgeKinds)
	if err != nil {
		return blastRadiusOptions{}, err
	}
	confidence, err := normalizeReferenceConfidence(arguments.Confidence)
	if err != nil {
		return blastRadiusOptions{}, err
	}
	limit := arguments.Limit
	if limit == 0 {
		limit = DefaultBlastRadiusLimit
	}
	if limit < 1 || limit > MaximumBlastRadiusLimit {
		return blastRadiusOptions{}, NewToolError(CodeInvalidArgument, fmt.Sprintf("limit must be between 1 and %d", MaximumBlastRadiusLimit))
	}
	return blastRadiusOptions{
		StableKey: stableKey, Depth: depth, MaxNodes: maxNodes,
		EdgeKinds: edgeKinds, Confidence: confidence, Limit: limit,
	}, nil
}

func blastRadiusTraversalOptions(ctx context.Context, options blastRadiusOptions) (hotsnapshot.TraversalOptions, error) {
	traversal, err := dependencyTraversalOptions(ctx, traceDependenciesOptions{
		Depth: options.Depth, MaxNodes: options.MaxNodes,
		EdgeKinds: options.EdgeKinds, Confidence: options.Confidence,
	})
	if err != nil {
		return hotsnapshot.TraversalOptions{}, err
	}
	traversal.Direction = hotsnapshot.TraversalIncoming
	return traversal, nil
}

// blastRadiusGroups folds the frontier into the four aggregation axes. The root
// is excluded: a symbol is not affected by its own change.
func blastRadiusGroups(
	snapshot *hotsnapshot.GraphSnapshot,
	visits []hotsnapshot.TraversalVisit,
) (BlastRadius, Coverage, error) {
	radius := BlastRadius{}
	coverage := Coverage{}
	repositories := make(map[string]int)
	kinds := make(map[string]int)
	depths := make(map[int]int)
	packages := make(map[string]*BlastRadiusPackageGroup)

	for _, visit := range visits {
		if visit.Source == hotsnapshot.InvalidSymbolID {
			continue
		}
		symbol, _, repositoryKey, _, err := symbolReferenceLocation(snapshot, visit.ID)
		if err != nil {
			return BlastRadius{}, Coverage{}, err
		}
		decoded, isReference, err := decodeReferenceEdge(visit.Edge)
		if err != nil {
			return BlastRadius{}, Coverage{}, err
		}
		if !isReference {
			return BlastRadius{}, Coverage{}, fmt.Errorf("symbol edge %d->%d has non-reference kind %d", visit.Source, visit.ID, visit.Edge.Kind)
		}
		packageKey, packageName, err := symbolPackageIdentity(snapshot, symbol)
		if err != nil {
			return BlastRadius{}, Coverage{}, err
		}

		radius.Affected++
		if int(visit.Depth) > radius.DeepestDepth {
			radius.DeepestDepth = int(visit.Depth)
		}
		repositories[repositoryKey]++
		kinds[string(decoded.Kind)]++
		depths[int(visit.Depth)]++
		group, exists := packages[packageKey]
		if !exists {
			group = &BlastRadiusPackageGroup{PackageKey: packageKey, PackageName: packageName, RepositoryKey: repositoryKey}
			packages[packageKey] = group
		}
		group.Count++
		addReferenceCoverage(&coverage, decoded.Confidence)
	}

	radius.ByRepository = sortedBlastRadiusGroups(repositories)
	radius.ByKind = sortedBlastRadiusGroups(kinds)
	radius.ByDepth = make([]BlastRadiusDepthGroup, 0, len(depths))
	for depth, count := range depths {
		radius.ByDepth = append(radius.ByDepth, BlastRadiusDepthGroup{Depth: depth, Count: count})
	}
	sort.Slice(radius.ByDepth, func(i, j int) bool { return radius.ByDepth[i].Depth < radius.ByDepth[j].Depth })
	radius.ByPackage = make([]BlastRadiusPackageGroup, 0, len(packages))
	for _, group := range packages {
		radius.ByPackage = append(radius.ByPackage, *group)
	}
	sort.Slice(radius.ByPackage, func(i, j int) bool { return radius.ByPackage[i].PackageKey < radius.ByPackage[j].PackageKey })
	return radius, coverage, nil
}

func symbolPackageIdentity(snapshot *hotsnapshot.GraphSnapshot, symbol hotsnapshot.SymbolRecord) (string, string, error) {
	file, found := snapshot.File(symbol.File)
	if !found {
		return "", "", fmt.Errorf("symbol %q references missing file %d", symbol.StableKey, symbol.File)
	}
	pkg, found := snapshot.Package(file.Package)
	if !found {
		return "", "", fmt.Errorf("symbol %q references missing package %d", symbol.StableKey, file.Package)
	}
	table := snapshot.Strings()
	key, keyOK := table.String(pkg.Key)
	name, nameOK := table.String(pkg.Name)
	if !keyOK || !nameOK {
		return "", "", fmt.Errorf("symbol %q references invalid package strings", symbol.StableKey)
	}
	return key, name, nil
}

func sortedBlastRadiusGroups(counts map[string]int) []BlastRadiusGroup {
	groups := make([]BlastRadiusGroup, 0, len(counts))
	for key, count := range counts {
		groups = append(groups, BlastRadiusGroup{Key: key, Count: count})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Key < groups[j].Key })
	return groups
}
