package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/indexer"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/topology"
	"github.com/Luqueee/kivgraph/internal/workspace"
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
		t.Fatalf("composed registry = %#v, want selected path %q and provider metadata", items, selectedPath)
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
		t.Fatalf("legacy registry unexpectedly carries topology provenance for config %q", loaded.ConfigPath)
	}
	if items := registry.List(); len(items) != 1 || items[0].Path != path {
		t.Fatalf("legacy registry = %#v, want configured path %q", items, path)
	}
}

func TestRegistryForProfileReportsInvalidLoadedConfiguration(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "missing.yaml")
	if _, err := registryForProfile(context.Background(), config.Loaded{
		ConfigPath: configPath,
		Profile:    "default",
	}); err == nil {
		t.Fatalf("registryForProfile() unexpectedly succeeded for missing config %q", configPath)
	}
}

// A profile is a search universe, not dependency evidence. An unrelated
// provider selected beside a consumer must not receive an edge merely because
// both repositories belong to the same composition.
func TestComposedProfileDoesNotCreateEdgesFromCoMembership(t *testing.T) {
	registry, workFile := newComposedGoFixture(t, false)
	set := indexComposedGoFixture(t, registry, workFile)

	consumer := factSymbol(t, set, "consumer", "Total")
	provider := factSymbol(t, set, "provider", "Value")
	for _, edge := range set.Edges {
		if edge.SourceKey == consumer.Key && edge.TargetKey == provider.Key {
			t.Fatalf("co-membership created an edge = %#v", edge)
		}
	}
}

// The selected worktree paths are the inputs to the real full pass. Keeping
// stale paths in the ordinary provider registry makes this fail if the CLI
// accidentally indexes provider metadata instead of the topology-selected
// worktrees. The import and Go type checker then provide the evidence for the
// exact cross-repository edge.
func TestComposedProfileResolvesGoEdgesAcrossSelectedWorktrees(t *testing.T) {
	registry, workFile := newComposedGoFixture(t, true)
	composition, present := registry.Composition()
	if !present || len(composition.Worktrees) != 2 {
		t.Fatalf("composition = %#v, present %t, want two selected worktrees", composition, present)
	}
	for _, worktree := range composition.Worktrees {
		if worktree.Path == "" {
			t.Fatalf("composition worktree = %#v, want an observed path", worktree)
		}
	}

	set := indexComposedGoFixture(t, registry, workFile)
	consumer := factSymbol(t, set, "consumer", "Total")
	provider := factSymbol(t, set, "provider", "Value")
	found := false
	for _, edge := range set.Edges {
		if edge.SourceKey != consumer.Key || edge.TargetKey != provider.Key {
			continue
		}
		found = true
		if !edge.Confidence.Exact() || edge.Provenance != facts.GoObjectPath {
			t.Fatalf("cross-repository edge = %#v, want exact Go object-path evidence", edge)
		}
		if edge.EvidenceKey == "" {
			t.Fatalf("cross-repository edge = %#v, want source evidence", edge)
		}
	}
	if !found {
		t.Fatalf("no exact edge from consumer.Total to provider.Value: %#v", set.Edges)
	}
}

func TestWriteProfileDiagnosticsReportsEffectiveWorktrees(t *testing.T) {
	registry, _ := newComposedGoFixture(t, false)
	composition, _ := registry.Composition()
	profile := "default"
	var stdout bytes.Buffer
	writeProfileDiagnostics(&stdout, profile, registry)

	report := stdout.String()
	if !strings.Contains(report, "index.profile: name=default composition=topology repositories=2") {
		t.Fatalf("profile diagnostics for profile %q = %q, want the topology summary", profile, report)
	}
	for index, repository := range composition.Repositories {
		worktree := composition.Worktrees[index]
		want := fmt.Sprintf("index.profile.worktree: repository=%s worktree=%s path=%s",
			repository.ID, worktree.ID, worktree.Path)
		if !strings.Contains(report, want) {
			t.Fatalf("profile diagnostics = %q, want %q", report, want)
		}
	}
}

