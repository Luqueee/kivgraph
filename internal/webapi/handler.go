// Package webapi exposes the read-only HTTP contract consumed by the web viewer.
package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/layout"
	"github.com/Luqueee/kivgraph/internal/webassets"
)

const APIVersion = "v1"

const (
	defaultSearchLimit = 50
	maxSearchLimit     = hotsnapshot.MaxExactResults
	defaultDepth       = 3
	defaultMaxNodes    = 1000

	defaultTileMaxNodes = 10_000
	maxRequestURIBytes  = 8 * 1024
)

var errSnapshotUnavailable = errors.New("snapshot is not published")

// Handler serves read-only viewer requests from one published snapshot store.
// It never accesses LadybugDB directly and never mutates the store.
type Handler struct {
	store           *hotsnapshot.SnapshotStore
	logger          *slog.Logger
	assets          http.Handler
	topologyOptions TopologyOptions
	topologyEnabled bool

	layoutMu               sync.Mutex
	cachedLayout           *layout.Layout
	cachedLayoutSnapshotID uint64

	topologyRelationshipsMu sync.Mutex
	topologyRelationships   map[string]topologyRelationshipCacheEntry
	topologyRelationshipLRU []string
}

// NewHandler creates an HTTP handler backed by store. A nil store is valid and
// makes graph-dependent endpoints return INDEX_NOT_READY.
func NewHandler(store *hotsnapshot.SnapshotStore) http.Handler {
	return newHandler(store, TopologyOptions{}, false)
}

// NewHandlerWithTopology creates a viewer handler with the optional
// generation-pinned topology API enabled. Existing callers can continue using
// NewHandler and retain the symbol and LGVB-only surface.
func NewHandlerWithTopology(store *hotsnapshot.SnapshotStore, options TopologyOptions) http.Handler {
	return newHandler(store, options, true)
}

func newHandler(store *hotsnapshot.SnapshotStore, options TopologyOptions, topologyEnabled bool) http.Handler {
	return &Handler{
		store:                 store,
		logger:                slog.Default(),
		assets:                webassets.New(),
		topologyOptions:       options,
		topologyEnabled:       topologyEnabled,
		topologyRelationships: make(map[string]topologyRelationshipCacheEntry),
	}
}

func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		handler.writeError(writer, request, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET and HEAD are supported")
		return
	}
	if status, code, message := validateHTTPRequest(request); status != 0 {
		handler.writeError(writer, request, status, code, message)
		return
	}

	switch request.URL.Path {
	case "/":
		handler.assets.ServeHTTP(writer, request)
	case "/index.html":
		handler.assets.ServeHTTP(writer, request)
	case "/healthz":
		handler.writeJSON(writer, request, http.StatusOK, map[string]string{"status": "ok"})
	case "/api/v1/meta":
		handler.meta(writer, request)
	case "/api/v1/topology":
		if !handler.topologyEnabled {
			handler.writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "endpoint not found")
			return
		}
		handler.topology(writer, request)
	case "/api/v1/search":
		handler.search(writer, request)
	case "/api/v1/symbol":
		handler.symbol(writer, request)
	case "/api/v1/tiles":
		handler.tiles(writer, request)
	case "/api/v1/neighborhood":
		handler.neighborhood(writer, request)
	default:
		if request.URL.Path == "/api" || strings.HasPrefix(request.URL.Path, "/api/") {
			handler.writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "endpoint not found")
			return
		}
		handler.assets.ServeHTTP(writer, request)
	}
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type metaResponse struct {
	APIVersion    string      `json:"api_version"`
	Status        string      `json:"status"`
	SnapshotID    *uint64     `json:"snapshot_id,omitempty"`
	SnapshotBuilt string      `json:"snapshot_built_at,omitempty"`
	SchemaVersion int         `json:"schema_version,omitempty"`
	Resolver      string      `json:"resolver_version,omitempty"`
	Counts        metaCounts  `json:"counts"`
	Layout        *metaLayout `json:"layout,omitempty"`
}

