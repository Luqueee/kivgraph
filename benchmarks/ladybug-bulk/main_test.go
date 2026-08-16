//go:build ladybug && cgo

package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/synthetic"
)

func TestRunCopiesAndVerifiesEveryRecord(t *testing.T) {
	corpusDir := filepath.Join(t.TempDir(), "corpus")
	generated, err := synthetic.Generate(context.Background(), synthetic.Config{
		Repositories: 2,
		Files:        10,
		Symbols:      20,
		Edges:        100,
		Seed:         42,
		OutputDir:    corpusDir,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	result, err := run(context.Background(), config{
		CorpusDir:      corpusDir,
		DatabasePath:   filepath.Join(t.TempDir(), "graph.db"),
		SchemaPath:     filepath.Join("..", "..", "schemas", "ladybug", "001-synthetic.cypher"),
		OutputDir:      t.TempDir(),
		IndividualPath: filepath.Join(t.TempDir(), "missing-individual.json"),
		BatchPath:      filepath.Join(t.TempDir(), "missing-batch.json"),
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	wantNodes := generated.Repositories + generated.Files + generated.Symbols
	if result.Nodes != wantNodes || result.Edges != generated.Edges {
		t.Fatalf("COPY loaded nodes=%d edges=%d, want nodes=%d edges=%d", result.Nodes, result.Edges, wantNodes, generated.Edges)
	}
	if result.RecordsPerSecond <= 0 || result.EndToEndRecordsPerSec <= 0 || result.PeakRSSBytes <= 0 || result.DatabaseBytes <= 0 {
		t.Fatalf("invalid COPY metrics: %#v", result)
	}
	if len(result.Comparison) != 1 || result.Comparison[0].Strategy != "COPY" || !result.Comparison[0].Comparable {
		t.Fatalf("comparison = %#v, want comparable COPY entry", result.Comparison)
	}
}

func TestCypherStringEscapesDoubleQuotes(t *testing.T) {
	if got := cypherString(`/tmp/a"b.csv`); got != `"/tmp/a\"b.csv"` {
		t.Fatalf("cypherString() = %q", got)
	}
}

func TestAssessGateRequiresFullScaleAndRSSLimit(t *testing.T) {
	result := results{FullInitialScale: true, PeakRSSBytes: maxQualificationRSS}
	if assessment := assessGate(result); !assessment.Passed || !assessment.RSSWithin2GiB {
		t.Fatalf("assessGate() = %#v, want passing full-scale result", assessment)
	}
	result.PeakRSSBytes++
	if assessment := assessGate(result); assessment.Passed || assessment.RSSWithin2GiB {
		t.Fatalf("assessGate() = %#v, want RSS failure", assessment)
	}
}
