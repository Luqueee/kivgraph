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
	StableKey      string   `json:"stable_key,omitempty"`
	QualifiedName  string   `json:"qualified_name,omitempty"`
	Repository     string   `json:"repository,omitempty"`
	Path           string   `json:"path,omitempty"`
	Depth          int      `json:"depth,omitempty"`
	MaxNodes       int      `json:"max_nodes,omitempty"`
	EdgeKinds      []string `json:"edge_kinds,omitempty"`
	Confidence     string   `json:"confidence,omitempty"`
	IncludeDerived bool     `json:"include_derived,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	Cursor         string   `json:"cursor,omitempty"`
	ResponseFormat string   `json:"response_format,omitempty"`
}

// BlastRadius is the impact of changing one symbol: the affected symbols
// themselves plus the four axes a reviewer acts on. The root is excluded
// everywhere, because a symbol is not affected by its own change.
//
// The envelope pages over Symbols. Every axis is computed over the whole
// traversal, not over the page, so aggregates stay stable while paging.
type BlastRadius struct {
	RootKey            string                    `json:"root_key"`
	RootRepository     string                    `json:"root_repository"`
	Depth              int                       `json:"depth"`
	MaxNodes           int                       `json:"max_nodes"`
	Affected           int                       `json:"affected"`
	DeepestDepth       int                       `json:"deepest_depth"`
	TraversalTruncated bool                      `json:"traversal_truncated"`
	Symbols            []ReachedSymbol           `json:"symbols"`
	ByRepository       []BlastRadiusGroup        `json:"by_repository"`
	ByDepth            []BlastRadiusDepthGroup   `json:"by_depth"`
	ByKind             []BlastRadiusGroup        `json:"by_kind"`
	ByPackage          []BlastRadiusPackageGroup `json:"by_package"`
}

// BlastRadiusGroup counts affected symbols under one repository key or one
// relation kind. A symbol reaching the traversed subgraph through several
// relation kinds is counted once per kind, so by_kind can exceed affected;
// by_repository and by_package always partition it.
type BlastRadiusGroup struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type BlastRadiusDepthGroup struct {
	Depth int `json:"depth"`
	Count int `json:"count"`
}

type BlastRadiusPackageGroup struct {
	PackageKey  string `json:"package_key"`
	PackageName string `json:"package_name"`
	Repository  string `json:"repository"`
	Count       int    `json:"count"`
}

type blastRadiusOptions struct {
	Selector   symbolSelector
	Depth      int
	MaxNodes   int
	EdgeKinds  []string
	Confidence string
	Limit      int
	Format     string
	Derived    derivedFilter
}

type blastRadiusQuery struct {
	Tool          string   `json:"tool"`
	StableKey     string   `json:"stable_key,omitempty"`
	QualifiedName string   `json:"qualified_name,omitempty"`
	Repository    string   `json:"repository,omitempty"`
	Path          string   `json:"path,omitempty"`
	Depth         int      `json:"depth"`
	MaxNodes      int      `json:"max_nodes"`
	EdgeKinds     []string `json:"edge_kinds,omitempty"`
	Confidence    string   `json:"confidence,omitempty"`
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
	callObservers ...CallObserver,
) {
	callObserver := firstCallObserver(callObservers)
	handler := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments GetBlastRadiusInput,
	) (*sdkmcp.CallToolResult, Response[BlastRadius], error) {
		return getBlastRadius(ctx, request, arguments, snapshotStore)
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments GetBlastRadiusInput,
		) (*sdkmcp.CallToolResult, Response[BlastRadius], error) {
			start := time.Now()
			result, response, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, blastRadiusToolName, start, response, err)
			return result, response, err
		}
	}
	addQueryTool(server, &sdkmcp.Tool{
		Name:        blastRadiusToolName,
		Description: "What a change to this symbol reaches, by repository, package, depth and relation kind. Grep does not follow a chain.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
		Meta:        traversalMeta(MaximumTraversalResultChars),
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
		Tool: blastRadiusToolName, StableKey: options.Selector.StableKey, QualifiedName: options.Selector.QualifiedName,
		Repository: options.Selector.Repository, Path: options.Selector.Path, Depth: options.Depth,
		MaxNodes: options.MaxNodes, EdgeKinds: options.EdgeKinds, Confidence: options.Confidence,
	})
	if err != nil {
		return nil, Response[BlastRadius]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[BlastRadius]{}, ErrIndexNotReady()
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[BlastRadius]{}, ErrIndexNotReady()
	}
	rootID, err := resolveSymbolSelector(snapshot, options.Selector)
	if err != nil {
		return nil, Response[BlastRadius]{}, err
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

	radius, coverage, err := blastRadiusGroups(snapshot, traversal.Visits, traversalOptions, options.Format, options.Derived)
	if err != nil {
		return nil, Response[BlastRadius]{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid impact metadata", err)
	}
	radius.RootKey = string(root.StableKey)
	radius.RootRepository = rootRepository.name
	radius.Depth = options.Depth
	radius.MaxNodes = options.MaxNodes
	radius.TraversalTruncated = traversal.Truncated

	total := len(radius.Symbols)
	if offset > total {
		offset = total
	}
	end := offset + options.Limit
	if end > total {
		end = total
	}
	radius.Symbols = append([]ReachedSymbol(nil), radius.Symbols[offset:end]...)
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

	// An impact answer is the one that most needs its own bound: an agent
	// reads it to decide whether a change is safe, and a silent gap here is
	// a refactor that compiles and breaks something nobody looked at.
	rootName, rootNameOK := snapshot.Strings().String(root.Name)
	if !rootNameOK {
		return nil, Response[BlastRadius]{}, WrapToolError(
			CodeSnapshotUnavailable,
			"active snapshot contains invalid impact metadata",
			fmt.Errorf("symbol %q has an invalid name", root.StableKey),
		)
	}
	rootFile, rootFileFound := snapshot.File(root.File)
	rootRepositoryID := hotsnapshot.InvalidRepositoryID
	if rootFileFound {
		rootRepositoryID = rootFile.Repository
	}
	completeness, unresolvedRelated, err := completenessFor(snapshot, rootName, rootRepositoryID)
	if err != nil {
		return nil, Response[BlastRadius]{}, WrapToolError(
			CodeSnapshotUnavailable,
			"active snapshot contains invalid unresolved metadata",
			err,
		)
	}
	coverage.UnresolvedRelated += unresolvedRelated

	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[BlastRadius]{
		SnapshotID: &snapshotID, SnapshotAgeMS: &snapshotAgeMS,
		Total: total, Returned: len(radius.Symbols), Truncated: hasMore, NextCursor: nextCursor,
		Coverage: coverage, Completeness: &completeness,
		Guidance: traversalGuidance(blastRadiusToolName, total, len(radius.Symbols), hasMore),
		Results:  radius,
	}, nil
}

func normalizeBlastRadiusInput(arguments GetBlastRadiusInput) (blastRadiusOptions, error) {
	selector, err := normalizeSymbolSelector(arguments.StableKey, arguments.Repository, arguments.Path, arguments.QualifiedName)
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
	format, err := normalizeResponseFormat(arguments.ResponseFormat)
	if err != nil {
		return blastRadiusOptions{}, err
	}
	derived := newDerivedFilter(arguments.IncludeDerived, "")
	return blastRadiusOptions{
		Selector: selector, Depth: depth, MaxNodes: maxNodes,
		EdgeKinds: edgeKinds, Confidence: confidence, Limit: limit, Format: format,
		Derived: derived,
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

// blastRadiusGroups folds the frontier into the affected symbol list and the
// four aggregation axes. The root is excluded: a symbol is not affected by its
// own change.
//
// by_kind is not read off the discovering edge. A consumer can reach the
// traversed subgraph through several relations at once -- calling a function
// and using its type, say -- and reporting only the edge BFS happened to take
// first would hide the others. Every edge from the affected symbol into the
// visited set is inspected, under the same filters the traversal used.
func blastRadiusGroups(
	snapshot *hotsnapshot.GraphSnapshot,
	visits []hotsnapshot.TraversalVisit,
	traversal hotsnapshot.TraversalOptions,
	format string,
	derived derivedFilter,
) (BlastRadius, Coverage, error) {
	radius := BlastRadius{Symbols: make([]ReachedSymbol, 0, len(visits))}
	coverage := Coverage{}
	repositories := make(map[string]int)
	kinds := make(map[string]int)
	depths := make(map[int]int)
	packages := make(map[string]*BlastRadiusPackageGroup)
	visited := visitedSymbolSet(visits)

	for _, visit := range visits {
		if visit.Source == hotsnapshot.InvalidSymbolID {
			continue
		}
		symbol, file, repository, languages, err := symbolReferenceLocation(snapshot, visit.ID)
		if err != nil {
			return BlastRadius{}, Coverage{}, err
		}
		if !derived.keepsRepository(repository.name) {
			continue
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
		source, found := snapshot.Symbol(visit.Source)
		if !found {
			return BlastRadius{}, Coverage{}, fmt.Errorf("symbol index %d is missing", visit.Source)
		}
		table := snapshot.Strings()
		name, nameOK := table.String(symbol.Name)
		qualifiedName, qualifiedOK := table.String(symbol.QualifiedName)
		kind, kindOK := table.String(symbol.Kind)
		if !nameOK || !qualifiedOK || !kindOK {
			return BlastRadius{}, Coverage{}, fmt.Errorf("symbol %q has invalid display strings", symbol.StableKey)
		}

		radius.Affected++
		if int(visit.Depth) > radius.DeepestDepth {
			radius.DeepestDepth = int(visit.Depth)
		}
		repositories[repository.name]++
		depths[int(visit.Depth)]++
		if err := countImpactKinds(snapshot, visit.ID, visited, traversal, kinds); err != nil {
			return BlastRadius{}, Coverage{}, err
		}
		group, exists := packages[packageKey]
		if !exists {
			group = &BlastRadiusPackageGroup{PackageKey: packageKey, PackageName: packageName, Repository: repository.name}
			packages[packageKey] = group
		}
		group.Count++
		addReferenceCoverage(&coverage, decoded.Confidence)
		reachedFrom, sourceOK := table.String(source.QualifiedName)
		if !sourceOK {
			return BlastRadius{}, Coverage{}, fmt.Errorf("symbol %q has an invalid qualified name", source.StableKey)
		}
		row := ReachedSymbol{
			Name: name, QualifiedName: qualifiedName, Kind: kind,
			Depth: int(visit.Depth), Repository: repository.name, Language: firstString(languages),
			FilePath: file.path, StartLine: symbol.StartLine, EndLine: symbol.EndLine,
			ReachedFrom: reachedFrom, ViaKind: string(decoded.Kind),
			ViaConfidence: string(decoded.Confidence), ViaProvenance: string(decoded.Provenance),
		}
		if format == ResponseFormatDetailed {
			row.StableKey = string(symbol.StableKey)
			row.FileKey = file.key
			row.ReachedFromKey = string(source.StableKey)
		}
		radius.Symbols = append(radius.Symbols, row)
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

// visitedSymbolSet marks every symbol the traversal reached, including the
// root: an edge into the root is part of the impact just like an edge into any
// other reached symbol.
func visitedSymbolSet(visits []hotsnapshot.TraversalVisit) map[hotsnapshot.SymbolID]struct{} {
	visited := make(map[hotsnapshot.SymbolID]struct{}, len(visits))
	for _, visit := range visits {
		visited[visit.ID] = struct{}{}
	}
	return visited
}

// countImpactKinds records each distinct relation kind through which symbol
// touches the traversed subgraph. Distinct is per symbol: two calls to the same
// function are one CALLS_DIRECT reason for that consumer, not two.
func countImpactKinds(
	snapshot *hotsnapshot.GraphSnapshot,
	symbol hotsnapshot.SymbolID,
	visited map[hotsnapshot.SymbolID]struct{},
	traversal hotsnapshot.TraversalOptions,
	kinds map[string]int,
) error {
	seen := make(map[string]struct{}, 2)
	for _, edge := range snapshot.Outgoing(symbol) {
		if _, reached := visited[edge.Target]; !reached {
			continue
		}
		if !traversalCodeAllowed(edge.Kind, traversal.EdgeKinds) ||
			!traversalCodeAllowed(edge.Confidence, traversal.Confidences) {
			continue
		}
		decoded, isReference, err := decodeReferenceEdge(edge)
		if err != nil {
			return err
		}
		if !isReference {
			return fmt.Errorf("symbol edge %d->%d has non-reference kind %d", symbol, edge.Target, edge.Kind)
		}
		if _, exists := seen[string(decoded.Kind)]; exists {
			continue
		}
		seen[string(decoded.Kind)] = struct{}{}
		kinds[string(decoded.Kind)]++
	}
	return nil
}

func traversalCodeAllowed(code uint8, allowed []uint8) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if code == candidate {
			return true
		}
	}
	return false
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