// metaLayout is the root viewport of the published layout. Without it a client
// cannot ask for a first tile: /api/v1/tiles requires explicit bounds, and
// guessing them either misses the graph or exceeds the query limits.
type metaLayout struct {
	MinX     int64 `json:"min_x"`
	MinY     int64 `json:"min_y"`
	MaxX     int64 `json:"max_x"`
	MaxY     int64 `json:"max_y"`
	MaxLOD   int   `json:"max_lod"`
	MaxNodes int   `json:"max_nodes"`
}

type metaCounts struct {
	Repositories uint64 `json:"repositories"`
	Packages     uint64 `json:"packages"`
	Files        uint64 `json:"files"`
	Symbols      uint64 `json:"symbols"`
	Evidence     uint64 `json:"evidence"`
	Edges        uint64 `json:"edges"`
	PackageEdges uint64 `json:"package_edges"`
	Unresolved   uint64 `json:"unresolved"`
}

type symbolResponse struct {
	SnapshotID uint64     `json:"snapshot_id"`
	Symbol     symbolView `json:"symbol"`
}

type symbolView struct {
	StableKey         string `json:"stable_key"`
	CanonicalIdentity string `json:"canonical_identity"`
	Repository        string `json:"repository"`
	RepositoryPath    string `json:"repository_path"`
	Package           string `json:"package"`
	ModulePath        string `json:"module_path"`
	File              string `json:"file"`
	Language          string `json:"language"`
	Name              string `json:"name"`
	QualifiedName     string `json:"qualified_name"`
	Kind              string `json:"kind"`
	Signature         string `json:"signature"`
	StartLine         uint32 `json:"start_line"`
	EndLine           uint32 `json:"end_line"`
}

type searchResponse struct {
	SnapshotID uint64       `json:"snapshot_id"`
	Total      int          `json:"total"`
	Returned   int          `json:"returned"`
	Truncated  bool         `json:"truncated"`
	Results    []symbolView `json:"results"`
}

type neighborhoodResponse struct {
	SnapshotID uint64       `json:"snapshot_id"`
	Root       string       `json:"root"`
	Direction  string       `json:"direction"`
	Depth      int          `json:"depth"`
	Truncated  bool         `json:"truncated"`
	Nodes      []symbolView `json:"nodes"`
	Edges      []edgeView   `json:"edges"`
}

type edgeView struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Kind       string `json:"kind"`
	Confidence string `json:"confidence"`
	Provenance string `json:"provenance"`
	Evidence   string `json:"evidence_key,omitempty"`
}

func (handler *Handler) meta(writer http.ResponseWriter, request *http.Request) {
	snapshot := handler.snapshot(writer, request)
	if snapshot == nil {
		return
	}
	metadata := snapshot.Metadata()
	id := metadata.ID
	response := metaResponse{
		APIVersion:    APIVersion,
		Status:        "ready",
		SnapshotID:    &id,
		SnapshotBuilt: metadata.CreatedAt.UTC().Format(time.RFC3339Nano),
		SchemaVersion: metadata.SchemaVersion,
		Resolver:      metadata.ResolverVersion,
		Counts: metaCounts{
			Repositories: metadata.Counts.Repositories,
			Packages:     metadata.Counts.Packages,
			Files:        metadata.Counts.Files,
			Symbols:      metadata.Counts.Symbols,
			Evidence:     metadata.Counts.Evidence,
			Edges:        metadata.Counts.Edges,
			PackageEdges: metadata.Counts.PackageEdges,
			Unresolved:   metadata.Counts.Unresolved,
		},
	}
	if viewerLayout, err := handler.viewerLayout(request.Context(), snapshot); err == nil {
		bounds := viewerLayout.Bounds()
		response.Layout = &metaLayout{
			MinX:     int64(bounds.MinX),
			MinY:     int64(bounds.MinY),
			MaxX:     int64(bounds.MaxX),
			MaxY:     int64(bounds.MaxY),
			MaxLOD:   int(layout.LODSymbols),
			MaxNodes: defaultTileMaxNodes,
		}
	}
	handler.writeJSON(writer, request, http.StatusOK, response)
}

