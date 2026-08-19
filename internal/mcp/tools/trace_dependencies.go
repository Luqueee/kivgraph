package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
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
//
// View is the granularity of the answer, never a different answer: "compact",
// the default, hoists what every reached symbol shares into the header and
// groups the rest by file, and "full" keeps the row-per-fact shape. A traversal
// is not a set of files, so "files" is rejected rather than reinterpreted.
type TraceDependenciesInput struct {
	StableKey      string   `json:"stable_key,omitempty"`
	QualifiedName  string   `json:"qualified_name,omitempty"`
	Repository     string   `json:"repository,omitempty"`
	Path           string   `json:"path,omitempty"`
	Depth          int      `json:"depth,omitempty"`
	MaxNodes       int      `json:"max_nodes,omitempty"`
	Repo           string   `json:"repo,omitempty"`
	Language       string   `json:"language,omitempty"`
	EdgeKinds      []string `json:"edge_kinds,omitempty"`
	Confidence     string   `json:"confidence,omitempty"`
	IncludeDerived bool     `json:"include_derived,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	Cursor         string   `json:"cursor,omitempty"`
	ResponseFormat string   `json:"response_format,omitempty"`
	View           string   `json:"view,omitempty"`
}

// DependencyTrace is the traversal itself: the root, what the bounds did to
// it, and one page of reached symbols. The envelope's total, returned and
// next_cursor page over Nodes.
type DependencyTrace struct {
	RootKey            string          `json:"root_key"`
	RootRepository     string          `json:"root_repository"`
	Depth              int             `json:"depth"`
	MaxNodes           int             `json:"max_nodes"`
	Reached            int             `json:"reached"`
	DeepestDepth       int             `json:"deepest_depth"`
	TraversalTruncated bool            `json:"traversal_truncated"`
	Nodes              []ReachedSymbol `json:"nodes"`

	// View decides how MarshalJSON spells the page. It never travels in it:
	// the caller already knows what it asked for.
	View string `json:"-"`
}

// ReachedSymbol is one symbol a bounded traversal reached, together with the
// edge it first arrived by. That edge is a fact of this traversal, not the only
// route: a breadth-first frontier records the shortest one it found. Both
// trace_dependencies and get_blast_radius return this shape.
//
// StartLine and EndLine are the declaration's own range, so a row can be opened
// without a second call. A traversal answers with one row per reached symbol,
// and asking for each range separately turned a page into a page of round
// trips.
type ReachedSymbol struct {
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name"`
	Kind          string `json:"kind"`
	Depth         int    `json:"depth"`
	Repository    string `json:"repository"`
	Language      string `json:"language"`
	FilePath      string `json:"file_path"`
	StartLine     uint32 `json:"start_line"`
	EndLine       uint32 `json:"end_line"`

	// ReachedFrom names the already-reached symbol this one was discovered
	// from, which is the edge's target when the traversal runs incoming. The
	// edge's own orientation is therefore not implied by this field. It is the
	// qualified name rather than the stable key because a key costs 35 tokens
	// on a row that costs 20, and the symbol it names is another row of the
	// same page.
	ReachedFrom   string `json:"reached_from"`
	ViaKind       string `json:"via_kind"`
	ViaConfidence string `json:"via_confidence"`
	ViaProvenance string `json:"via_provenance"`

	// The identifiers below are derived: the path beside them already spells out
	// the file, and the reached-from key is the stable key of a symbol this same
	// page names.
	StableKey      string `json:"stable_key,omitempty"`
	FileKey        string `json:"file_key,omitempty"`
	ReachedFromKey string `json:"reached_from_key,omitempty"`
}

// compactReachedHeader is what every row of a page of reached symbols shares.
// An empty field means the rows disagreed, and each row then carries its own
// value in its tail. Both trace_dependencies and get_blast_radius answer with
// ReachedSymbol, so both hoist it the same way.
//
// HopDepth is the depth of the rows, not the traversal's bound: a trace states
// the bound it was given under `depth`, and these two numbers are different
// facts that happen to share a name in the full view.
type compactReachedHeader struct {
	Repository    string `json:"repository,omitempty"`
	Kind          string `json:"kind,omitempty"`
	HopDepth      int    `json:"hop_depth,omitempty"`
	ReachedFrom   string `json:"reached_from,omitempty"`
	ViaKind       string `json:"via_kind,omitempty"`
	ViaConfidence string `json:"via_confidence,omitempty"`
	ViaProvenance string `json:"via_provenance,omitempty"`
}