func newComposedGoFixture(t *testing.T, dependency bool) (*workspace.Registry, string) {
	t.Helper()
	providerPath := t.TempDir()
	consumerPath := t.TempDir()
	initGitRepository(t, providerPath)
	initGitRepository(t, consumerPath)
	writeComposedGoFile(t, filepath.Join(providerPath, "go.mod"), "module example.com/provider\n\ngo 1.24\n")
	writeComposedGoFile(t, filepath.Join(providerPath, "value.go"), "package provider\n\nconst Value = 41\n")
	consumerModule := "module example.com/consumer\n\ngo 1.24\n"
	consumerSource := "package consumer\n\nfunc Total() int { return 1 }\n"
	if dependency {
		consumerModule += "\nrequire example.com/provider v0.0.0\n"
		consumerSource = "package consumer\n\nimport \"example.com/provider\"\n\nfunc Total() int { return provider.Value }\n"
	}
	writeComposedGoFile(t, filepath.Join(consumerPath, "go.mod"), consumerModule)
	writeComposedGoFile(t, filepath.Join(consumerPath, "main.go"), consumerSource)

	configRoot := t.TempDir()
	configPath := filepath.Join(configRoot, "config.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	loaded, err := config.LoadProfile(configPath, "default")
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}
	// These paths are intentionally not usable. The topology-selected paths
	// below are the only source locations the composed registry may register.
	source := config.RepositoriesFile{
		Version: config.CurrentSchemaVersion,
		Repositories: []config.Repository{
			{Name: "consumer", Path: filepath.Join(configRoot, "stale-consumer"), Languages: []string{"go"}},
			{Name: "provider", Path: filepath.Join(configRoot, "stale-provider"), Languages: []string{"go"}},
		},
	}
	if err := config.SaveRepositories(loaded.RepositoriesPath, source); err != nil {
		t.Fatalf("SaveRepositories() error = %v", err)
	}
	if err := config.SaveProfileTopology(configPath, "default", topology.Topology{
		Version: topology.CurrentSchemaVersion,
		Repositories: []topology.LogicalRepository{
			{ID: "consumer"},
			{ID: "provider"},
		},
		Worktrees: []topology.Worktree{
			{ID: "consumer-main", Repository: "consumer", Path: consumerPath},
			{ID: "provider-main", Repository: "provider", Path: providerPath},
		},
		Profiles: []topology.Profile{{
			ID: "default",
			Worktrees: []topology.WorktreeSelection{
				{Repository: "consumer", Worktree: "consumer-main"},
				{Repository: "provider", Worktree: "provider-main"},
			},
		}},
	}); err != nil {
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
	return registry, filepath.Join(t.TempDir(), "go.work")
}

func indexComposedGoFixture(t *testing.T, registry *workspace.Registry, workFile string) facts.Set {
	t.Helper()
	set, _, err := indexer.Full(context.Background(), indexer.FullOptions{
		Profile:           "default",
		Repositories:      registry.List(),
		SyntheticWorkFile: workFile,
		GoMaximumLoads:    1,
	})
	if err != nil {
		t.Fatalf("indexer.Full() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("composed facts validation error = %v", err)
	}
	return set
}

func factSymbol(t *testing.T, set facts.Set, repository, name string) facts.Symbol {
	t.Helper()
	for _, symbol := range set.Symbols {
		if symbol.RepositoryKey == facts.RepositoryKey(repository) && symbol.Name == name {
			return symbol
		}
	}
	t.Fatalf("symbol %s.%s is missing from %#v", repository, name, set.Symbols)
	return facts.Symbol{}
}

func writeComposedGoFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

// registryForProfile's Compose error path is defensive: LoadProfileTopology
// validates the same selected profile before returning. Reaching it requires
// mutating an impossible Loaded value, so no observable test can cover it
// without adding a production seam solely for testing.

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