func (handler *Handler) symbol(writer http.ResponseWriter, request *http.Request) {
	stableKey, ok := requiredQuery(request, "stable_key")
	if !ok {
		handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "stable_key is required")
		return
	}
	snapshot := handler.snapshot(writer, request)
	if snapshot == nil {
		return
	}
	id, found := snapshot.SymbolByStableKey(hotsnapshot.StableKey(stableKey))
	if !found {
		handler.writeError(writer, request, http.StatusNotFound, "SYMBOL_NOT_FOUND", "symbol was not found")
		return
	}
	view, valid := makeSymbolView(snapshot, id)
	if !valid {
		handler.writeError(writer, request, http.StatusInternalServerError, "SNAPSHOT_UNAVAILABLE", "snapshot indexes are inconsistent")
		return
	}
	handler.writeJSON(writer, request, http.StatusOK, symbolResponse{SnapshotID: snapshot.Metadata().ID, Symbol: view})
}

func (handler *Handler) search(writer http.ResponseWriter, request *http.Request) {
	name, ok := requiredQuery(request, "name")
	if !ok {
		handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "name is required")
		return
	}
	mode := request.URL.Query().Get("mode")
	if mode == "" {
		mode = "exact"
	}
	offset, limit, valid := pageArguments(request)
	if !valid {
		handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "offset or limit is invalid")
		return
	}
	snapshot := handler.snapshot(writer, request)
	if snapshot == nil {
		return
	}
	var page hotsnapshot.SymbolPage
	var err error
	switch mode {
	case "exact", "qualified_exact":
		interned, found := snapshot.Strings().Lookup(name)
		if found {
			if mode == "exact" {
				page, err = snapshot.SearchSymbolsByName(interned, hotsnapshot.SymbolFilter{}, offset, limit)
			} else {
				page, err = snapshot.SearchSymbolsByQName(interned, hotsnapshot.SymbolFilter{}, offset, limit)
			}
		} else {
			page = hotsnapshot.SymbolPage{Offset: offset, Limit: limit}
		}
	case "prefix":
		page, err = snapshot.SearchSymbolsByNamePrefix(name, hotsnapshot.SymbolFilter{}, offset, limit)
	default:
		handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "mode must be exact, qualified_exact or prefix")
		return
	}
	if err != nil {
		handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "search bounds are invalid")
		return
	}
	results := make([]symbolView, 0, len(page.IDs))
	for _, id := range page.IDs {
		view, valid := makeSymbolView(snapshot, id)
		if !valid {
			handler.writeError(writer, request, http.StatusInternalServerError, "SNAPSHOT_UNAVAILABLE", "snapshot indexes are inconsistent")
			return
		}
		results = append(results, view)
	}
	handler.writeJSON(writer, request, http.StatusOK, searchResponse{
		SnapshotID: snapshot.Metadata().ID,
		Total:      page.Total,
		Returned:   len(results),
		Truncated:  page.HasMore,
		Results:    results,
	})
}
func (handler *Handler) tiles(writer http.ResponseWriter, request *http.Request) {
	if err := validateViewerBinaryVersion(request.URL.Query().Get("format_version")); err != nil {
		handler.writeError(writer, request, http.StatusBadRequest, "UNSUPPORTED_VERSION", "viewer binary version is unsupported")
		return
	}
	query, ok := tileArguments(request)
	if !ok {
		handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "tile bounds, lod or max_nodes are invalid")
		return
	}
	snapshot := handler.snapshot(writer, request)
	if snapshot == nil {
		return
	}
	viewerLayout, err := handler.viewerLayout(request.Context(), snapshot)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			handler.writeError(writer, request, http.StatusGatewayTimeout, "REQUEST_CANCELED", "layout construction was canceled")
			return
		}
		handler.writeError(writer, request, http.StatusInternalServerError, "SNAPSHOT_UNAVAILABLE", "snapshot layout is unavailable")
		return
	}
	result, err := viewerLayout.QueryViewport(query)
	if err != nil {
		handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "tile viewport is invalid")
		return
	}
	payload, err := encodeTilePayload(request.Context(), snapshot, result.Nodes, result.Truncated, query.MaxLevel)
	if err != nil {
		if errors.Is(err, errViewerPayloadTooLarge) {
			handler.writeError(writer, request, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "tile payload exceeds the configured limit")
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			handler.writeError(writer, request, http.StatusGatewayTimeout, "REQUEST_CANCELED", "tile encoding was canceled")
			return
		}
		handler.writeError(writer, request, http.StatusInternalServerError, "SNAPSHOT_UNAVAILABLE", "snapshot edge indexes are inconsistent")
		return
	}
	handler.writeBinary(writer, request, http.StatusOK, payload)
}

