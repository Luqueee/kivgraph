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
	"github.com/Luqueee/ladygraph/internal/storage/generation"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
)

// updateFixture is a small Validate-passing fact set: one repository, one
// package and two files, each defining one symbol.
func updateFixture(t *testing.T) facts.Set {
	t.Helper()
	repositoryKey := facts.RepositoryKey("acme/widgets")
	packageKey := facts.PackageKey(facts.LanguageGo, repositoryKey, "widgets")
	fileA := facts.FileKey(repositoryKey, "a.go")
	fileB := facts.FileKey(repositoryKey, "b.go")
	symbolA := "symbol:go:acme/widgets.A"
	symbolB := "symbol:go:acme/widgets.B"

	set := facts.Set{
		Repositories: []facts.Repository{{
			Key: repositoryKey, Name: "acme/widgets", RootPath: "/repos/widgets",
			Languages: []facts.Language{facts.LanguageGo},
		}},
		Packages: []facts.Package{{
			Key: packageKey, RepositoryKey: repositoryKey, Language: facts.LanguageGo,
			Name: "widgets", RootPath: "/repos/widgets",
		}},
		Files: []facts.File{
			{Key: fileA, RepositoryKey: repositoryKey, PackageKey: packageKey, Path: "a.go", Language: facts.LanguageGo, ContentHash: "hash-a-1"},
			{Key: fileB, RepositoryKey: repositoryKey, PackageKey: packageKey, Path: "b.go", Language: facts.LanguageGo, ContentHash: "hash-b-1"},
		},
		Symbols: []facts.Symbol{
			{
				Key: symbolA, CanonicalIdentity: "go:acme/widgets.A", RepositoryKey: repositoryKey, PackageKey: packageKey,
				FileKey: fileA, Language: facts.LanguageGo, Name: "A", QualifiedName: "widgets.A", Kind: "func",
				Exported: true, Start: facts.Position{Line: 1, Offset: 0}, End: facts.Position{Line: 3, Offset: 40},
			},
			{
				Key: symbolB, CanonicalIdentity: "go:acme/widgets.B", RepositoryKey: repositoryKey, PackageKey: packageKey,
				FileKey: fileB, Language: facts.LanguageGo, Name: "B", QualifiedName: "widgets.B", Kind: "func",
				Exported: true, Start: facts.Position{Line: 1, Offset: 0}, End: facts.Position{Line: 3, Offset: 40},
			},
		},
		Edges: []facts.Edge{
			{Kind: facts.ContainsPackage, SourceKey: repositoryKey, TargetKey: packageKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.ContainsFile, SourceKey: packageKey, TargetKey: fileA, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.ContainsFile, SourceKey: packageKey, TargetKey: fileB, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.Defines, SourceKey: fileA, TargetKey: symbolA, Confidence: facts.StructuralCertain, Provenance: facts.GoTypesDefinition},
			{Kind: facts.Defines, SourceKey: fileB, TargetKey: symbolB, Confidence: facts.StructuralCertain, Provenance: facts.GoTypesDefinition},
		},
	}
	set.Sort()
	if err := set.Validate(); err != nil {
		t.Fatalf("fixture is invalid: %v", err)
	}
	return set
}

// touchFileA returns the fixture with a.go's content and symbol span
// changed, so exactly one file differs.
func touchFileA(t *testing.T, set facts.Set) facts.Set {
	t.Helper()
	changed := facts.Set{
		Repositories: append([]facts.Repository(nil), set.Repositories...),
		Packages:     append([]facts.Package(nil), set.Packages...),
		Files:        append([]facts.File(nil), set.Files...),
		Symbols:      append([]facts.Symbol(nil), set.Symbols...),
		Evidence:     append([]facts.Evidence(nil), set.Evidence...),
		Edges:        append([]facts.Edge(nil), set.Edges...),
		Unresolved:   append([]facts.UnresolvedReference(nil), set.Unresolved...),
	}
	fileA := facts.FileKey(facts.RepositoryKey("acme/widgets"), "a.go")
	for index := range changed.Files {
		if changed.Files[index].Key == fileA {
			changed.Files[index].ContentHash = "hash-a-2"
		}
	}
	for index := range changed.Symbols {
		if changed.Symbols[index].FileKey == fileA {
			changed.Symbols[index].End = facts.Position{Line: 4, Offset: 55}
		}
	}
	if err := changed.Validate(); err != nil {
		t.Fatalf("changed fixture is invalid: %v", err)
	}
	return changed
}

