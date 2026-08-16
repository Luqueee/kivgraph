//go:build ladybug && cgo

package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/synthetic"
)

func TestRunLoadsEveryRecordWithConfiguredTransactions(t *testing.T) {
	corpusDir := filepath.Join(t.TempDir(), "corpus")
	manifest, err := synthetic.Generate(context.Background(), synthetic.Config{
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
		CorpusDir:       corpusDir,
		DatabasePath:    filepath.Join(t.TempDir(), "graph.db"),
		SchemaPath:      filepath.Join("..", "..", "schemas", "ladybug", "001-synthetic.cypher"),
		OutputDir:       t.TempDir(),
		TransactionSize: 10,
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	wantNodes := manifest.Repositories + manifest.Files + manifest.Symbols
	if result.Nodes != wantNodes || result.Edges != manifest.Edges {
		t.Fatalf("loaded nodes=%d edges=%d, want nodes=%d edges=%d", result.Nodes, result.Edges, wantNodes, manifest.Edges)
	}
	if result.Transactions != 14 {
		t.Fatalf("transactions = %d, want 14", result.Transactions)
	}
	if result.NodesPerSecond <= 0 || result.EdgesPerSecond <= 0 || result.DatabaseBytes <= 0 {
		t.Fatalf("invalid benchmark metrics: %#v", result)
	}
}

func TestRunSupportsAutocommit(t *testing.T) {
	corpusDir := filepath.Join(t.TempDir(), "corpus")
	if _, err := synthetic.Generate(context.Background(), synthetic.Config{
		Repositories: 1,
		Files:        4,
		Symbols:      9,
		Edges:        30,
		Seed:         7,
		OutputDir:    corpusDir,
	}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	result, err := run(context.Background(), config{
		CorpusDir:       corpusDir,
		DatabasePath:    filepath.Join(t.TempDir(), "graph.db"),
		SchemaPath:      filepath.Join("..", "..", "schemas", "ladybug", "001-synthetic.cypher"),
		OutputDir:       t.TempDir(),
		TransactionSize: 0,
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if result.Transactions != 0 {
		t.Fatalf("transactions = %d, want 0 in autocommit mode", result.Transactions)
	}
}

func TestRunRejectsNegativeTransactionSize(t *testing.T) {
	_, err := run(context.Background(), config{TransactionSize: -1})
	if err == nil {
		t.Fatal("run() error = nil, want validation error")
	}
}
