//go:build darwin || linux

package sourceobservation

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// The signature is only allowed to say "unchanged" when reading the tree again
// would give the digest it already has. These tests are written from that
// direction: first what must never be reported, then the doubts that must fall
// back, and only then that a quiet tree is skipped at all.
//
// Entries written within racyWindow of a capture are not trusted, so every
// fixture is written first, the test waits once for the window to pass, and only
// then runs. One wait serves every subtest, which is why they share a parent.

type mutation struct {
	name  string
	apply func(t *testing.T, root string)
}

func mutationWrite(root, name, content string) error {
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

const restoredMtime = "same-size edit, then the file's own modification time restored"

func mutations() []mutation {
	must := func(t *testing.T, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	return []mutation{
		{"edit an analysed file, new size", func(t *testing.T, r string) {
			must(t, mutationWrite(r, "src/a.go", "package a\n\nvar A = 100\n"))
		}},
		{"edit an analysed file, same size", func(t *testing.T, r string) {
			must(t, mutationWrite(r, "src/a.go", "package a\n\nvar A = 2\n"))
		}},
		{restoredMtime, func(t *testing.T, r string) {
			path := filepath.Join(r, "src/a.go")
			before, err := os.Stat(path)
			must(t, err)
			must(t, mutationWrite(r, "src/a.go", "package a\n\nvar A = 3\n"))
			must(t, os.Chtimes(path, before.ModTime(), before.ModTime()))
		}},
		{"replace an analysed file by renaming over it", func(t *testing.T, r string) {
			must(t, mutationWrite(r, "src/a.go.tmp", "package a\n\nvar A = 4\n"))
			must(t, os.Rename(filepath.Join(r, "src/a.go.tmp"), filepath.Join(r, "src/a.go")))
		}},
		{"add an analysed file", func(t *testing.T, r string) {
			must(t, mutationWrite(r, "src/new.go", "package a\n"))
		}},
		{"delete an analysed file", func(t *testing.T, r string) {
			must(t, os.Remove(filepath.Join(r, "src/b.go")))
		}},
		{"rename an analysed file", func(t *testing.T, r string) {
			must(t, os.Rename(filepath.Join(r, "src/b.go"), filepath.Join(r, "src/c.go")))
		}},
		{"add an analysed file in a new directory", func(t *testing.T, r string) {
			must(t, mutationWrite(r, "pkg/new/n.go", "package n\n"))
		}},
		{"delete a directory of analysed files", func(t *testing.T, r string) {
			must(t, os.RemoveAll(filepath.Join(r, "src")))
		}},
		{"add a file, then put the old directory time back", func(t *testing.T, r string) {
			dir := filepath.Join(r, "src")
			before, err := os.Stat(dir)
			must(t, err)
			must(t, mutationWrite(r, "src/added.go", "package a\n"))
			must(t, os.Chtimes(dir, before.ModTime(), before.ModTime()))
		}},
		{"change nothing", func(*testing.T, string) {}},
	}
}

var treeContent = map[string]string{
	"src/a.go": "package a\n\nvar A = 0\n", "src/b.go": "package a\n\nvar B = 0\n",
	"docs/readme.md": "# readme\n", "target/debug/deps/keep.o": "o",
}

func writeTree(t *testing.T) string {
	t.Helper()
	root := testsupport.TempDir(t)
	for path, content := range treeContent {
		if err := mutationWrite(root, path, content); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func writeRepository(t *testing.T) workspace.Repository {
	t.Helper()
	repository := sourceFixtureRepository(t)
	for path, content := range treeContent {
		if err := mutationWrite(repository.RealPath, path, content); err != nil {
			t.Fatal(err)
		}
	}
	return repository
}

// errCountingContext counts how many times a walk asks whether it was cancelled,
// which the digest's walk does once per entry it visits and a stat-only check
// never does.
type errCountingContext struct {
	context.Context
	calls int
}

func (c *errCountingContext) Err() error {
	c.calls++
	return c.Context.Err()
}

func TestSignature(t *testing.T) {
	type matrixFixture struct {
		mutation   mutation
		root       string
		repository workspace.Repository
	}
	var matrix []matrixFixture
	for _, one := range mutations() {
		matrix = append(matrix, matrixFixture{one, writeTree(t), writeRepository(t)})
	}
	restore := writeTree(t)
	removed := writeTree(t)
	unreadable := writeTree(t)
	linked := writeTree(t)
	if err := os.Symlink(filepath.Join(linked, "src", "a.go"), filepath.Join(linked, "link.go")); err != nil {
		t.Fatal(err)
	}
	wide := sourceFixtureRepository(t)
	for index := range 150 {
		path := filepath.Join("wide", string(rune('a'+index%10)), "f"+string(rune('a'+index/10))+".go")
		if err := mutationWrite(wide.RealPath, path, "package w\n"); err != nil {
			t.Fatal(err)
		}
	}
	quiet, moved := sourceFixtureRepository(t), sourceFixtureRepository(t)
	elsewhere := sourceFixtureRepository(t)
	moved.Name = "moved"

	time.Sleep(racyWindow + 150*time.Millisecond)

	for _, f := range matrix {
		t.Run("never unchanged for a changed digest/"+f.mutation.name, func(t *testing.T) {
			before, signature, err := TreeDigestSigned(context.Background(), f.root)
			if err != nil {
				t.Fatal(err)
			}
			f.mutation.apply(t, f.root)
			after, err := TreeDigest(context.Background(), f.root)
			if err != nil {
				t.Fatal(err)
			}
			unchanged := signature.Unchanged()
			if before != after && unchanged {
				t.Fatal("the digest changed and the signature reported the tree unchanged")
			}
			if f.mutation.name == "change nothing" && !unchanged {
				t.Fatal("a quiet tree older than the racy window was not reported unchanged: the signature never skips anything")
			}
		})

		// The property a pass depends on: whatever the tree did, the manifest the
		// signature path produces is the manifest a full capture produces.
		t.Run("same manifest as a full capture/"+f.mutation.name, func(t *testing.T) {
			ctx := context.Background()
			repositories := []workspace.Repository{f.repository}
			_, _, signatures, err := CaptureWithSignatures(ctx, "default", "resolver", "analyzer", repositories)
			if err != nil {
				t.Fatal(err)
			}
			f.mutation.apply(t, f.repository.RealPath)
			full, fullRepositories, err := CaptureWithRepositories(ctx, "default", "resolver", "analyzer", repositories)
			if err != nil {
				t.Fatal(err)
			}
			reused, reusedRepositories, _, err := CaptureReusing(ctx, "default", "resolver", "analyzer", repositories, signatures)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(full, reused) {
				t.Fatalf("manifest from the signature path = %#v, want the full capture's %#v", reused, full)
			}
			if !reflect.DeepEqual(fullRepositories, reusedRepositories) {
				t.Fatalf("observed repositories differ: %#v against %#v", reusedRepositories, fullRepositories)
			}
		})
	}

	// The reason the stamp carries ctime. A program can restore a modification
	// time; it cannot restore a change time. Without it the scenario that restores
	// the file's own modification time after a same-size edit is unseen.
	t.Run("compares the inode change time", func(t *testing.T) {
		before, signature, err := TreeDigestSigned(context.Background(), restore)
		if err != nil {
			t.Fatal(err)
		}
		for _, one := range mutations() {
			if one.name == restoredMtime {
				one.apply(t, restore)
			}
		}
		after, err := TreeDigest(context.Background(), restore)
		if err != nil {
			t.Fatal(err)
		}
		if before == after {
			t.Fatal("the scenario did not change the digest, so it proves nothing")
		}
		if signature.Unchanged() {
			t.Fatal("a same-size edit with the modification time restored went unseen: ctime is not compared")
		}
	})

	t.Run("falls back when a file the capture saw is gone", func(t *testing.T) {
		_, signature, err := TreeDigestSigned(context.Background(), removed)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(removed, "src/a.go")); err != nil {
			t.Fatal(err)
		}
		if signature.Unchanged() {
			t.Fatal("a removed file was reported unchanged")
		}
	})

	t.Run("falls back when a stat fails for a reason other than absence", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can stat through a directory it has no permission on")
		}
		_, signature, err := TreeDigestSigned(context.Background(), unreadable)
		if err != nil {
			t.Fatal(err)
		}
		src := filepath.Join(unreadable, "src")
		if err := os.Chmod(src, 0o000); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(src, 0o755)
		if signature.Unchanged() {
			t.Fatal("an unreadable directory was reported unchanged")
		}
	})

	// The digest follows a link, so its content is the target's, and a stat of the
	// link says nothing about it. The signature does not vouch for such a tree.
	t.Run("does not vouch for a tree with a symlinked source", func(t *testing.T) {
		_, signature, err := TreeDigestSigned(context.Background(), linked)
		if err != nil {
			t.Fatal(err)
		}
		if signature.Unchanged() {
			t.Fatal("a tree with a symlinked source was reported unchanged")
		}
	})

	t.Run("a missing signature vouches for nothing", func(t *testing.T) {
		var missing *TreeSignature
		if missing.Unchanged() {
			t.Fatal("a nil signature vouched for a tree")
		}
	})

	// A file written within the window of the capture can be written again in the
	// same tick without its stat moving, so it is never trusted. The tree here is
	// written after the wait above: it is exactly what the window distrusts.
	t.Run("distrusts an entry written just before the capture", func(t *testing.T) {
		_, signature, err := TreeDigestSigned(context.Background(), writeTree(t))
		if err != nil {
			t.Fatal(err)
		}
		if signature.Unchanged() {
			t.Fatal("a tree written a moment ago was reported unchanged: its entries are racy")
		}
	})

	t.Run("does not read the tree when nothing moved", func(t *testing.T) {
		repositories := []workspace.Repository{wide}
		_, _, signatures, err := CaptureWithSignatures(context.Background(), "default", "resolver", "analyzer", repositories)
		if err != nil {
			t.Fatal(err)
		}
		full := &errCountingContext{Context: context.Background()}
		if _, _, err := CaptureWithRepositories(full, "default", "resolver", "analyzer", repositories); err != nil {
			t.Fatal(err)
		}
		reusing := &errCountingContext{Context: context.Background()}
		_, _, report, err := CaptureReusing(reusing, "default", "resolver", "analyzer", repositories, signatures)
		if err != nil {
			t.Fatal(err)
		}
		if report.Repositories != 1 || report.Reused != 1 {
			t.Fatalf("report = %+v, want the one repository reused", report)
		}
		if full.calls < 150 {
			t.Fatalf("a full capture asked %d times, want one per entry (at least 150): the comparison below proves nothing", full.calls)
		}
		if reusing.calls >= full.calls/10 {
			t.Fatalf("the signature path asked %d times against %d for a full capture: it walked the tree", reusing.calls, full.calls)
		}
	})

	// A repository whose signature does not hold is read as it always was; one whose
	// signature holds beside it is still skipped. They do not contaminate each other.
	t.Run("falls back per repository", func(t *testing.T) {
		repositories := []workspace.Repository{moved, quiet}
		_, _, signatures, err := CaptureWithSignatures(context.Background(), "default", "resolver", "analyzer", repositories)
		if err != nil {
			t.Fatal(err)
		}
		if err := mutationWrite(moved.RealPath, "main.go", "package source\nconst Value = 2\n"); err != nil {
			t.Fatal(err)
		}
		_, _, report, err := CaptureReusing(context.Background(), "default", "resolver", "analyzer", repositories, signatures)
		if err != nil {
			t.Fatal(err)
		}
		if report.Repositories != 2 || report.Reused != 1 {
			t.Fatalf("report = %+v, want two repositories observed and one of them reused", report)
		}
	})

	t.Run("is not reused for a root other than the one it was taken from", func(t *testing.T) {
		_, _, signatures, err := CaptureWithSignatures(context.Background(), "default", "resolver", "analyzer", []workspace.Repository{quiet})
		if err != nil {
			t.Fatal(err)
		}
		_, _, report, err := CaptureReusing(context.Background(), "default", "resolver", "analyzer", []workspace.Repository{elsewhere}, signatures)
		if err != nil {
			t.Fatal(err)
		}
		if report.Reused != 0 {
			t.Fatalf("report = %+v, want a signature from another root not to be reused", report)
		}
	})
}
