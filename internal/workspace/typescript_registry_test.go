package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewTypeScriptPackageRegistryBuildsProvidersAndRoots(t *testing.T) {
	root := t.TempDir()
	core := filepath.Join(root, "packages", "core")
	if err := os.MkdirAll(filepath.Join(core, "src"), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{
  "name": "@example/root",
  "version": "1.0.0",
  "private": true,
  "exports": {
    ".": {"types": "./types/index.d.ts", "import": "./dist/index.js"},
    "./feature": "./dist/feature.js"
  },
  "types": "./types/index.d.ts",
  "workspaces": ["packages/*"]
}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{
  // root project
  "compilerOptions": {"rootDir": "src", "declarationDir": "types"},
  "include": ["src/**/*.ts"]
}`)
	writeDiscoveryFile(t, filepath.Join(core, "package.json"), `{
  "name": "@example/core",
  "version": "2.3.4",
  "typings": "./dist/index.d.ts",
  "source": "./src/index.ts"
}`)
	writeDiscoveryFile(t, filepath.Join(core, "tsconfig.json"), `{
  "compilerOptions": {"rootDirs": ["src", "generated"], "declarationDir": "dist"},
  "files": ["src/index.ts"]
}`)

	registry, err := NewTypeScriptPackageRegistry(context.Background(), Repository{Name: "repo", RealPath: root})
	if err != nil {
		t.Fatalf("NewTypeScriptPackageRegistry() error = %v", err)
	}
	packages := registry.List()
	if len(packages) != 2 {
		t.Fatalf("List() returned %d packages, want 2: %#v", len(packages), packages)
	}
	if packages[0].Name != "@example/core" || packages[1].Name != "@example/root" {
		t.Fatalf("List() order = %#v, want core then root", packages)
	}

	rootPackage, ok := registry.Get("@example/root")
	if !ok {
		t.Fatal("Get(root) reported package as missing")
	}
	if rootPackage.Repository != "repo" || rootPackage.Version != "1.0.0" || !rootPackage.Private {
		t.Fatalf("root metadata = %#v", rootPackage)
	}
	if rootPackage.RootPath != root || rootPackage.ManifestPath != filepath.Join(root, "package.json") {
		t.Fatalf("root paths = %#v", rootPackage)
	}
	if rootPackage.TypesPath != filepath.Join(root, "types", "index.d.ts") {
		t.Fatalf("root TypesPath = %q", rootPackage.TypesPath)
	}
	if rootPackage.ProjectPath != filepath.Join(root, "tsconfig.json") {
		t.Fatalf("root ProjectPath = %q", rootPackage.ProjectPath)
	}
	if !equalStrings(rootPackage.SourceRoots, []string{filepath.Join(root, "src")}) {
		t.Fatalf("root SourceRoots = %#v", rootPackage.SourceRoots)
	}
	if !equalStrings(rootPackage.DeclarationRoots, []string{filepath.Join(root, "types")}) {
		t.Fatalf("root DeclarationRoots = %#v", rootPackage.DeclarationRoots)
	}
	if !strings.Contains(string(rootPackage.Exports), `"./feature"`) {
		t.Fatalf("root Exports = %s", rootPackage.Exports)
	}

	corePackage, ok := registry.Get("@example/core")
	if !ok {
		t.Fatal("Get(core) reported package as missing")
	}
	if corePackage.TypesPath != filepath.Join(core, "dist", "index.d.ts") {
		t.Fatalf("core TypesPath = %q", corePackage.TypesPath)
	}
	if corePackage.ProjectPath != filepath.Join(core, "tsconfig.json") {
		t.Fatalf("core ProjectPath = %q", corePackage.ProjectPath)
	}
	if !equalStrings(corePackage.SourceRoots, []string{filepath.Join(core, "src"), filepath.Join(core, "generated")}) {
		t.Fatalf("core SourceRoots = %#v", corePackage.SourceRoots)
	}
	if !equalStrings(corePackage.DeclarationRoots, []string{filepath.Join(core, "dist")}) {
		t.Fatalf("core DeclarationRoots = %#v", corePackage.DeclarationRoots)
	}

	rootPackage.Exports[0] = 'x'
	rootPackage.SourceRoots[0] = "mutated"
	fresh, ok := registry.Get("@example/root")
	if !ok || fresh.Exports[0] == 'x' || fresh.SourceRoots[0] == "mutated" {
		t.Fatalf("registry returned mutable internal state: %#v", fresh)
	}
}

func TestNewTypeScriptPackageRegistryRejectsDuplicateAndEscapingProviders(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(string)
		wantError string
	}{
		{
			name: "duplicate names",
			setup: func(root string) {
				writeDiscoveryFile(t, filepath.Join(root, "a", "package.json"), `{"name":"same","version":"1.0.0"}`)
				writeDiscoveryFile(t, filepath.Join(root, "b", "package.json"), `{"name":"same","version":"2.0.0"}`)
			},
			wantError: "duplicate TypeScript package name",
		},
		{
			name: "types escape",
			setup: func(root string) {
				writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name":"escape","types":"../outside.d.ts"}`)
			},
			wantError: "types path",
		},
		{
			name: "exports escape",
			setup: func(root string) {
				writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name":"escape","exports":"../outside.js"}`)
			},
			wantError: "exports path",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			test.setup(root)
			_, err := NewTypeScriptPackageRegistry(context.Background(), Repository{RealPath: root})
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("NewTypeScriptPackageRegistry() error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestNewTypeScriptPackageRegistrySkipsUnnamedRootAndHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"private":true}`)
	writeDiscoveryFile(t, filepath.Join(root, "named", "package.json"), `{"name":"named"}`)
	registry, err := NewTypeScriptPackageRegistry(context.Background(), Repository{RealPath: root})
	if err != nil {
		t.Fatalf("NewTypeScriptPackageRegistry() error = %v", err)
	}
	if packages := registry.List(); len(packages) != 1 || packages[0].Name != "named" {
		t.Fatalf("unnamed package handling = %#v", packages)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewTypeScriptPackageRegistry(ctx, Repository{RealPath: root})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled registry error = %v, want context.Canceled", err)
	}
}