func (handler *Handler) neighborhoodBinary(writer http.ResponseWriter, request *http.Request) {
	if err := validateViewerBinaryVersion(request.URL.Query().Get("format_version")); err != nil {
		handler.writeError(writer, request, http.StatusBadRequest, "UNSUPPORTED_VERSION", "viewer binary version is unsupported")
		return
	}
	stableKey, ok := requiredQuery(request, "stable_key")
	if !ok {
		handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "stable_key is required")
		return
	}
	depth, maxNodes, direction, valid := neighborhoodArguments(request)
	if !valid {
		handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "neighborhood bounds or direction are invalid")
		return
	}
	snapshot := handler.snapshot(writer, request)
	if snapshot == nil {
		return
	}
	root, found := snapshot.SymbolByStableKey(hotsnapshot.StableKey(stableKey))
	if !found {
		handler.writeError(writer, request, http.StatusNotFound, "SYMBOL_NOT_FOUND", "symbol was not found")
		return
	}
	visits, truncated, err := collectNeighborhood(request.Context(), snapshot, root, depth, maxNodes, direction)
	if err != nil {
		if errors.Is(err, hotsnapshot.ErrTraversalTimeout) {
			handler.writeError(writer, request, http.StatusGatewayTimeout, "TRAVERSAL_LIMIT_REACHED", "neighborhood traversal timed out")
			return
		}
		handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "neighborhood bounds are invalid")
		return
	}
	payload, err := encodeNeighborhoodPayload(request.Context(), snapshot, visits, truncated)
	if err != nil {
		if errors.Is(err, errViewerPayloadTooLarge) {
			handler.writeError(writer, request, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "neighborhood payload exceeds the configured limit")
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			handler.writeError(writer, request, http.StatusGatewayTimeout, "REQUEST_CANCELED", "neighborhood encoding was canceled")
			return
		}
		handler.writeError(writer, request, http.StatusInternalServerError, "SNAPSHOT_UNAVAILABLE", "snapshot edge indexes are inconsistent")
		return
	}
	handler.writeBinary(writer, request, http.StatusOK, payload)
}

func viewerResponseIsBinary(request *http.Request) (bool, error) {
	format := request.URL.Query().Get("format")
	switch format {
	case "", "json":
	case "bin", "binary":
		return true, nil
	default:
		return false, errors.New("unsupported viewer response format")
	}
	accept := strings.ToLower(request.Header.Get("Accept"))
	return strings.Contains(accept, "application/octet-stream"), nil
}

func tileArguments(request *http.Request) (layout.ViewportQuery, bool) {
	minX, ok := requiredCoord(request, "min_x")
	if !ok {
		return layout.ViewportQuery{}, false
	}
	minY, ok := requiredCoord(request, "min_y")
	if !ok {
		return layout.ViewportQuery{}, false
	}
	maxX, ok := requiredCoord(request, "max_x")
	if !ok {
		return layout.ViewportQuery{}, false
	}
	maxY, ok := requiredCoord(request, "max_y")
	if !ok || minX >= maxX || minY >= maxY {
		return layout.ViewportQuery{}, false
	}
	levelValue, ok := optionalInt(request, "lod", int(layout.LODSymbols))
	if !ok || levelValue < int(layout.LODRepositories) || levelValue > int(layout.LODSymbols) {
		return layout.ViewportQuery{}, false
	}
	maxNodes, ok := optionalInt(request, "max_nodes", defaultTileMaxNodes)
	if !ok || maxNodes < 1 || maxNodes > defaultTileMaxNodes {
		return layout.ViewportQuery{}, false
	}
	return layout.ViewportQuery{
		Bounds:   layout.Rect{MinX: minX, MinY: minY, MaxX: maxX, MaxY: maxY},
		MaxLevel: layout.LOD(levelValue),
		MaxNodes: maxNodes,
	}, true
}

func requiredCoord(request *http.Request, key string) (layout.Coord, bool) {
	value := request.URL.Query().Get(key)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return layout.Coord(parsed), err == nil
}

