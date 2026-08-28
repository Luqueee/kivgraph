package scratchtree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// gitRepository builds a small committed repository with a dirty working tree,
// which is the shape that separates a correct materialisation from one that
// silently indexes the last commit.
func gitRepository(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := testsupport.TempDir(t)
	write(t, filepath.Join(root, "src", "kept.txt"), "committed\n")
	write(t, filepath.Join(root, "src", "removed.txt"), "doomed\n")
	write(t, filepath.Join(root, "src", "edited.txt"), "before\n")
	run(t, root, "init", "-q")
	run(t, root, "add", "-A")
	run(t, root, "-c", "user.email=a@b", "-c", "user.name=t", "commit", "-qm", "fixture")

	// Now make the working tree differ from HEAD in all three ways.
	write(t, filepath.Join(root, "src", "edited.txt"), "after\n")
	if err := os.Remove(filepath.Join(root, "src", "removed.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "src", "added.txt"), "untracked\n")
	// And some build output, which must not be carried.
	write(t, filepath.Join(root, "target", "classes", "Thing.class"), "binary\n")
	return root
}

func TestMaterialiseReproducesTheWorkingTreeNotHead(t *testing.T) {
	root := gitRepository(t)
	tree, err := Materialise(t.Context(),
		workspace.Repository{Name: "fixture", Path: root, RealPath: root},
		testsupport.TempDir(t))
	if err != nil {
		t.Fatalf("materialise: %v", err)
	}
	defer tree.Close()

	if tree.Strategy != StrategyArchive {
		t.Errorf("strategy = %q, want %q on a git repository", tree.Strategy, StrategyArchive)
	}
	// A user editing code expects the graph to describe what is on disk. A
	// materialisation of HEAD would answer about code nobody has.
	if got := read(t, filepath.Join(tree.Path, "src", "edited.txt")); got != "after\n" {
		t.Errorf("edited.txt = %q, want the working-tree content", got)
	}
	if got := read(t, filepath.Join(tree.Path, "src", "added.txt")); got != "untracked\n" {
		t.Errorf("added.txt = %q, want the untracked file to be carried", got)
	}
	if _, err := os.Stat(filepath.Join(tree.Path, "src", "removed.txt")); !os.IsNotExist(err) {
		t.Error("a file deleted in the working tree survived into the scratch tree")
	}
	if got := read(t, filepath.Join(tree.Path, "src", "kept.txt")); got != "committed\n" {
		t.Errorf("kept.txt = %q", got)
	}
}

func TestMaterialiseLeavesBuildOutputBehind(t *testing.T) {
	root := gitRepository(t)
	tree, err := Materialise(t.Context(),
		workspace.Repository{Name: "fixture", Path: root, RealPath: root},
		testsupport.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	if _, err := os.Stat(filepath.Join(tree.Path, "target")); !os.IsNotExist(err) {
		t.Error("target/ was carried into the scratch tree: an analyzer would index its own previous output")
	}
	if _, err := os.Stat(filepath.Join(tree.Path, ".git")); !os.IsNotExist(err) {
		t.Error(".git was carried into the scratch tree")
	}
}

// TestMaterialiseWritesNothingInsideTheRepository is the rule this package
// exists to keep: AGENTS.md says an indexed repository is never modified, and
// `git worktree add` -- the obvious alternative -- registers metadata under
// .git/worktrees/ that a dead pass leaves behind.
//
// It checks two things and deliberately not a third. The working tree has to
// be byte-identical, and `.git` must gain no worktree registration. What it
// does not compare is the content of `.git` itself: reading a repository with
// git refreshes its stat cache, so `.git/index` changes on any machine where
// the checkout is newer than its index -- which is every CI runner and was not
// this laptop. That churn is git bookkeeping about files nobody touched, not a
// modification of the repository in the sense the rule means.
func TestMaterialiseWritesNothingInsideTheRepository(t *testing.T) {
	root := gitRepository(t)
	before := snapshotTree(t, root)

	tree, err := Materialise(t.Context(),
		workspace.Repository{Name: "fixture", Path: root, RealPath: root},
		testsupport.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	// Whatever an analyzer would do to the tree, it happens over here.
	write(t, filepath.Join(tree.Path, "target", "classes", "New.class"), "output\n")
	write(t, filepath.Join(tree.Path, "src", "edited.txt"), "an analyzer rewrote this\n")
	if err := tree.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if after := snapshotTree(t, root); before != after {
		t.Errorf("the working tree changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	// The specific thing `git worktree add` would have left behind.
	if _, err := os.Stat(filepath.Join(root, ".git", "worktrees")); !os.IsNotExist(err) {
		t.Error("a worktree registration was left inside .git")
	}
}

func TestCloseRemovesEverythingTheAnalyzerWrote(t *testing.T) {
	root := gitRepository(t)
	tree, err := Materialise(t.Context(),
		workspace.Repository{Name: "fixture", Path: root, RealPath: root},
		testsupport.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(tree.Path, "target", "big.jar"), "artefact\n")
	path := tree.Path
	if err := tree.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the scratch tree survived Close")
	}
	if err := tree.Close(); err != nil {
		t.Errorf("a second Close is not harmless: %v", err)
	}
}

// TestMaterialiseFallsBackWithoutGit covers a registered directory that is not
// a repository. The tree it produces is the same; only the cost differs.
func TestMaterialiseFallsBackWithoutGit(t *testing.T) {
	root := testsupport.TempDir(t)
	write(t, filepath.Join(root, "src", "main.java"), "class A {}\n")
	write(t, filepath.Join(root, "target", "A.class"), "binary\n")
	write(t, filepath.Join(root, "node_modules", "dep", "index.js"), "module\n")

	tree, err := Materialise(t.Context(),
		workspace.Repository{Name: "plain", Path: root, RealPath: root},
		testsupport.TempDir(t))
	if err != nil {
		t.Fatalf("materialise: %v", err)
	}
	defer tree.Close()
	if tree.Strategy != StrategyCopy {
		t.Errorf("strategy = %q, want %q outside git", tree.Strategy, StrategyCopy)
	}
	if got := read(t, filepath.Join(tree.Path, "src", "main.java")); got != "class A {}\n" {
		t.Errorf("source = %q", got)
	}
	for _, excluded := range []string{"target", "node_modules"} {
		if _, err := os.Stat(filepath.Join(tree.Path, excluded)); !os.IsNotExist(err) {
			t.Errorf("%s was copied", excluded)
		}
	}
}

func TestMaterialiseRefusesABaseInsideTheRepository(t *testing.T) {
	root := gitRepository(t)
	_, err := Materialise(t.Context(),
		workspace.Repository{Name: "fixture", Path: root, RealPath: root},
		filepath.Join(root, "scratch"))
	if err == nil || !strings.Contains(err.Error(), "inside the repository") {
		t.Fatalf("err = %v, want a refusal", err)
	}
}

func TestSecurePathRefusesAnEscapingEntry(t *testing.T) {
	for _, name := range []string{"../outside", "../../etc/passwd", "/absolute"} {
		if _, err := securePath("/tmp/tree", name); err == nil {
			t.Errorf("securePath accepted %q", name)
		}
	}
	if _, err := securePath("/tmp/tree", "src/main.java"); err != nil {
		t.Errorf("securePath refused a normal entry: %v", err)
	}
}

// snapshotTree renders every path and content of the working tree, so a
// comparison catches a file added, removed or rewritten anywhere in it.
//
// `.git` is excluded because git rewrites its own stat cache when it reads a
// repository; the one thing that matters inside it is checked by name.
func snapshotTree(t *testing.T, root string) string {
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
		slashed := filepath.ToSlash(relative)
		if slashed == ".git" || strings.HasPrefix(slashed, ".git/") {
			if info.IsDir() && slashed == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			builder.WriteString("d " + slashed + "\n")
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			// Naming an unreadable file beats skipping it silently.
			builder.WriteString("? " + slashed + "\n")
			return nil
		}
		builder.WriteString("f " + slashed + " " + string(data) + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return builder.String()
}

func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func run(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}
