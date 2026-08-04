package synthetic

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGenerateIsReproducibleForSameSeed(t *testing.T) {
	firstDir := filepath.Join(t.TempDir(), "first")
	secondDir := filepath.Join(t.TempDir(), "second")
	firstConfig := Config{Repositories: 3, Files: 12, Symbols: 20, Edges: 100, Seed: 42, OutputDir: firstDir}
	secondConfig := firstConfig
	secondConfig.OutputDir = secondDir

	firstManifest, err := Generate(context.Background(), firstConfig)
	if err != nil {
		t.Fatalf("first Generate() error = %v", err)
	}
	secondManifest, err := Generate(context.Background(), secondConfig)
	if err != nil {
		t.Fatalf("second Generate() error = %v", err)
	}
	if !reflect.DeepEqual(firstManifest, secondManifest) {
		t.Fatalf("manifests differ:\nfirst=%#v\nsecond=%#v", firstManifest, secondManifest)
	}

	for _, filename := range []string{"manifest.json", "repositories.jsonl", "files.jsonl", "symbols.jsonl", "edges.jsonl"} {
		first, err := os.ReadFile(filepath.Join(firstDir, filename))
		if err != nil {
			t.Fatalf("read first %s: %v", filename, err)
		}
		second, err := os.ReadFile(filepath.Join(secondDir, filename))
		if err != nil {
			t.Fatalf("read second %s: %v", filename, err)
		}
		if string(first) != string(second) {
			t.Fatalf("%s differs for equal seeds", filename)
		}
	}
}

func TestGenerateChangesCorpusForDifferentSeed(t *testing.T) {
	firstConfig := Config{Repositories: 3, Files: 12, Symbols: 20, Edges: 100, Seed: 41, OutputDir: filepath.Join(t.TempDir(), "first")}
	secondConfig := firstConfig
	secondConfig.Seed = 42
	secondConfig.OutputDir = filepath.Join(t.TempDir(), "second")

	if _, err := Generate(context.Background(), firstConfig); err != nil {
		t.Fatalf("first Generate() error = %v", err)
	}
	if _, err := Generate(context.Background(), secondConfig); err != nil {
		t.Fatalf("second Generate() error = %v", err)
	}
	firstEdges, err := os.ReadFile(filepath.Join(firstConfig.OutputDir, "edges.jsonl"))
	if err != nil {
		t.Fatalf("read first edges: %v", err)
	}
	secondEdges, err := os.ReadFile(filepath.Join(secondConfig.OutputDir, "edges.jsonl"))
	if err != nil {
		t.Fatalf("read second edges: %v", err)
	}
	if string(firstEdges) == string(secondEdges) {
		t.Fatal("edges are identical for different seeds")
	}
}

func TestGenerateContainsRequiredGraphFeatures(t *testing.T) {
	config := Config{Repositories: 3, Files: 12, Symbols: 20, Edges: 100, Seed: 7, OutputDir: t.TempDir()}
	manifest, err := Generate(context.Background(), config)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if manifest.Repositories != config.Repositories || manifest.Files != config.Files || manifest.Symbols != config.Symbols || manifest.Edges != config.Edges {
		t.Fatalf("manifest counts = %#v, want repository=%d file=%d symbol=%d edge=%d", manifest, config.Repositories, config.Files, config.Symbols, config.Edges)
	}
	if manifest.EdgeCounts["CONTAINS"] != config.Files || manifest.EdgeCounts["DEFINES"] != config.Symbols {
		t.Fatalf("structural edge counts = %#v", manifest.EdgeCounts)
	}
	semanticEdges := config.Edges - config.Files - config.Symbols
	if manifest.EdgeCounts["REFERENCES"] < semanticEdges/4 || manifest.EdgeCounts["CALLS_DIRECT"] < semanticEdges/4 {
		t.Fatalf("semantic edge counts = %#v, want both relation types broadly represented", manifest.EdgeCounts)
	}
	if len(manifest.DepthFiveChain) != 6 || len(manifest.ControlledCycle) != 3 || manifest.IsolatedSymbolCount == 0 || len(manifest.HubSymbols) != 2 {
		t.Fatalf("feature manifest = %#v", manifest)
	}

	edges, err := readEdges(filepath.Join(config.OutputDir, "edges.jsonl"))
	if err != nil {
		t.Fatalf("read edges: %v", err)
	}
	incoming := map[string]int{}
	outgoing := map[string]int{}
	isolatedStart := config.Symbols - manifest.IsolatedSymbolCount
	for _, edge := range edges {
		if edge.Type != "REFERENCES" && edge.Type != "CALLS_DIRECT" {
			continue
		}
		outgoing[edge.From]++
		incoming[edge.To]++
		if edge.From >= symbolKey(isolatedStart) || edge.To >= symbolKey(isolatedStart) {
			t.Fatalf("semantic edge touches isolated symbol: %#v", edge)
		}
	}
	for _, hub := range manifest.HubSymbols {
		if incoming[hub] == 0 || outgoing[hub] == 0 {
			t.Fatalf("hub %s has incoming=%d outgoing=%d", hub, incoming[hub], outgoing[hub])
		}
	}
}

func TestGenerateRejectsInsufficientEdges(t *testing.T) {
	_, err := Generate(context.Background(), Config{Repositories: 1, Files: 2, Symbols: 9, Edges: 10, OutputDir: t.TempDir()})
	if err == nil {
		t.Fatal("Generate() error = nil, want validation error")
	}
}

func TestGenerateHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Generate(ctx, Config{Repositories: 1, Files: 10, Symbols: 20, Edges: 100, OutputDir: t.TempDir()})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Generate() error = %v, want context.Canceled", err)
	}
}

func readEdges(path string) ([]edgeRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var edges []edgeRecord
	for decoder.More() {
		var edge edgeRecord
		if err := decoder.Decode(&edge); err != nil {
			return nil, err
		}
		edges = append(edges, edge)
	}
	return edges, nil
}

func symbolKey(index int) string {
	return "symbol-" + zeroPad(index, 8)
}

func zeroPad(index, width int) string {
	value := ""
	for len(value) < width {
		value += "0"
	}
	text := []byte(value)
	for position := len(text) - 1; index > 0 && position >= 0; position-- {
		text[position] = byte('0' + index%10)
		index /= 10
	}
	return string(text)
}
