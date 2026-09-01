package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/topology"
)

func TestRegistryForProfileUsesSelectedTopologyWorktree(t *testing.T) {
	root := testsupport.TempDir(t)
	configPath := filepath.Join(root, "config.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	loaded, err := config.LoadProfile(configPath, "default")
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}
	selectedPath := testsupport.TempDir(t)
	initGitRepository(t, selectedPath)
	source := config.RepositoriesFile{
		Version: config.CurrentSchemaVersion,
		Repositories: []config.Repository{{
			Name: "backend", Path: filepath.Join(root, "old-worktree"), Languages: []string{"go"},
		}},
	}
	if err := config.SaveRepositories(loaded.RepositoriesPath, source); err != nil {
		t.Fatalf("SaveRepositories() error = %v", err)
	}
	composition := topology.Topology{
		Version:      topology.CurrentSchemaVersion,
		Repositories: []topology.LogicalRepository{{ID: "backend"}},
		Worktrees:    []topology.Worktree{{ID: "backend-main", Repository: "backend", Path: selectedPath}},
		Profiles: []topology.Profile{{
			ID:        "default",
			Worktrees: []topology.WorktreeSelection{{Repository: "backend", Worktree: "backend-main"}},
		}},
	}
	if err := config.SaveProfileTopology(configPath, "default", composition); err != nil {
		t.Fatalf("SaveProfileTopology() error = %v", err)
	}
	loaded, err = config.LoadProfile(configPath, "default")
	if err != nil {
		t.Fatalf("LoadProfile(after topology) error = %v", err)
	}

	registry, err := registryForProfile(context.Background(), loaded)
	if err != nil {
		t.Fatalf("registryForProfile() error = %v", err)
	}
	items := registry.List()
	if len(items) != 1 || items[0].Path != selectedPath || items[0].Languages[0] != "go" {
		t.Fatalf("composed registry = %#v, want selected path and provider metadata", items)
	}
	provenance, present := registry.Composition()
	if !present || len(provenance.Worktrees) != 1 || provenance.Worktrees[0].Path != selectedPath {
		t.Fatalf("registry composition = %#v, present %t, want selected provenance", provenance, present)
	}
}

func TestRegistryForProfileKeepsLegacyRegistryWithoutTopology(t *testing.T) {
	root := testsupport.TempDir(t)
	configPath := filepath.Join(root, "config.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	path := testsupport.TempDir(t)
	initGitRepository(t, path)
	loaded, err := config.LoadProfile(configPath, "default")
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}
	if err := config.SaveRepositories(loaded.RepositoriesPath, config.RepositoriesFile{
		Version:      config.CurrentSchemaVersion,
		Repositories: []config.Repository{{Name: "backend", Path: path, Languages: []string{"go"}}},
	}); err != nil {
		t.Fatalf("SaveRepositories() error = %v", err)
	}
	loaded, err = config.LoadProfile(configPath, "default")
	if err != nil {
		t.Fatalf("LoadProfile(after registry) error = %v", err)
	}
	registry, err := registryForProfile(context.Background(), loaded)
	if err != nil {
		t.Fatalf("registryForProfile() error = %v", err)
	}
	if _, present := registry.Composition(); present {
		t.Fatal("legacy registry unexpectedly carries topology provenance")
	}
	if items := registry.List(); len(items) != 1 || items[0].Path != path {
		t.Fatalf("legacy registry = %#v, want the configured path", items)
	}
}

func TestRegistryForProfileReportsInvalidLoadedConfiguration(t *testing.T) {
	if _, err := registryForProfile(context.Background(), config.Loaded{
		ConfigPath: filepath.Join(t.TempDir(), "missing.yaml"),
		Profile:    "default",
	}); err == nil {
		t.Fatal("registryForProfile() succeeded with a missing config")
	}
}

func TestRegistryForProfileReportsInvalidLoadedProfile(t *testing.T) {
	configPath, loaded := configProfileForRegistry(t)
	if err := config.SaveProfileTopology(configPath, "default", topology.Topology{
		Version:  topology.CurrentSchemaVersion,
		Profiles: []topology.Profile{{ID: "default"}},
	}); err != nil {
		t.Fatalf("SaveProfileTopology() error = %v", err)
	}
	loaded.Profile = ""
	if _, err := registryForProfile(context.Background(), loaded); err == nil {
		t.Fatal("registryForProfile() succeeded with an invalid loaded profile")
	}
}

func configProfileForRegistry(t *testing.T) (string, config.Loaded) {
	t.Helper()
	root := testsupport.TempDir(t)
	configPath := filepath.Join(root, "config.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	loaded, err := config.LoadProfile(configPath, "default")
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}
	return configPath, loaded
}

func initGitRepository(t *testing.T, path string) {
	t.Helper()
	for _, args := range [][]string{
		{"-C", path, "init", "-q"},
		{"-C", path, "config", "user.email", "tests@example.com"},
		{"-C", path, "config", "user.name", "Kivgraph Tests"},
	} {
		runGitTestCommand(t, args...)
	}
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatalf("write Git fixture: %v", err)
	}
	runGitTestCommand(t, "-C", path, "add", "README.md")
	runGitTestCommand(t, "-C", path, "commit", "-qm", "fixture")
}

func runGitTestCommand(t *testing.T, args ...string) {
	t.Helper()
	if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, output)
	}
}
