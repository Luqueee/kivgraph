package goworkspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/testsupport"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

func TestBuildPlanComposesEveryModuleAndAgreedReplacements(t *testing.T) {
	root := testsupport.TempDir(t)
	first := writeModule(t, filepath.Join(root, "first"), `module example.com/first

go 1.23

replace example.com/vendored => example.com/vendored/v2 v2.1.0
`)
	second := writeModule(t, filepath.Join(root, "second"), `module example.com/second

go 1.24

replace example.com/vendored => example.com/vendored/v2 v2.1.0
`)
	nested := writeModule(t, filepath.Join(first, "tools"), `module example.com/first/tools

go 1.22
`)

	plan, err := BuildPlan(context.Background(), []workspace.Repository{
		{Name: "first", Path: first, RealPath: first},
		{Name: "second", Path: second, RealPath: second},
	}, Options{})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}

	if plan.GoVersion != "1.24" {
		t.Fatalf("GoVersion = %q, want the highest declared version", plan.GoVersion)
	}
	wantModules := []string{"example.com/first", "example.com/first/tools", "example.com/second"}
	if got := modulePaths(plan); !equalStrings(got, wantModules) {
		t.Fatalf("modules = %v, want %v", got, wantModules)
	}
	if plan.Modules[1].RootPath != nested {
		t.Fatalf("nested module root = %q, want %q", plan.Modules[1].RootPath, nested)
	}
	if len(plan.Replaces) != 1 || plan.Replaces[0].OldPath != "example.com/vendored" ||
		plan.Replaces[0].NewPath != "example.com/vendored/v2" || plan.Replaces[0].NewVersion != "v2.1.0" {
		t.Fatalf("replaces = %#v, want the single agreed directive", plan.Replaces)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("conflicts = %#v, want none", plan.Conflicts)
	}

	rendered, err := plan.Render()
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	contents := string(rendered)
	for _, fragment := range []string{
		"go 1.24",
		filepath.ToSlash(first),
		filepath.ToSlash(nested),
		filepath.ToSlash(second),
		"replace example.com/vendored => example.com/vendored/v2 v2.1.0",
	} {
		if !strings.Contains(contents, fragment) {
			t.Fatalf("rendered workspace missing %q:\n%s", fragment, contents)
		}
	}

	repeated, err := plan.Render()
	if err != nil || string(repeated) != contents {
		t.Fatalf("Render() is not deterministic: %v", err)
	}
}

func TestBuildPlanUsesDiscoveredPackagePatterns(t *testing.T) {
	root := testsupport.TempDir(t)
	module := writeModule(t, root, `module example.com/root

go 1.24
`)
	benchmarkDirectory := filepath.Join(module, "benchmarks")
	if err := os.MkdirAll(benchmarkDirectory, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(benchmarkDirectory, "invalid.go"), []byte("package invalid\n\nvar _ = missing.Symbol\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	plan, err := BuildPlan(context.Background(), []workspace.Repository{{
		Name: "root", Path: root, RealPath: root, Exclusions: []string{"**/benchmarks"},
	}}, Options{})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if len(plan.Modules) != 1 || !equalStrings(plan.Modules[0].PackagePatterns, []string{"example.com/root"}) {
		t.Fatalf("package patterns = %#v, want only the included package", plan.Modules)
	}
}

func TestBuildPlanExcludesUndecidableFacts(t *testing.T) {
	root := testsupport.TempDir(t)
	left := writeModule(t, filepath.Join(root, "left"), `module example.com/shared

go 1.24

replace example.com/pinned => example.com/pinned v1.0.0
`)
	right := writeModule(t, filepath.Join(root, "right"), `module example.com/shared

go 1.24
`)
	consumer := writeModule(t, filepath.Join(root, "consumer"), `module example.com/consumer

go 1.24

replace example.com/pinned => example.com/pinned v2.0.0
`)

	plan, err := BuildPlan(context.Background(), []workspace.Repository{
		{Name: "left", Path: left, RealPath: left},
		{Name: "right", Path: right, RealPath: right},
		{Name: "consumer", Path: consumer, RealPath: consumer},
	}, Options{})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}

	if got := modulePaths(plan); !equalStrings(got, []string{"example.com/consumer"}) {
		t.Fatalf("modules = %v, want only the unambiguous module", got)
	}
	// go refuses to load a workspace whose members disagree on a replacement,
	// so the workspace overrides it deterministically and reports the conflict.
	if len(plan.Replaces) != 1 ||
		plan.Replaces[0].OldPath != "example.com/pinned" ||
		plan.Replaces[0].NewVersion != "v1.0.0" {
		t.Fatalf("replaces = %#v, want the deterministic override", plan.Replaces)
	}
	if len(plan.Conflicts) != 2 {
		t.Fatalf("conflicts = %#v, want ambiguous module and replace conflict", plan.Conflicts)
	}
	if plan.Conflicts[0].Kind != AmbiguousModule || plan.Conflicts[0].Subject != "example.com/shared" {
		t.Fatalf("conflicts[0] = %#v", plan.Conflicts[0])
	}
	if !equalStrings(plan.Conflicts[0].Repositories, []string{"left", "right"}) {
		t.Fatalf("ambiguous repositories = %v", plan.Conflicts[0].Repositories)
	}
	if plan.Conflicts[1].Kind != ReplaceConflict || plan.Conflicts[1].Subject != "example.com/pinned" {
		t.Fatalf("conflicts[1] = %#v", plan.Conflicts[1])
	}
}