func (handler *Handler) viewerLayout(ctx context.Context, snapshot *hotsnapshot.GraphSnapshot) (*layout.Layout, error) {
	metadata := snapshot.Metadata()
	handler.layoutMu.Lock()
	defer handler.layoutMu.Unlock()
	if handler.cachedLayout != nil && handler.cachedLayoutSnapshotID == metadata.ID {
		return handler.cachedLayout, nil
	}
	built, err := layout.Build(ctx, snapshot, layout.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("build viewer layout: %w", err)
	}
	handler.cachedLayout = built
	handler.cachedLayoutSnapshotID = metadata.ID
	return built, nil
}

func (handler *Handler) neighborhood(writer http.ResponseWriter, request *http.Request) {
	binaryResponse, err := viewerResponseIsBinary(request)
	if err != nil {
		handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "format must be json or binary")
		return
	}
	if binaryResponse {
		handler.neighborhoodBinary(writer, request)
		return
	}
	stableKey, ok := requiredQuery(request, "stable_key")
	if !ok {
		handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "stable_key is required")
		return
	}
	depth, maxNodes, direction, valid := neighborhoodArguments(request)
	if !valid {
		handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "neighborhood bounds or direction are invalid")
		return
	}
	snapshot := handler.snapshot(writer, request)
	if snapshot == nil {
		return
	}
	root, found := snapshot.SymbolByStableKey(hotsnapshot.StableKey(stableKey))
	if !found {
		handler.writeError(writer, request, http.StatusNotFound, "SYMBOL_NOT_FOUND", "symbol was not found")
		return
	}
	visits, truncated, err := collectNeighborhood(request.Context(), snapshot, root, depth, maxNodes, direction)
	if err != nil {
		if errors.Is(err, hotsnapshot.ErrTraversalTimeout) {
			handler.writeError(writer, request, http.StatusGatewayTimeout, "TRAVERSAL_LIMIT_REACHED", "neighborhood traversal timed out")
			return
		}
		handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "neighborhood bounds are invalid")
		return
	}
	seen := make(map[hotsnapshot.SymbolID]struct{}, len(visits))
	nodes := make([]symbolView, 0, len(visits))
	for _, id := range visits {
		seen[id] = struct{}{}
		view, valid := makeSymbolView(snapshot, id)
		if !valid {
			handler.writeError(writer, request, http.StatusInternalServerError, "SNAPSHOT_UNAVAILABLE", "snapshot indexes are inconsistent")
			return
		}
		nodes = append(nodes, view)
	}
	edges := make([]edgeView, 0)
	for _, source := range visits {
		for _, edge := range snapshot.Outgoing(source) {
			if _, exists := seen[edge.Target]; !exists {
				continue
			}
			view, valid := makeEdgeView(snapshot, source, edge)
			if !valid {
				handler.writeError(writer, request, http.StatusInternalServerError, "SNAPSHOT_UNAVAILABLE", "snapshot edge indexes are inconsistent")
				return
			}
			edges = append(edges, view)
		}
	}
	handler.writeJSON(writer, request, http.StatusOK, neighborhoodResponse{
		SnapshotID: snapshot.Metadata().ID,
		Root:       stableKey,
		Direction:  direction,
		Depth:      depth,
		Truncated:  truncated,
		Nodes:      nodes,
		Edges:      edges,
	})
}

func collectNeighborhood(ctx context.Context, snapshot *hotsnapshot.GraphSnapshot, root hotsnapshot.SymbolID, depth, maxNodes int, direction string) ([]hotsnapshot.SymbolID, bool, error) {
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(2 * time.Second)
	}
	options := hotsnapshot.TraversalOptions{MaxDepth: depth, MaxNodes: maxNodes, Deadline: deadline}
	ordered := []hotsnapshot.SymbolID{root}
	seen := map[hotsnapshot.SymbolID]struct{}{root: {}}
	truncated := false
	for _, traversalDirection := range directions(direction) {
		options.Direction = traversalDirection
		result, err := snapshot.Traverse(root, options)
		if err != nil {
			return nil, truncated, err
		}
		truncated = truncated || result.Truncated
		for _, visit := range result.Visits {
			if _, exists := seen[visit.ID]; exists {
				continue
			}
			if len(ordered) >= maxNodes {
				truncated = true
				break
			}
			seen[visit.ID] = struct{}{}
			ordered = append(ordered, visit.ID)
		}
	}
	return ordered, truncated, nil
}

