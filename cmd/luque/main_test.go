package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/luque/internal/synthetic"
	"github.com/Luqueee/luque/internal/version"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if got := run([]string{"luque", "version"}, &stdout, &stderr); got != 0 {
		t.Fatalf("run() exit code = %d, want 0", got)
	}
	if got := stdout.String(); got != version.Value+"\n" {
		t.Fatalf("version output = %q, want %q", got, version.Value+"\n")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunWithoutVersionPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if got := run([]string{"luque"}, &stdout, &stderr); got != 2 {
		t.Fatalf("run() exit code = %d, want 2", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "usage: luque version|serve|benchmark generate-graph") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}

func TestRunGenerateGraph(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "synthetic")
	var stdout, stderr bytes.Buffer
	args := []string{
		"luque", "benchmark", "generate-graph",
		"--repositories", "2",
		"--files", "10",
		"--symbols", "20",
		"--edges", "100",
		"--seed", "42",
		"--output", outputDir,
	}
	if got := run(args, &stdout, &stderr); got != 0 {
		t.Fatalf("run() exit code = %d, stderr = %q", got, stderr.String())
	}
	if !strings.Contains(stdout.String(), "generated 2 repositories, 10 files, 20 symbols, 100 edges") {
		t.Fatalf("stdout = %q, want generation summary", stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(outputDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest synthetic.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Seed != 42 || manifest.Edges != 100 {
		t.Fatalf("manifest = %#v, want seed 42 and 100 edges", manifest)
	}
}

func TestRunGenerateGraphRejectsInvalidSize(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{"luque", "benchmark", "generate-graph", "--files", "2", "--symbols", "9", "--edges", "10"}
	if got := run(args, &stdout, &stderr); got != 1 {
		t.Fatalf("run() exit code = %d, want 1", got)
	}
	if !strings.Contains(stderr.String(), "edges must be at least") {
		t.Fatalf("stderr = %q, want validation error", stderr.String())
	}
}
