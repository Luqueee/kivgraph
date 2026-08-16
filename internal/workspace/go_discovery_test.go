package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestDiscoverGoFindsModulesWorkspacesPackagesAndReplaces(t *testing.T) {
	root := testsupport.TempDir(t)
	nested := filepath.Join(root, "nested")
	internal := filepath.Join(root, "internal", "util")
	vendor := filepath.Join(root, "vendor", "dependency")
	for _, directory := range []string{nested, internal, vendor, filepath.Join(root, "node_modules", "dependency")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", directory, err)
		}
	}
	writeGoDiscoveryFile(t, filepath.Join(root, "go.mod"), `module example.com/root

go 1.24

require example.com/remote v1.2.3
replace example.com/local => ./nested
`)
	writeGoDiscoveryFile(t, filepath.Join(root, "go.sum"), "example.com/remote v1.2.3 h1:checksum\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "go.work"), `go 1.24

use (
  .
  ./nested
)

replace example.com/dep => ./nested
`)
	writeGoDiscoveryFile(t, filepath.Join(nested, "go.mod"), "module example.com/nested\n\ngo 1.23\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	writeGoDiscoveryFile(t, filepath.Join(internal, "util.go"), "package util\n\nfunc Value() int { return 1 }\n")
	writeGoDiscoveryFile(t, filepath.Join(internal, "util_test.go"), "package util_test\n\nfunc TestValue() {}\n")
	writeGoDiscoveryFile(t, filepath.Join(nested, "nested.go"), "package nested\n")
	writeGoDiscoveryFile(t, filepath.Join(vendor, "vendor.go"), "package dependency\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "node_modules", "dependency", "node.go"), "package dependency\n")

	discovery, err := DiscoverGo(context.Background(), Repository{RealPath: root})
	if err != nil {
		t.Fatalf("DiscoverGo() error = %v", err)
	}
	if len(discovery.Modules) != 2 {
		t.Fatalf("Modules = %#v, want two modules", discovery.Modules)
	}
	rootModule := discovery.Modules[0]
	nestedModule := discovery.Modules[1]
	if rootModule.ManifestPath != filepath.Join(root, "go.mod") || rootModule.ModulePath != "example.com/root" || rootModule.GoVersion != "1.24" || rootModule.SumPath != filepath.Join(root, "go.sum") {
		t.Fatalf("root module = %#v", rootModule)
	}
	if len(rootModule.Replaces) != 1 || rootModule.Replaces[0].OldPath != "example.com/local" || rootModule.Replaces[0].NewLocalPath != nested {
		t.Fatalf("root replacements = %#v", rootModule.Replaces)
	}
	if nestedModule.ManifestPath != filepath.Join(nested, "go.mod") || nestedModule.ModulePath != "example.com/nested" || nestedModule.GoVersion != "1.23" {
		t.Fatalf("nested module = %#v", nestedModule)
	}
	if !equalStrings(discovery.SumFiles, []string{filepath.Join(root, "go.sum")}) {
		t.Fatalf("SumFiles = %#v", discovery.SumFiles)
	}
	if len(discovery.Workspaces) != 1 {
		t.Fatalf("Workspaces = %#v, want one workspace", discovery.Workspaces)
	}
	workspace := discovery.Workspaces[0]
	if workspace.Path != filepath.Join(root, "go.work") || workspace.GoVersion != "1.24" {
		t.Fatalf("workspace = %#v", workspace)
	}
	wantWorkspaceModules := []string{filepath.Join(root, "go.mod"), filepath.Join(nested, "go.mod")}
	if !equalStrings(workspace.Modules, wantWorkspaceModules) {
		t.Fatalf("workspace modules = %#v, want %#v", workspace.Modules, wantWorkspaceModules)
	}
	if len(workspace.Replaces) != 1 || workspace.Replaces[0].OldPath != "example.com/dep" || workspace.Replaces[0].NewLocalPath != nested {
		t.Fatalf("workspace replacements = %#v", workspace.Replaces)
	}
	if len(discovery.Packages) != 3 {
		t.Fatalf("Packages = %#v, want three packages", discovery.Packages)
	}
	wantPackages := []GoPackage{
		{
			Directory:  root,
			ImportPath: "example.com/root",
			ModulePath: "example.com/root",
			Name:       "main",
			Files:      []string{filepath.Join(root, "main.go")},
		},
		{
			Directory:  internal,
			ImportPath: "example.com/root/internal/util",
			ModulePath: "example.com/root",
			Name:       "util",
			Files:      []string{filepath.Join(internal, "util.go")},
		},
		{
			Directory:  nested,
			ImportPath: "example.com/nested",
			ModulePath: "example.com/nested",
			Name:       "nested",
			Files:      []string{filepath.Join(nested, "nested.go")},
		},
	}
	for index := range wantPackages {
		if discovery.Packages[index].Directory != wantPackages[index].Directory || discovery.Packages[index].ImportPath != wantPackages[index].ImportPath || discovery.Packages[index].ModulePath != wantPackages[index].ModulePath || discovery.Packages[index].Name != wantPackages[index].Name || !equalStrings(discovery.Packages[index].Files, wantPackages[index].Files) {
			t.Fatalf("Packages[%d] = %#v, want %#v", index, discovery.Packages[index], wantPackages[index])
		}
	}
}

func TestDiscoverGoHonorsConfiguredExclusions(t *testing.T) {
	root := testsupport.TempDir(t)
	writeGoDiscoveryFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.24\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "benchmarks", "invalid.go"), "package invalid\n\nvar _ = missing.Symbol\n")

	discovery, err := DiscoverGo(context.Background(), Repository{
		RealPath: root, Exclusions: []string{"**/benchmarks"},
	})
	if err != nil {
		t.Fatalf("DiscoverGo() error = %v", err)
	}
	if len(discovery.Packages) != 1 || discovery.Packages[0].Directory != root {
		t.Fatalf("Packages = %#v, want only the repository package", discovery.Packages)
	}
}

func TestDiscoverGoRejectsInvalidManifestsAndPackages(t *testing.T) {
	tests := []struct {
		name      string
		filename  string
		contents  string
		wantError string
	}{
		{
			name:      "malformed module",
			filename:  "go.mod",
			contents:  "module\n",
			wantError: "parse go.mod",
		},
		{
			name:      "workspace use escape",
			filename:  "go.work",
			contents:  "go 1.24\nuse ../../outside\n",
			wantError: "path escapes repository realpath",
		},
		{
			name:      "workspace use missing module",
			filename:  "go.work",
			contents:  "go 1.24\nuse ./missing\n",
			wantError: "path does not exist or is inaccessible",
		},
		{
			name:      "local replacement escape",
			filename:  "go.mod",
			contents:  "module example.com/root\n\ngo 1.24\nreplace example.com/other => ../../outside\n",
			wantError: "local replacement",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := testsupport.TempDir(t)
			writeGoDiscoveryFile(t, filepath.Join(root, test.filename), test.contents)
			_, err := DiscoverGo(context.Background(), Repository{RealPath: root})
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("DiscoverGo() error = %v, want substring %q", err, test.wantError)
			}
		})
	}

	t.Run("multiple packages", func(t *testing.T) {
		root := testsupport.TempDir(t)
		writeGoDiscoveryFile(t, filepath.Join(root, "one.go"), "package one\n")
		writeGoDiscoveryFile(t, filepath.Join(root, "two.go"), "package two\n")
		_, err := DiscoverGo(context.Background(), Repository{RealPath: root})
		if err == nil || !strings.Contains(err.Error(), "contains multiple Go packages") {
			t.Fatalf("DiscoverGo() error = %v, want multiple package error", err)
		}
	})
}

func TestDiscoverGoSkipsSymlinkTrees(t *testing.T) {
	root := testsupport.TempDir(t)
	external := testsupport.TempDir(t)
	writeGoDiscoveryFile(t, filepath.Join(external, "go.mod"), "module example.com/external\n\ngo 1.24\n")
	writeGoDiscoveryFile(t, filepath.Join(external, "external.go"), "package external\n")
	link := filepath.Join(root, "linked")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	discovery, err := DiscoverGo(context.Background(), Repository{RealPath: root})
	if err != nil {
		t.Fatalf("DiscoverGo() error = %v", err)
	}
	if len(discovery.Modules) != 0 || len(discovery.Packages) != 0 {
		t.Fatalf("discovery followed symlink: %#v", discovery)
	}
}

func TestDiscoverGoHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := DiscoverGo(ctx, Repository{RealPath: testsupport.TempDir(t)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DiscoverGo(canceled) error = %v, want context.Canceled", err)
	}
}

func writeGoDiscoveryFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