func directions(direction string) []hotsnapshot.TraversalDirection {
	switch direction {
	case "incoming":
		return []hotsnapshot.TraversalDirection{hotsnapshot.TraversalIncoming}
	case "outgoing":
		return []hotsnapshot.TraversalDirection{hotsnapshot.TraversalOutgoing}
	default:
		return []hotsnapshot.TraversalDirection{hotsnapshot.TraversalOutgoing, hotsnapshot.TraversalIncoming}
	}
}

func makeSymbolView(snapshot *hotsnapshot.GraphSnapshot, id hotsnapshot.SymbolID) (symbolView, bool) {
	symbol, ok := snapshot.Symbol(id)
	if !ok {
		return symbolView{}, false
	}
	file, ok := snapshot.File(symbol.File)
	if !ok {
		return symbolView{}, false
	}
	pkg, ok := snapshot.Package(file.Package)
	if !ok {
		return symbolView{}, false
	}
	repository, ok := snapshot.Repository(file.Repository)
	if !ok {
		return symbolView{}, false
	}
	stableKey, ok := snapshot.StableKey(symbol.StableKey)
	if !ok {
		return symbolView{}, false
	}
	strings := snapshot.Strings()
	return symbolView{
		StableKey:         string(stableKey),
		CanonicalIdentity: stringValue(strings, symbol.CanonicalIdentity),
		Repository:        stringValue(strings, repository.Name),
		RepositoryPath:    stringValue(strings, repository.Path),
		Package:           stringValue(strings, pkg.Name),
		ModulePath:        stringValue(strings, pkg.ModulePath),
		File:              stringValue(strings, file.Path),
		Language:          stringValue(strings, symbol.Language),
		Name:              stringValue(strings, symbol.Name),
		QualifiedName:     stringValue(strings, symbol.QualifiedName),
		Kind:              stringValue(strings, symbol.Kind),
		Signature:         stringValue(strings, symbol.Signature),
		StartLine:         symbol.StartLine,
		EndLine:           symbol.EndLine,
	}, true
}

func makeEdgeView(snapshot *hotsnapshot.GraphSnapshot, source hotsnapshot.SymbolID, edge hotsnapshot.PackedEdge) (edgeView, bool) {
	sourceRecord, sourceOK := snapshot.Symbol(source)
	targetRecord, targetOK := snapshot.Symbol(edge.Target)
	kind, kindErr := facts.EdgeKindFromCode(edge.Kind)
	confidence, confidenceErr := facts.ConfidenceFromCode(edge.Confidence)
	provenance, provenanceErr := facts.ProvenanceFromCode(edge.Provenance)
	if !sourceOK || !targetOK || kindErr != nil || confidenceErr != nil || provenanceErr != nil {
		return edgeView{}, false
	}
	sourceKey, sourceKeyOK := snapshot.StableKey(sourceRecord.StableKey)
	targetKey, targetKeyOK := snapshot.StableKey(targetRecord.StableKey)
	if !sourceKeyOK || !targetKeyOK {
		return edgeView{}, false
	}
	view := edgeView{
		Source:     string(sourceKey),
		Target:     string(targetKey),
		Kind:       string(kind),
		Confidence: string(confidence),
		Provenance: string(provenance),
	}
	if edge.Evidence != hotsnapshot.InvalidEvidenceID {
		evidence, ok := snapshot.Evidence(edge.Evidence)
		if !ok {
			return edgeView{}, false
		}
		view.Evidence = stringValue(snapshot.Strings(), evidence.Key)
	}
	return view, true
}

func stringValue(table hotsnapshot.StringTable, id hotsnapshot.InternedString) string {
	value, _ := table.String(id)
	return value
}