// compactSymbolGroup is one file and the symbols this page holds in it. Repo
// is present only when the group is not in the hoisted repository, which on a
// single-repository answer is never.
type compactSymbolGroup struct {
	File string `json:"file"`
	Repo string `json:"repo,omitempty"`
	At   []any  `json:"at"`
}

// declarationLabel names a declaration and the lines it occupies:
// `qualified_name@start` when it is one line, `qualified_name@start-end` when
// it is not. One string instead of two numbered fields, and no range is lost.
func declarationLabel(qualifiedName string, startLine, endLine uint32) string {
	label := symbolAtLine(qualifiedName, startLine)
	if endLine > startLine {
		return label + "-" + strconv.FormatUint(uint64(endLine), 10)
	}
	return label
}

// compactReachedGroup is every reached symbol that shares one exact tuple of
// the columns the page header could not hoist: kind, the depth of the hop,
// and the edge that reached it. Absent means this group's rows hold the
// page's hoisted value too.
//
// ReachedFrom is not part of that tuple -- it names the specific symbol one
// hop closer to the root, which in a bulk fan-in is close to unique per row,
// on a real 29-row page 27 rows named 27 different ones, and folding it into
// the grouping key fragmented every group back down to one row each. It is
// still hoisted here when the rows a tuple happened to collect share one: the
// four depth-1 callers of one root all share the root as ReachedFrom even
// though none of the tuple fields drove that. A group whose rows disagree on
// it leaves the field blank and each row carries its own, exactly like Name.
type compactReachedGroup struct {
	Kind          string `json:"kind,omitempty"`
	HopDepth      int    `json:"hop_depth,omitempty"`
	ReachedFrom   string `json:"reached_from,omitempty"`
	ViaKind       string `json:"via_kind,omitempty"`
	ViaConfidence string `json:"via_confidence,omitempty"`
	ViaProvenance string `json:"via_provenance,omitempty"`

	Files []compactSymbolGroup `json:"files"`
}

