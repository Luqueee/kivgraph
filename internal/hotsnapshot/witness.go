package hotsnapshot

import "time"

// WitnessPath returns one shortest route from the nearest seed to target as the
// ordered visits along it, seed first, or nil when the bounds hold no route.
//
// It is a different question from Traverse rather than a mode of it: a frontier
// reports a set and needs no route, a witness reports one route and needs no
// set. The two share every gate -- direction, edge kinds, confidences, depth,
// the node budget and the deadline -- so a witness is only ever a route the
// same traversal could have walked, and it is made of the same edges, with the
// same confidence and the same provenance.
//
// The route is lifted out of the queue instead of a predecessor array per
// symbol. A breadth-first queue already records how it first arrived at every
// node it discovered, and a parent is always enqueued before its children, so
// one backward pass over the queue finds every link. That keeps the pooled
// workspace exactly the size it already was, which is the number ADR 0067
// measured per resident server.
//
// Which shortest route, when several tie, follows the order the edges sit in
// the CSR: stable for one snapshot and one query, and not otherwise meaningful.
// The claim is that a route of this length exists and that these edges are one.
//
// The second return reports that the node budget cut the search short. An empty
// route with it set is not an absence: the route may lie past the budget, and a
// caller that reads the two as one thing states an absence the search never
// established.
func (snapshot *GraphSnapshot) WitnessPath(
	starts []SymbolID,
	target SymbolID,
	options TraversalOptions,
) ([]TraversalVisit, bool, error) {
	if len(starts) == 0 {
		return nil, false, ErrInvalidTraversal
	}
	for _, start := range starts {
		if uint64(start) >= uint64(len(snapshot.symbols)) {
			return nil, false, ErrInvalidTraversal
		}
	}
	if uint64(target) >= uint64(len(snapshot.symbols)) {
		return nil, false, ErrInvalidTraversal
	}
	if (options.Direction != TraversalOutgoing && options.Direction != TraversalIncoming) ||
		options.MaxDepth < 0 || options.MaxDepth > MaxTraversalDepth ||
		options.MaxNodes < 1 || options.MaxNodes > MaxTraversalNodes ||
		len(starts) > options.MaxNodes {
		return nil, false, ErrInvalidTraversal
	}
	if !options.Deadline.IsZero() && !time.Now().Before(options.Deadline) {
		return nil, false, ErrTraversalTimeout
	}
	for _, start := range starts {
		if start == target {
			// A seed is its own witness, and the route is no hops long. Saying
			// "no route" here would deny a reachability the caller can see.
			return []TraversalVisit{{ID: target, Source: InvalidSymbolID}}, false, nil
		}
	}

	workspace := snapshot.traversalWorkspacePool.Get()
	if workspace == nil {
		workspace = &traversalWorkspace{}
	}
	scratch := workspace.(*traversalWorkspace)
	generation := scratch.prepare(len(snapshot.symbols), len(snapshot.repositories), options.MaxNodes)
	defer snapshot.traversalWorkspacePool.Put(scratch)

	discovered := 0
	for _, start := range starts {
		if scratch.visited[start] == generation {
			continue
		}
		scratch.visited[start] = generation
		scratch.queue = append(scratch.queue, traversalQueueItem{ID: start, Source: InvalidSymbolID})
		discovered++
	}

	edges := snapshot.forwardEdges
	offsets := snapshot.forwardOffsets
	if options.Direction == TraversalIncoming {
		edges = snapshot.reverseEdges
		offsets = snapshot.reverseOffsets
	}
	truncated := false
	found := false
	for queueIndex := 0; queueIndex < len(scratch.queue) && !found; queueIndex++ {
		if deadlineExceeded(options.Deadline) {
			return nil, truncated, ErrTraversalTimeout
		}
		item := scratch.queue[queueIndex]
		if int(item.Depth) >= options.MaxDepth {
			continue
		}
		for _, edge := range edges[offsets[item.ID]:offsets[item.ID+1]] {
			if deadlineExceeded(options.Deadline) {
				return nil, truncated, ErrTraversalTimeout
			}
			if !codeAllowed(edge.Kind, options.EdgeKinds) ||
				!codeAllowed(edge.Confidence, options.Confidences) ||
				scratch.visited[edge.Target] == generation {
				continue
			}
			if discovered >= options.MaxNodes {
				truncated = true
				continue
			}
			scratch.visited[edge.Target] = generation
			discovered++
			scratch.queue = append(scratch.queue, traversalQueueItem{
				ID: edge.Target, Depth: item.Depth + 1, Source: item.ID, Edge: edge,
			})
			if edge.Target == target {
				// The first discovery of a node is its shortest distance from
				// the nearest seed, so nothing later can shorten this route and
				// there is no reason to keep walking.
				found = true
				break
			}
		}
	}
	if !found {
		return nil, truncated, nil
	}
	return witnessFromQueue(scratch.queue, target, options.MaxDepth), truncated, nil
}

// witnessFromQueue walks a breadth-first queue backwards once and returns the
// chain that ends at target, seed first.
//
// Backwards is the only direction that needs one pass: a node is enqueued after
// the node it was reached from, so every parent sits at a lower index than its
// child, and the visited mark means no symbol was enqueued twice.
func witnessFromQueue(queue []traversalQueueItem, target SymbolID, maxDepth int) []TraversalVisit {
	path := make([]TraversalVisit, 0, maxDepth+1)
	wanted := target
	for index := len(queue) - 1; index >= 0; index-- {
		item := queue[index]
		if item.ID != wanted {
			continue
		}
		path = append(path, TraversalVisit{
			ID: item.ID, Depth: item.Depth, Source: item.Source, Edge: item.Edge,
		})
		if item.Source == InvalidSymbolID {
			break
		}
		wanted = item.Source
	}
	for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
		path[left], path[right] = path[right], path[left]
	}
	return path
}
