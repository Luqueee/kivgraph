package rebuild

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
)

// TestLoadOrBuildSnapshotPrefersThePublishedFile is the contract phase 1 of
// ADR 0045 exists for: a generation that carries its snapshot is not scanned
// again. The scan is a stub that fails the test if it runs, because "did not
// scan" is the whole claim -- a load that quietly derived the same graph would
// pass every equality check and cost exactly what it was meant to save.
func TestLoadOrBuildSnapshotPrefersThePublishedFile(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "graph.lbdb")
	built := seedPublishedGeneration(t, directory, databasePath)

	loaded, report, err := LoadOrBuildSnapshot(context.Background(), BuildSnapshotOptions{
		DatabasePath: databasePath,
		SnapshotID:   99,
		Scan:         refusingScan(t),
	})
	if err != nil {
		t.Fatalf("LoadOrBuildSnapshot() error = %v", err)
	}
	if !report.Passed || !report.Loaded {
		t.Fatalf("report = %+v, want a loaded snapshot", report)
	}
	if report.LoadRefused != "" {
		t.Fatalf("LoadRefused = %q, want empty for a snapshot that loaded", report.LoadRefused)
	}
	// The id comes from the file, not from the request: a loaded snapshot is
	// the one that was published, and renaming it on load would let two
	// processes disagree about which generation they serve.
	if loaded.Metadata() != built.Metadata() {
		t.Fatalf("metadata\n got %+v\nwant %+v", loaded.Metadata(), built.Metadata())
	}
	if loaded.Metadata().Counts != built.Metadata().Counts {
		t.Fatalf("counts\n got %+v\nwant %+v", loaded.Metadata().Counts, built.Metadata().Counts)
	}
}

// TestLoadOrBuildSnapshotFallsBackAndSaysWhy covers every way a file can fail
// to be trustworthy. None of them may cost an answer: a generation always
// carries the canonical graph, so each one costs a rebuild and is declared.
func TestLoadOrBuildSnapshotFallsBackAndSaysWhy(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		break_  func(t *testing.T, directory string)
		expects string
	}{
		{
			name:    "no published snapshot",
			break_:  func(t *testing.T, directory string) { remove(t, directory, PublishedSnapshotFileName) },
			expects: "read published snapshot",
		},
		{
			name: "digest of another graph",
			break_: func(t *testing.T, directory string) {
				write(t, directory, snapshotFileName, strings.Repeat("ab", 32))
			},
			expects: "content digest",
		},
		{
			name: "no generation digest",
			break_: func(t *testing.T, directory string) {
				remove(t, directory, snapshotFileName)
			},
			expects: "read generation digest",
		},
		{
			name: "digest that is not a digest",
			break_: func(t *testing.T, directory string) {
				write(t, directory, snapshotFileName, "not-hexadecimal")
			},
			expects: "not hexadecimal",
		},
		{
			name: "corrupt payload",
			break_: func(t *testing.T, directory string) {
				path := filepath.Join(directory, PublishedSnapshotFileName)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read snapshot: %v", err)
				}
				data[len(data)-1] ^= 0xFF
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatalf("corrupt snapshot: %v", err)
				}
			},
			expects: "payload digest",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			databasePath := filepath.Join(directory, "graph.lbdb")
			seedPublishedGeneration(t, directory, databasePath)
			testCase.break_(t, directory)

			graph := fakeCanonicalGraph()
			snapshot, report, err := LoadOrBuildSnapshot(context.Background(), BuildSnapshotOptions{
				DatabasePath: databasePath,
				SnapshotID:   77,
				Scan:         fixedScan(graph),
			})
			if err != nil {
				t.Fatalf("LoadOrBuildSnapshot() error = %v, want a rebuild", err)
			}
			if snapshot == nil || !report.Passed {
				t.Fatalf("snapshot = %v, report = %+v, want a rebuilt snapshot", snapshot, report)
			}
			if report.Loaded {
				t.Fatal("report says the snapshot was loaded, but the file was not usable")
			}
			if !strings.Contains(report.LoadRefused, testCase.expects) {
				t.Fatalf("LoadRefused = %q, want it to name %q", report.LoadRefused, testCase.expects)
			}
			if report.SnapshotID != 77 {
				t.Fatalf("SnapshotID = %d, want the requested 77 for a rebuild", report.SnapshotID)
			}
		})
	}
}

// seedPublishedGeneration writes a generation directory that carries both its
// digest and its snapshot, the way a rebuild leaves one behind.
func seedPublishedGeneration(t *testing.T, directory, databasePath string) *hotsnapshot.GraphSnapshot {
	t.Helper()
	graph := fakeCanonicalGraph()
	built, report, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{
		DatabasePath: databasePath,
		SnapshotID:   42,
		Scan:         fixedScan(graph),
	})
	if err != nil {
		t.Fatalf("build the snapshot to publish: %v", err)
	}
	if err := os.WriteFile(databasePath, []byte("graph"), 0o600); err != nil {
		t.Fatalf("seed database file: %v", err)
	}
	write(t, directory, snapshotFileName, report.Digest)
	if err := writePublishedSnapshot(directory, built, report.Digest); err != nil {
		t.Fatalf("write the published snapshot: %v", err)
	}
	return built
}

func refusingScan(t *testing.T) func(context.Context, string) (ladybug.CanonicalGraph, error) {
	t.Helper()
	return func(context.Context, string) (ladybug.CanonicalGraph, error) {
		t.Fatal("the canonical graph was scanned even though the generation carried its snapshot")
		return ladybug.CanonicalGraph{}, nil
	}
}

func write(t *testing.T, directory, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte(contents+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func remove(t *testing.T, directory, name string) {
	t.Helper()
	if err := os.Remove(filepath.Join(directory, name)); err != nil {
		t.Fatalf("remove %s: %v", name, err)
	}
}
