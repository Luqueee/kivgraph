package hotsnapshot

import (
	"errors"
	"sort"
	"time"
)

const MaxTraversalDepth = 5
const MaxTraversalNodes = 25_000

var (
	ErrInvalidTraversal = errors.New("invalid graph traversal options")
	ErrTraversalTimeout = errors.New("graph traversal deadline exceeded")
)

type TraversalDirection uint8

const (
	TraversalOutgoing TraversalDirection = iota + 1
	TraversalIncoming
)

// TraversalOptions bounds one breadth-first traversal. The start symbol is
// included at depth zero and counts toward MaxNodes. EdgeKinds and Confidences
// gate which edges the traversal may follow, so they change reachability, not
// just the shape of the result.
type TraversalOptions struct {
	Direction   TraversalDirection
	MaxDepth    int
	MaxNodes    int
	EdgeKinds   []uint8
	Confidences []uint8
	Deadline    time.Time
}

// TraversalVisit is one reached symbol. Source and Edge record how the BFS
// first arrived at it, which is the only path fact a breadth-first frontier
// can state without retaining every route. The start symbol has no such edge:
// its Source is InvalidSymbolID and its Edge is zero.
type TraversalVisit struct {
	ID     SymbolID
	Depth  uint32
	Source SymbolID
	Edge   PackedEdge
}

type TraversalRepositoryGroup struct {
	Repository RepositoryID
	Count      int
}

type TraversalResult struct {
	Visits       []TraversalVisit
	Repositories []TraversalRepositoryGroup
	Truncated    bool
}

// Traverse performs a bounded BFS over forward or reverse CSR. Visited state is
// a dense generation array indexed by SymbolID; no per-node map is allocated.
func (snapshot *GraphSnapshot) Traverse(start SymbolID, options TraversalOptions) (TraversalResult, error) {
	if uint64(start) >= uint64(len(snapshot.symbols)) ||
		(options.Direction != TraversalOutgoing && options.Direction != TraversalIncoming) ||
		options.MaxDepth < 0 || options.MaxDepth > MaxTraversalDepth ||
		options.MaxNodes < 1 || options.MaxNodes > MaxTraversalNodes {
		return TraversalResult{}, ErrInvalidTraversal
	}
	if !options.Deadline.IsZero() && !time.Now().Before(options.Deadline) {
		return TraversalResult{}, ErrTraversalTimeout
	}

	result := TraversalResult{Visits: make([]TraversalVisit, 0, minInt(options.MaxNodes, 64))}
	visited := make([]uint32, len(snapshot.symbols))
	queue := make([]traversalQueueItem, 0, minInt(options.MaxNodes, 64))
	visited[start] = 1
	queue = append(queue, traversalQueueItem{ID: start, Source: InvalidSymbolID})
	repositoryCounts := make([]int, len(snapshot.repositories))
	repositoriesSeen := make([]bool, len(snapshot.repositories))
	repositories := make([]RepositoryID, 0)
	discovered := 1

	for queueIndex := 0; queueIndex < len(queue); queueIndex++ {
		if deadlineExceeded(options.Deadline) {
			result.Repositories = traversalRepositoryGroups(repositoryCounts, repositories)
			return result, ErrTraversalTimeout
		}
		item := queue[queueIndex]
		result.Visits = append(result.Visits, TraversalVisit{ID: item.ID, Depth: item.Depth, Source: item.Source, Edge: item.Edge})
		if repository, ok := snapshot.symbolRepository(item.ID); ok {
			if !repositoriesSeen[repository] {
				repositoriesSeen[repository] = true
				repositories = append(repositories, repository)
			}
			repositoryCounts[repository]++
		}
		if int(item.Depth) >= options.MaxDepth {
			continue
		}

		edges := snapshot.forwardEdges
		offsets := snapshot.forwardOffsets
		if options.Direction == TraversalIncoming {
			edges = snapshot.reverseEdges
			offsets = snapshot.reverseOffsets
		}
		for _, edge := range edges[offsets[item.ID]:offsets[item.ID+1]] {
			if deadlineExceeded(options.Deadline) {
				result.Repositories = traversalRepositoryGroups(repositoryCounts, repositories)
				return result, ErrTraversalTimeout
			}
			if !codeAllowed(edge.Kind, options.EdgeKinds) || !codeAllowed(edge.Confidence, options.Confidences) || visited[edge.Target] != 0 {
				continue
			}
			if discovered >= options.MaxNodes {
				result.Truncated = true
				continue
			}
			visited[edge.Target] = 1
			discovered++
			queue = append(queue, traversalQueueItem{ID: edge.Target, Depth: item.Depth + 1, Source: item.ID, Edge: edge})
		}
	}
	result.Repositories = traversalRepositoryGroups(repositoryCounts, repositories)
	return result, nil
}

type traversalQueueItem struct {
	ID     SymbolID
	Depth  uint32
	Source SymbolID
	Edge   PackedEdge
}

func (snapshot *GraphSnapshot) symbolRepository(symbol SymbolID) (RepositoryID, bool) {
	if uint64(symbol) >= uint64(len(snapshot.symbols)) {
		return 0, false
	}
	file := snapshot.symbols[symbol].File
	if uint64(file) >= uint64(len(snapshot.files)) {
		return 0, false
	}
	repository := snapshot.files[file].Repository
	if uint64(repository) >= uint64(len(snapshot.repositories)) {
		return 0, false
	}
	return repository, true
}

func codeAllowed(code uint8, allowed []uint8) bool {
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

func traversalRepositoryGroups(counts []int, repositories []RepositoryID) []TraversalRepositoryGroup {
	sort.Slice(repositories, func(i, j int) bool { return repositories[i] < repositories[j] })
	groups := make([]TraversalRepositoryGroup, 0, len(repositories))
	for _, repository := range repositories {
		groups = append(groups, TraversalRepositoryGroup{Repository: repository, Count: counts[repository]})
	}
	return groups
}

func deadlineExceeded(deadline time.Time) bool {
	return !deadline.IsZero() && !time.Now().Before(deadline)
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
