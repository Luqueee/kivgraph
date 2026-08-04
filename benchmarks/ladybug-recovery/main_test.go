//go:build ladybug && cgo && linux

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseProcRChar(t *testing.T) {
	value, err := parseProcRChar("rchar: 12345\nwchar: 99\n")
	if err != nil {
		t.Fatalf("parseProcRChar() error = %v", err)
	}
	if value != 12_345 {
		t.Fatalf("parseProcRChar() = %d, want 12345", value)
	}
}

func TestParseProcRCharRejectsMissingAndInvalidValues(t *testing.T) {
	for _, input := range []string{"wchar: 1\n", "rchar: invalid\n"} {
		if _, err := parseProcRChar(input); err == nil {
			t.Fatalf("parseProcRChar(%q) error = nil", input)
		}
	}
}

func TestWriteBulkCSVProducesDeterministicSymbolRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "symbols.csv")
	if err := writeBulkCSV(context.Background(), path, 3); err != nil {
		t.Fatalf("writeBulkCSV() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("line count = %d, want 3", len(lines))
	}
	if !strings.HasPrefix(lines[0], "recovery-bulk-00000000,repository-0000,file-00000000,") {
		t.Fatalf("first row = %q", lines[0])
	}
	if !strings.HasPrefix(lines[2], "recovery-bulk-00000002,repository-0000,file-00000000,") {
		t.Fatalf("last row = %q", lines[2])
	}
}

func TestCompletedCasePreservesFailureEvidence(t *testing.T) {
	probe := completedCase("case", "expected", time.Now(), false, os.ErrPermission, nil, []string{"check"})
	if probe.Passed {
		t.Fatal("Passed = true")
	}
	if !strings.Contains(probe.Observed, "permission denied") {
		t.Fatalf("Observed = %q", probe.Observed)
	}
	if len(probe.Checks) != 1 || probe.Checks[0] != "check" {
		t.Fatalf("Checks = %#v", probe.Checks)
	}
}
