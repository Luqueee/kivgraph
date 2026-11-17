package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
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

// DiscoverGo reads the packages a "..." pattern of the go tool would match, so a
// directory the tool never enters must neither be reported nor be able to fail
// discovery: testdata holds files that are fixtures on purpose, many of them
// not valid Go.
func TestDiscoverGoIgnoresPackagesTheGoToolIgnores(t *testing.T) {
	root := testsupport.TempDir(t)
	writeGoDiscoveryFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.24\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "kept", "kept.go"), "package kept\n")
	for name, contents := range map[string]string{
		"testdata/not_go.go":              "this is not go code\n",
		"testdata/multi/a.go":             "package a\n",
		"testdata/multi/b.go":             "package b\n",
		"testdata/ok/ok.go":               "package ok\n",
		"kept/testdata/inner.go":          "not go either\n",
		"_scratch/scratch.go":             "package scratch\n",
		"kept/_generated/generated.go":    "package generated\n",
		".hidden/hidden.go":               "package hidden\n",
		"kept/.cache/cache.go":            "package cache\n",
		"testdata/deep/er/still/inner.go": "package inner\n",
	} {
		writeGoDiscoveryFile(t, filepath.Join(root, filepath.FromSlash(name)), contents)
	}

	discovery, err := DiscoverGo(context.Background(), Repository{RealPath: root})
	if err != nil {
		t.Fatalf("DiscoverGo() error = %v, want the ignored directories left alone", err)
	}
	var got []string
	for _, pkg := range discovery.Packages {
		got = append(got, pkg.ImportPath)
	}
	want := []string{"example.com/root", "example.com/root/kept"}
	if !slices.Equal(got, want) {
		t.Fatalf("Packages = %v, want %v", got, want)
	}
}

// Control for the test above: the rule is about the name and not about the
// content, so the same files in directories the go tool does enter are packages,
// and a file that does not parse is listed in one for go to answer for.
func TestDiscoverGoStillReadsTheSameFilesUnderOtherNames(t *testing.T) {
	for _, name := range []string{"fixtures", "testdata2", "my_testdata", "a_b", "test.data"} {
		root := testsupport.TempDir(t)
		writeGoDiscoveryFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.24\n")
		writeGoDiscoveryFile(t, filepath.Join(root, name, "ok.go"), "package ok\n")
		discovery, err := DiscoverGo(context.Background(), Repository{RealPath: root})
		if err != nil || len(discovery.Packages) != 1 || discovery.Packages[0].ImportPath != "example.com/root/"+name {
			t.Errorf("%s: DiscoverGo() = %#v, %v, want the package reported", name, discovery.Packages, err)
		}
		writeGoDiscoveryFile(t, filepath.Join(root, name, "bad.go"), "this is not go code\n")
		discovery, err = DiscoverGo(context.Background(), Repository{RealPath: root})
		if err != nil || len(discovery.Packages) != 1 || len(discovery.Packages[0].Files) != 2 {
			t.Errorf("%s: DiscoverGo() = %#v, %v, want the directory still reported, with the file go has to answer for", name, discovery.Packages, err)
		}
	}
}

// The directory the repository is checked out in is not part of the module's
// packages, so a name above the root must not hide it.
func TestDiscoverGoDoesNotJudgeTheNamesAboveTheModuleRoot(t *testing.T) {
	root := filepath.Join(testsupport.TempDir(t), "testdata", "_work", ".repo")
	writeGoDiscoveryFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.24\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "main.go"), "package main\n")
	discovery, err := DiscoverGo(context.Background(), Repository{RealPath: root})
	if err != nil || len(discovery.Packages) != 1 || discovery.Packages[0].Directory != root {
		t.Fatalf("DiscoverGo() = %#v, %v, want the root package", discovery.Packages, err)
	}
}

