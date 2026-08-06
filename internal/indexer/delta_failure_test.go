package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/rebuild"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
)

// TestUpdateNeverPublishesAfterAFailedStep walks every step of the delta route
// that can fail and states the same rule for all of them: an update that did
// not finish never becomes the graph readers see.
//
// The engine's own rollback is proved on real storage in
// delta_rollback_native_test.go. What this covers is the orchestration around
// it, which no transaction can undo.
func TestUpdateNeverPublishesAfterAFailedStep(t *testing.T) {
	previous := updateFixture(t)
	next := touchFileA(t, previous)
	stepFailure := errors.New("step failed")

	cases := map[string]struct {
		mutate     func(*UpdateOptions)
		wantApply  bool
		wantDigest bool
	}{
		"apply": {
			mutate: func(options *UpdateOptions) {
				options.ApplyDelta = func(context.Context, string, facts.Delta, ladybug.CanonicalLoadOptions) (ladybug.CanonicalMutationResult, error) {
					return ladybug.CanonicalMutationResult{}, stepFailure
				}
			},
		},
		"table counts": {
			mutate: func(options *UpdateOptions) {
				options.Counts = func(context.Context, string) (map[string]int64, error) {
					return nil, stepFailure
				}
			},
			wantApply: true,
		},
		"snapshot build": {
			mutate: func(options *UpdateOptions) {
				options.BuildSnapshot = func(context.Context, rebuild.BuildSnapshotOptions) (*hotsnapshot.GraphSnapshot, rebuild.SnapshotReport, error) {
					return nil, rebuild.SnapshotReport{}, stepFailure
				}
			},
			wantApply:  true,
			wantDigest: true,
		},
		"snapshot report did not pass": {
			mutate: func(options *UpdateOptions) {
				options.BuildSnapshot = func(context.Context, rebuild.BuildSnapshotOptions) (*hotsnapshot.GraphSnapshot, rebuild.SnapshotReport, error) {
					return nil, rebuild.SnapshotReport{Passed: false}, nil
				}
			},
			wantApply:  true,
			wantDigest: true,
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			layout := publishedLayout(t)
			served := servedSnapshot(t, 5)
			store := hotsnapshot.NewSnapshotStore(served)
			applied := false

			options := UpdateOptions{
				Root:          "/state",
				Plans:         []InvalidationPlan{{Class: ChangeBodyOnly, Actions: []InvalidationAction{ActionReindexFile}}},
				Previous:      previous,
				Next:          next,
				SnapshotID:    6,
				SnapshotStore: store,
				Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
					return layout, nil
				},
				ApplyDelta: func(context.Context, string, facts.Delta, ladybug.CanonicalLoadOptions) (ladybug.CanonicalMutationResult, error) {
					applied = true
					return ladybug.CanonicalMutationResult{}, nil
				},
				Counts: func(context.Context, string) (map[string]int64, error) {
					return map[string]int64{"Symbol": 2}, nil
				},
			}
			test.mutate(&options)

			result, err := Update(context.Background(), options)
			if !errors.Is(err, ErrUpdateFailed) {
				t.Fatalf("Update() error = %v, want ErrUpdateFailed", err)
			}
			if result.Passed {
				t.Fatalf("result = %#v, want a failed update", result)
			}
			if applied != test.wantApply {
				t.Fatalf("delta applied = %t, want %t", applied, test.wantApply)
			}
			if snapshot := store.Load(); snapshot != served {
				t.Fatalf("published snapshot changed after a failed update: %#v", snapshot.Metadata())
			}

			_, statErr := os.Stat(filepath.Join(layout.Active.Path, "snapshot.sha256"))
			if wrote := statErr == nil; wrote != test.wantDigest {
				t.Fatalf("digest written = %t, want %t (stat err = %v)", wrote, test.wantDigest, statErr)
			}
		})
	}
}

