package csharploader

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestRunReportsAMissingIndexerAsNotFound(t *testing.T) {
	// The pass reads exec.ErrNotFound to isolate the repository rather than
	// fail every other one. A machine without the .NET SDK must not decide
	// whether a Go repository gets a graph.
	_, err := Run(t.Context(), Options{
		Command:         "kivgraph-csharp-indexer-that-does-not-exist",
		TargetDirectory: testsupport.TempDir(t),
		Repository:      repositoryFixture(t),
	})
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("err = %v, want it to wrap exec.ErrNotFound", err)
	}
}

func TestRunRefusesToWriteInsideTheRepository(t *testing.T) {
	root := repositoryFixture(t)
	_, err := Run(t.Context(), Options{
		Command:         "scip-dotnet",
		TargetDirectory: filepath.Join(root.RealPath, "obj"),
		Repository:      root,
	})
	if err == nil || !strings.Contains(err.Error(), "inside the indexed repository") {
		t.Fatalf("err = %v, want a refusal to write inside the repository", err)
	}
}

// TestResolveProjectPrefersASolution is the discovery rule that decides how
// much of a repository is indexed. Picking one .csproj of a multi-project
// repository silently drops the rest, which reads as a repository with less
// code rather than an index that looked at part of it.
func TestResolveProjectPrefersASolution(t *testing.T) {
	root := testsupport.TempDir(t)
	writeFile(t, filepath.Join(root, "src", "app", "app.csproj"), "<Project/>")
	writeFile(t, filepath.Join(root, "src", "lib", "lib.csproj"), "<Project/>")
	writeFile(t, filepath.Join(root, "Everything.sln"), "Microsoft Visual Studio Solution File")

	project, err := resolveProject(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(project) != "Everything.sln" {
		t.Errorf("resolved %q, want the solution", project)
	}

	// With no solution, a project is the fallback and the choice is
	// deterministic: two passes over one repository must index the same thing.
	if err := os.Remove(filepath.Join(root, "Everything.sln")); err != nil {
		t.Fatal(err)
	}
	first, err := resolveProject(root, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolveProject(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("discovery is not deterministic: %q then %q", first, second)
	}
}

func TestResolveProjectIgnoresBuildOutput(t *testing.T) {
	root := testsupport.TempDir(t)
	writeFile(t, filepath.Join(root, "obj", "stale.csproj"), "<Project/>")
	writeFile(t, filepath.Join(root, "real.csproj"), "<Project/>")
	project, err := resolveProject(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(project) != "real.csproj" {
		t.Errorf("resolved %q, want the project outside obj/", project)
	}
}

func TestIncludeFileExcludesBuildOutputAndTests(t *testing.T) {
	closed := Options{}
	for path, want := range map[string]bool{
		"Shapes.cs":      true,
		"src/Catalog.cs": true,
		"obj/Debug/net8.0/coverage.GlobalUsings.g.cs": false,
		"bin/Debug/net8.0/Thing.cs":                   false,
		"src/obj/Generated.cs":                        false,
		"tests/CatalogTests.cs":                       false,
		"src/CatalogTest.cs":                          false,
		"Form.Designer.cs":                            false,
		"coverage.csproj":                             false,
	} {
		if got := includeFile(path, closed); got != want {
			t.Errorf("includeFile(%q) = %t, want %t", path, got, want)
		}
	}
	if !includeFile("tests/CatalogTests.cs", Options{IncludeTests: true}) {
		t.Error("include_tests does not reach a test source")
	}
}

// TestRunAgainstTheFixture drives the real scip-dotnet over a copy of the
// fixture. It copies rather than indexing in place: the indexer runs
// `dotnet restore`, which writes obj/ and bin/ into the directory it builds.
func TestRunAgainstTheFixture(t *testing.T) {
	requireToolchain(t)

	work := testsupport.TempDir(t)
	source := filepath.Join(work, "coverage")
	copyTree(t, coverageFixture, source)

	payload, err := Run(t.Context(), Options{
		Command:          "scip-dotnet",
		TargetDirectory:  filepath.Join(work, "target"),
		Repository:       workspace.Repository{Name: "coverage", Path: source, RealPath: source},
		MaximumIndexTime: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if payload.Language != facts.LanguageCSharp {
		t.Fatalf("language = %q", payload.Language)
	}
	if !payload.Authoritative {
		t.Fatal("a scip-dotnet payload is not authoritative, so every edge would be a candidate")
	}
	if len(payload.Symbols) == 0 || len(payload.References) == 0 {
		t.Fatalf("payload is empty: symbols=%d references=%d",
			len(payload.Symbols), len(payload.References))
	}
	set, err := facts.NormalizeSemantic(t.Context(),
		workspace.Repository{Name: "coverage", Path: source, RealPath: source}, payload)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	// The checked-in fixture must survive an indexing pass elsewhere.
	entries, err := os.ReadDir(coverageFixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "obj", "bin", "index.scip":
			t.Errorf("indexing left %q in the fixture", entry.Name())
		}
	}
}

// TestRecordedIndexMatchesTheToolchain keeps the checked-in index honest: the
// hermetic tests read a recorded file, and a fixture that changes without a
// re-recording would leave them asserting about code that is gone.
func TestRecordedIndexMatchesTheToolchain(t *testing.T) {
	requireToolchain(t)

	work := testsupport.TempDir(t)
	source := filepath.Join(work, "coverage")
	copyTree(t, coverageFixture, source)

	payload, err := Run(t.Context(), Options{
		Command:          "scip-dotnet",
		TargetDirectory:  filepath.Join(work, "target"),
		Repository:       workspace.Repository{Name: "coverage", Path: source, RealPath: source},
		MaximumIndexTime: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	recorded := coveragePayload(t)
	if len(payload.Symbols) != len(recorded.Symbols) {
		t.Errorf("the toolchain produced %d symbols and the recorded index has %d: re-record testdata/csharp/index/coverage.scip",
			len(payload.Symbols), len(recorded.Symbols))
	}
	live := map[string]bool{}
	for _, symbol := range payload.Symbols {
		live[symbol.QualifiedName] = true
	}
	for _, symbol := range recorded.Symbols {
		if !live[symbol.QualifiedName] {
			t.Errorf("the recorded index has %q and the toolchain does not", symbol.QualifiedName)
		}
	}
}

// requireToolchain skips unless the indexer actually runs.
//
// Resolving it on the PATH is not enough and that is not hypothetical: the
// `scip-dotnet` launcher is on the PATH the moment the tool is installed, and
// it exits 131 with a ".NET location: Not found" when the runtime it needs is
// not where it looks. A guard that only checked the PATH turned that into a
// test failure on a machine that never had a working toolchain, which is the
// opposite of what a skip is for.
func requireToolchain(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dotnet"); err != nil {
		t.Skip("the .NET SDK is not installed")
	}
	path, err := exec.LookPath("scip-dotnet")
	if err != nil {
		t.Skip("scip-dotnet is not installed")
	}
	probe, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	if output, err := exec.CommandContext(probe, path, "--help").CombinedOutput(); err != nil {
		t.Skipf("scip-dotnet is installed but does not run: %v: %s", err, lastLines(string(output), 4))
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	err := filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}