// compactReachedSymbols hoists the columns the whole page agrees on, groups
// what is left by the exact tuple each row still shares, and files what
// remains inside a group by path. A row is the bare `qn@lines` label once its
// group states kind, depth and the edge that reached it; only the name, what
// it was reached from, and, under the detailed format, the stable key can
// still travel on the row itself.
//
// `language` is absent by construction: it is the file's extension.
//
// Grouping is the second tier of ADR 0046: on a real 29-row impact page,
// (kind, via_kind, via_confidence, via_provenance, depth) collapsed to four
// distinct tuples, one of them covering 26 rows on its own -- with
// `reached_from` excluded from the tuple; see compactReachedGroup. Files and
// Groups are mutually exclusive in the return value the same way; a caller
// emits whichever is non-nil.
func compactReachedSymbols(nodes []ReachedSymbol) (compactReachedHeader, []compactSymbolGroup, []compactReachedGroup) {
	header := compactReachedHeader{
		Repository:    hoistString(len(nodes), func(index int) string { return nodes[index].Repository }),
		Kind:          hoistString(len(nodes), func(index int) string { return nodes[index].Kind }),
		ReachedFrom:   hoistString(len(nodes), func(index int) string { return nodes[index].ReachedFrom }),
		ViaKind:       hoistString(len(nodes), func(index int) string { return nodes[index].ViaKind }),
		ViaConfidence: hoistString(len(nodes), func(index int) string { return nodes[index].ViaConfidence }),
		ViaProvenance: hoistString(len(nodes), func(index int) string { return nodes[index].ViaProvenance }),
	}
	depth := hoistString(len(nodes), func(index int) string { return strconv.Itoa(nodes[index].Depth) })
	if depth != "" {
		header.HopDepth = nodes[0].Depth
	}

	residual := func(node ReachedSymbol) []string {
		rowDepth := ""
		if header.HopDepth == 0 {
			rowDepth = strconv.Itoa(node.Depth)
		}
		return []string{
			blankWhenHoisted(node.Kind, header.Kind),
			rowDepth,
			blankWhenHoisted(node.ViaKind, header.ViaKind),
			blankWhenHoisted(node.ViaConfidence, header.ViaConfidence),
			blankWhenHoisted(node.ViaProvenance, header.ViaProvenance),
		}
	}
	flat := reachedFileGroups(nodes, header.Repository, header.Kind, header.HopDepth, header.ReachedFrom, header.ViaKind, header.ViaConfidence, header.ViaProvenance)
	buckets := groupByResidual(nodes, residual)
	if len(buckets) <= 1 {
		return header, flat, nil
	}

	// effectiveOf is what a row is compared against to stay silent on a
	// grouping-tuple field: whichever of the page or this bucket already
	// states it. It must stay distinct from the group's own JSON field, which
	// is deliberately blank when the page already carries it -- passing that
	// blank into reachedFileGroups as though nothing were hoisted put `kind`
	// back on every one of eight rows the first time this ran.
	effectiveOf := func(pageValue, bucketValue string) string {
		if pageValue != "" {
			return pageValue
		}
		return bucketValue
	}
	groups := make([]compactReachedGroup, 0, len(buckets))
	for _, bucket := range buckets {
		first := bucket[0]
		group := compactReachedGroup{
			Kind:          blankWhenHoisted(first.Kind, header.Kind),
			ViaKind:       blankWhenHoisted(first.ViaKind, header.ViaKind),
			ViaConfidence: blankWhenHoisted(first.ViaConfidence, header.ViaConfidence),
			ViaProvenance: blankWhenHoisted(first.ViaProvenance, header.ViaProvenance),
		}
		groupDepth := header.HopDepth
		if groupDepth == 0 {
			groupDepth = first.Depth
			group.HopDepth = groupDepth
		}
		groupReachedFrom := header.ReachedFrom
		if groupReachedFrom == "" {
			// Not a grouping key, but still worth a second look now that the
			// bucket is fixed: it costs one more hoistString over rows the
			// caller already holds.
			groupReachedFrom = hoistString(len(bucket), func(index int) string { return bucket[index].ReachedFrom })
			group.ReachedFrom = groupReachedFrom
		}
		group.Files = reachedFileGroups(bucket, header.Repository,
			effectiveOf(header.Kind, first.Kind), groupDepth, groupReachedFrom,
			effectiveOf(header.ViaKind, first.ViaKind),
			effectiveOf(header.ViaConfidence, first.ViaConfidence),
			effectiveOf(header.ViaProvenance, first.ViaProvenance))
		groups = append(groups, group)
	}
	// Grouping only wins when a tuple repeats enough to pay for its own
	// header; a page where every row disagrees on everything -- three hops
	// down three different edges is the common case, not the exception -- is
	// cheaper left flat. There is no cost to marshaling both candidates on a
	// page this small, and it is the only way to guarantee grouping never
	// costs more than not grouping instead of hoping a heuristic holds.
	if flatBytes, err := json.Marshal(flat); err == nil {
		if groupedBytes, err := json.Marshal(groups); err == nil && len(groupedBytes) >= len(flatBytes) {
			return header, flat, nil
		}
	}
	return header, nil, groups
}

