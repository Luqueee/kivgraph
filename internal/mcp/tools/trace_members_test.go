package tools

import (
	"context"
	"testing"
)

// The members of a container seed its walk because they are its content. They
// are not dependencies, so they are not rows -- and they must not be counted as
// reached, nor spend the node budget the caller set for what is reached.
func TestTraceDependenciesMembersCostNeitherReachNorBudget(t *testing.T) {
	store := containerMemberStore(t, 51)

	_, whole, err := traceDependencies(context.Background(), nil,
		TraceDependenciesInput{StableKey: "sym-class", Depth: 1}, store)
	if err != nil {
		t.Fatal(err)
	}
	// shared.BaseCache and shared.Payload: two, not two plus the two members.
	if whole.Results.Reached != 2 || whole.Total != 2 || whole.Results.TraversalTruncated {
		t.Fatalf("reached = %d, total = %d, truncated = %v, want the two dependencies and a whole walk",
			whole.Results.Reached, whole.Total, whole.Results.TraversalTruncated)
	}

	// A budget of two is the root and one dependency. It used to be refused
	// outright, because the three seeds alone exceeded it.
	_, bounded, err := traceDependencies(context.Background(), nil,
		TraceDependenciesInput{StableKey: "sym-class", Depth: 1, MaxNodes: 2}, store)
	if err != nil {
		t.Fatalf("max_nodes below the member count: error = %v, want a bounded walk", err)
	}
	if bounded.Results.Reached != 1 || !bounded.Results.TraversalTruncated || bounded.Results.MaxNodes != 2 {
		t.Fatalf("bounded reached = %d, truncated = %v, max_nodes = %d, want one dependency, a cut walk and the caller's own bound echoed",
			bounded.Results.Reached, bounded.Results.TraversalTruncated, bounded.Results.MaxNodes)
	}

	// The same bound when asking for a route.
	_, route, err := traceDependencies(context.Background(), nil,
		TraceDependenciesInput{StableKey: "sym-class", To: "BaseCache", MaxNodes: 2}, store)
	if err != nil {
		t.Fatalf("route with max_nodes below the member count: error = %v", err)
	}
	if route.Total != 1 {
		t.Fatalf("route rows = %d, want the one hop to shared.BaseCache", route.Total)
	}

	// A root with no members is unchanged: the root still counts toward it.
	_, plain, err := traceDependencies(context.Background(), nil,
		TraceDependenciesInput{StableKey: "sym-method", Depth: 1, MaxNodes: 1}, store)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Results.Reached != 0 || !plain.Results.TraversalTruncated {
		t.Fatalf("member-less root with max_nodes 1: reached = %d, truncated = %v, want nothing reached and a cut walk",
			plain.Results.Reached, plain.Results.TraversalTruncated)
	}
}
