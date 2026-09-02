package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/topology"
)

func TestNewComposedRegistryRejectsMalformedCompositionBeforeReadingProviders(t *testing.T) {
	root := testsupport.TempDir(t)
	source := config.RepositoriesFile{
		Version: config.CurrentSchemaVersion,
		Repositories: []config.Repository{{
			Name: "frontend", Path: root, Languages: []string{"go"},
		}},
	}
	tests := []struct {
		name        string
		composition topology.ProfileComposition
		want        string
		class       error
	}{
		{
			name: "count mismatch",
			composition: topology.ProfileComposition{
				Profile:      topology.Profile{ID: "feature"},
				Repositories: []topology.LogicalRepository{{ID: "frontend"}},
			},
			want:  "repository/worktree count differs",
			class: topology.ErrInvalidTopology,
		},
		{
			name: "worktree belongs to another repository",
			composition: topology.ProfileComposition{
				Profile: topology.Profile{
					ID:        "feature",
					Worktrees: []topology.WorktreeSelection{{Repository: "frontend", Worktree: "frontend"}},
				},
				Repositories: []topology.LogicalRepository{{ID: "frontend"}},
				Worktrees:    []topology.Worktree{{ID: "frontend", Repository: "backend", Path: root}},
			},
			want:  "belongs to",
			class: topology.ErrInvalidTopology,
		},
		{
			name: "selection count mismatch",
			composition: topology.ProfileComposition{
				Profile:      topology.Profile{ID: "feature"},
				Repositories: []topology.LogicalRepository{{ID: "frontend"}},
				Worktrees:    []topology.Worktree{{ID: "frontend", Repository: "frontend", Path: root}},
			},
			want:  "selection/worktree count differs",
			class: topology.ErrInvalidTopology,
		},
		{
			name: "selection does not match effective registry",
			composition: topology.ProfileComposition{
				Profile: topology.Profile{
					ID:        "feature",
					Worktrees: []topology.WorktreeSelection{{Repository: "backend", Worktree: "frontend"}},
				},
				Repositories: []topology.LogicalRepository{{ID: "frontend"}},
				Worktrees:    []topology.Worktree{{ID: "frontend", Repository: "frontend", Path: root}},
			},
			want:  "does not match",
			class: topology.ErrInvalidTopology,
		},
		{
			name: "empty worktree path",
			composition: topology.ProfileComposition{
				Profile: topology.Profile{
					ID:        "feature",
					Worktrees: []topology.WorktreeSelection{{Repository: "frontend", Worktree: "frontend"}},
				},
				Repositories: []topology.LogicalRepository{{ID: "frontend"}},
				Worktrees:    []topology.Worktree{{ID: "frontend", Repository: "frontend"}},
			},
			want:  "path",
			class: topology.ErrInvalidTopology,
		},
		{
			name: "invalid profile identity",
			composition: topology.ProfileComposition{
				Profile: topology.Profile{ID: "feature/login"},
			},
			want:  "composition profile",
			class: topology.ErrInvalidID,
		},
		{
			name: "invalid repository identity",
			composition: topology.ProfileComposition{
				Profile: topology.Profile{
					ID:        "feature",
					Worktrees: []topology.WorktreeSelection{{Repository: "/bad-repository", Worktree: "frontend"}},
				},
				Repositories: []topology.LogicalRepository{{ID: "/bad-repository"}},
				Worktrees:    []topology.Worktree{{ID: "frontend", Repository: "/bad-repository", Path: root}},
			},
			want:  "composition repositories[0].id",
			class: topology.ErrInvalidID,
		},
		{
			name: "duplicate repository identity",
			composition: topology.ProfileComposition{
				Profile: topology.Profile{
					ID: "feature",
					Worktrees: []topology.WorktreeSelection{
						{Repository: "frontend", Worktree: "frontend-a"},
						{Repository: "frontend", Worktree: "frontend-b"},
					},
				},
				Repositories: []topology.LogicalRepository{
					{ID: "frontend"}, {ID: "frontend"},
				},
				Worktrees: []topology.Worktree{
					{ID: "frontend-a", Repository: "frontend", Path: root},
					{ID: "frontend-b", Repository: "frontend", Path: root},
				},
			},
			want:  "duplicate of repositories[0]",
			class: topology.ErrInvalidTopology,
		},
		{
			name: "invalid worktree identity",
			composition: topology.ProfileComposition{
				Profile: topology.Profile{
					ID:        "feature",
					Worktrees: []topology.WorktreeSelection{{Repository: "frontend", Worktree: "bad/worktree"}},
				},
				Repositories: []topology.LogicalRepository{{ID: "frontend"}},
				Worktrees:    []topology.Worktree{{ID: "bad/worktree", Repository: "frontend", Path: root}},
			},
			want:  "composition worktrees[0].id",
			class: topology.ErrInvalidID,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newComposedRegistry(context.Background(), source, test.composition, func(context.Context, string, ...string) (string, error) {
				return "", errors.New("provider metadata must not be read")
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("newComposedRegistry() error = %v, want substring %q", err, test.want)
			}
			if !errors.Is(err, test.class) {
				t.Fatalf("newComposedRegistry() error = %v, want %v", err, test.class)
			}
		})
	}
}

