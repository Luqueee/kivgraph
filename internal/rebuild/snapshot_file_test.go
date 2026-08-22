package rebuild

import (
	"context"
	"encoding/binary"
	"errors"
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

// TestALoadedSnapshotOutlivesTheMappedFile is the guard the mapped load depends
// on. The snapshot reads its string values out of the mapping, so it keeps that
// mapping alive; everything else it decoded is a copy. Deleting the file proves
// both halves at once: a mapping survives its path being unlinked, so the graph
// has to stay whole, and a snapshot that still needed the file would be one that
// cannot survive its generation being pruned -- which is exactly what happens to
// every generation but two.
func TestALoadedSnapshotOutlivesTheMappedFile(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "graph.lbdb")
	built := seedPublishedGeneration(t, directory, databasePath)

	loaded, err := loadPublishedSnapshot(directory)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	remove(t, directory, PublishedSnapshotFileName)

	counts := loaded.Metadata().Counts
	if counts.Symbols == 0 || counts.Edges == 0 {
		t.Fatal("the fixture carries no symbols or edges, so nothing here is exercised")
	}
	for id := range hotsnapshot.SymbolID(counts.Symbols) {
		record, ok := loaded.Symbol(id)
		if !ok {
			t.Fatalf("symbol %d vanished", id)
		}
		// Every string this record names is read after the mapping is gone,
		// including its stable key, whose characters the format carries as bytes
		// rather than as an interned id.
		key, okKey := loaded.StableKey(record.StableKey)
		if !okKey || key == "" {
			t.Fatalf("symbol %d has no stable key (%v)", id, okKey)
		}
		if resolved, found := loaded.SymbolByStableKey(key); !found || resolved != id {
			t.Fatalf("stable key %q resolves to %d, want %d", key, resolved, id)
		}
		for _, interned := range []hotsnapshot.InternedString{record.Name, record.QualifiedName, record.Kind} {
			value, ok := loaded.Strings().String(interned)
			if !ok || value == "" {
				t.Fatalf("interned string %d of symbol %d is %q (%v)", interned, id, value, ok)
			}
			if got, found := loaded.Strings().Lookup(value); !found || got != interned {
				t.Fatalf("Lookup(%q) = %d (%v), want %d", value, got, found, interned)
			}
		}
		for _, edge := range loaded.Outgoing(id) {
			if _, ok := loaded.Evidence(edge.Evidence); !ok {
				t.Fatalf("edge from %d names evidence %d, which is not there", id, edge.Evidence)
			}
		}
	}
	if loaded.Metadata() != built.Metadata() {
		t.Fatalf("metadata\n got %+v\nwant %+v", loaded.Metadata(), built.Metadata())
	}
}

// TestInspectPublishedSnapshotDistinguishesAbsentFromUnusable is what makes the
// answer actionable. A generation without a snapshot is a generation a server
// derives, which is what always happened; one whose snapshot cannot be used is a
// store with something wrong in it. A single "not available" would report the two
// as the same thing, and the second one is the one worth waking up for.
func TestInspectPublishedSnapshotDistinguishesAbsentFromUnusable(t *testing.T) {
	t.Run("usable", func(t *testing.T) {
		directory := t.TempDir()
		databasePath := filepath.Join(directory, "graph.lbdb")
		built := seedPublishedGeneration(t, directory, databasePath)
		info, err := InspectPublishedSnapshot(directory)
		if err != nil {
			t.Fatalf("InspectPublishedSnapshot() error = %v", err)
		}
		if info.Symbols != int(built.Metadata().Counts.Symbols) || info.Symbols == 0 {
			t.Fatalf("symbols = %d, want %d", info.Symbols, built.Metadata().Counts.Symbols)
		}
		if info.Bytes == 0 || info.ID != built.Metadata().ID {
			t.Fatalf("info = %+v, want the published file's size and id", info)
		}
	})

	t.Run("absent", func(t *testing.T) {
		directory := t.TempDir()
		databasePath := filepath.Join(directory, "graph.lbdb")
		seedPublishedGeneration(t, directory, databasePath)
		remove(t, directory, PublishedSnapshotFileName)
		if _, err := InspectPublishedSnapshot(directory); !errors.Is(err, ErrNoPublishedSnapshot) {
			t.Fatalf("error = %v, want ErrNoPublishedSnapshot", err)
		}
	})

	// An older format is neither of the two the test above separates, and that
	// is the point: nothing is wrong with the store, the layout moved. A caller
	// that reported it as a defect would make every upgrade look like corruption.
	t.Run("written by an older format", func(t *testing.T) {
		directory := t.TempDir()
		databasePath := filepath.Join(directory, "graph.lbdb")
		seedPublishedGeneration(t, directory, databasePath)
		path := filepath.Join(directory, PublishedSnapshotFileName)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read snapshot: %v", err)
		}
		binary.LittleEndian.PutUint32(data[8:12], hotsnapshot.SnapshotFileFormatVersion-1)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}
		_, err = InspectPublishedSnapshot(directory)
		if !errors.Is(err, hotsnapshot.ErrSnapshotFileVersion) {
			t.Fatalf("error = %v, want ErrSnapshotFileVersion", err)
		}
		if errors.Is(err, ErrNoPublishedSnapshot) {
			t.Fatal("an older format was reported as an absent file")
		}
	})

	t.Run("unusable", func(t *testing.T) {
		directory := t.TempDir()
		databasePath := filepath.Join(directory, "graph.lbdb")
		seedPublishedGeneration(t, directory, databasePath)
		write(t, directory, snapshotFileName, strings.Repeat("cd", 32))
		_, err := InspectPublishedSnapshot(directory)
		if err == nil || errors.Is(err, ErrNoPublishedSnapshot) {
			t.Fatalf("error = %v, want a refusal that is not absence", err)
		}
	})
}
