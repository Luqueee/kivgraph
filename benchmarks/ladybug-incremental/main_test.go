//go:build ladybug && cgo

package main

import (
	"testing"

	"github.com/Luqueee/kivgraph/internal/indexer"
)

func TestPercentileUsesUpperObservedRank(t *testing.T) {
	got := percentile([]float64{4, 1, 3, 2, 5}, 0.95)
	if got != 5 {
		t.Fatalf("percentile(95%%) = %v, want upper observed rank 5", got)
	}
}

func TestAssessGateRequiresEveryScenarioAndZeroViolations(t *testing.T) {
	passing := func(name string, route indexer.Route) scenarioResult {
		return scenarioResult{
			Scenario: name, ExpectedRoute: route,
			Summary:   summary{P95MS: 100},
			Integrity: integrityAssessment{Passed: true},
		}
	}
	gate := assessGate([]scenarioResult{
		passing("simple_file", indexer.RouteDelta),
		passing("imports_exports", indexer.RouteDelta),
		passing("manifest", indexer.RouteRepublish),
	})
	if !gate.Passed || !gate.NoGhostEdges {
		t.Fatalf("passing gate = %#v, want PASS", gate)
	}

	failing := passing("manifest", indexer.RouteRepublish)
	failing.Integrity = integrityAssessment{Passed: false, Violations: 1}
	gate = assessGate([]scenarioResult{
		passing("simple_file", indexer.RouteDelta),
		passing("imports_exports", indexer.RouteDelta),
		failing,
	})
	if gate.Passed || gate.NoGhostEdges {
		t.Fatalf("integrity failure gate = %#v, want rejection", gate)
	}
}

func TestBenchmarkCorpusIsValidAndDeterministic(t *testing.T) {
	first := benchmarkCorpus(8, 3)
	second := benchmarkCorpus(8, 3)
	if err := first.Validate(); err != nil {
		t.Fatalf("first corpus validation error = %v", err)
	}
	if err := second.Validate(); err != nil {
		t.Fatalf("second corpus validation error = %v", err)
	}
	if len(first.Files) != 8 || len(first.Symbols) != 24 || len(first.Evidence) != 24 {
		t.Fatalf("corpus cardinalities = files=%d symbols=%d evidence=%d", len(first.Files), len(first.Symbols), len(first.Evidence))
	}
	for index := range first.Edges {
		if first.Edges[index] != second.Edges[index] {
			t.Fatalf("corpus edge %d differs between runs", index)
		}
	}
}
