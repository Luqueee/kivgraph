package indexer

import (
	"context"
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