// reachedFileGroups groups rows by file with a label per row: bare once kind,
// depth and the edge are accounted for above the row, on the page header or
// on its group. `reached_from` and `name` stay row-level regardless of
// grouping: `name` is dropped page-wide when every row's name is implied by
// its qualified name, and a group narrower than the page can only make that
// true for more rows, never fewer, so the page-wide test is never wrong to
// reuse inside a group too.
func reachedFileGroups(nodes []ReachedSymbol, hoistedRepository, kind string, depth int, reachedFrom, viaKind, viaConfidence, viaProvenance string) []compactSymbolGroup {
	namesImplied := true
	for _, node := range nodes {
		if !nameIsLastSegment(node.Name, node.QualifiedName) {
			namesImplied = false
			break
		}
	}
	rowDepth := ""
	groups := make([]compactSymbolGroup, 0, len(nodes))
	index := make(map[string]int, len(nodes))
	for _, node := range nodes {
		position, exists := index[node.FilePath]
		if !exists {
			position = len(groups)
			index[node.FilePath] = position
			group := compactSymbolGroup{File: node.FilePath}
			if node.Repository != hoistedRepository {
				group.Repo = node.Repository
			}
			groups = append(groups, group)
		}
		rowName := ""
		if !namesImplied {
			rowName = node.Name
		}
		if depth == 0 {
			rowDepth = strconv.Itoa(node.Depth)
		}
		groups[position].At = append(groups[position].At, compactRowTail(
			declarationLabel(node.QualifiedName, node.StartLine, node.EndLine),
			rowName,
			blankWhenHoisted(node.Kind, kind),
			rowDepth,
			blankWhenHoisted(node.ReachedFrom, reachedFrom),
			blankWhenHoisted(node.ViaKind, viaKind),
			blankWhenHoisted(node.ViaConfidence, viaConfidence),
			blankWhenHoisted(node.ViaProvenance, viaProvenance),
			// Set only under `response_format: "detailed"`, which is the only
			// way a stable key reaches any view.
			node.StableKey,
		))
	}
	return groups
}

// nameIsLastSegment reports whether the qualified name already spells the name
// out at its end, which is what makes a separate `name` field redundant. The
// separator is the language's, so the test is a suffix rather than a split.
//
// A trailing `#N` is the snapshot's discriminator between two declarations of
// one qualified name, not part of any name, and it is stripped before the test:
// one `getRequiredField#2` export in a page used to put `name` back on all
// twenty-nine rows, which is the repetition this shape exists to remove.
func nameIsLastSegment(name, qualifiedName string) bool {
	return name != "" && strings.HasSuffix(withoutDiscriminator(qualifiedName), name)
}

// withoutDiscriminator drops a trailing `#<digits>`. A `#` followed by anything
// else is a language's own separator -- a TypeScript private field -- and stays.
func withoutDiscriminator(qualifiedName string) string {
	hash := strings.LastIndexByte(qualifiedName, '#')
	if hash <= 0 || hash == len(qualifiedName)-1 {
		return qualifiedName
	}
	for _, digit := range qualifiedName[hash+1:] {
		if digit < '0' || digit > '9' {
			return qualifiedName
		}
	}
	return qualifiedName[:hash]
}

// blankWhenHoisted drops a row's value when the header already states it.
func blankWhenHoisted(value, hoisted string) string {
	if hoisted != "" {
		return ""
	}
	return value
}

// MarshalJSON writes the traversal at the granularity the caller asked for.
// The compact page states the root, the bounds and what every reached symbol
// shares once, and then one entry per symbol grouped by file.
func (trace DependencyTrace) MarshalJSON() ([]byte, error) {
	type fullTrace DependencyTrace
	if trace.View == ViewFull || trace.View == "" {
		return json.Marshal(fullTrace(trace))
	}
	header, files, groups := compactReachedSymbols(trace.Nodes)
	return json.Marshal(struct {
		RootKey            string `json:"root_key"`
		RootRepository     string `json:"root_repository"`
		Depth              int    `json:"depth"`
		MaxNodes           int    `json:"max_nodes"`
		Reached            int    `json:"reached"`
		DeepestDepth       int    `json:"deepest_depth"`
		TraversalTruncated bool   `json:"traversal_truncated,omitempty"`
		compactReachedHeader
		Files  []compactSymbolGroup  `json:"files,omitempty"`
		Groups []compactReachedGroup `json:"groups,omitempty"`
	}{
		RootKey: trace.RootKey, RootRepository: trace.RootRepository,
		Depth: trace.Depth, MaxNodes: trace.MaxNodes,
		Reached: trace.Reached, DeepestDepth: trace.DeepestDepth,
		TraversalTruncated:   trace.TraversalTruncated,
		compactReachedHeader: header,
		Files:                files,
		Groups:               groups,
	})
}

type traceDependenciesOptions struct {
	Selector   symbolSelector
	Depth      int
	MaxNodes   int
	Repo       string
	Language   string
	EdgeKinds  []string
	Confidence string
	Derived    derivedFilter
	Limit      int
	Format     string
	View       string
}

