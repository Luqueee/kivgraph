package tools

import (
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// normalizeWitnessTarget validates a `to` request and returns the view the
// route must be spelled with.
//
// Everything it refuses, it refuses because honouring it would answer a
// different question under the same name. The row filters select after
// reachability, so they can remove a link the route needs and leave a hole
// where an edge was. `compact` groups rows by file, and for a route the order
// of the rows is the answer. `limit` and `cursor` page a set, and a route is
// bounded by depth already: it is at most one row per hop, so there is no page
// after the first and no honest way to spell half of it.
func normalizeWitnessTarget(arguments TraceDependenciesInput, view string) (string, string, string, error) {
	target := arguments.To
	if target == "" {
		if arguments.ToPath != "" {
			return "", "", "", NewToolError(CodeInvalidArgument, "to_path narrows to, so it needs to")
		}
		return "", "", view, nil
	}
	for _, refused := range []struct {
		set  bool
		name string
		why  string
	}{
		{arguments.Repo != "", "repo", "it selects rows after reachability and would leave a hole in the route"},
		{arguments.Language != "", "language", "it selects rows after reachability and would leave a hole in the route"},
		{arguments.IncludeDerived, "include_derived", "a route is reported whole, whichever repositories it crosses"},
		{arguments.Limit != 0, "limit", "a route is at most one row per hop and is never paged"},
		{arguments.Cursor != "", "cursor", "a route is at most one row per hop and is never paged"},
		{arguments.View == ViewCompact, "view=compact", "it groups rows by file, and the order of the rows is the answer"},
	} {
		if refused.set {
			return "", "", "", NewToolError(CodeInvalidArgument, fmt.Sprintf(
				"%s cannot be combined with to: %s", refused.name, refused.why,
			))
		}
	}
	return target, arguments.ToPath, ViewFull, nil
}

// traceWitness answers by which route a root reaches one named symbol.
//
// It is the same walk as the fan-out page, with the same gates, and it converts
// its rows with the same function: a hop of a route is a reached symbol, and the
// edge on the row is the edge the route crossed. What changes is the claim. A
// page states a set and says nothing about how any member was arrived at; this
// states one route, in order, and every row of it is a type-checked edge the
// caller can open.
func traceWitness(
	snapshot *hotsnapshot.GraphSnapshot,
	options traceDependenciesOptions,
	traversalOptions hotsnapshot.TraversalOptions,
	seeds []hotsnapshot.SymbolID,
	rootID hotsnapshot.SymbolID,
	root hotsnapshot.SymbolRecord,
	rootRepository string,
) (*sdkmcp.CallToolResult, Response[DependencyTrace], error) {
	targetID, targetQualifiedName, err := resolveDeclarationByName(
		snapshot, options.Target, options.Repo, options.TargetPath,
	)
	if err != nil {
		return nil, Response[DependencyTrace]{}, err
	}

	path, truncated, err := snapshot.WitnessPath(seeds, targetID, traversalOptions)
	if err != nil {
		return nil, Response[DependencyTrace]{}, classifyTraversalError(err)
	}
	nodes, coverage, deepest, err := dependencyNodes(snapshot, path, options)
	if err != nil {
		return nil, Response[DependencyTrace]{}, WrapToolError(
			CodeSnapshotUnavailable, "active snapshot contains invalid dependency metadata", err)
	}

	// An empty route is the case that needs the blind spots most: the walk
	// crossed what the resolver could read, and what it could not read is
	// exactly where a route could still be hiding.
	rootName, _ := snapshot.Strings().String(root.Name)
	rootRepositoryID := hotsnapshot.InvalidRepositoryID
	if file, found := snapshot.File(root.File); found {
		rootRepositoryID = file.Repository
	}
	completeness, unresolvedRelated, err := completenessOutwardFor(snapshot, rootID, rootName, rootRepositoryID)
	if err != nil {
		return nil, Response[DependencyTrace]{}, WrapToolError(
			CodeSnapshotUnavailable, "active snapshot contains invalid unresolved metadata", err)
	}
	coverage.UnresolvedRelated += unresolvedRelated

	metadata := snapshot.Metadata()
	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[DependencyTrace]{
		SnapshotID: &snapshotID, SnapshotAgeMS: &snapshotAgeMS,
		Total: len(nodes), Returned: len(nodes),
		Coverage:     coverage,
		Completeness: &completeness,
		Guidance:     witnessGuidance(len(nodes), options.Depth, truncated, completeness.Verdict),
		Results: DependencyTrace{
			RootKey: symbolStableKey(snapshot, root), RootRepository: rootRepository,
			Depth: options.Depth, MaxNodes: options.MaxNodes,
			Reached: len(nodes), DeepestDepth: deepest,
			TraversalTruncated: truncated, Nodes: nodes,
			WitnessTo: targetQualifiedName, WitnessHops: len(nodes),
			View: ViewFull,
		},
		View: ViewFull,
	}, nil
}

// witnessGuidance speaks only when no route was found, because that is the
// answer a caller is most likely to over-read.
//
// A bounded search that found nothing has three different meanings and they
// need three different next calls: the budget ran out, the resolver could not
// read something on the way, or there is genuinely no route this shallow. None
// of them is "these two symbols are unrelated", and saying so would be the one
// wrong answer that stops the caller from asking again.
func witnessGuidance(hops, depth int, truncated bool, verdict string) string {
	switch {
	case hops > 0:
		return ""
	case truncated:
		return "the search ran out of its node budget before reaching that symbol, so this is a bound and not an absence: raise max_nodes, or narrow the walk with edge_kinds"
	case verdict == VerdictLowerBound:
		return "no route within this depth, and the index recorded places it could not read that the route may cross: read completeness.blind_spots before concluding these are unrelated"
	default:
		return fmt.Sprintf("no route of %d hops or fewer, which is a bound and not an absence: raise depth, or ask from the other end with get_blast_radius", depth)
	}
}
