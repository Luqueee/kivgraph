package javaloader

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

// TestRunLeavesTheRepositoryUntouched is the one that would have failed before
// internal/scratchtree existed, and it indexes the repository **in place** on
// purpose.
//
// Every other end-to-end test here copies the fixture first, which is what a
// test is allowed to do and also what hid the problem: `scip-java` drives Maven,
// Maven writes `target/`, and a pass over a user's repository left it there.
// AGENTS.md states the rule without an exception.
func TestRunLeavesTheRepositoryUntouched(t *testing.T) {
	requireJavaToolchain(t)

	// A committed copy, because the isolation reproduces a working tree and a
	// repository is what a user registers.
	work := testsupport.TempDir(t)
	source := filepath.Join(work, "basic")
	copyTree(t, fixture, source)
	git(t, source, "init", "-q")
	git(t, source, "add", "-A")
	git(t, source, "-c", "user.email=a@b", "-c", "user.name=t", "commit", "-qm", "fixture")

	before := listTree(t, source)

	payload, err := Run(t.Context(), Options{
		Command:          "scip-java",
		TargetDirectory:  filepath.Join(work, "target"),
		Repository:       workspace.Repository{Name: "basic", Path: source, RealPath: source},
		MaximumIndexTime: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(payload.Symbols) == 0 {
		t.Fatal("the pass produced no symbols, so it proves nothing about isolation")
	}

	after := listTree(t, source)
	if before != after {
		t.Errorf("indexing modified the repository.\nbefore:\n%s\nafter:\n%s", before, after)
	}
	// Named separately, because this is the exact artefact that used to be
	// left behind and the diff above would report it as one line among many.
	if _, err := os.Stat(filepath.Join(source, "target")); err == nil {
		t.Error("the build left target/ inside the indexed repository")
	}
}

// TestRunKeepsOneIdentityAcrossPasses guards the defect the scratch tree
// introduced and that nothing else would catch: the package name reaches every
// stable key, and the tree is a fresh temporary directory on each pass. Taking
// the identity from where the files happened to be read would give the same
// code a different key every time it is indexed.
func TestRunKeepsOneIdentityAcrossPasses(t *testing.T) {
	requireJavaToolchain(t)

	work := testsupport.TempDir(t)
	source := filepath.Join(work, "basic")
	copyTree(t, fixture, source)
	git(t, source, "init", "-q")
	git(t, source, "add", "-A")
	git(t, source, "-c", "user.email=a@b", "-c", "user.name=t", "commit", "-qm", "fixture")

	repository := workspace.Repository{Name: "basic", Path: source, RealPath: source}
	options := Options{
		Command:          "scip-java",
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
	if first.Package.Name != "basic" {
		t.Errorf("package name = %q, want the repository directory", first.Package.Name)
	}

	// And the keys themselves, which is what a published graph is compared by.
	firstKeys := stableKeys(t, repository, first)
	secondKeys := stableKeys(t, repository, second)
	if firstKeys != secondKeys {
		t.Error("two passes over unchanged code derived different stable keys")
	}
}

func stableKeys(t *testing.T, repository workspace.Repository, payload facts.SemanticPayload) string {
	t.Helper()
	set, err := facts.NormalizeSemantic(t.Context(), repository, payload)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	keys := make([]string, 0, len(set.Symbols))
	for _, symbol := range set.Symbols {
		keys = append(keys, symbol.Key)
	}
	return strings.Join(keys, "\n")
}

func requireJavaToolchain(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"scip-java", "mvn", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
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
		// .git churns on its own -- a gc, an index refresh -- and this test is
		// about the working tree the user sees.
		if strings.HasPrefix(filepath.ToSlash(relative), ".git/") || relative == ".git" {
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
