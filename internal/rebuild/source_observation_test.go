package rebuild

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/sourceobservation"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/topology"
)

func TestRunWritesSourceObservationsIntoThePublishedGeneration(t *testing.T) {
	root := testsupport.TempDir(t)
	options := buildOptions(t, root, "000001", sampleFacts())
	manifest := rebuildSourceManifest(t)
	options.SourceManifest = &manifest
	options.VerifySources = func(context.Context) error { return nil }

	report, err := Run(context.Background(), options)
	if err != nil {
		testsupport.RequireSpaceOrSkip(t, err)
		t.Fatal(err)
	}
	stored, err := sourceobservation.Read(report.Publication.Generation.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceobservation.Compare(manifest, stored); err != nil {
		t.Fatalf("published source observations profile=%q generation=%q: %v", manifest.Profile, options.GenerationID, err)
	}
}

func TestRunDoesNotReplaceTheCurrentGenerationWhenSourcesMove(t *testing.T) {
	root := testsupport.TempDir(t)
	seedGeneration(t, root)
	options := buildOptions(t, root, "000002", sampleFacts())
	manifest := rebuildSourceManifest(t)
	options.SourceManifest = &manifest
	options.VerifySources = func(context.Context) error {
		return sourceobservation.ErrChanged
	}

	if _, err := Run(context.Background(), options); !errors.Is(err, sourceobservation.ErrChanged) {
		t.Fatalf("Run() error = %v, want source change rejection", err)
	}
	store, err := generation.New(root, generation.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != "000001" {
		t.Fatalf("current generation = %q, want prior valid generation", current.ID)
	}
}

func TestRunRejectsASourceVerifierWithoutAPersistedManifest(t *testing.T) {
	options := buildOptions(t, testsupport.TempDir(t), "000001", sampleFacts())
	options.VerifySources = func(context.Context) error { return nil }
	if _, err := Run(context.Background(), options); err == nil || !strings.Contains(err.Error(), "source verifier requires") {
		t.Fatalf("Run() error = %v, want incomplete source request refusal", err)
	}
}

func rebuildSourceManifest(t *testing.T) sourceobservation.Manifest {
	t.Helper()
	observation, err := topology.NewSourceObservation("source-main", "0123456789abcdef", "main", false,
		strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	return sourceobservation.Manifest{
		Version:             sourceobservation.CurrentVersion,
		Profile:             "default",
		ResolverVersion:     "resolver-test-1",
		AnalyzerFingerprint: "analyzer-test-1",
		Sources: []sourceobservation.Source{{
			Repository:  "source",
			Observation: observation,
		}},
	}
}
