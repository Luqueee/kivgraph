package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
		t.Fatalf("registry composition for profile %q and path %q = %#v, present %t, want selected provenance",
			loaded.Profile, selectedPath, provenance, present)
	}
}

// A composition changes source paths, not the provider configuration each
// language-specific indexer receives. Every selected worktree must retain its
// registered language metadata while replacing the stale ordinary path.
func TestRegistryForProfileRetainsProviderConfigurationAcrossLanguages(t *testing.T) {
	root := testsupport.TempDir(t)
	configPath := filepath.Join(root, "config.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	loaded, err := config.LoadProfile(configPath, "default")
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}
	type provider struct {
		name       string
		languages  []string
		manifests  []string
		roots      []string
		exclusions []string
		path       string
	}
	providers := []provider{
		{name: "go-provider", languages: []string{"go"}},
		{name: "typescript-provider", languages: []string{"typescript"}, manifests: []string{"package.json"}, roots: []string{"src"}, exclusions: []string{"node_modules"}},
		{name: "rust-provider", languages: []string{"rust"}},
		{name: "java-provider", languages: []string{"java"}},
		{name: "csharp-provider", languages: []string{"csharp"}},
	}
	source := config.RepositoriesFile{Version: config.CurrentSchemaVersion}
	composition := topology.Topology{
		Version:  topology.CurrentSchemaVersion,
		Profiles: []topology.Profile{{ID: "default"}},
	}
	for index := range providers {
		providers[index].path = testsupport.TempDir(t)
		initGitRepository(t, providers[index].path)

		worktree := providers[index].name + "-selected"
		source.Repositories = append(source.Repositories, config.Repository{
			Name:       providers[index].name,
			Path:       filepath.Join(root, "stale-"+providers[index].name),
			Languages:  providers[index].languages,
			Manifests:  providers[index].manifests,
			Roots:      providers[index].roots,
			Exclusions: providers[index].exclusions,
		})
		composition.Repositories = append(composition.Repositories, topology.LogicalRepository{
			ID: topology.LogicalRepositoryID(providers[index].name),
		})
		composition.Worktrees = append(composition.Worktrees, topology.Worktree{
			ID:         topology.WorktreeID(worktree),
			Repository: topology.LogicalRepositoryID(providers[index].name),
			Path:       providers[index].path,
		})
		composition.Profiles[0].Worktrees = append(composition.Profiles[0].Worktrees, topology.WorktreeSelection{
			Repository: topology.LogicalRepositoryID(providers[index].name), Worktree: topology.WorktreeID(worktree),
		})
	}
	if err := config.SaveRepositories(loaded.RepositoriesPath, source); err != nil {
		t.Fatalf("SaveRepositories() error = %v", err)
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
	registered := make(map[string]workspace.Repository, len(registry.List()))
	for _, repository := range registry.List() {
		registered[repository.Name] = repository
	}
	if len(registered) != len(providers) {
		t.Fatalf("registered providers for inputs %#v = %#v, want %d selected worktrees",
			providers, registered, len(providers))
	}
	for _, provider := range providers {
		repository, ok := registered[provider.name]
		if !ok {
			t.Fatalf("selected provider %q is missing from %#v", provider.name, registered)
		}
		if repository.Path != provider.path || repository.RealPath != provider.path {
			t.Fatalf("provider %q paths = %q and %q, want selected worktree %q",
				provider.name, repository.Path, repository.RealPath, provider.path)
		}
		if !reflect.DeepEqual(repository.Languages, provider.languages) {
			t.Fatalf("provider %q languages = %v, want %v", provider.name, repository.Languages, provider.languages)
		}
		if provider.name != "typescript-provider" {
			continue
		}
		if want := []string{filepath.Join(provider.path, "package.json")}; !reflect.DeepEqual(repository.Manifests, want) {
			t.Fatalf("TypeScript manifests = %v, want %v", repository.Manifests, want)
		}
		if want := []string{filepath.Join(provider.path, "src")}; !reflect.DeepEqual(repository.Roots, want) {
			t.Fatalf("TypeScript roots = %v, want %v", repository.Roots, want)
		}
		if !reflect.DeepEqual(repository.Exclusions, provider.exclusions) {
			t.Fatalf("TypeScript exclusions = %v, want %v", repository.Exclusions, provider.exclusions)
		}
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
	var stdout bytes.Buffer
	writeProfileDiagnostics(&stdout, loaded.Profile, registry)
	want := fmt.Sprintf("index.profile: name=%s composition=legacy repositories=1\n", loaded.Profile)
	if stdout.String() != want {
		t.Fatalf("legacy profile diagnostics for profile %q = %q, want %q",
			loaded.Profile, stdout.String(), want)
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
			t.Fatalf("co-membership created an edge with dependency=false: %#v", edge)
		}
	}
}

// Selecting two providers that declare the same module makes the provider
// universe ambiguous. The profile must retain both candidates as unresolved
// evidence instead of choosing a worktree or fabricating an exact edge.
func TestComposedProfileRetainsAmbiguousGoProviders(t *testing.T) {
	registry, workFile := newComposedGoFixtureWithProviders(t, true, "provider-one", "provider-two")
	set := indexComposedGoFixture(t, registry, workFile)

	providers := map[string]bool{"provider-one": false, "provider-two": false}
	for _, unresolved := range set.Unresolved {
		provider := facts.RepositoryNameFromKey(unresolved.RepositoryKey)
		if _, selected := providers[provider]; !selected || unresolved.RequestedPackage != "example.com/provider" ||
			unresolved.Reason != "AMBIGUOUS_MODULE_PROVIDER" {
			continue
		}
		providers[provider] = true
		for _, provider := range []string{"provider-one", "provider-two"} {
			if !strings.Contains(unresolved.Detail, provider) {
				t.Fatalf("ambiguous provider detail = %q, want candidate %q", unresolved.Detail, provider)
			}
		}
	}
	for provider, found := range providers {
		if !found {
			t.Fatalf("no ambiguous provider evidence for %q: %#v", provider, set.Unresolved)
		}
	}
	for _, edge := range set.Edges {
		if edge.Confidence.Exact() {
			t.Fatalf("ambiguous providers created an exact edge: %#v", edge)
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
	fixture := fmt.Sprintf("workFile=%q, worktrees=%#v", workFile, composition.Worktrees)
	if !present || len(composition.Worktrees) != 2 {
		t.Fatalf("composition (%s) = %#v, present %t, want two selected worktrees",
			fixture, composition, present)
	}
	for _, worktree := range composition.Worktrees {
		if worktree.Path == "" {
			t.Fatalf("composition worktree (%s) = %#v, want an observed path", fixture, worktree)
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
			t.Fatalf("cross-repository edge (%s) = %#v, want exact Go object-path evidence",
				fixture, edge)
		}
		if edge.EvidenceKey == "" {
			t.Fatalf("cross-repository edge (%s) = %#v, want source evidence", fixture, edge)
		}
	}
	if !found {
		t.Fatalf("no exact edge with dependency=true from consumer.Total to provider.Value (%s): %#v",
			fixture, set.Edges)
	}
}

func TestWriteProfileDiagnosticsReportsEffectiveWorktrees(t *testing.T) {
	registry, _ := newComposedGoFixture(t, false)
	composition, _ := registry.Composition()
	profile := "default"
	var stdout bytes.Buffer
	writeProfileDiagnostics(&stdout, profile, registry)

	report := stdout.String()
	var want strings.Builder
	fmt.Fprintf(&want, "index.profile: name=%s composition=topology repositories=%d\n",
		profile, len(composition.Repositories))
	for index, repository := range composition.Repositories {
		worktree := composition.Worktrees[index]
		fmt.Fprintf(&want, "index.profile.worktree: repository=%s worktree=%s path=%s\n",
			repository.ID, worktree.ID, worktree.Path)
	}
	if report != want.String() {
		t.Fatalf("profile diagnostics for profile %q = %q, want %q", profile, report, want.String())
	}
}

func newComposedGoFixture(t *testing.T, dependency bool) (*workspace.Registry, string) {
	t.Helper()
	return newComposedGoFixtureWithProviders(t, dependency, "provider")
}

func newComposedGoFixtureWithProviders(
	t *testing.T,
	dependency bool,
	providerNames ...string,
) (*workspace.Registry, string) {
	t.Helper()
	consumerPath := testsupport.TempDir(t)
	initGitRepository(t, consumerPath)
	consumerModule := "module example.com/consumer\n\ngo 1.24\n"
	consumerSource := "package consumer\n\nfunc Total() int { return 1 }\n"
	if dependency {
		consumerModule += "\nrequire example.com/provider v0.0.0\n"
		consumerSource = "package consumer\n\nimport \"example.com/provider\"\n\nfunc Total() int { return provider.Value }\n"
	}
	writeComposedGoFile(t, filepath.Join(consumerPath, "go.mod"), consumerModule)
	writeComposedGoFile(t, filepath.Join(consumerPath, "main.go"), consumerSource)

	configRoot := testsupport.TempDir(t)
	configPath := filepath.Join(configRoot, "config.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	loaded, err := config.LoadProfile(configPath, "default")
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}
	repositories := []config.Repository{{
		Name: "consumer", Path: filepath.Join(configRoot, "stale-consumer"), Languages: []string{"go"},
	}}
	logicalRepositories := []topology.LogicalRepository{{ID: "consumer"}}
	worktrees := []topology.Worktree{{ID: "consumer-main", Repository: "consumer", Path: consumerPath}}
	selections := []topology.WorktreeSelection{{Repository: "consumer", Worktree: "consumer-main"}}
	for _, providerName := range providerNames {
		providerPath := testsupport.TempDir(t)
		initGitRepository(t, providerPath)
		writeComposedGoFile(t, filepath.Join(providerPath, "go.mod"), "module example.com/provider\n\ngo 1.24\n")
		writeComposedGoFile(t, filepath.Join(providerPath, "value.go"), "package provider\n\nconst Value = 41\n")

		worktreeName := providerName + "-main"
		repositories = append(repositories, config.Repository{
			Name: providerName, Path: filepath.Join(configRoot, "stale-"+providerName), Languages: []string{"go"},
		})
		logicalRepositories = append(logicalRepositories, topology.LogicalRepository{ID: topology.LogicalRepositoryID(providerName)})
		worktrees = append(worktrees, topology.Worktree{
			ID:         topology.WorktreeID(worktreeName),
			Repository: topology.LogicalRepositoryID(providerName),
			Path:       providerPath,
		})
		selections = append(selections, topology.WorktreeSelection{
			Repository: topology.LogicalRepositoryID(providerName), Worktree: topology.WorktreeID(worktreeName),
		})
	}
	// These paths are intentionally not usable. The topology-selected paths
	// below are the only source locations the composed registry may register.
	source := config.RepositoriesFile{
		Version:      config.CurrentSchemaVersion,
		Repositories: repositories,
	}
	if err := config.SaveRepositories(loaded.RepositoriesPath, source); err != nil {
		t.Fatalf("SaveRepositories() error = %v", err)
	}
	if err := config.SaveProfileTopology(configPath, "default", topology.Topology{
		Version:      topology.CurrentSchemaVersion,
		Repositories: logicalRepositories,
		Worktrees:    worktrees,
		Profiles: []topology.Profile{{
			ID:        "default",
			Worktrees: selections,
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
	return registry, filepath.Join(testsupport.TempDir(t), "go.work")
}

func indexComposedGoFixture(t *testing.T, registry *workspace.Registry, workFile string) facts.Set {
	t.Helper()
	repositories := registry.List()
	set, _, err := indexer.Full(context.Background(), indexer.FullOptions{
		Profile:           "default",
		Repositories:      repositories,
		SyntheticWorkFile: workFile,
		GoMaximumLoads:    1,
	})
	if err != nil {
		t.Fatalf("indexer.Full(workFile=%q, repositories=%#v) error = %v", workFile, repositories, err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("composed facts validation for workFile=%q, repositories=%#v error = %v",
			workFile, repositories, err)
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
