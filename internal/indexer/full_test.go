package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if _, err := os.Stat(workFile); err != nil {
		t.Fatalf("synthetic work file %q: %v", workFile, err)
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

	packages, err := discoverTypeScriptPackages(context.Background(), []workspace.Repository{{
		Name: "repo", Path: root, RealPath: root, Languages: []string{"typescript"},
	}})
	if err != nil {
		t.Fatalf("discoverTypeScriptPackages() error = %v", err)
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
