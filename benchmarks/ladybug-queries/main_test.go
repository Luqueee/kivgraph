//go:build ladybug && cgo

package main

import (
	"testing"

	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
)

func TestPercentileUsesNearestRank(t *testing.T) {
	values := make([]int64, 100)
	for index := range values {
		values[index] = int64(index + 1)
	}
	for _, test := range []struct {
		quantile float64
		want     float64
	}{{0.50, 50}, {0.95, 95}, {0.99, 99}, {1, 100}} {
		if got := percentile(values, test.quantile); got != test.want {
			t.Fatalf("percentile(q=%.2f) = %.0f, want %.0f", test.quantile, got, test.want)
		}
	}
}

func TestValidateTraversalResultEnforcesDepthUniquenessAndOrder(t *testing.T) {
	valid := []ladybug.TraversalNode{{StableKey: "a", Depth: 1}, {StableKey: "b", Depth: 2}}
	if err := validateTraversalResult(valid, 3, 2); err != nil {
		t.Fatalf("validateTraversalResult() error = %v", err)
	}
	for name, nodes := range map[string][]ladybug.TraversalNode{
		"empty":     nil,
		"too deep":  {{StableKey: "a", Depth: 4}},
		"duplicate": {{StableKey: "a", Depth: 1}, {StableKey: "a", Depth: 2}},
		"unordered": {{StableKey: "b", Depth: 2}, {StableKey: "a", Depth: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateTraversalResult(nodes, 3, 10); err == nil {
				t.Fatal("validateTraversalResult() error = nil")
			}
		})
	}
}
