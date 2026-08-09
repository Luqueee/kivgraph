package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/testsupport"
)

func TestNewGoModuleRegistryBuildsProvidersPackagesAndReplaces(t *testing.T) {
	root := testsupport.TempDir(t)
	nested := filepath.Join(root, "nested")
	internal := filepath.Join(root, "internal", "util")
	for _, directory := range []string{nested, internal} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", directory, err)
		}
	}
	writeGoDiscoveryFile(t, filepath.Join(root, "go.mod"), `module example.com/root

go 1.24

replace example.com/local => ./nested
`)
	writeGoDiscoveryFile(t, filepath.Join(root, "go.work"), `go 1.24

use (
  .
  ./nested
)

replace example.com/work => ./nested
`)
	writeGoDiscoveryFile(t, filepath.Join(nested, "go.mod"), "module example.com/nested\n\ngo 1.23\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	writeGoDiscoveryFile(t, filepath.Join(internal, "util.go"), "package util\n\nfunc Value() int { return 1 }\n")
	writeGoDiscoveryFile(t, filepath.Join(nested, "nested.go"), "package nested\n")

	registry, err := NewGoModuleRegistry(context.Background(), Repository{Name: "repo", RealPath: root})
	if err != nil {
		t.Fatalf("NewGoModuleRegistry() error = %v", err)
	}
	modules := registry.List()
	if len(modules) != 2 {
		t.Fatalf("List() returned %d modules, want 2: %#v", len(modules), modules)
	}
	if modules[0].ModulePath != "example.com/nested" || modules[1].ModulePath != "example.com/root" {
		t.Fatalf("List() order = %#v, want nested then root", modules)
	}

	nestedModule, ok := registry.Get("example.com/nested")
	if !ok {
		t.Fatal("Get(nested) reported module as missing")
	}
	if nestedModule.Repository != "repo" || nestedModule.RootPath != nested || nestedModule.ManifestPath != filepath.Join(nested, "go.mod") || nestedModule.GoVersion != "1.23" {
		t.Fatalf("nested metadata = %#v", nestedModule)
	}
	if len(nestedModule.Packages) != 1 || nestedModule.Packages[0].ImportPath != "example.com/nested" {
		t.Fatalf("nested packages = %#v", nestedModule.Packages)
	}
	if len(nestedModule.Replaces) != 0 {
		t.Fatalf("nested module replacements = %#v", nestedModule.Replaces)
	}
	if len(nestedModule.WorkspaceReplaces) != 1 || nestedModule.WorkspaceReplaces[0].OldPath != "example.com/work" || nestedModule.WorkspaceReplaces[0].NewLocalPath != nested {
		t.Fatalf("nested workspace replacements = %#v", nestedModule.WorkspaceReplaces)
	}

	rootModule, ok := registry.Get("example.com/root")
	if !ok {
		t.Fatal("Get(root) reported module as missing")
	}
	if rootModule.SumPath != "" || len(rootModule.Packages) != 2 {
		t.Fatalf("root metadata = %#v", rootModule)
	}
	if rootModule.Packages[0].Directory != root || rootModule.Packages[1].Directory != internal {
		t.Fatalf("root package order = %#v", rootModule.Packages)
	}
	if len(rootModule.Replaces) != 1 || rootModule.Replaces[0].OldPath != "example.com/local" || rootModule.Replaces[0].NewLocalPath != nested {
		t.Fatalf("root module replacements = %#v", rootModule.Replaces)
	}
	if len(rootModule.WorkspaceReplaces) != 1 || rootModule.WorkspaceReplaces[0].OldPath != "example.com/work" {
		t.Fatalf("root workspace replacements = %#v", rootModule.WorkspaceReplaces)
	}

	rootModule.Packages[0].Files[0] = "mutated.go"
	rootModule.Replaces[0].OldPath = "mutated/module"
	fresh, ok := registry.Get("example.com/root")
	if !ok || fresh.Packages[0].Files[0] == "mutated.go" || fresh.Replaces[0].OldPath == "mutated/module" {
		t.Fatalf("registry returned mutable internal state: %#v", fresh)
	}
}

func TestNewGoModuleRegistryRejectsDuplicateModulePaths(t *testing.T) {
	root := testsupport.TempDir(t)
	writeGoDiscoveryFile(t, filepath.Join(root, "a", "go.mod"), "module example.com/duplicate\n\ngo 1.24\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "b", "go.mod"), "module example.com/duplicate\n\ngo 1.24\n")
	_, err := NewGoModuleRegistry(context.Background(), Repository{RealPath: root})
	if err == nil || !strings.Contains(err.Error(), "duplicate Go module path") {
		t.Fatalf("NewGoModuleRegistry() error = %v, want duplicate module error", err)
	}
}

func TestNewGoModuleRegistrySkipsPackagesOutsideModulesAndHonorsCancellation(t *testing.T) {
	root := testsupport.TempDir(t)
	writeGoDiscoveryFile(t, filepath.Join(root, "standalone.go"), "package standalone\n")
	registry, err := NewGoModuleRegistry(context.Background(), Repository{RealPath: root})
	if err != nil {
		t.Fatalf("NewGoModuleRegistry() error = %v", err)
	}
	if modules := registry.List(); len(modules) != 0 {
		t.Fatalf("standalone package produced module providers: %#v", modules)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewGoModuleRegistry(ctx, Repository{RealPath: root})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled registry error = %v, want context.Canceled", err)
	}
}
