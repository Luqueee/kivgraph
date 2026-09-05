package indexing

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/topology"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestObserveSourcesRejectsACompositionForAnotherPath(t *testing.T) {
	options, composition := topologyObservationOptions(t)
	composition.Worktrees[0].Path = filepath.Join(composition.Worktrees[0].Path, "other")
	options.Composition = &composition

	if _, _, err := ObserveSources(context.Background(), options); err == nil || !strings.Contains(err.Error(), "selects path") {
		t.Fatalf("ObserveSources(profile=%q composition=%#v repositories=%#v) error = %v, want an observed-path refusal", options.Profile, composition, options.Repositories, err)
	}
}

func TestObservedTopologyCompositionRejectsAnUnobservedSource(t *testing.T) {
	options, _ := topologyObservationOptions(t)
	manifest, _, err := ObserveSources(context.Background(), options)
	if err != nil {
		t.Fatalf("ObserveSources(profile=%q repositories=%#v) error = %v", options.Profile, options.Repositories, err)
	}
	manifest.Composition = nil

	if _, err := observedTopologyComposition(manifest, nil); err == nil || !strings.Contains(err.Error(), "was not observed") {
		t.Fatalf("observedTopologyComposition(manifest=%#v observed=nil) error = %v, want an unobserved-source refusal", manifest, err)
	}
}

func TestValidateObservedCompositionNormalizesRepositoryNames(t *testing.T) {
	options, composition := topologyObservationOptions(t)
	manifest, observed, err := ObserveSources(context.Background(), options)
	if err != nil {
		t.Fatalf("ObserveSources(profile=%q repositories=%#v) error = %v", options.Profile, options.Repositories, err)
	}
	observed[0].Name = " source "

	if err := validateObservedComposition(composition, manifest, observed); err != nil {
		t.Fatalf("validateObservedComposition(composition=%#v observed=%#v) error = %v, want normalized source name", composition, observed, err)
	}
}

func TestObserveSourcesDerivesCompositionForPlainRegistry(t *testing.T) {
	options, _ := topologyObservationOptions(t)
	options.Composition = nil
	options.Repositories[0].Worktree = ""

	manifest, observed, err := ObserveSources(context.Background(), options)
	if err != nil {
		t.Fatalf("ObserveSources(profile=%q repositories=%#v) error = %v", options.Profile, options.Repositories, err)
	}
	if len(observed) != 1 || manifest.Composition == nil {
		t.Fatalf("observed repositories/manifest composition = %#v / %#v, want one repository and a composition", observed, manifest.Composition)
	}
	got, err := manifest.Composition.ProfileComposition()
	if err != nil {
		t.Fatal(err)
	}
	want := topology.ProfileComposition{
		Profile:      topology.Profile{ID: "default", Worktrees: []topology.WorktreeSelection{{Repository: "source", Worktree: "legacy:source"}}},
		Repositories: []topology.LogicalRepository{{ID: "source", Name: "source"}},
		Worktrees:    []topology.Worktree{{ID: "legacy:source", Repository: "source", Path: options.Repositories[0].Path}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("derived plain-registry topology composition = %#v, want %#v", got, want)
	}
}

func TestObserveSourcesPersistsTheEffectiveTopologyComposition(t *testing.T) {
	options, composition := topologyObservationOptions(t)
	want := cloneProfileComposition(composition)
	manifest, observed, err := ObserveSources(context.Background(), options)
	if err != nil {
		t.Fatalf("ObserveSources(profile=%q repositories=%#v composition=%#v) error = %v", options.Profile, options.Repositories, options.Composition, err)
	}
	if len(observed) != 1 || manifest.Composition == nil {
		t.Fatalf("observed repositories/manifest composition = %#v / %#v, want one repository and a composition", observed, manifest.Composition)
	}
	got, err := manifest.Composition.ProfileComposition()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("observed topology composition = %#v, want %#v", got, want)
	}
}

func cloneProfileComposition(value topology.ProfileComposition) topology.ProfileComposition {
	value.Repositories = append([]topology.LogicalRepository(nil), value.Repositories...)
	value.Worktrees = append([]topology.Worktree(nil), value.Worktrees...)
	value.Profile.Worktrees = append([]topology.WorktreeSelection(nil), value.Profile.Worktrees...)
	return value
}

func topologyObservationOptions(t *testing.T) (FullOptions, topology.ProfileComposition) {
	t.Helper()
	root := testsupport.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	initializeObservationSource(t, root)
	composition := topology.ProfileComposition{
		Profile:      topology.Profile{ID: "default", Worktrees: []topology.WorktreeSelection{{Repository: "source", Worktree: "source-main"}}},
		Repositories: []topology.LogicalRepository{{ID: "source", Name: "Source"}},
		Worktrees:    []topology.Worktree{{ID: "source-main", Repository: "source", Path: root}},
	}
	options := FullOptions{
		Profile:         "default",
		ResolverVersion: "resolver-test",
		Composition:     &composition,
		Repositories: []workspace.Repository{{
			Name: "source", Worktree: "source-main",
			Path: root, RealPath: root, Languages: []string{"go"},
		}},
	}
	return options, composition
}

func initializeObservationSource(t *testing.T, root string) {
	t.Helper()
	for _, arguments := range [][]string{
		{"init", "-q", root},
		{"-C", root, "add", "source.go"},
		{"-C", root, "-c", "user.email=source@example.test", "-c", "user.name=Source Fixture", "commit", "-qm", "initial"},
	} {
		command := exec.Command("git", arguments...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
}
