package tools

import "fmt"

// Guidance is the sentence a response adds when the count itself is the
// interesting part.
//
// An empty answer is the moment a session defects: read as "no such thing", it
// sends the agent to grep and it does not come back. Read as "nothing references
// this, and here is how to widen the question", it is an answer. A truncated
// answer has the mirror problem -- the agent cannot tell whether it saw the
// important rows.
//
// It costs about fifteen tokens and only appears when there is something to say.
// Nothing here is derived from the graph beyond the counts already in the
// envelope, so it cannot rewrite a client's cached prompt.
func referenceGuidance(direction string, total, returned int, truncated bool, verdict string) string {
	switch {
	case total == 0 && verdict == VerdictLowerBound:
		// A recorded failure asked for this name, so the empty list is a
		// minimum and not an absence. Saying otherwise sends the agent away
		// certain, which is the one outcome worse than sending it to grep.
		return "nothing resolved references this symbol, but the index recorded places it could not read that ask for this name: read completeness.blind_spots and fall back to its pattern before concluding anything"
	case total == 0 && direction == FindReferencesDirectionIncoming:
		return "nothing references this symbol in the published graph; the edges are type-checked, so this is an absence rather than a miss. Widen with find_cross_repo_consumers, or check graph_status if the tree moved since it was indexed"
	case total == 0:
		return "this symbol reaches nothing in the published graph. Ask the other direction with direction=incoming"
	case truncated:
		return truncatedGuidance(returned, total, "edge_kinds, confidence, repo or language")
	default:
		return ""
	}
}

// traversalGuidance says the same thing for a bounded walk, where an empty
// answer more often means the bound rather than the graph.
func traversalGuidance(tool string, total, returned int, truncated bool) string {
	switch {
	case total == 0:
		return "the traversal reached nothing within its bounds; raise depth, or call find_references for the direct relations only"
	case truncated:
		return truncatedGuidance(returned, total, "depth, max_nodes, edge_kinds or confidence")
	default:
		return ""
	}
}

// truncatedGuidance names the two ways out of a partial answer, in the order
// that costs less: narrowing beats paging, because a second page of rows the
// caller did not want is a second payload.
func truncatedGuidance(returned, total int, narrowBy string) string {
	return fmt.Sprintf("showing %d of %d; narrow with %s, or pass the cursor for the next page",
		returned, total, narrowBy)
}