func (handler *Handler) snapshot(writer http.ResponseWriter, request *http.Request) *hotsnapshot.GraphSnapshot {
	if handler.store == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable, "INDEX_NOT_READY", errSnapshotUnavailable.Error())
		return nil
	}
	snapshot := handler.store.Load()
	if snapshot == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable, "INDEX_NOT_READY", errSnapshotUnavailable.Error())
		return nil
	}
	if requestedID := request.URL.Query().Get("snapshot_id"); requestedID != "" {
		id, err := strconv.ParseUint(requestedID, 10, 64)
		if err != nil {
			handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "snapshot_id is invalid")
			return nil
		}
		if id != snapshot.Metadata().ID {
			handler.writeError(writer, request, http.StatusConflict, "SNAPSHOT_MISMATCH", "requested snapshot is not published")
			return nil
		}
	}
	return snapshot
}
func validateHTTPRequest(request *http.Request) (int, string, string) {
	if request == nil || request.URL == nil {
		return http.StatusBadRequest, "INVALID_ARGUMENT", "request URL is missing"
	}
	if len(request.URL.RequestURI()) > maxRequestURIBytes {
		return http.StatusRequestURITooLong, "REQUEST_TOO_LARGE", "request URI exceeds the configured limit"
	}
	if request.ContentLength > maxRequestURIBytes {
		return http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "request body exceeds the configured limit"
	}
	if request.ContentLength > 0 || (request.Body != nil && request.Body != http.NoBody) {
		return http.StatusBadRequest, "REQUEST_BODY_NOT_ALLOWED", "GET and HEAD requests must not contain a body"
	}
	if origin := request.Header.Get("Origin"); origin != "" && !sameOrigin(request, origin) {
		return http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "request origin is not allowed"
	}
	return 0, "", ""
}

func sameOrigin(request *http.Request, origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := request.Host
	if host == "" {
		host = request.URL.Host
	}
	return host != "" && strings.EqualFold(parsed.Host, host)
}

func requiredQuery(request *http.Request, key string) (string, bool) {
	value := request.URL.Query().Get(key)
	return value, value != ""
}

func pageArguments(request *http.Request) (int, int, bool) {
	offset, ok := optionalInt(request, "offset", 0)
	if !ok || offset < 0 {
		return 0, 0, false
	}
	limit, ok := optionalInt(request, "limit", defaultSearchLimit)
	if !ok || limit < 1 || limit > maxSearchLimit {
		return 0, 0, false
	}
	return offset, limit, true
}

func neighborhoodArguments(request *http.Request) (int, int, string, bool) {
	depth, ok := optionalInt(request, "depth", defaultDepth)
	if !ok || depth < 0 || depth > hotsnapshot.MaxTraversalDepth {
		return 0, 0, "", false
	}
	maxNodes, ok := optionalInt(request, "max_nodes", defaultMaxNodes)
	if !ok || maxNodes < 1 || maxNodes > hotsnapshot.MaxTraversalNodes {
		return 0, 0, "", false
	}
	direction := request.URL.Query().Get("direction")
	if direction == "" {
		direction = "both"
	}
	if direction != "incoming" && direction != "outgoing" && direction != "both" {
		return 0, 0, "", false
	}
	return depth, maxNodes, direction, true
}

func optionalInt(request *http.Request, key string, defaultValue int) (int, bool) {
	value := request.URL.Query().Get(key)
	if value == "" {
		return defaultValue, true
	}
	parsed, err := strconv.Atoi(value)
	return parsed, err == nil
}

func (handler *Handler) writeError(writer http.ResponseWriter, request *http.Request, status int, code, message string) {
	handler.writeJSON(writer, request, status, apiError{Code: code, Message: message})
}
func (handler *Handler) writeBinary(writer http.ResponseWriter, request *http.Request, status int, payload []byte) {
	if len(payload) > maxViewerPayloadBytes {
		handler.writeError(writer, request, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "response payload exceeds the configured limit")
		return
	}
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	if request.Method == http.MethodHead {
		return
	}
	if _, err := writer.Write(payload); err != nil {
		handler.logger.Error("write binary response", "error", err)
	}
}

func (handler *Handler) writeJSON(writer http.ResponseWriter, request *http.Request, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	if request.Method == http.MethodHead {
		return
	}
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		handler.logger.Error("encode web response", "error", err)
	}
}

var _ http.Handler = (*Handler)(nil)
