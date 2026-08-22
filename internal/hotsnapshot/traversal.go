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

// Traverse walks outward or inward from one symbol.
func (snapshot *GraphSnapshot) Traverse(start SymbolID, options TraversalOptions) (TraversalResult, error) {
	return snapshot.TraverseFrom([]SymbolID{start}, options)
}

// TraverseFrom walks from several symbols at once, all of them at depth zero.
//
// It exists for a container: the reach of a class is the reach of what its own
// source names, and the members are inside that source. Running one traversal
// per member and merging the results would count a node shared by two members
// twice and would report its depth from whichever member happened to run first.
// One BFS over every seed reports each node once, at its true distance from the
// nearest seed.
//
// Every seed is visited, so a caller that does not want the seeds themselves in
// the answer filters them out; the visits carry InvalidSymbolID as their Source,
// which is how a seed is told from a reached node.
func (snapshot *GraphSnapshot) TraverseFrom(starts []SymbolID, options TraversalOptions) (TraversalResult, error) {
	if len(starts) == 0 {
		return TraversalResult{}, ErrInvalidTraversal
	}
	for _, start := range starts {
		if uint64(start) >= uint64(len(snapshot.symbols)) {
			return TraversalResult{}, ErrInvalidTraversal
		}
	}
	if (options.Direction != TraversalOutgoing && options.Direction != TraversalIncoming) ||
		options.MaxDepth < 0 || options.MaxDepth > MaxTraversalDepth ||
		options.MaxNodes < 1 || options.MaxNodes > MaxTraversalNodes ||
		len(starts) > options.MaxNodes {
		return TraversalResult{}, ErrInvalidTraversal
	}
	if !options.Deadline.IsZero() && !time.Now().Before(options.Deadline) {
		return TraversalResult{}, ErrTraversalTimeout
	}

	workspace := snapshot.traversalWorkspacePool.Get()
	if workspace == nil {
		workspace = &traversalWorkspace{}
	}
	scratch := workspace.(*traversalWorkspace)
	generation := scratch.prepare(len(snapshot.symbols), len(snapshot.repositories), options.MaxNodes)
	defer snapshot.traversalWorkspacePool.Put(scratch)

	result := TraversalResult{Visits: make([]TraversalVisit, 0, minInt(options.MaxNodes, 64))}
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
	for queueIndex := 0; queueIndex < len(scratch.queue); queueIndex++ {
		if deadlineExceeded(options.Deadline) {
			result.Repositories = traversalRepositoryGroups(scratch.repositoryCounts, scratch.repositories)
			return result, ErrTraversalTimeout
		}
		item := scratch.queue[queueIndex]
		result.Visits = append(result.Visits, TraversalVisit{ID: item.ID, Depth: item.Depth, Source: item.Source, Edge: item.Edge})
		if repository, ok := snapshot.symbolRepository(item.ID); ok {
			if !scratch.repositoriesSeen[repository] {
				scratch.repositoriesSeen[repository] = true
				scratch.repositories = append(scratch.repositories, repository)
			}
			scratch.repositoryCounts[repository]++
		}
		if int(item.Depth) >= options.MaxDepth {
			continue
		}

		for _, edge := range edges[offsets[item.ID]:offsets[item.ID+1]] {
			if deadlineExceeded(options.Deadline) {
				result.Repositories = traversalRepositoryGroups(scratch.repositoryCounts, scratch.repositories)
				return result, ErrTraversalTimeout
			}
			if !codeAllowed(edge.Kind, options.EdgeKinds) || !codeAllowed(edge.Confidence, options.Confidences) || scratch.visited[edge.Target] == generation {
				continue
			}
			if discovered >= options.MaxNodes {
				result.Truncated = true
				continue
			}
			scratch.visited[edge.Target] = generation
			discovered++
			scratch.queue = append(scratch.queue, traversalQueueItem{ID: edge.Target, Depth: item.Depth + 1, Source: item.ID, Edge: edge})
		}
	}
	result.Repositories = traversalRepositoryGroups(scratch.repositoryCounts, scratch.repositories)
	return result, nil
}

type traversalQueueItem struct {
	ID     SymbolID
	Depth  uint32
	Source SymbolID
	Edge   PackedEdge
}

type traversalWorkspace struct {
	visited          []uint32
	generation       uint32
	queue            []traversalQueueItem
	repositoryCounts []int
	repositoriesSeen []bool
	repositories     []RepositoryID
}

func (workspace *traversalWorkspace) prepare(symbolCount, repositoryCount, maxNodes int) uint32 {
	if cap(workspace.visited) < symbolCount {
		workspace.visited = make([]uint32, symbolCount)
	} else {
		workspace.visited = workspace.visited[:symbolCount]
	}
	workspace.generation++
	if workspace.generation == 0 {
		clear(workspace.visited)
		workspace.generation = 1
	}

	initialQueueCapacity := minInt(maxNodes, 64)
	if cap(workspace.queue) < initialQueueCapacity {
		workspace.queue = make([]traversalQueueItem, 0, initialQueueCapacity)
	} else {
		workspace.queue = workspace.queue[:0]
	}

	if cap(workspace.repositoryCounts) < repositoryCount {
		workspace.repositoryCounts = make([]int, repositoryCount)
	} else {
		workspace.repositoryCounts = workspace.repositoryCounts[:repositoryCount]
		clear(workspace.repositoryCounts)
	}
	if cap(workspace.repositoriesSeen) < repositoryCount {
		workspace.repositoriesSeen = make([]bool, repositoryCount)
	} else {
		workspace.repositoriesSeen = workspace.repositoriesSeen[:repositoryCount]
		clear(workspace.repositoriesSeen)
	}
	if cap(workspace.repositories) < repositoryCount {
		workspace.repositories = make([]RepositoryID, 0, repositoryCount)
	} else {
		workspace.repositories = workspace.repositories[:0]
	}
	return workspace.generation
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