func TestRegistryWithoutCompositionReportsNoProvenance(t *testing.T) {
	root := testsupport.TempDir(t)
	source := config.RepositoriesFile{
		Version:      config.CurrentSchemaVersion,
		Repositories: []config.Repository{{Name: "frontend", Path: root, Languages: []string{"go"}}},
	}
	git := fakeGit(map[string]string{
		"rev-parse HEAD":                              "commit",
		"symbolic-ref --quiet --short HEAD":           "main",
		"status --porcelain=v1 --untracked-files=all": "",
	}, nil)
	registry, err := newRegistry(context.Background(), source, git)
	if err != nil {
		t.Fatalf("newRegistry() error = %v", err)
	}
	if _, ok := registry.Composition(); ok {
		t.Fatal("Composition() = true for a plain registry, want false")
	}
	var nilRegistry *Registry
	if _, ok := nilRegistry.Composition(); ok {
		t.Fatal("Composition() = true for a nil registry, want false")
	}
}

func TestNewComposedRegistryRejectsDuplicateProviderMetadata(t *testing.T) {
	root := testsupport.TempDir(t)
	source := config.RepositoriesFile{
		Version: config.CurrentSchemaVersion,
		Repositories: []config.Repository{
			{Name: "frontend", Path: root, Languages: []string{"go"}},
			{Name: "frontend", Path: root, Languages: []string{"go"}},
		},
	}
	composition := topology.ProfileComposition{
		Profile: topology.Profile{
			ID:        "feature",
			Worktrees: []topology.WorktreeSelection{{Repository: "frontend", Worktree: "frontend-main"}},
		},
		Repositories: []topology.LogicalRepository{{ID: "frontend"}},
		Worktrees:    []topology.Worktree{{ID: "frontend-main", Repository: "frontend", Path: root}},
	}

	_, err := newComposedRegistry(context.Background(), source, composition, func(context.Context, string, ...string) (string, error) {
		return "", errors.New("provider metadata must not be read")
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate provider metadata") {
		t.Fatalf("newComposedRegistry() error = %v, want duplicate provider metadata", err)
	}
}

func TestNewComposedRegistryRejectsEmptyProviderMetadataName(t *testing.T) {
	root := testsupport.TempDir(t)
	source := config.RepositoriesFile{
		Version:      config.CurrentSchemaVersion,
		Repositories: []config.Repository{{Name: "", Path: root, Languages: []string{"go"}}},
	}
	composition := topology.ProfileComposition{
		Profile: topology.Profile{
			ID:        "feature",
			Worktrees: []topology.WorktreeSelection{{Repository: "frontend", Worktree: "frontend-main"}},
		},
		Repositories: []topology.LogicalRepository{{ID: "frontend"}},
		Worktrees:    []topology.Worktree{{ID: "frontend-main", Repository: "frontend", Path: root}},
	}
	_, err := newComposedRegistry(context.Background(), source, composition, func(context.Context, string, ...string) (string, error) {
		return "", errors.New("provider metadata must not be read")
	})
	if err == nil || !strings.Contains(err.Error(), "repositories[0].name") {
		t.Fatalf("newComposedRegistry() error = %v, want empty provider name", err)
	}
}

func TestNewComposedRegistryPropagatesProviderRegistrationFailure(t *testing.T) {
	root := testsupport.TempDir(t)
	source := config.RepositoriesFile{
		Version:      config.CurrentSchemaVersion,
		Repositories: []config.Repository{{Name: "frontend", Path: root, Languages: []string{"go"}}},
	}
	composition := topology.ProfileComposition{
		Profile: topology.Profile{
			ID:        "feature",
			Worktrees: []topology.WorktreeSelection{{Repository: "frontend", Worktree: "frontend-main"}},
		},
		Repositories: []topology.LogicalRepository{{ID: "frontend"}},
		Worktrees:    []topology.Worktree{{ID: "frontend-main", Repository: "frontend", Path: root}},
	}
	_, err := newComposedRegistry(context.Background(), source, composition, func(context.Context, string, ...string) (string, error) {
		return "", errors.New("git metadata unavailable")
	})
	if err == nil || !strings.Contains(err.Error(), "register composed profile") || !strings.Contains(err.Error(), "git metadata unavailable") {
		t.Fatalf("newComposedRegistry() error = %v, want provider registration failure", err)
	}
}

func TestNewComposedRegistryUsesRealGitMetadataThroughExportedAPI(t *testing.T) {
	if err := exec.Command("git", "--version").Run(); err != nil {
		t.Skipf("git is required for composed registry registration: %v", err)
	}
	root := testsupport.TempDir(t)
	gitTestCommand(t, "-C", root, "init", "-q")
	gitTestCommand(t, "-C", root, "symbolic-ref", "HEAD", "refs/heads/main")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("initial\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	gitTestCommand(t, "-C", root, "add", "README.md")
	gitTestCommand(t, "-C", root, "-c", "user.name=Kivgraph Test", "-c", "user.email=kivgraph-test@example.invalid", "commit", "-qm", "initial")

	source := config.RepositoriesFile{
		Version:      config.CurrentSchemaVersion,
		Repositories: []config.Repository{{Name: "frontend", Path: root, Languages: []string{"go"}}},
	}
	composition := topology.ProfileComposition{
		Profile: topology.Profile{
			ID:        "feature",
			Worktrees: []topology.WorktreeSelection{{Repository: "frontend", Worktree: "frontend-main"}},
		},
		Repositories: []topology.LogicalRepository{{ID: "frontend"}},
		Worktrees:    []topology.Worktree{{ID: "frontend-main", Repository: "frontend", Path: root}},
	}
	registry, err := NewComposedRegistry(context.Background(), source, composition)
	if err != nil {
		t.Fatalf("NewComposedRegistry() error = %v", err)
	}
	if _, err := NewComposedRegistry(noContext(), source, composition); err != nil {
		t.Fatalf("NewComposedRegistry(nil) error = %v", err)
	}
	if repository, ok := registry.Get("frontend"); !ok || repository.Branch != "main" || repository.Dirty || repository.Worktree != "frontend-main" {
		t.Fatalf("registered Git metadata = %#v, want clean selected main worktree", repository)
	}
}

func noContext() context.Context { return nil }

func TestNewComposedRegistryRejectsMissingProviderMetadata(t *testing.T) {
	root := testsupport.TempDir(t)
	source := config.RepositoriesFile{
		Version:      config.CurrentSchemaVersion,
		Repositories: []config.Repository{{Name: "frontend", Path: root, Languages: []string{"go"}}},
	}
	composition := topology.ProfileComposition{
		Profile: topology.Profile{
			ID: "feature",
			Worktrees: []topology.WorktreeSelection{
				{Repository: "frontend", Worktree: "frontend-main"},
				{Repository: "backend", Worktree: "backend-main"},
			},
		},
		Repositories: []topology.LogicalRepository{
			{ID: "frontend"},
			{ID: "backend"},
		},
		Worktrees: []topology.Worktree{
			{ID: "frontend-main", Repository: "frontend", Path: root},
			{ID: "backend-main", Repository: "backend", Path: root},
		},
	}

	_, err := newComposedRegistry(context.Background(), source, composition, func(context.Context, string, ...string) (string, error) {
		return "", errors.New("provider metadata must not be read")
	})
	if err == nil || !strings.Contains(err.Error(), "backend") {
		t.Fatalf("newComposedRegistry() error = %v, want missing backend metadata", err)
	}
	if !strings.Contains(err.Error(), "no provider metadata") {
		t.Fatalf("newComposedRegistry() error = %q, want provider metadata reason", err)
	}
}

func TestNewComposedRegistryUsesSelectedWorktreesAndRetainsProviderMetadata(t *testing.T) {
	frontend := testsupport.TempDir(t)
	backend := testsupport.TempDir(t)
	oldFrontend := testsupport.TempDir(t)
	oldBackend := testsupport.TempDir(t)
	source := config.RepositoriesFile{
		Version: config.CurrentSchemaVersion,
		Repositories: []config.Repository{
			{
				Name:       "frontend",
				Path:       oldFrontend,
				Languages:  []string{"typescript"},
				Manifests:  []string{"package.json"},
				Roots:      []string{"src"},
				Exclusions: []string{"node_modules"},
			},
			{Name: "backend", Path: oldBackend, Languages: []string{"go"}},
			{Name: "unrelated", Path: testsupport.TempDir(t), Languages: []string{"rust"}},
		},
	}
	composition := topology.ProfileComposition{
		Profile: topology.Profile{
			ID: "feature",
			Worktrees: []topology.WorktreeSelection{
				{Repository: "frontend", Worktree: "frontend-feature"},
				{Repository: "backend", Worktree: "backend-feature"},
			},
		},
		Repositories: []topology.LogicalRepository{
			{ID: "frontend", Name: "Frontend"},
			{ID: "backend", Name: "Backend"},
		},
		Worktrees: []topology.Worktree{
			{ID: "frontend-feature", Repository: "frontend", Path: frontend},
			{ID: "backend-feature", Repository: "backend", Path: backend},
		},
	}
	git := fakeGit(map[string]string{
		"rev-parse HEAD":                              "commit",
		"symbolic-ref --quiet --short HEAD":           "feature",
		"status --porcelain=v1 --untracked-files=all": "",
	}, nil)

	registry, err := newComposedRegistry(context.Background(), source, composition, git)
	if err != nil {
		t.Fatalf("newComposedRegistry() error = %v", err)
	}
	got := registry.List()
	want := []Repository{
		{
			Name:       "frontend",
			Worktree:   "frontend-feature",
			Path:       frontend,
			RealPath:   frontend,
			Commit:     "commit",
			Branch:     "feature",
			Languages:  []string{"typescript"},
			Manifests:  []string{filepath.Join(frontend, "package.json")},
			Roots:      []string{filepath.Join(frontend, "src")},
			Exclusions: []string{"node_modules"},
		},
		{
			Name:      "backend",
			Worktree:  "backend-feature",
			Path:      backend,
			RealPath:  backend,
			Commit:    "commit",
			Branch:    "feature",
			Languages: []string{"go"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registered repositories = %#v, want %#v", got, want)
	}
	provenance, ok := registry.Composition()
	if !ok {
		t.Fatal("Composition() = false, want composition provenance")
	}
	if !reflect.DeepEqual(provenance, composition) {
		t.Fatalf("composition provenance = %#v, want %#v", provenance, composition)
	}

	provenance.Worktrees[0].Path = "changed"
	provenance.Profile.Worktrees[0].Worktree = "changed"
	again, ok := registry.Composition()
	if !ok || again.Worktrees[0].Path != frontend || again.Profile.Worktrees[0].Worktree != "frontend-feature" {
		t.Fatalf("Composition() returned aliased data = %#v", again)
	}
}

func TestNewComposedRegistryKeepsWorktreeVariantsIsolated(t *testing.T) {
	mainWorktree := testsupport.TempDir(t)
	maintenanceWorktree := testsupport.TempDir(t)
	oldPath := testsupport.TempDir(t)
	source := config.RepositoriesFile{
		Version: config.CurrentSchemaVersion,
		Repositories: []config.Repository{{
			Name: "backend", Path: oldPath, Languages: []string{"go"},
		}},
	}
	git := fakeGit(map[string]string{
		"rev-parse HEAD":                              "commit",
		"symbolic-ref --quiet --short HEAD":           "branch",
		"status --porcelain=v1 --untracked-files=all": "",
	}, nil)
	composition := func(worktreeID topology.WorktreeID, path string) topology.ProfileComposition {
		return topology.ProfileComposition{
			Profile: topology.Profile{
				ID:        topology.ProfileID(string(worktreeID)),
				Worktrees: []topology.WorktreeSelection{{Repository: "backend", Worktree: worktreeID}},
			},
			Repositories: []topology.LogicalRepository{{ID: "backend"}},
			Worktrees:    []topology.Worktree{{ID: worktreeID, Repository: "backend", Path: path}},
		}
	}

	mainRegistry, err := newComposedRegistry(context.Background(), source, composition("main", mainWorktree), git)
	if err != nil {
		t.Fatalf("main composition error = %v", err)
	}
	maintenanceRegistry, err := newComposedRegistry(context.Background(), source, composition("maintenance", maintenanceWorktree), git)
	if err != nil {
		t.Fatalf("maintenance composition error = %v", err)
	}
	if mainRegistry.List()[0].Path != mainWorktree || maintenanceRegistry.List()[0].Path != maintenanceWorktree {
		t.Fatalf("variant paths = %q and %q, want isolated worktrees", mainRegistry.List()[0].Path, maintenanceRegistry.List()[0].Path)
	}
	if mainRegistry.List()[0].Worktree != "main" || maintenanceRegistry.List()[0].Worktree != "maintenance" {
		t.Fatalf("variant source identities = %q and %q, want selected worktrees", mainRegistry.List()[0].Worktree, maintenanceRegistry.List()[0].Worktree)
	}
	mainProvenance, _ := mainRegistry.Composition()
	maintenanceProvenance, _ := maintenanceRegistry.Composition()
	if mainProvenance.Profile.ID == maintenanceProvenance.Profile.ID {
		t.Fatalf("profile IDs for main and maintenance worktrees = %q and %q, want distinct values", mainProvenance.Profile.ID, maintenanceProvenance.Profile.ID)
	}
}