func publishedLayout(t *testing.T) rebuild.Layout {
	t.Helper()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "graph.db")
	if err := os.WriteFile(databasePath, []byte("db"), 0o600); err != nil {
		t.Fatalf("seed database: %v", err)
	}
	return rebuild.Layout{
		Active: generation.Generation{ID: "000001", Path: directory, DatabasePath: databasePath},
		NextID: "000002",
	}
}

func TestDecideRoutesByScopeAndRatio(t *testing.T) {
	delta := facts.Delta{ReplacedFiles: []string{"file:a"}}
	empty := facts.Delta{}

	if decision := Decide(nil, empty, true, 10, 0); decision.Route != RouteNoop {
		t.Fatalf("empty delta decision = %#v, want NOOP", decision)
	}
	if decision := Decide(nil, delta, true, 10, 0); decision.Route != RouteDelta {
		t.Fatalf("small delta decision = %#v, want DELTA", decision)
	}
	if decision := Decide(nil, delta, false, 10, 0); decision.Route != RouteRepublish {
		t.Fatalf("no active generation decision = %#v, want REPUBLISH", decision)
	}
	if decision := Decide(nil, delta, true, 1, 0); decision.Route != RouteRepublish {
		t.Fatalf("above-ratio decision = %#v, want REPUBLISH", decision)
	}

	// A registry rebuild or a project reindex cannot be expressed as a
	// per-file delta, so it forces a republish even when the delta itself
	// is empty and even when the ratio would allow one.
	forcing := []InvalidationPlan{
		{Actions: []InvalidationAction{ActionReindexFile}},
		{Actions: []InvalidationAction{ActionRebuildRegistry, ActionReindexProject}},
	}
	decision := Decide(forcing, empty, true, 1000, 0)
	if decision.Route != RouteRepublish {
		t.Fatalf("forced decision = %#v, want REPUBLISH", decision)
	}
	if len(decision.ForcedBy) != 2 || decision.ForcedBy[0] != ActionRebuildRegistry || decision.ForcedBy[1] != ActionReindexProject {
		t.Fatalf("forced actions = %v, want registry then project", decision.ForcedBy)
	}
}