type traceDependenciesQuery struct {
	Tool          string   `json:"tool"`
	StableKey     string   `json:"stable_key,omitempty"`
	QualifiedName string   `json:"qualified_name,omitempty"`
	Repository    string   `json:"repository,omitempty"`
	Path          string   `json:"path,omitempty"`
	Depth         int      `json:"depth"`
	MaxNodes      int      `json:"max_nodes"`
	Repo          string   `json:"repo,omitempty"`
	Language      string   `json:"language,omitempty"`
	EdgeKinds     []string `json:"edge_kinds,omitempty"`
	Confidence    string   `json:"confidence,omitempty"`
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
	addQueryTool(server, &sdkmcp.Tool{
		Name:        traceDependenciesToolName,
		Description: "What this symbol reaches outward, bounded by depth. Grep does not follow a chain.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
		Meta:        boundedResultMeta(MaximumTraversalResultChars),
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
		Tool: traceDependenciesToolName, StableKey: options.Selector.StableKey, QualifiedName: options.Selector.QualifiedName,
		Repository: options.Selector.Repository, Path: options.Selector.Path, Depth: options.Depth,
		MaxNodes: options.MaxNodes, Repo: options.Repo, Language: options.Language,
		EdgeKinds: options.EdgeKinds, Confidence: options.Confidence,
	})
	if err != nil {
		return nil, Response[DependencyTrace]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[DependencyTrace]{}, ErrIndexNotReady()
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[DependencyTrace]{}, ErrIndexNotReady()
	}
	rootID, err := resolveSymbolSelector(snapshot, options.Selector)
	if err != nil {
		return nil, Response[DependencyTrace]{}, err
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
		Guidance: traversalGuidance(traceDependenciesToolName, total, len(page), hasMore),
		Results: DependencyTrace{
			RootKey: string(root.StableKey), RootRepository: rootRepository.name,
			Depth: options.Depth, MaxNodes: options.MaxNodes,
			Reached: len(traversal.Visits) - 1, DeepestDepth: deepest,
			TraversalTruncated: traversal.Truncated, Nodes: page,
			View: options.View,
		},
		View: options.View,
	}, nil
}

func normalizeTraceDependenciesInput(arguments TraceDependenciesInput) (traceDependenciesOptions, error) {
	selector, err := normalizeSymbolSelector(arguments.StableKey, arguments.Repository, arguments.Path, arguments.QualifiedName)
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
	format, err := normalizeResponseFormat(arguments.ResponseFormat)
	if err != nil {
		return traceDependenciesOptions{}, err
	}
	view, err := normalizeView(arguments.View, false)
	if err != nil {
		return traceDependenciesOptions{}, err
	}
	return traceDependenciesOptions{
		Selector: selector, Depth: depth, MaxNodes: maxNodes, Repo: repo,
		Language: language, EdgeKinds: edgeKinds, Confidence: confidence, Limit: limit, Format: format,
		Derived: newDerivedFilter(arguments.IncludeDerived, repo), View: view,
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
		symbol, file, repository, languages, err := symbolReferenceLocation(snapshot, visit.ID)
		if err != nil {
			return nil, Coverage{}, 0, err
		}
		if options.Repo != "" && repository.name != options.Repo {
			continue
		}
		if !options.Derived.keepsRepository(repository.name) {
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
		reachedFrom, sourceOK := table.String(source.QualifiedName)
		if !sourceOK {
			return nil, Coverage{}, 0, fmt.Errorf("symbol %q has an invalid qualified name", source.StableKey)
		}
		addReferenceCoverage(&coverage, decoded.Confidence)
		row := ReachedSymbol{
			Name: name, QualifiedName: qualifiedName, Kind: kind,
			Depth: int(visit.Depth), Repository: repository.name, Language: firstString(languages),
			FilePath: file.path, StartLine: symbol.StartLine, EndLine: symbol.EndLine,
			ReachedFrom: reachedFrom, ViaKind: string(decoded.Kind),
			ViaConfidence: string(decoded.Confidence), ViaProvenance: string(decoded.Provenance),
		}
		if options.Format == ResponseFormatDetailed {
			row.StableKey = string(symbol.StableKey)
			row.FileKey = file.key
			row.ReachedFromKey = string(source.StableKey)
		}
		nodes = append(nodes, row)
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