// A module inside testdata is still a module, judged from its own root: its
// packages are reported, and testdata below that root is ignored again. This is
// what lets a go.mod replace a dependency with a fixture in testdata.
func TestDiscoverGoKeepsModulesThatLiveInsideTestdata(t *testing.T) {
	root := testsupport.TempDir(t)
	writeGoDiscoveryFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.24\n\nrequire example.com/fixture v0.0.0\n\nreplace example.com/fixture => ./testdata/fixture\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "testdata", "fixture", "go.mod"), "module example.com/fixture\n\ngo 1.24\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "testdata", "fixture", "fixture.go"), "package fixture\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "testdata", "fixture", "sub", "sub.go"), "package sub\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "testdata", "fixture", "testdata", "inner.go"), "not go\n")

	discovery, err := DiscoverGo(context.Background(), Repository{RealPath: root})
	if err != nil {
		t.Fatalf("DiscoverGo() error = %v", err)
	}
	var modules, packages []string
	for _, module := range discovery.Modules {
		modules = append(modules, module.ModulePath)
	}
	for _, pkg := range discovery.Packages {
		packages = append(packages, pkg.ImportPath)
	}
	slices.Sort(modules)
	slices.Sort(packages)
	if want := []string{"example.com/fixture", "example.com/root"}; !slices.Equal(modules, want) {
		t.Fatalf("Modules = %v, want %v", modules, want)
	}
	if want := []string{"example.com/fixture", "example.com/fixture/sub", "example.com/root"}; !slices.Equal(packages, want) {
		t.Fatalf("Packages = %v, want %v", packages, want)
	}
}

// A go.mod the go tool is never going to read -- one below testdata, or below a
// directory that begins with "_" or "." -- may be a fixture that is malformed on
// purpose, and it must not stop the index of the repository around it. The same
// manifest anywhere else is the repository's own, and is still an error.
func TestDiscoverGoSkipsAManifestThatIsMalformedWhereTheGoToolNeverLooks(t *testing.T) {
	for _, hidden := range []string{"testdata/broken", "kept/testdata/broken", "_scratch/broken", ".cache/broken"} {
		root := testsupport.TempDir(t)
		writeGoDiscoveryFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.24\n")
		writeGoDiscoveryFile(t, filepath.Join(root, "main.go"), "package main\n")
		writeGoDiscoveryFile(t, filepath.Join(root, filepath.FromSlash(hidden), "go.mod"), "module\n")
		discovery, err := DiscoverGo(context.Background(), Repository{RealPath: root})
		if err != nil {
			t.Errorf("%s: DiscoverGo() error = %v, want the fixture left alone", hidden, err)
			continue
		}
		if len(discovery.Modules) != 1 || discovery.Modules[0].ModulePath != "example.com/root" {
			t.Errorf("%s: Modules = %#v, want only the root module", hidden, discovery.Modules)
		}
	}

	for _, visible := range []string{"broken", "kept/broken", "testdata2/broken"} {
		root := testsupport.TempDir(t)
		writeGoDiscoveryFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.24\n")
		writeGoDiscoveryFile(t, filepath.Join(root, filepath.FromSlash(visible), "go.mod"), "module\n")
		if _, err := DiscoverGo(context.Background(), Repository{RealPath: root}); err == nil || !strings.Contains(err.Error(), "parse go.mod") {
			t.Errorf("%s: DiscoverGo() error = %v, want the repository's own malformed manifest reported", visible, err)
		}
	}
}

// Packages outside any module are judged from the repository root, the only
// root there is.
func TestDiscoverGoIgnoresTestdataInATreeWithoutAModule(t *testing.T) {
	root := testsupport.TempDir(t)
	writeGoDiscoveryFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "testdata", "bad.go"), "this is not go code\n")
	discovery, err := DiscoverGo(context.Background(), Repository{RealPath: root})
	if err != nil || len(discovery.Packages) != 1 || discovery.Packages[0].Directory != root {
		t.Fatalf("DiscoverGo() = %#v, %v, want only the root package", discovery.Packages, err)
	}
}