func TestBuildPlanPreservesDuplicateModulesWithinRepositoryAsConflict(t *testing.T) {
	root := testsupport.TempDir(t)
	writeModule(t, filepath.Join(root, "unique"), `module example.com/unique

go 1.24
`)
	writeModule(t, filepath.Join(root, "duplicate-a"), `module example.com/duplicate

go 1.24
`)
	writeModule(t, filepath.Join(root, "duplicate-b"), `module example.com/duplicate

go 1.24
`)

	plan, err := BuildPlan(context.Background(), []workspace.Repository{{
		Name: "repo", Path: root, RealPath: root,
	}}, Options{})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if got := modulePaths(plan); !equalStrings(got, []string{"example.com/unique"}) {
		t.Fatalf("modules = %v, want only the unambiguous module", got)
	}
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Kind != AmbiguousModule ||
		plan.Conflicts[0].Subject != "example.com/duplicate" {
		t.Fatalf("conflicts = %#v, want one duplicate-module conflict", plan.Conflicts)
	}
	if !equalStrings(plan.Conflicts[0].Repositories, []string{"repo"}) {
		t.Fatalf("conflict repositories = %v, want the declaring repository", plan.Conflicts[0].Repositories)
	}
}

func TestBuildPlanSkipsReplacementOfAWorkspaceModule(t *testing.T) {
	root := testsupport.TempDir(t)
	provider := writeModule(t, filepath.Join(root, "provider"), `module example.com/provider

go 1.24
`)
	consumer := writeModule(t, filepath.Join(root, "consumer"), `module example.com/consumer

go 1.24

replace example.com/provider => example.com/provider/v2 v2.0.0
`)
	// `use` already provides example.com/provider, so promoting a replacement
	// for it would shadow the very module the workspace loads from disk.
	plan, err := BuildPlan(context.Background(), []workspace.Repository{
		{Name: "provider", Path: provider, RealPath: provider},
		{Name: "consumer", Path: consumer, RealPath: consumer},
	}, Options{})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if len(plan.Replaces) != 0 {
		t.Fatalf("replaces = %#v, want none: use already provides the module", plan.Replaces)
	}
	if got := modulePaths(plan); !equalStrings(got, []string{"example.com/consumer", "example.com/provider"}) {
		t.Fatalf("modules = %v", got)
	}
}

func TestBuildPlanRejectsAWorkspaceWithoutModules(t *testing.T) {
	empty := testsupport.TempDir(t)
	_, err := BuildPlan(context.Background(), []workspace.Repository{
		{Name: "empty", Path: empty, RealPath: empty},
	}, Options{})
	if !errors.Is(err, ErrNoModules) {
		t.Fatalf("BuildPlan() error = %v, want ErrNoModules", err)
	}
}

func TestWriteInstallsOutsideRepositoriesAndIsIdempotent(t *testing.T) {
	root := testsupport.TempDir(t)
	module := writeModule(t, filepath.Join(root, "module"), `module example.com/module

go 1.24
`)
	repositories := []workspace.Repository{{Name: "module", Path: module, RealPath: module}}
	plan, err := BuildPlan(context.Background(), repositories, Options{})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}

	state := filepath.Join(root, "state", "ladygraph")
	target := filepath.Join(state, "go.work")
	first, err := Write(context.Background(), target, plan, repositories)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !first.Changed || first.Path != target {
		t.Fatalf("first write = %#v", first)
	}
	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read workspace: %v", err)
	}
	if !strings.Contains(string(contents), filepath.ToSlash(module)) {
		t.Fatalf("workspace does not use the module:\n%s", contents)
	}

	second, err := Write(context.Background(), target, plan, repositories)
	if err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	if second.Changed {
		t.Fatalf("second write reported a change for identical content")
	}

	entries, err := os.ReadDir(state)
	if err != nil {
		t.Fatalf("read state directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "go.work" {
		t.Fatalf("state directory = %#v, want only go.work", entries)
	}
}

func TestWriteRefusesAPathInsideARepository(t *testing.T) {
	root := testsupport.TempDir(t)
	module := writeModule(t, filepath.Join(root, "module"), `module example.com/module

go 1.24
`)
	repositories := []workspace.Repository{{Name: "module", Path: module, RealPath: module}}
	plan, err := BuildPlan(context.Background(), repositories, Options{})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}

	target := filepath.Join(module, "go.work")
	if _, err := Write(context.Background(), target, plan, repositories); !errors.Is(err, ErrRepositoryTarget) {
		t.Fatalf("Write() error = %v, want ErrRepositoryTarget", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("workspace was written inside the repository: %v", err)
	}
}

func TestBuildPlanHonoursCancellation(t *testing.T) {
	root := testsupport.TempDir(t)
	module := writeModule(t, filepath.Join(root, "module"), `module example.com/module

go 1.24
`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := BuildPlan(ctx, []workspace.Repository{
		{Name: "module", Path: module, RealPath: module},
	}, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildPlan() error = %v, want context.Canceled", err)
	}
}

func writeModule(t *testing.T, directory, manifest string) string {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create %q: %v", directory, err)
	}
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest in %q: %v", directory, err)
	}
	if err := os.WriteFile(filepath.Join(directory, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write source in %q: %v", directory, err)
	}
	return directory
}

func modulePaths(plan Plan) []string {
	paths := make([]string, 0, len(plan.Modules))
	for _, module := range plan.Modules {
		paths = append(paths, module.ModulePath)
	}
	return paths
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
