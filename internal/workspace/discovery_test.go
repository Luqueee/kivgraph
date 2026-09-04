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

func TestDiscoverTypeScriptFindsManifestsWorkspacesAndReferences(t *testing.T) {
	root := testsupport.TempDir(t)
	packageA := filepath.Join(root, "packages", "a")
	packageB := filepath.Join(root, "packages", "b")
	ignored := filepath.Join(root, "ignored")
	for _, directory := range []string{packageA, packageB, filepath.Join(root, "generated", "nested"), filepath.Join(root, "ignored", "nested"), filepath.Join(root, "node_modules", "dependency")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", directory, err)
		}
	}
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{
  "name": "root",
  "workspaces": {
    "packages": ["packages/*", "tools/*"],
    "nohoist": ["**/fixtures"]
  }
}`)
	writeDiscoveryFile(t, filepath.Join(root, "pnpm-workspace.yaml"), "packages:\n  - packages/*\n  - tools/*\n")
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{
  // Composite projects use JSONC in real repositories.
  "references": [
    {"path": "./packages/a"},
    {"path": "./packages/b/tsconfig.build.json"},
  ],
}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.base.json"), `{"compilerOptions":{"strict":true}}`)
	writeDiscoveryFile(t, filepath.Join(packageA, "package.json"), `{"name":"@example/a"}`)
	writeDiscoveryFile(t, filepath.Join(packageA, "tsconfig.json"), `{
  "compilerOptions": {"composite": true},
  "references": [],
}`)
	writeDiscoveryFile(t, filepath.Join(packageB, "tsconfig.build.json"), `{"compilerOptions":{"composite":true}}`)
	writeDiscoveryFile(t, filepath.Join(filepath.Dir(ignored), "ignored", "nested", "package.json"), `{"name":"ignored"}`)
	writeDiscoveryFile(t, filepath.Join(root, "generated", "nested", "package.json"), `{"name":"generated"}`)

	discovery, err := DiscoverTypeScript(context.Background(), Repository{
		RealPath:   root,
		Exclusions: []string{"ignored", "generated/**"},
	})
	if err != nil {
		t.Fatalf("DiscoverTypeScript() error = %v", err)
	}
	wantPackages := []string{
		filepath.Join(root, "package.json"),
		filepath.Join(packageA, "package.json"),
	}
	if !equalStrings(discovery.PackageManifests, wantPackages) {
		t.Fatalf("PackageManifests = %#v, want %#v", discovery.PackageManifests, wantPackages)
	}
	wantProjects := []TypeScriptProject{
		{ConfigPath: filepath.Join(packageA, "tsconfig.json")},
		{ConfigPath: filepath.Join(packageB, "tsconfig.build.json")},
		{ConfigPath: filepath.Join(root, "tsconfig.base.json")},
		{
			ConfigPath: filepath.Join(root, "tsconfig.json"),
			References: []string{filepath.Join(packageA, "tsconfig.json"), filepath.Join(packageB, "tsconfig.build.json")},
		},
	}
	if len(discovery.Projects) != len(wantProjects) {
		t.Fatalf("Projects = %#v, want %#v", discovery.Projects, wantProjects)
	}
	for index := range wantProjects {
		if discovery.Projects[index].ConfigPath != wantProjects[index].ConfigPath || !equalStrings(discovery.Projects[index].References, wantProjects[index].References) {
			t.Fatalf("Projects[%d] = %#v, want %#v", index, discovery.Projects[index], wantProjects[index])
		}
	}
	if len(discovery.Workspaces) != 2 {
		t.Fatalf("Workspaces = %#v, want two declarations", discovery.Workspaces)
	}
	if discovery.Workspaces[0].ManifestPath != filepath.Join(root, "package.json") || discovery.Workspaces[0].Format != TypeScriptWorkspacePackageJSON || !equalStrings(discovery.Workspaces[0].Patterns, []string{"packages/*", "tools/*"}) {
		t.Fatalf("package workspace = %#v", discovery.Workspaces[0])
	}
	if discovery.Workspaces[1].ManifestPath != filepath.Join(root, "pnpm-workspace.yaml") || discovery.Workspaces[1].Format != TypeScriptWorkspacePNPM || !equalStrings(discovery.Workspaces[1].Patterns, []string{"packages/*", "tools/*"}) {
		t.Fatalf("pnpm workspace = %#v", discovery.Workspaces[1])
	}
}

func TestDiscoverTypeScriptAcceptsPnpmConfigurationWithoutPackagePatterns(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "pnpm-workspace.yaml"), `
minimumReleaseAgeExclude:
  - typescript@7.0.0
