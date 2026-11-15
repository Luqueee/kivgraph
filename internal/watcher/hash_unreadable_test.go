//go:build unix

package watcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/workspace"
)

// One path that cannot be read used to fail the whole batch: every other
// change in it was thrown away, the cache was not updated, and each later
// reconciliation failed the same way for as long as the path stayed unreadable.
func TestContentHasherProcessesTheRestOfABatchWithAnUnreadablePath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file without permission, so nothing is unreadable")
	}
	root := t.TempDir()
	first, locked, last := filepath.Join(root, "a.go"), filepath.Join(root, "b.go"), filepath.Join(root, "c.go")
	for _, file := range []string{first, locked, last} {
		writeHashTestFile(t, file, "package p\n")
	}
	hasher, err := NewContentHasher(nil)
	if err != nil {
		t.Fatal(err)
	}
	batch := Batch{Events: []Event{
		{Repository: "repo", Path: first, Operations: OperationWrite},
		{Repository: "repo", Path: locked, Operations: OperationWrite},
		{Repository: "repo", Path: last, Operations: OperationWrite},
	}}
	if _, err := hasher.Process(context.Background(), batch); err != nil {
		t.Fatalf("Process(seed) error = %v", err)
	}

	writeHashTestFile(t, first, "package p\n\nvar A = 1\n")
	writeHashTestFile(t, last, "package p\n\nvar C = 1\n")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })

	result, err := hasher.Process(context.Background(), batch)
	if !errors.Is(err, ErrUnreadablePath) {
		t.Fatalf("Process() error = %v, want ErrUnreadablePath naming the path", err)
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("Process() error = %v, want the cause kept", err)
	}
	assertStatePaths(t, "changed", result.Changed, first, last)
	// It left the tracked set: its content is no longer something this
	// cache can vouch for, and that is a change a caller has to act on.
	assertStatePaths(t, "removed", result.Removed, locked)
	for _, file := range []string{first, last} {
		if hash, known := hasher.KnownHash("repo", file); !known || hash == hashTestContents("package p\n") {
			t.Fatalf("KnownHash(%s) = %q, %v, want the new content recorded", filepath.Base(file), hash, known)
		}
	}
	if _, known := hasher.KnownHash("repo", locked); known {
		t.Fatal("the unreadable path kept a hash nobody can verify")
	}

	// Still unreadable: reported again, and not announced a second time.
	again, err := hasher.Process(context.Background(), batch)
	if !errors.Is(err, ErrUnreadablePath) {
		t.Fatalf("Process(again) error = %v, want ErrUnreadablePath", err)
	}
	if len(again.Removed) != 0 || len(again.Changed) != 0 {
		t.Fatalf("second result = %#v, want no change announced twice", again)
	}

	// Readable again: it comes back as a change.
	if err := os.Chmod(locked, 0o600); err != nil {
		t.Fatal(err)
	}
	back, err := hasher.Process(context.Background(), batch)
	if err != nil {
		t.Fatalf("Process(readable again) error = %v", err)
	}
	assertStatePaths(t, "changed", back.Changed, locked)
}

// A parent directory that became a file answers ENOTDIR, not ENOENT. The
// tracked file under it is just as gone.
func TestContentHasherTreatsAParentTurnedFileAsRemoved(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "pkg")
	file := filepath.Join(directory, "a.go")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeHashTestFile(t, file, "package pkg\n")
	hasher, err := NewContentHasher(nil)
	if err != nil {
		t.Fatal(err)
	}
	batch := Batch{Events: []Event{{Repository: "repo", Path: file, Operations: OperationWrite}}}
	if _, err := hasher.Process(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(directory); err != nil {
		t.Fatal(err)
	}
	writeHashTestFile(t, directory, "now a file\n")

	result, err := hasher.Process(context.Background(), batch)
	if err != nil {
		t.Fatalf("Process() error = %v, want the path treated as absent", err)
	}
	assertStatePaths(t, "removed", result.Removed, file)
}

// The reconciler hands the partial result on with the error, so the caller can
// both act on the changes and report what could not be read.
func TestReconcilerReturnsTheReadableChangesBesideAnUnreadablePath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file without permission, so nothing is unreadable")
	}
	root := t.TempDir()
	readable, locked := filepath.Join(root, "a.go"), filepath.Join(root, "b.go")
	writeHashTestFile(t, readable, "package p\n")
	writeHashTestFile(t, locked, "package p\n")
	hasher, err := NewContentHasher(nil)
	if err != nil {
		t.Fatal(err)
	}
	reconciler, err := NewReconciler([]workspace.Repository{{Name: "repo", Path: root, RealPath: root, Languages: []string{"go"}}}, hasher)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile(seed) error = %v", err)
	}
	writeHashTestFile(t, readable, "package p\n\nvar A = 1\n")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })

	for name, run := range map[string]func() (ReconciliationResult, error){
		"Reconcile": func() (ReconciliationResult, error) { return reconciler.Reconcile(context.Background()) },
		"Process": func() (ReconciliationResult, error) {
			writeHashTestFile(t, readable, "package p\n\nvar A = 2\n")
			return reconciler.Process(context.Background(), Batch{Events: []Event{
				{Repository: "repo", Path: readable, Operations: OperationWrite},
				{Repository: "repo", Path: locked, Operations: OperationWrite},
			}})
		},
	} {
		result, err := run()
		if !errors.Is(err, ErrUnreadablePath) {
			t.Fatalf("%s() error = %v, want ErrUnreadablePath", name, err)
		}
		assertStatePaths(t, name+" modified", result.Modified, readable)
	}
}
