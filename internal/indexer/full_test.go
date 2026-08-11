package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/goworkspace"
	"github.com/Luqueee/ladygraph/internal/testsupport"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

func TestFullBuildsGoFactsOutsideTheRepository(t *testing.T) {
	root := testsupport.TempDir(t)
	writeFullFixture(t, filepath.Join(root, "go.mod"), "module example.com/fullfixture\n\ngo 1.24\n")
	writeFullFixture(t, filepath.Join(root, "fixture.go"), `package fixture

func Greeting() string { return "hello" }
`)
	workFile := filepath.Join(testsupport.TempDir(t), "go.work")

	set, report, err := Full(context.Background(), FullOptions{
		Repositories: []workspace.Repository{{
			Name:      "fixture",
			Path:      root,
			RealPath:  root,
			Languages: []string{"go"},
		}},
		SyntheticWorkFile: workFile,
	})
	if err != nil {
		t.Fatalf("Full() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("full facts validation error = %v", err)
	}
	if report.GoRepositories != 1 || report.GoModules != 1 || report.GoDefinitions == 0 {
		t.Fatalf("full report = %+v, want one repository/module and definitions", report)
	}
	// A lone module resolves from its own manifest. A workspace could only
	// widen its build list, so none is installed.
	if report.GoWorkspaces != 0 {
		t.Fatalf("workspaces = %d, want none for a single module", report.GoWorkspaces)
	}
	if _, err := os.Stat(workFile); !os.IsNotExist(err) {
		t.Fatalf("synthetic work file %q error = %v, want absent", workFile, err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.work")); !os.IsNotExist(err) {
		t.Fatalf("repository go.work error = %v, want absent", err)
	}
}

func TestFullHonoursConfiguredGoExclusionsDuringLoad(t *testing.T) {
	root := testsupport.TempDir(t)
	writeFullFixture(t, filepath.Join(root, "go.mod"), "module example.com/fullfixture\n\ngo 1.24\n")
	writeFullFixture(t, filepath.Join(root, "fixture.go"), `package fixture

func Greeting() string { return "hello" }
`)
	excluded := filepath.Join(root, "excluded")
	if err := os.MkdirAll(excluded, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", excluded, err)
	}
	writeFullFixture(t, filepath.Join(excluded, "invalid.go"), `package invalid

var _ = missing.Symbol
`)

	set, report, err := Full(context.Background(), FullOptions{
		Repositories: []workspace.Repository{{
			Name:       "fixture",
			Path:       root,
			RealPath:   root,
			Languages:  []string{"go"},
			Exclusions: []string{"excluded"},
		}},
		SyntheticWorkFile: filepath.Join(testsupport.TempDir(t), "go.work"),
	})
	if err != nil {
		t.Fatalf("Full() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("full facts validation error = %v", err)
	}
	if report.GoLoads != 1 || report.GoLoadErrors != 0 || report.GoDefinitions == 0 {
		t.Fatalf("full report = %+v, want one clean load with definitions", report)
	}
}

// A directory that the build configuration excludes is not a broken index: it
// contributes no symbol, the pass completes, and the gap is declared as an
// unresolved reference instead of being dropped. Requesting the tag brings
// the same package into the graph.
func TestFullIndexesPackagesGuardedByBuildTags(t *testing.T) {
	fixture := func(t *testing.T) string {
		t.Helper()
		root := testsupport.TempDir(t)
		writeFullFixture(t, filepath.Join(root, "go.mod"), "module example.com/fullfixture\n\ngo 1.24\n")
		writeFullFixture(t, filepath.Join(root, "fixture.go"), `package fixture

func Greeting() string { return "hello" }
`)
		writeFullFixture(t, filepath.Join(root, "tagged", "tagged.go"), `//go:build fixturetag

package tagged

func Tagged() string { return "tagged" }
`)
		return root
	}
	repository := func(root string) workspace.Repository {
		return workspace.Repository{Name: "fixture", Path: root, RealPath: root, Languages: []string{"go"}}
	}

	root := fixture(t)
	set, report, err := Full(context.Background(), FullOptions{
		Repositories:      []workspace.Repository{repository(root)},
		SyntheticWorkFile: filepath.Join(testsupport.TempDir(t), "go.work"),
	})
	if err != nil {
		t.Fatalf("Full() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("full facts validation error = %v", err)
	}
	if report.GoLoadErrors != 0 || report.GoLoadDiagnostics == 0 {
		t.Fatalf("full report = %+v, want no blocking error and a declared diagnostic", report)
	}
	declared := false
	for _, entry := range set.Unresolved {
		if entry.Reason == "PACKAGE_NOT_BUILDABLE" && entry.RequestedPackage == "example.com/fullfixture/tagged" {
			declared = true
		}
	}
	if !declared {
		t.Fatalf("unresolved = %#v, want the excluded package declared", set.Unresolved)
	}
	for _, symbol := range set.Symbols {
		if symbol.Name == "Tagged" {
			t.Fatalf("excluded package contributed symbol %q", symbol.Name)
		}
	}

	tagged := fixture(t)
	set, report, err = Full(context.Background(), FullOptions{
		Repositories:      []workspace.Repository{repository(tagged)},
		SyntheticWorkFile: filepath.Join(testsupport.TempDir(t), "go.work"),
		GoBuildTags:       []string{"fixturetag"},
	})
	if err != nil {
		t.Fatalf("Full() with build tags error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("full facts validation error = %v", err)
	}
	if report.GoLoadErrors != 0 || report.GoLoadDiagnostics != 0 {
		t.Fatalf("full report = %+v, want a clean load once the tag is requested", report)
	}
	indexed := false
	for _, symbol := range set.Symbols {
		if symbol.Name == "Tagged" {
			indexed = true
		}
	}
	if !indexed {
		t.Fatalf("symbols = %d, want the tagged package indexed", len(set.Symbols))
	}
}

// A repository written for a newer Go than this build can type-check must fail
// the pass by name. Publishing a generation that silently lacks a registered
// repository is worse than refusing to publish one.
func TestFullRejectsARepositoryAboveTheSupportedLanguageVersion(t *testing.T) {
	root := testsupport.TempDir(t)
	writeFullFixture(t, filepath.Join(root, "go.mod"), "module example.com/future\n\ngo 1.99.0\n")
	writeFullFixture(t, filepath.Join(root, "fixture.go"), "package fixture\n\nfunc Greeting() string { return \"hello\" }\n")

	_, _, err := Full(context.Background(), FullOptions{
		Repositories: []workspace.Repository{{
			Name: "future", Path: root, RealPath: root, Languages: []string{"go"},
		}},
		SyntheticWorkFile: filepath.Join(testsupport.TempDir(t), "go.work"),
	})
	if err == nil {
		t.Fatal("Full() error = nil, want the unsupported language version reported")
	}
	if !errors.Is(err, goworkspace.ErrGoVersionUnsupported) {
		t.Fatalf("Full() error = %v, want ErrGoVersionUnsupported", err)
	}
	for _, want := range []string{"future", "1.99.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Full() error = %q, want it to name %q", err, want)
		}
	}
}

// A repository indexed by several passes -- one per Go module, plus
// TypeScript -- must still declare each language once. The merge carries the
// languages of both sides, so a union appended on top of them multiplies every
// entry by the number of merged fact sets, and the count reaches the published
// snapshot.
func TestFullReportsEachRepositoryLanguageOnce(t *testing.T) {
	root := testsupport.TempDir(t)
	writeFullFixture(t, filepath.Join(root, "go.mod"), "module example.com/multi\n\ngo 1.24\n")
	writeFullFixture(t, filepath.Join(root, "fixture.go"), "package fixture\n\nfunc Greeting() string { return \"hello\" }\n")
	writeFullFixture(t, filepath.Join(root, "tools", "go.mod"), "module example.com/multi/tools\n\ngo 1.24\n")
	writeFullFixture(t, filepath.Join(root, "tools", "tool.go"), "package tools\n\nfunc Name() string { return \"tool\" }\n")

	set, _, err := Full(context.Background(), FullOptions{
		Repositories: []workspace.Repository{{
			Name: "multi", Path: root, RealPath: root, Languages: []string{"go"},
		}},
		SyntheticWorkFile: filepath.Join(testsupport.TempDir(t), "go.work"),
	})
	if err != nil {
		t.Fatalf("Full() error = %v", err)
	}
	if len(set.Repositories) != 1 {
		t.Fatalf("repositories = %#v, want one", set.Repositories)
	}
	seen := make(map[facts.Language]int)
	for _, language := range set.Repositories[0].Languages {
		seen[language]++
	}
	for language, count := range seen {
		if count != 1 {
			t.Fatalf("language %q declared %d times: %#v", language, count, set.Repositories[0].Languages)
		}
	}
}

// Two repositories that never name each other must not share a build list.
// One workspace resolves a single minimum version selection for everything it
// uses, so a dependency bumped in one repository changes the versions selected
// for the other, and a version neither downloaded breaks both loads at once.
// A repository that does require the other still resolves through one
// workspace, because that is what makes the cross-repository edge exact.
func TestFullIsolatesRepositoriesThatDoNotReachEachOther(t *testing.T) {
	independent := func(t *testing.T) []workspace.Repository {
		t.Helper()
		root := testsupport.TempDir(t)
		alone := filepath.Join(root, "alone")
		other := filepath.Join(root, "other")
		writeFullFixture(t, filepath.Join(alone, "go.mod"), "module example.com/alone\n\ngo 1.24\n")
		writeFullFixture(t, filepath.Join(alone, "alone.go"), "package alone\n\nfunc Alone() string { return \"alone\" }\n")
		writeFullFixture(t, filepath.Join(other, "go.mod"), "module example.com/other\n\ngo 1.24\n")
		writeFullFixture(t, filepath.Join(other, "other.go"), "package other\n\nfunc Other() string { return \"other\" }\n")
		return []workspace.Repository{
			{Name: "alone", Path: alone, RealPath: alone, Languages: []string{"go"}},
			{Name: "other", Path: other, RealPath: other, Languages: []string{"go"}},
		}
	}

	workFile := filepath.Join(testsupport.TempDir(t), "go.work")
	_, report, err := Full(context.Background(), FullOptions{
		Repositories: independent(t), SyntheticWorkFile: workFile,
	})
	if err != nil {
		t.Fatalf("Full() error = %v", err)
	}
	if report.GoModules != 2 || report.GoWorkspaces != 0 {
		t.Fatalf("report = %+v, want two modules and no shared workspace", report)
	}

	root := testsupport.TempDir(t)
	provider := filepath.Join(root, "provider")
	consumer := filepath.Join(root, "consumer")
	writeFullFixture(t, filepath.Join(provider, "go.mod"), "module example.com/provider\n\ngo 1.24\n")
	writeFullFixture(t, filepath.Join(provider, "value.go"), "package provider\n\n// Value is the shared fact.\nconst Value = 41\n")
	writeFullFixture(t, filepath.Join(consumer, "go.mod"), "module example.com/consumer\n\ngo 1.24\n\nrequire example.com/provider v0.0.0\n")
	writeFullFixture(t, filepath.Join(consumer, "main.go"), "package consumer\n\nimport \"example.com/provider\"\n\n// Total reads the provider.\nfunc Total() int { return provider.Value }\n")

	shared := filepath.Join(testsupport.TempDir(t), "go.work")
	_, report, err = Full(context.Background(), FullOptions{
		Repositories: []workspace.Repository{
			{Name: "provider", Path: provider, RealPath: provider, Languages: []string{"go"}},
			{Name: "consumer", Path: consumer, RealPath: consumer, Languages: []string{"go"}},
		},
		SyntheticWorkFile: shared,
	})
	if err != nil {
		t.Fatalf("Full() error = %v", err)
	}
	if report.GoWorkspaces != 1 {
		t.Fatalf("workspaces = %d, want one shared workspace", report.GoWorkspaces)
	}
	if _, err := os.Stat(shared); err != nil {
		t.Fatalf("shared workspace %q: %v", shared, err)
	}
}

// The units run concurrently, so the published graph must not depend on the
// order they finished. Merging follows the order of the units, and the same
// corpus indexed twice has to produce byte-identical facts -- that equality is
// what makes the concurrency safe to ship.
func TestFullProducesTheSameFactsOnEveryRun(t *testing.T) {
	root := testsupport.TempDir(t)
	for index := range 6 {
		module := filepath.Join(root, fmt.Sprintf("svc-%d", index))
		writeFullFixture(t, filepath.Join(module, "go.mod"),
			fmt.Sprintf("module example.com/svc%d\n\ngo 1.24\n", index))
		writeFullFixture(t, filepath.Join(module, "a.go"),
			fmt.Sprintf("package svc%d\n\n// Value is a fact.\nfunc Value() int { return %d }\n", index, index))
	}
	repositories := make([]workspace.Repository, 0, 6)
	for index := range 6 {
		module := filepath.Join(root, fmt.Sprintf("svc-%d", index))
		repositories = append(repositories, workspace.Repository{
			Name: fmt.Sprintf("svc-%d", index), Path: module, RealPath: module, Languages: []string{"go"},
		})
	}

	encode := func(t *testing.T) string {
		t.Helper()
		set, _, err := Full(context.Background(), FullOptions{
			Repositories:      repositories,
			SyntheticWorkFile: filepath.Join(testsupport.TempDir(t), "go.work"),
		})
		if err != nil {
			t.Fatalf("Full() error = %v", err)
		}
		encoded, err := json.Marshal(set)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		return string(encoded)
	}
	first := encode(t)
	for range 3 {
		if again := encode(t); again != first {
			t.Fatal("two runs of the same corpus produced different facts")
		}
	}
}

// A budget of one is the sequential pass. It has to produce exactly what the
// concurrent one does, or the concurrency changed the graph.
func TestFullIsIndifferentToTheConcurrencyBudget(t *testing.T) {
	root := testsupport.TempDir(t)
	for index := range 4 {
		module := filepath.Join(root, fmt.Sprintf("svc-%d", index))
		writeFullFixture(t, filepath.Join(module, "go.mod"),
			fmt.Sprintf("module example.com/svc%d\n\ngo 1.24\n", index))
		writeFullFixture(t, filepath.Join(module, "a.go"),
			fmt.Sprintf("package svc%d\n\nfunc Value() int { return %d }\n", index, index))
	}
	repositories := make([]workspace.Repository, 0, 4)
	for index := range 4 {
		module := filepath.Join(root, fmt.Sprintf("svc-%d", index))
		repositories = append(repositories, workspace.Repository{
			Name: fmt.Sprintf("svc-%d", index), Path: module, RealPath: module, Languages: []string{"go"},
		})
	}

	encode := func(t *testing.T, loads int) string {
		t.Helper()
		set, _, err := Full(context.Background(), FullOptions{
			Repositories:      repositories,
			SyntheticWorkFile: filepath.Join(testsupport.TempDir(t), "go.work"),
			GoMaximumLoads:    loads,
		})
		if err != nil {
			t.Fatalf("Full(loads=%d) error = %v", loads, err)
		}
		encoded, err := json.Marshal(set)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		return string(encoded)
	}
	if encode(t, 1) != encode(t, 8) {
		t.Fatal("the concurrency budget changed the facts")
	}
}

// A module the loader cannot read must not decide whether every other
// repository gets a graph. Its facts are absent -- they would not be
// trustworthy -- and the reason is declared where a consumer can find it.
func TestFullIsolatesAModuleThatCannotLoad(t *testing.T) {
	root := testsupport.TempDir(t)
	healthy := filepath.Join(root, "healthy")
	writeFullFixture(t, filepath.Join(healthy, "go.mod"), "module example.com/healthy\n\ngo 1.24\n")
	writeFullFixture(t, filepath.Join(healthy, "a.go"), "package healthy\n\nfunc Healthy() int { return 1 }\n")
	broken := filepath.Join(root, "broken")
	writeFullFixture(t, filepath.Join(broken, "go.mod"), "module example.com/broken\n\ngo 1.24\n")
	writeFullFixture(t, filepath.Join(broken, "a.go"),
		"package broken\n\nimport \"example.com/absent/dependency\"\n\nfunc Broken() int { return dependency.Value }\n")

	set, report, err := Full(context.Background(), FullOptions{
		Repositories: []workspace.Repository{
			{Name: "healthy", Path: healthy, RealPath: healthy, Languages: []string{"go"}},
			{Name: "broken", Path: broken, RealPath: broken, Languages: []string{"go"}},
		},
		SyntheticWorkFile: filepath.Join(testsupport.TempDir(t), "go.work"),
	})
	if err != nil {
		t.Fatalf("Full() error = %v, want the healthy repository indexed", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("full facts validation error = %v", err)
	}
	if report.GoModulesNotLoaded != 1 {
		t.Fatalf("modules not loaded = %d, want exactly the broken one", report.GoModulesNotLoaded)
	}

	declared := false
	for _, entry := range set.Unresolved {
		if entry.Reason == "MODULE_NOT_LOADED" && entry.RequestedPackage == "example.com/broken" {
			declared = true
			if !strings.Contains(entry.Detail, "absent/dependency") {
				t.Fatalf("detail = %q, want the observed diagnostic", entry.Detail)
			}
		}
	}
	if !declared {
		t.Fatalf("unresolved = %#v, want the module declared", set.Unresolved)
	}
	healthySymbols, brokenSymbols := 0, 0
	for _, symbol := range set.Symbols {
		switch symbol.Name {
		case "Healthy":
			healthySymbols++
		case "Broken":
			brokenSymbols++
		}
	}
	if healthySymbols != 1 {
		t.Fatal("the healthy repository lost its symbols to its neighbour")
	}
	if brokenSymbols != 0 {
		t.Fatal("a module that did not load contributed facts anyway")
	}
}

// A pass ends when its slowest unit ends, so the queue is drained longest
// first. The weight is a proxy over the files a unit will read, and the only
// thing that matters is that the heavy unit is not left for last.
func TestAnalysisQueueStartsWithTheHeaviestUnit(t *testing.T) {
	units := []analysisUnit{
		{repository: workspace.Repository{Name: "small"}, pkg: typeScriptPackageUnit{files: 3}},
		{repository: workspace.Repository{Name: "huge"}, pkg: typeScriptPackageUnit{files: 900}},
		{repository: workspace.Repository{Name: "medium"}, pkg: typeScriptPackageUnit{files: 40}},
	}
	if units[1].weight() <= units[2].weight() || units[2].weight() <= units[0].weight() {
		t.Fatalf("weights = %d/%d/%d, want the file counts to order the queue",
			units[0].weight(), units[1].weight(), units[2].weight())
	}

	var order []string
	_, _, err := analyse(context.Background(), FullOptions{
		TypeScriptMaximumWorkers: 1,
		Progress: func(event ProgressEvent) {
			if event.Started {
				order = append(order, event.Repository)
			}
		},
	}, units, analysisInputs{})
	// The units have no project, so the worker fails; the dispatch order is
	// still observable and is what this defends.
	if err == nil {
		t.Skip("the fixture units unexpectedly analysed")
	}
	if len(order) == 0 || order[0] != "huge" {
		t.Fatalf("dispatch order = %v, want the heaviest unit first", order)
	}
}

// A package name two manifests declare is an ambiguity, not a broken index:
// the pass completes, the name provides nothing, and the graph says so. The
// alternative -- what this used to do -- is that a fixture or a vendored copy
// anywhere in a repository makes every other repository unindexable.
func TestFullDeclaresAmbiguousTypeScriptPackages(t *testing.T) {
	root := testsupport.TempDir(t)
	for _, unit := range []struct{ directory, name string }{
		{"a", "@fixture/same"}, {"b", "@fixture/same"}, {"c", "@fixture/unique"},
	} {
		packageRoot := filepath.Join(root, unit.directory)
		writeFullFixture(t, filepath.Join(packageRoot, "package.json"), fmt.Sprintf(`{"name":"%s","version":"1.0.0"}`, unit.name))
		writeFullFixture(t, filepath.Join(packageRoot, "tsconfig.json"), `{"compilerOptions":{"strict":true}}`)
		writeFullFixture(t, filepath.Join(packageRoot, "src", "index.ts"), "export const value = 1;\n")
	}

	packages, conflicts, err := discoverTypeScriptPackages(context.Background(), []workspace.Repository{{
		Name: "repo", Path: root, RealPath: root, Languages: []string{"typescript"},
	}})
	if err != nil {
		t.Fatalf("discoverTypeScriptPackages() error = %v", err)
	}
	if len(conflicts) != 1 || conflicts[0].conflict.Name != "@fixture/same" {
		t.Fatalf("conflicts = %#v, want the duplicated name", conflicts)
	}
	names := make([]string, 0, len(packages))
	for _, unit := range packages {
		names = append(names, unit.packageValue.Name)
	}
	if !equalStringSlices(names, []string{"@fixture/unique"}) {
		t.Fatalf("packages = %v, want only the unambiguous package", names)
	}

	set := ambiguousPackageFacts(conflicts[0])
	if err := set.Validate(); err != nil {
		t.Fatalf("ambiguity facts validation error = %v", err)
	}
	entry := set.Unresolved[0]
	if entry.Reason != "AMBIGUOUS_PACKAGE_PROVIDER" || entry.RequestedPackage != "@fixture/same" {
		t.Fatalf("unresolved = %#v, want the ambiguous name declared", entry)
	}
	if !strings.Contains(entry.Detail, "package.json") {
		t.Fatalf("detail = %q, want the observed manifests", entry.Detail)
	}
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestDiscoverTypeScriptPackagesUsesEachProjectAndSkipsUnconfiguredPackages(t *testing.T) {
	root := testsupport.TempDir(t)
	writeFullFixture(t, filepath.Join(root, "pnpm-workspace.yaml"), "packages:\n  - packages/*\n")
	writeFullFixture(t, filepath.Join(root, "package.json"), `{"name":"@example/root"}`)
	packageNames := []struct {
		name      string
		directory string
	}{
		{name: "@example/a", directory: "a"},
		{name: "@example/b", directory: "b"},
	}
	for _, packageInfo := range packageNames {
		packageRoot := filepath.Join(root, "packages", packageInfo.directory)
		writeFullFixture(t, filepath.Join(packageRoot, "package.json"), fmt.Sprintf(`{"name":"%s"}`, packageInfo.name))
		writeFullFixture(t, filepath.Join(packageRoot, "tsconfig.json"), `{"compilerOptions":{"strict":true}}`)
	}

	packages, conflicts, err := discoverTypeScriptPackages(context.Background(), []workspace.Repository{{
		Name: "repo", Path: root, RealPath: root, Languages: []string{"typescript"},
	}})
	if err != nil {
		t.Fatalf("discoverTypeScriptPackages() error = %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("conflicts = %#v, want none", conflicts)
	}
	if len(packages) != 2 {
		t.Fatalf("packages = %#v, want only configured package projects", packages)
	}
	for index, packageInfo := range packageNames {
		if packages[index].packageValue.Name != packageInfo.name {
			t.Fatalf("packages[%d].Name = %q, want %q", index, packages[index].packageValue.Name, packageInfo.name)
		}
		wantProject := filepath.Join(root, "packages", packageInfo.directory, "tsconfig.json")
		if packages[index].packageValue.ProjectPath != wantProject {
			t.Fatalf("packages[%d].ProjectPath = %q, want %q", index, packages[index].packageValue.ProjectPath, wantProject)
		}
	}
}

func TestFullRejectsGoIndexWithoutSyntheticWorkFile(t *testing.T) {
	root := testsupport.TempDir(t)
	set, report, err := Full(context.Background(), FullOptions{
		Repositories: []workspace.Repository{{
			Name:      "fixture",
			Path:      root,
			RealPath:  root,
			Languages: []string{"go"},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "synthetic Go work file is required") {
		t.Fatalf("Full() error = %v, want missing synthetic work file", err)
	}
	if len(set.Repositories) != 0 || report.GoRepositories != 1 {
		t.Fatalf("partial full result = set=%+v report=%+v", set, report)
	}
}

func TestFullRejectsUnsupportedLanguageBeforeIndexing(t *testing.T) {
	_, report, err := Full(context.Background(), FullOptions{
		Repositories: []workspace.Repository{{
			Name:      "fixture",
			Path:      "/does/not/matter",
			RealPath:  "/does/not/matter",
			Languages: []string{"rust"},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported language "rust"`) {
		t.Fatalf("Full() error = %v, want unsupported language", err)
	}
	if report.GoRepositories != 0 || report.TypeScriptRepositories != 0 {
		t.Fatalf("report after unsupported language = %+v, want no work", report)
	}
}

func writeFullFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create fixture directory %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture %q: %v", path, err)
	}
}

// TestMergeSetsUnionsTheLanguagesOfEveryUnit keeps a repository that holds
// both languages from losing one of them. Each unit declares only the
// language it analysed, and the merge keeps the first record of a key: a
// repository whose Go module happened to be merged before its TypeScript
// package would otherwise be published as a Go-only repository.
func TestMergeSetsUnionsTheLanguagesOfEveryUnit(t *testing.T) {
	repository := func(key string, languages ...facts.Language) facts.Set {
		return facts.Set{Repositories: []facts.Repository{{
			Key: key, Name: key, RootPath: "/repos/" + key, Languages: languages,
		}}}
	}

	merged := mergeSets([]facts.Set{
		repository("mixed", facts.LanguageGo),
		repository("go-only", facts.LanguageGo),
		repository("mixed", facts.LanguageTypeScript),
		repository("mixed", facts.LanguageGo),
	})

	if len(merged.Repositories) != 2 {
		t.Fatalf("repositories = %d, want the duplicate keys merged", len(merged.Repositories))
	}
	byKey := make(map[string][]facts.Language, len(merged.Repositories))
	for _, entry := range merged.Repositories {
		byKey[entry.Key] = entry.Languages
	}
	want := map[string][]facts.Language{
		"go-only": {facts.LanguageGo},
		"mixed":   {facts.LanguageGo, facts.LanguageTypeScript},
	}
	for key, languages := range want {
		if fmt.Sprint(byKey[key]) != fmt.Sprint(languages) {
			t.Fatalf("%s languages = %v, want %v", key, byKey[key], languages)
		}
	}
}