// TestUpdateLeavesAStaleDigestWhenTheMutationOutlivesTheUpdate documents the one
// window a transaction cannot close. Once the engine commits, the database is
// mutated; a later failure cannot undo it. If the digest refresh is what failed,
// the generation directory keeps the digest of the graph it held before.
//
// That is a real inconsistency, and it fails closed: rollback revalidates a
// destination by recomputing the digest from the database, so a generation
// whose recorded digest no longer matches its content is refused rather than
// silently restored.
func TestUpdateLeavesAStaleDigestWhenTheMutationOutlivesTheUpdate(t *testing.T) {
	previous := updateFixture(t)
	next := touchFileA(t, previous)
	layout := publishedLayout(t)

	digestPath := filepath.Join(layout.Active.Path, "snapshot.sha256")
	if err := os.WriteFile(digestPath, []byte("digest-of-the-previous-content\n"), 0o600); err != nil {
		t.Fatalf("seed digest: %v", err)
	}

	_, err := Update(context.Background(), UpdateOptions{
		Root:     "/state",
		Plans:    []InvalidationPlan{{Class: ChangeBodyOnly, Actions: []InvalidationAction{ActionReindexFile}}},
		Previous: previous,
		Next:     next,
		Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
			return layout, nil
		},
		ApplyDelta: func(context.Context, string, facts.Delta, ladybug.CanonicalLoadOptions) (ladybug.CanonicalMutationResult, error) {
			return ladybug.CanonicalMutationResult{}, nil
		},
		Counts: func(context.Context, string) (map[string]int64, error) {
			return nil, errors.New("counts unavailable")
		},
	})
	if !errors.Is(err, ErrUpdateFailed) {
		t.Fatalf("Update() error = %v, want ErrUpdateFailed", err)
	}

	content, readErr := os.ReadFile(digestPath)
	if readErr != nil {
		t.Fatalf("read digest: %v", readErr)
	}
	if string(content) != "digest-of-the-previous-content\n" {
		t.Fatalf("digest = %q, want the previous digest left untouched", content)
	}
}

// TestUpdateHonoursCancellationBeforeTouchingTheGraph keeps the cheapest
// guarantee explicit: a cancelled update must not open a transaction at all.
func TestUpdateHonoursCancellationBeforeTouchingTheGraph(t *testing.T) {
	previous := updateFixture(t)
	next := touchFileA(t, previous)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	applied := false
	_, err := Update(ctx, UpdateOptions{
		Root:     "/state",
		Previous: previous,
		Next:     next,
		Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
			return publishedLayout(t), nil
		},
		ApplyDelta: func(context.Context, string, facts.Delta, ladybug.CanonicalLoadOptions) (ladybug.CanonicalMutationResult, error) {
			applied = true
			return ladybug.CanonicalMutationResult{}, nil
		},
	})
	if !errors.Is(err, ErrUpdateFailed) {
		t.Fatalf("Update() error = %v, want ErrUpdateFailed", err)
	}
	if applied {
		t.Fatal("a cancelled update applied a delta")
	}
}

func servedSnapshot(t *testing.T, id uint64) *hotsnapshot.GraphSnapshot {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repo-served", Name: "served", Languages: "go"}},
		Packages:     []hotsnapshot.PackageRow{{Key: "pkg-served", RepositoryKey: "repo-served", Language: "go", Name: "served", ModulePath: "example.com/served"}},
		Files:        []hotsnapshot.FileRow{{Key: "file-served", RepositoryKey: "repo-served", PackageKey: "pkg-served", Path: "served.go", Language: "go"}},
		Symbols: []hotsnapshot.SymbolRow{{
			StableKey: "sym-served", CanonicalIdentity: "go:served.Served", FileKey: "file-served",
			Language: "go", Name: "Served", QualifiedName: "served.Served", Kind: "func",
		}},
	}, id, time.Unix(int64(id), 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return snapshot
}