`)

	discovery, err := DiscoverTypeScript(context.Background(), Repository{RealPath: root})
	if err != nil {
		t.Fatalf("DiscoverTypeScript() error = %v", err)
	}
	if len(discovery.Workspaces) != 1 || len(discovery.Workspaces[0].Patterns) != 0 {
		t.Fatalf("workspaces = %#v, want one declaration without patterns", discovery.Workspaces)
	}
}

func TestDiscoverTypeScriptSkipsSymlinks(t *testing.T) {
	root := testsupport.TempDir(t)
	external := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(external, "package.json"), `{"name":"external"}`)
	link := filepath.Join(root, "linked")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	discovery, err := DiscoverTypeScript(context.Background(), Repository{RealPath: root})
	if err != nil {
		t.Fatalf("DiscoverTypeScript() error = %v", err)
	}
	if len(discovery.PackageManifests) != 0 || len(discovery.Projects) != 0 {
		t.Fatalf("discovery followed symlink: %#v", discovery)
	}
}

func TestDiscoverTypeScriptRejectsInvalidManifests(t *testing.T) {
	tests := []struct {
		name      string
		filename  string
		contents  string
		wantError string
	}{
		{
			name:      "invalid package workspace type",
			filename:  "package.json",
			contents:  `{"workspaces":"packages/*"}`,
			wantError: "workspaces must be an array",
		},
		{
			name:      "workspace escape",
			filename:  "package.json",
			contents:  `{"workspaces":["../outside/*"]}`,
			wantError: "escapes repository realpath",
		},
		{
			name:      "malformed tsconfig",
			filename:  "tsconfig.json",
			contents:  `{"references":[`,
			wantError: "parse JSON",
		},
		{
			name:      "reference escape",
			filename:  "tsconfig.json",
			contents:  `{"references":[{"path":"../../outside"}]}`,
			wantError: "path escapes repository realpath",
		},
		{
			name:      "missing reference",
			filename:  "tsconfig.json",
			contents:  `{"references":[{"path":"./missing"}]}`,
			wantError: "target does not exist or is inaccessible",
		},
		{
			name:      "malformed pnpm workspace",
			filename:  "pnpm-workspace.yaml",
			contents:  "packages: [\n",
			wantError: "parse YAML",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := testsupport.TempDir(t)
			writeDiscoveryFile(t, filepath.Join(root, test.filename), test.contents)
			_, err := DiscoverTypeScript(context.Background(), Repository{RealPath: root})
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("DiscoverTypeScript() error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestDiscoverTypeScriptRejectsInvalidExclusionPattern(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name":"invalid-exclusion"}`)

	_, err := DiscoverTypeScript(context.Background(), Repository{
		RealPath:   root,
		Exclusions: []string{"["},
	})
	if err == nil || !strings.Contains(err.Error(), "exclusions[0]") {
		t.Fatalf("DiscoverTypeScript() exclusions=%q error = %v, want invalid exclusion error", []string{"["}, err)
	}
}

func TestDiscoverTypeScriptHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := DiscoverTypeScript(ctx, Repository{RealPath: testsupport.TempDir(t)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DiscoverTypeScript(canceled) error = %v, want context.Canceled", err)
	}
}

func writeDiscoveryFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