// Whether a directory is a valid package is the go command's answer to give, and
// it gives it per package. Discovery must name what it can and keep going: one
// directory it cannot read as a single package may not stop the index.
func TestDiscoverGoNeverFailsOnADirectoryItCannotReadAsOnePackage(t *testing.T) {
	for _, test := range []struct {
		name  string
		files map[string]string
		name_ string
		kept  []string
	}{
		{
			name:  "two packages, the larger one wins",
			files: map[string]string{"a.go": "package a\n", "b1.go": "package b\n", "b2.go": "package b\n"},
			name_: "b",
			kept:  []string{"b1.go", "b2.go"},
		},
		{
			name:  "two packages of one file each, the first by name",
			files: map[string]string{"one.go": "package one\n", "two.go": "package two\n"},
			name_: "one",
			kept:  []string{"one.go"},
		},
		{
			name:  "a file that does not parse stays listed so its error can be attributed",
			files: map[string]string{"ok.go": "package ok\n", "bad.go": "this is not go code\n"},
			name_: "ok",
			kept:  []string{"bad.go", "ok.go"},
		},
		{
			name:  "nothing parses",
			files: map[string]string{"bad.go": "this is not go code\n", "worse.go": "package\n"},
			name_: "",
			kept:  []string{"bad.go", "worse.go"},
		},
		{
			name:  "an external test package does not make two",
			files: map[string]string{"x.go": "package x\n", "x_test.go": "package x_test\n"},
			name_: "x",
			kept:  []string{"x.go"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := testsupport.TempDir(t)
			writeGoDiscoveryFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.24\n")
			writeGoDiscoveryFile(t, filepath.Join(root, "other", "other.go"), "package other\n")
			for name, contents := range test.files {
				writeGoDiscoveryFile(t, filepath.Join(root, "dir", name), contents)
			}
			discovery, err := DiscoverGo(context.Background(), Repository{RealPath: root})
			if err != nil {
				t.Fatalf("DiscoverGo() error = %v, want discovery to go on", err)
			}
			var found *GoPackage
			for index := range discovery.Packages {
				if discovery.Packages[index].ImportPath == "example.com/root/dir" {
					found = &discovery.Packages[index]
				}
			}
			if found == nil {
				t.Fatalf("Packages = %#v, want the directory reported so that go can answer for it", discovery.Packages)
			}
			var names []string
			for _, file := range found.Files {
				names = append(names, filepath.Base(file))
			}
			if found.Name != test.name_ || !slices.Equal(names, test.kept) {
				t.Fatalf("package = %q %v, want %q %v", found.Name, names, test.name_, test.kept)
			}
			if len(discovery.Packages) != 2 {
				t.Fatalf("Packages = %#v, want the neighbouring package kept too", discovery.Packages)
			}
		})
	}
}

// The generator idiom: a `package main` kept out of every build with an ignore
// constraint, beside the package it generates for. It is not a second package,
// and it must not decide the name even when it sorts first.
func TestDiscoverGoIgnoresFilesExcludedByAnIgnoreConstraint(t *testing.T) {
	for name, header := range map[string]string{
		"go:build ignore":   "//go:build ignore\n\npackage aaa\n",
		"plus build ignore": "// +build ignore\n\npackage aaa\n",
		"after a comment":   "// Copyright.\n\n//go:build ignore\n\npackage aaa\n",
	} {
		root := testsupport.TempDir(t)
		writeGoDiscoveryFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.24\n")
		writeGoDiscoveryFile(t, filepath.Join(root, "lib.go"), "package zzz\n")
		writeGoDiscoveryFile(t, filepath.Join(root, "gen.go"), header)
		discovery, err := DiscoverGo(context.Background(), Repository{RealPath: root})
		if err != nil || len(discovery.Packages) != 1 {
			t.Fatalf("%s: DiscoverGo() = %#v, %v", name, discovery.Packages, err)
		}
		got := discovery.Packages[0]
		if got.Name != "zzz" || len(got.Files) != 1 || filepath.Base(got.Files[0]) != "lib.go" {
			t.Errorf("%s: package = %q %v, want zzz with lib.go only: the ignored file is not part of the package", name, got.Name, got.Files)
		}
	}

	// Other constraints are the go command's to evaluate, and a constraint that
	// merely mentions ignore is not one.
	for name, header := range map[string]string{
		"a platform":                      "//go:build linux\n\npackage aaa\n",
		"ignore and another":              "//go:build ignore && linux\n\npackage aaa\n",
		"ignore after the package clause": "package aaa\n\n//go:build ignore\n",
	} {
		root := testsupport.TempDir(t)
		writeGoDiscoveryFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.24\n")
		writeGoDiscoveryFile(t, filepath.Join(root, "lib.go"), "package zzz\n")
		writeGoDiscoveryFile(t, filepath.Join(root, "gen.go"), header)
		discovery, err := DiscoverGo(context.Background(), Repository{RealPath: root})
		if err != nil || len(discovery.Packages) != 1 || discovery.Packages[0].Name != "aaa" {
			t.Errorf("%s: DiscoverGo() = %#v, %v, want the file counted as a package of its own", name, discovery.Packages, err)
		}
	}

	// A directory where every file is ignored has no other answer to give.
	root := testsupport.TempDir(t)
	writeGoDiscoveryFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.24\n")
	writeGoDiscoveryFile(t, filepath.Join(root, "gen.go"), "//go:build ignore\n\npackage main\n")
	discovery, err := DiscoverGo(context.Background(), Repository{RealPath: root})
	if err != nil || len(discovery.Packages) != 1 || discovery.Packages[0].Name != "main" {
		t.Fatalf("DiscoverGo() = %#v, %v, want the ignored-only directory reported", discovery.Packages, err)
	}
}
