package rebuild

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

var errInjected = errors.New("injected failure")

// TestRunFailureAtAnyStageLeavesCurrentGenerationUntouched is the LUQUE-1202
// contract stated once for every way a rebuild can die. Individual tests
// already cover the interesting stages one by one; this one fixes the general
// rule, so a stage added later without abort handling shows up here.
//
// The published generation, its database and the CURRENT pointer must be
// exactly what the first successful rebuild left behind, and the failed
// candidate must not exist on disk at all.
func TestRunFailureAtAnyStageLeavesCurrentGenerationUntouched(t *testing.T) {
	cases := map[string]struct {
		mutate    func(*Options)
		wantStage StageName
	}{
		"facts": {
			mutate: func(options *Options) {
				invalid := sampleFacts()
				invalid.Symbols[0].Key = ""
				options.Facts = invalid
			},
			wantStage: StageFacts,
		},
		"bulk load": {
			mutate: func(options *Options) {
				options.Load = func(context.Context, string, facts.Set, ladybug.CanonicalLoadOptions) (ladybug.LoadReport, error) {
					return ladybug.LoadReport{}, errInjected
				}
			},
			wantStage: StageBulkLoad,
		},
		"counts": {
			mutate: func(options *Options) {
				options.Counts = func(context.Context, string) (map[string]int64, error) {
					return nil, errInjected
				}
			},
			wantStage: StageIntegrity,
		},
		"integrity": {
			mutate: func(options *Options) {
				options.Integrity = func(context.Context, string) (ladybug.CanonicalIntegrityReport, error) {
					return ladybug.CanonicalIntegrityReport{}, errInjected
				}
			},
			wantStage: StageIntegrity,
		},
		"snapshot scan": {
			mutate: func(options *Options) {
				options.Scan = func(context.Context, string) (ladybug.CanonicalGraph, error) {
					return ladybug.CanonicalGraph{}, errInjected
				}
			},
			wantStage: StageSnapshot,
		},
		"golden probes": {
			mutate: func(options *Options) {
				options.Probes = func(context.Context, string, []ladybug.CanonicalProbe) ([]ladybug.CanonicalProbeResult, error) {
					return nil, errInjected
				}
			},
			wantStage: StageProbes,
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			seedGeneration(t, root)
			before := captureGeneration(t, root)

			failing := buildOptions(t, root, "000002", sampleFacts())
			test.mutate(&failing)

			report, err := Run(context.Background(), failing)
			testsupport.RequireSpaceOrSkip(t, err)
			if !errors.Is(err, ErrRebuildFailed) {
				t.Fatalf("Run() error = %v, want ErrRebuildFailed", err)
			}
			if report.Passed {
				t.Fatalf("Report.Passed = true, want false: %+v", report.Stages)
			}
			stage, found := stageByName(report.Stages, test.wantStage)
			if !found || stage.Passed {
				t.Fatalf("stage %q = %+v, want a recorded failure", test.wantStage, stage)
			}
			if report.Publication.Generation.ID != "" {
				t.Fatalf("Publication = %+v, want no publication", report.Publication)
			}

			after := captureGeneration(t, root)
			if before != after {
				t.Fatalf("active generation changed across a failed rebuild:\n%s\n%s", before, after)
			}
			if _, err := os.Stat(filepath.Join(root, "generations", "000002")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed candidate 000002 survived on disk: stat err = %v", err)
			}
		})
	}
}

// TestRunCancellationLeavesCurrentGenerationUntouched covers the operational
// case the injected errors above do not: the process is asked to stop while a
// rebuild is running.
func TestRunCancellationLeavesCurrentGenerationUntouched(t *testing.T) {
	root := t.TempDir()
	seedGeneration(t, root)
	before := captureGeneration(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	options := buildOptions(t, root, "000002", sampleFacts())
	options.Load = func(_ context.Context, path string, set facts.Set, loadOptions ladybug.CanonicalLoadOptions) (ladybug.LoadReport, error) {
		// Cancel exactly once the candidate database exists, so the run is
		// interrupted with a half-built generation on disk.
		report, err := fakeLoad(context.Background(), path, set, loadOptions)
		cancel()
		return report, err
	}

	if _, err := Run(ctx, options); !errors.Is(err, ErrRebuildFailed) {
		t.Fatalf("Run() error = %v, want ErrRebuildFailed", err)
	}
	if after := captureGeneration(t, root); before != after {
		t.Fatalf("active generation changed across a cancelled rebuild:\n%s\n%s", before, after)
	}
	if got := currentGenerationID(t, root); got != "000001" {
		t.Fatalf("CURRENT = %q, want 000001", got)
	}
}

// TestRepeatedFailuresDoNotErodeTheActiveGeneration guards the case an operator
// actually hits: a rebuild that keeps failing, run after run. The active
// generation must be as intact after the fifth failure as after the first.
func TestRepeatedFailuresDoNotErodeTheActiveGeneration(t *testing.T) {
	root := t.TempDir()
	seedGeneration(t, root)
	before := captureGeneration(t, root)

	for attempt := 2; attempt <= 6; attempt++ {
		options := buildOptions(t, root, generationIDFor(attempt), sampleFacts())
		options.Probes = func(context.Context, string, []ladybug.CanonicalProbe) ([]ladybug.CanonicalProbeResult, error) {
			return nil, errInjected
		}
		if _, err := Run(context.Background(), options); !errors.Is(err, ErrRebuildFailed) {
			t.Fatalf("attempt %d: Run() error = %v, want ErrRebuildFailed", attempt, err)
		}
	}

	if after := captureGeneration(t, root); before != after {
		t.Fatalf("active generation eroded after repeated failures:\n%s\n%s", before, after)
	}
}

// TestSuccessfulRebuildDoesChangeTheActiveGeneration is the control for the
// three tests above: captureGeneration must actually notice a publication.
// Without it, "unchanged" could be true because nothing is ever observed.
func TestSuccessfulRebuildDoesChangeTheActiveGeneration(t *testing.T) {
	root := t.TempDir()
	seedGeneration(t, root)
	before := captureGeneration(t, root)

	if _, err := Run(context.Background(), buildOptions(t, root, "000002", sampleFacts())); err != nil {
		testsupport.RequireSpaceOrSkip(t, err)
		t.Fatalf("second Run() error = %v", err)
	}
	if after := captureGeneration(t, root); after == before {
		t.Fatalf("captureGeneration did not observe a successful publication: %s", after)
	}
}

func generationIDFor(attempt int) string {
	return "00000" + string(rune('0'+attempt))
}

// captureGeneration renders everything a reader of the active graph depends
// on: which generation CURRENT names, and the bytes of every file inside it.
func captureGeneration(t *testing.T, root string) string {
	t.Helper()
	current := currentGenerationID(t, root)
	directory := filepath.Join(root, "generations", current)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", directory, err)
	}
	rendered := "CURRENT=" + current
	for _, entry := range entries {
		content, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", entry.Name(), err)
		}
		rendered += "\n" + entry.Name() + "=" + string(content)
	}
	return rendered
}
