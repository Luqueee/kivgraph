package csharploader

import (
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

// TestRunLeavesTheRepositoryUntouched indexes the repository **in place**,
// which is what every other end-to-end test here declines to do.
//
// `dotnet restore` writes `obj/` and `bin/` into the project it restores.
// AGENTS.md forbids modifying an indexed repository without an exception, and
// a test that copies the fixture first proves the fixture is fine while saying
// nothing about a user's repository.
func TestRunLeavesTheRepositoryUntouched(t *testing.T) {
	requireToolchain(t)
	requireGit(t)

	work := testsupport.TempDir(t)
	source := filepath.Join(work, "coverage")
	copyTree(t, coverageFixture, source)
	git(t, source, "init", "-q")
	git(t, source, "add", "-A")
	git(t, source, "-c", "user.email=a@b", "-c", "user.name=t", "commit", "-qm", "fixture")

	before := listTree(t, source)

	payload, err := Run(t.Context(), Options{
		Command:          "scip-dotnet",
		TargetDirectory:  filepath.Join(work, "target"),
		Repository:       workspace.Repository{Name: "coverage", Path: source, RealPath: source},
		MaximumIndexTime: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(payload.Symbols) == 0 {
		t.Fatal("the pass produced no symbols, so it proves nothing about isolation")
	}

	if after := listTree(t, source); before != after {
		t.Errorf("indexing modified the repository.\nbefore:\n%s\nafter:\n%s", before, after)
	}
	for _, artefact := range []string{"obj", "bin"} {
		if _, err := os.Stat(filepath.Join(source, artefact)); err == nil {
			t.Errorf("the restore left %s/ inside the indexed repository", artefact)
		}
	}
}

// TestRunKeepsOneIdentityAcrossPasses guards what the scratch tree threatens:
// the package name reaches every stable key, and the tree is a fresh temporary
// directory on each pass.
func TestRunKeepsOneIdentityAcrossPasses(t *testing.T) {
	requireToolchain(t)
	requireGit(t)

	work := testsupport.TempDir(t)
	source := filepath.Join(work, "coverage")
	copyTree(t, coverageFixture, source)
	git(t, source, "init", "-q")
	git(t, source, "add", "-A")
	git(t, source, "-c", "user.email=a@b", "-c", "user.name=t", "commit", "-qm", "fixture")

	repository := workspace.Repository{Name: "coverage", Path: source, RealPath: source}
	options := Options{
		Command:          "scip-dotnet",
		TargetDirectory:  filepath.Join(work, "target"),
		Repository:       repository,
		MaximumIndexTime: 15 * time.Minute,
	}

	first, err := Run(t.Context(), options)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	second, err := Run(t.Context(), options)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if first.Package.Name != second.Package.Name {
		t.Fatalf("package name differs between passes: %q then %q",
			first.Package.Name, second.Package.Name)
	}
	if first.Package.Name != "coverage" {
		t.Errorf("package name = %q, want the project name", first.Package.Name)
	}
	// The manifest travels into the graph too, and a temporary directory in it
	// would be as unstable as the name.
	if strings.Contains(first.Package.ManifestPath, "tree-") {
		t.Errorf("manifest path leaks the scratch tree: %q", first.Package.ManifestPath)
	}
	if keys(t, repository, first) != keys(t, repository, second) {
		t.Error("two passes over unchanged code derived different stable keys")
	}
}

func keys(t *testing.T, repository workspace.Repository, payload facts.SemanticPayload) string {
	t.Helper()
	set, err := facts.NormalizeSemantic(t.Context(), repository, payload)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	collected := make([]string, 0, len(set.Symbols))
	for _, symbol := range set.Symbols {
		collected = append(collected, symbol.Key)
	}
	return strings.Join(collected, "\n")
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func listTree(t *testing.T, root string) string {
	t.Helper()
	var builder strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// .git churns on its own -- an index refresh, a maintenance lock that
		// exists for a moment -- and this test is about the working tree. It
		// has to be SkipDir and not nil: returning nil for a directory lets
		// Walk descend into it anyway, and the walk then races a lock file
		// that vanishes between readdir and lstat.
		if relative == ".git" {
			return filepath.SkipDir
		}
		if strings.HasPrefix(filepath.ToSlash(relative), ".git/") {
			return nil
		}
		if info.IsDir() {
			builder.WriteString("d " + filepath.ToSlash(relative) + "\n")
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		builder.WriteString("f " + filepath.ToSlash(relative) + " " + string(data) + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return builder.String()
}

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}
