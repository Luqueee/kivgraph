package workspace

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func projectPathOf(t *testing.T, root, name string) string {
	t.Helper()
	registry, err := NewTypeScriptPackageRegistry(context.Background(), Repository{RealPath: root})
	if err != nil {
		t.Fatalf("NewTypeScriptPackageRegistry() error = %v", err)
	}
	packageValue, found := registry.Get(name)
	if !found {
		t.Fatalf("package %q is not in the registry", name)
	}
	return packageValue.ProjectPath
}

func writeNestedPackages(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{"web", "shared"} {
		directory := filepath.Join(root, "packages", name)
		writeDiscoveryFile(t, filepath.Join(directory, "package.json"), `{"name":"@mono/`+name+`"}`)
		writeDiscoveryFile(t, filepath.Join(directory, "tsconfig.json"),
			`{"extends":"../../tsconfig.base.json","include":["src"]}`)
		writeDiscoveryFile(t, filepath.Join(directory, "src", "index.ts"), "export const value = 1\n")
	}
}

// TestRegistryGivesAMonorepoRootWithASourcelessConfigNoProject is the layout
// that made `index --full` fail at publish: a named root package whose only
// config is a base that every package extends. A config that lists no source
// takes every file below it, so the root payload claimed the files of every
// nested package too, each one then sat under two packages, and the load
// refused the second CONTAINS_FILE edge.
func TestRegistryGivesAMonorepoRootWithASourcelessConfigNoProject(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name":"mono","private":true}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.base.json"), `{"compilerOptions":{"strict":true}}`)
	writeNestedPackages(t, root)

	if got := projectPathOf(t, root, "mono"); got != "" {
		t.Errorf("root ProjectPath = %q, want none: the nested packages own every file below it", got)
	}
	for _, name := range []string{"web", "shared"} {
		want := filepath.Join(root, "packages", name, "tsconfig.json")
		if got := projectPathOf(t, root, "@mono/"+name); got != want {
			t.Errorf("%s ProjectPath = %q, want %q", name, got, want)
		}
	}
}

// The three cases below are what the rule must not touch. Each one keeps the
// root's project, because in none of them does the root take a file a nested
// package also takes.

func TestRegistryKeepsARootProjectThatNamesItsSources(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name":"mono","private":true}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{"include":["scripts"]}`)
	writeDiscoveryFile(t, filepath.Join(root, "scripts", "release.ts"), "export const release = 1\n")
	writeNestedPackages(t, root)

	if got, want := projectPathOf(t, root, "mono"), filepath.Join(root, "tsconfig.json"); got != want {
		t.Errorf("root ProjectPath = %q, want %q", got, want)
	}
}

func TestRegistryKeepsARootProjectWhenNoNestedPackageHasItsOwn(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name":"mono","private":true}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{"compilerOptions":{"strict":true}}`)
	// No tsconfig of its own: this package inherits the root's project, so the
	// root is not taking anything from it that it does not already share.
	writeDiscoveryFile(t, filepath.Join(root, "packages", "lib", "package.json"), `{"name":"@mono/lib"}`)
	writeDiscoveryFile(t, filepath.Join(root, "packages", "lib", "index.ts"), "export const lib = 1\n")

	want := filepath.Join(root, "tsconfig.json")
	if got := projectPathOf(t, root, "mono"); got != want {
		t.Errorf("root ProjectPath = %q, want %q", got, want)
	}
	if got := projectPathOf(t, root, "@mono/lib"); got != want {
		t.Errorf("nested ProjectPath = %q, want the root's %q", got, want)
	}
}

func TestRegistryKeepsTheProjectOfASinglePackageWithAPlainConfig(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name":"solo"}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{"compilerOptions":{"strict":true}}`)
	writeDiscoveryFile(t, filepath.Join(root, "index.ts"), "export const solo = 1\n")

	if got, want := projectPathOf(t, root, "solo"), filepath.Join(root, "tsconfig.json"); got != want {
		t.Errorf("ProjectPath = %q, want %q", got, want)
	}
}
