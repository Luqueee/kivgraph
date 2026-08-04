package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/luque/internal/storage/ladybug"
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
	if !strings.Contains(stderr.String(), "usage: luque version|serve|doctor storage") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}

func TestRunDoctorStorageReportsEveryHealthyCheck(t *testing.T) {
	var stdout, stderr bytes.Buffer
	checks := make([]ladybug.DiagnosticCheck, 0, 10)
	for _, name := range []string{"location", "size", "permissions", "lock", "open", "version", "schema", "transactions", "counts", "integrity"} {
		checks = append(checks, ladybug.DiagnosticCheck{Name: name, Status: ladybug.DiagnosticPass, Detail: name + " ok"})
	}
	diagnose := func(_ context.Context, path string) (ladybug.StorageDiagnosis, error) {
		if path != "/tmp/graph.db" {
			t.Fatalf("diagnostic path = %q", path)
		}
		return ladybug.StorageDiagnosis{Path: path, Checks: checks, Healthy: true}, nil
	}

	code := runWithStorageDiagnoser([]string{"luque", "doctor", "storage", "--database", "/tmp/graph.db"}, &stdout, &stderr, diagnose)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	for _, check := range checks {
		if !strings.Contains(stdout.String(), "[PASS] "+check.Name+": "+check.Detail) {
			t.Fatalf("stdout missing %s: %q", check.Name, stdout.String())
		}
	}
}

func TestRunDoctorStorageReturnsFailureForUnhealthyDatabase(t *testing.T) {
	var stdout, stderr bytes.Buffer
	diagnose := func(context.Context, string) (ladybug.StorageDiagnosis, error) {
		return ladybug.StorageDiagnosis{
			Path:   "/tmp/graph.db",
			Checks: []ladybug.DiagnosticCheck{{Name: "integrity", Status: ladybug.DiagnosticFail, Detail: "1 violation"}},
		}, nil
	}

	code := runWithStorageDiagnoser([]string{"luque", "doctor", "storage", "--database=/tmp/graph.db"}, &stdout, &stderr, diagnose)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "storage doctor: FAIL") || !strings.Contains(stdout.String(), "[FAIL] integrity: 1 violation") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunDoctorStorageRequiresDatabasePath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	diagnose := func(context.Context, string) (ladybug.StorageDiagnosis, error) {
		called = true
		return ladybug.StorageDiagnosis{}, nil
	}

	if code := runWithStorageDiagnoser([]string{"luque", "doctor", "storage"}, &stdout, &stderr, diagnose); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if called {
		t.Fatal("diagnoser was called")
	}
	if !strings.Contains(stderr.String(), "--database is required") {
		t.Fatalf("stderr = %q", stderr.String())
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