func TestUpdateAppliesDeltaAndRefreshesDigest(t *testing.T) {
	previous := updateFixture(t)
	next := touchFileA(t, previous)
	layout := publishedLayout(t)

	var appliedPath string
	var applied facts.Delta
	result, err := Update(context.Background(), UpdateOptions{
		Root:            "/state",
		Plans:           []InvalidationPlan{{Class: ChangeBodyOnly, Actions: []InvalidationAction{ActionReindexFile}}},
		Previous:        previous,
		Next:            next,
		SnapshotID:      7,
		ResolverVersion: "test",
		Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
			return layout, nil
		},
		ApplyDelta: func(_ context.Context, path string, delta facts.Delta, options ladybug.CanonicalLoadOptions) (ladybug.CanonicalMutationResult, error) {
			appliedPath = path
			applied = delta
			if options.SnapshotID != 7 || options.ResolverVersion != "test" {
				t.Fatalf("load options = %#v, want the update's provenance", options)
			}
			return ladybug.CanonicalMutationResult{RemovedSymbols: 1, UpsertedNodes: 2}, nil
		},
		Counts: func(context.Context, string) (map[string]int64, error) {
			return map[string]int64{"Symbol": 2, "File": 2}, nil
		},
		Republish: func(context.Context, rebuild.Options) (rebuild.Report, error) {
			t.Fatal("republish must not run on the delta route")
			return rebuild.Report{}, nil
		},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if result.Decision.Route != RouteDelta || !result.Passed {
		t.Fatalf("result = %#v, want a passing DELTA route", result)
	}
	if appliedPath != layout.Active.DatabasePath {
		t.Fatalf("applied to %q, want the active database %q", appliedPath, layout.Active.DatabasePath)
	}
	fileA := facts.FileKey(facts.RepositoryKey("acme/widgets"), "a.go")
	if len(applied.ReplacedFiles) != 1 || applied.ReplacedFiles[0] != fileA {
		t.Fatalf("replaced files = %v, want only %q", applied.ReplacedFiles, fileA)
	}
	if len(applied.RemovedFiles) != 0 {
		t.Fatalf("removed files = %v, want none", applied.RemovedFiles)
	}
	if result.Mutation.UpsertedNodes != 2 {
		t.Fatalf("mutation = %#v, want the applier's counts", result.Mutation)
	}
	if result.Generation.ID != layout.Active.ID {
		t.Fatalf("serving generation = %#v, want the mutated active one", result.Generation)
	}

	// The digest must be rewritten from the mutated counts, or a later
	// rollback to this generation would fail its revalidation.
	recorded, err := os.ReadFile(filepath.Join(layout.Active.Path, "snapshot.sha256"))
	if err != nil {
		t.Fatalf("read refreshed digest: %v", err)
	}
	if got, want := string(recorded), result.SnapshotDigest+"\n"; got != want {
		t.Fatalf("recorded digest = %q, want %q", got, want)
	}
	expected, err := rebuild.RefreshSnapshotDigest(t.TempDir(), map[string]int64{"Symbol": 2, "File": 2})
	if err != nil {
		t.Fatalf("recompute digest: %v", err)
	}
	if result.SnapshotDigest != expected {
		t.Fatalf("digest = %q, want the rebuild calculation %q", result.SnapshotDigest, expected)
	}
}
func testHotSnapshot(t *testing.T, id uint64) *hotsnapshot.GraphSnapshot {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{}, id, time.Unix(int64(id), 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot(%d): %v", id, err)
	}
	return snapshot
}

func TestUpdateRebuildsAndPublishesHotSnapshotAfterDelta(t *testing.T) {
	previous := updateFixture(t)
	next := touchFileA(t, previous)
	layout := publishedLayout(t)
	store := hotsnapshot.NewSnapshotStore(testHotSnapshot(t, 1))
	var builtPath string
	var builtID uint64

	result, err := Update(context.Background(), UpdateOptions{
		Root: "/state", Plans: []InvalidationPlan{{Actions: []InvalidationAction{ActionReindexFile}}},
		Previous: previous, Next: next, SnapshotID: 2, SnapshotStore: store,
		Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) { return layout, nil },
		ApplyDelta: func(context.Context, string, facts.Delta, ladybug.CanonicalLoadOptions) (ladybug.CanonicalMutationResult, error) {
			return ladybug.CanonicalMutationResult{}, nil
		},
		Counts: func(context.Context, string) (map[string]int64, error) {
			return map[string]int64{"File": 2}, nil
		},
		BuildSnapshot: func(_ context.Context, options rebuild.BuildSnapshotOptions) (*hotsnapshot.GraphSnapshot, rebuild.SnapshotReport, error) {
			builtPath, builtID = options.DatabasePath, options.SnapshotID
			return testHotSnapshot(t, options.SnapshotID), rebuild.SnapshotReport{SnapshotID: options.SnapshotID, Passed: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if result.Decision.Route != RouteDelta || !result.Passed {
		t.Fatalf("result = %#v, want passing DELTA", result)
	}
	if builtPath != layout.Active.DatabasePath || builtID != 2 {
		t.Fatalf("build = path %q id %d, want %q id 2", builtPath, builtID, layout.Active.DatabasePath)
	}
	if got := store.Load(); got == nil || got.Metadata().ID != 2 {
		t.Fatalf("published snapshot = %#v, want generation 2", got)
	}
	if result.Snapshot.Report.SnapshotID != 2 {
		t.Fatalf("snapshot report = %#v, want id 2", result.Snapshot)
	}
}

func TestUpdateKeepsPreviousHotSnapshotWhenRebuildFails(t *testing.T) {
	previous := updateFixture(t)
	next := touchFileA(t, previous)
	layout := publishedLayout(t)
	initial := testHotSnapshot(t, 1)
	store := hotsnapshot.NewSnapshotStore(initial)
	buildFailure := errors.New("invalid canonical graph")

	result, err := Update(context.Background(), UpdateOptions{
		Root: "/state", Plans: []InvalidationPlan{{Actions: []InvalidationAction{ActionReindexFile}}},
		Previous: previous, Next: next, SnapshotID: 2, SnapshotStore: store,
		Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) { return layout, nil },
		ApplyDelta: func(context.Context, string, facts.Delta, ladybug.CanonicalLoadOptions) (ladybug.CanonicalMutationResult, error) {
			return ladybug.CanonicalMutationResult{}, nil
		},
		Counts: func(context.Context, string) (map[string]int64, error) { return map[string]int64{"File": 2}, nil },
		BuildSnapshot: func(context.Context, rebuild.BuildSnapshotOptions) (*hotsnapshot.GraphSnapshot, rebuild.SnapshotReport, error) {
			return nil, rebuild.SnapshotReport{}, buildFailure
		},
	})
	if !errors.Is(err, ErrUpdateFailed) || !errors.Is(err, buildFailure) {
		t.Fatalf("Update() error = %v, want wrapped snapshot build failure", err)
	}
	if result.Passed {
		t.Fatalf("result = %#v, want failed update", result)
	}
	if got := store.Load(); got != initial {
		t.Fatalf("active snapshot = %p, want previous %p", got, initial)
	}
}
func TestUpdateRejectsStaleHotSnapshotGenerationWithoutReplacingReader(t *testing.T) {
	previous := updateFixture(t)
	next := touchFileA(t, previous)
	layout := publishedLayout(t)
	initial := testHotSnapshot(t, 3)
	store := hotsnapshot.NewSnapshotStore(initial)

	result, err := Update(context.Background(), UpdateOptions{
		Root: "/state", Plans: []InvalidationPlan{{Actions: []InvalidationAction{ActionReindexFile}}},
		Previous: previous, Next: next, SnapshotID: 3, SnapshotStore: store,
		Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) { return layout, nil },
		ApplyDelta: func(context.Context, string, facts.Delta, ladybug.CanonicalLoadOptions) (ladybug.CanonicalMutationResult, error) {
			return ladybug.CanonicalMutationResult{}, nil
		},
		Counts: func(context.Context, string) (map[string]int64, error) { return map[string]int64{"File": 2}, nil },
		BuildSnapshot: func(_ context.Context, options rebuild.BuildSnapshotOptions) (*hotsnapshot.GraphSnapshot, rebuild.SnapshotReport, error) {
			return testHotSnapshot(t, options.SnapshotID), rebuild.SnapshotReport{SnapshotID: options.SnapshotID, Passed: true}, nil
		},
	})
	if !errors.Is(err, ErrUpdateFailed) || !errors.Is(err, hotsnapshot.ErrSnapshotGeneration) {
		t.Fatalf("Update() error = %v, want stale-generation failure", err)
	}
	if result.Passed {
		t.Fatalf("result = %#v, want failed update", result)
	}
	if got := store.Load(); got != initial {
		t.Fatalf("active snapshot = %p, want previous %p", got, initial)
	}
}

func TestUpdateRepublishesWhenScopeExceedsAFileDelta(t *testing.T) {
	previous := updateFixture(t)
	next := touchFileA(t, previous)
	layout := publishedLayout(t)

	var republished rebuild.Options
	result, err := Update(context.Background(), UpdateOptions{
		Root:            "/state",
		Plans:           []InvalidationPlan{{Class: ChangeManifestChanged, Actions: []InvalidationAction{ActionRebuildRegistry, ActionReindexProject}}},
		Previous:        previous,
		Next:            next,
		ResolverVersion: "test",
		SnapshotID:      9,
		Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
			return layout, nil
		},
		ApplyDelta: func(context.Context, string, facts.Delta, ladybug.CanonicalLoadOptions) (ladybug.CanonicalMutationResult, error) {
			t.Fatal("a forced republish must never mutate graph.active")
			return ladybug.CanonicalMutationResult{}, nil
		},
		Republish: func(_ context.Context, options rebuild.Options) (rebuild.Report, error) {
			republished = options
			return rebuild.Report{
				GenerationID: options.GenerationID,
				Publication: generation.Publication{
					Generation: generation.Generation{ID: options.GenerationID},
					PreviousID: layout.Active.ID,
				},
				Passed: true,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if result.Decision.Route != RouteRepublish || !result.Passed {
		t.Fatalf("result = %#v, want a passing REPUBLISH route", result)
	}
	if republished.GenerationID != layout.NextID {
		t.Fatalf("republished generation = %q, want graph.next id %q", republished.GenerationID, layout.NextID)
	}
	if len(republished.Facts.Files) != len(next.Files) {
		t.Fatalf("republished facts = %d file(s), want the whole next state (%d)", len(republished.Facts.Files), len(next.Files))
	}
	if result.Generation.ID != layout.NextID {
		t.Fatalf("serving generation = %#v, want the newly published one", result.Generation)
	}
	// snapshot.sha256 belongs to the publish pipeline on this route; the
	// active generation must be left exactly as it was.
	if _, err := os.Stat(filepath.Join(layout.Active.Path, "snapshot.sha256")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat active digest = %v, want it untouched", err)
	}
}

func TestUpdateWithoutChangesTouchesNothing(t *testing.T) {
	previous := updateFixture(t)
	layout := publishedLayout(t)

	result, err := Update(context.Background(), UpdateOptions{
		Root:     "/state",
		Previous: previous,
		Next:     previous,
		Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
			return layout, nil
		},
		ApplyDelta: func(context.Context, string, facts.Delta, ladybug.CanonicalLoadOptions) (ladybug.CanonicalMutationResult, error) {
			t.Fatal("an empty delta must not open a transaction")
			return ladybug.CanonicalMutationResult{}, nil
		},
		Republish: func(context.Context, rebuild.Options) (rebuild.Report, error) {
			t.Fatal("an empty delta must not republish")
			return rebuild.Report{}, nil
		},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if result.Decision.Route != RouteNoop || !result.Passed {
		t.Fatalf("result = %#v, want a passing NOOP route", result)
	}
	if result.Generation.ID != layout.Active.ID {
		t.Fatalf("serving generation = %#v, want the untouched active one", result.Generation)
	}
}

func TestUpdateReportsFailuresWithoutClaimingSuccess(t *testing.T) {
	previous := updateFixture(t)
	next := touchFileA(t, previous)
	layout := publishedLayout(t)
	applyFailure := errors.New("engine refused the delta")

	result, err := Update(context.Background(), UpdateOptions{
		Root:     "/state",
		Previous: previous,
		Next:     next,
		Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
			return layout, nil
		},
		ApplyDelta: func(context.Context, string, facts.Delta, ladybug.CanonicalLoadOptions) (ladybug.CanonicalMutationResult, error) {
			return ladybug.CanonicalMutationResult{}, applyFailure
		},
	})
	if !errors.Is(err, ErrUpdateFailed) || !errors.Is(err, applyFailure) {
		t.Fatalf("Update() error = %v, want ErrUpdateFailed wrapping the engine failure", err)
	}
	if result.Passed {
		t.Fatalf("result = %#v, want a failed update", result)
	}
	if _, statErr := os.Stat(filepath.Join(layout.Active.Path, "snapshot.sha256")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stat digest = %v, want no digest written for a failed delta", statErr)
	}

	// A republish that reports a failed run is a failure too, even without
	// an error: a report that did not pass never becomes the serving graph.
	result, err = Update(context.Background(), UpdateOptions{
		Root:     "/state",
		Plans:    []InvalidationPlan{{Actions: []InvalidationAction{ActionReindexProject}}},
		Previous: previous,
		Next:     next,
		Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
			return layout, nil
		},
		Republish: func(context.Context, rebuild.Options) (rebuild.Report, error) {
			return rebuild.Report{Passed: false}, nil
		},
	})
	if !errors.Is(err, ErrUpdateFailed) {
		t.Fatalf("Update() error = %v, want ErrUpdateFailed", err)
	}
	if result.Passed || result.Generation.ID != "" {
		t.Fatalf("result = %#v, want no serving generation", result)
	}
}
