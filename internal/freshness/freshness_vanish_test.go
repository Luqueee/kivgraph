package freshness

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/workspace"
)

// vanishingContext runs act the moment its Err method is called for the at-th
// time. The inventory walk asks its context at the start of every entry, so this
// removes a path after the walk listed it and before it opened it -- the race a
// build in a registered repository produces -- without a seam in the walk.
type vanishingContext struct {
	context.Context
	at, calls int
	act       func()
}

func (c *vanishingContext) Err() error {
	c.calls++
	if c.calls == c.at {
		c.act()
	}
	return c.Context.Err()
}

func capturingAround(t *testing.T, root string, at int, act func()) (string, error) {
	t.Helper()
	ctx := &vanishingContext{Context: t.Context(), at: at, act: act}
	return Capture(ctx, []workspace.Repository{{Name: "test", Path: root}})
}

// Capture asks its context once for the repository and then once per entry, so
// with a lone entry under the root the entry is the third call.
const entryCall = 3

// TestInventoryIgnoresADirectoryThatVanishesMidWalk is the failure of a pass
// that ran while `cargo` was writing in a registered repository: a temporary
// directory existed when its parent was listed and not when it was read, and
// the whole pass stopped on it.
func TestInventoryIgnoresADirectoryThatVanishesMidWalk(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "gone"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gone", "x.go"), []byte("package gone"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := capturingAround(t, root, entryCall, func() { _ = os.RemoveAll(filepath.Join(root, "gone")) })
	if err != nil {
		t.Fatalf("Capture() error = %v, want a vanished directory skipped", err)
	}
	// An entry that vanished contributes exactly what one that was never there does.
	want, err := Capture(t.Context(), []workspace.Repository{{Name: "test", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("a vanished directory left a trace in the digest")
	}
}

func TestInventoryIgnoresAFileThatVanishesMidWalk(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := capturingAround(t, root, entryCall, func() { _ = os.Remove(filepath.Join(root, "a.go")) })
	if err != nil {
		t.Fatalf("Capture() error = %v, want a vanished file skipped", err)
	}
	want, err := Capture(t.Context(), []workspace.Repository{{Name: "test", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("a vanished file left a trace in the digest")
	}
}

// What must stay an error: the repository itself going away is not an entry
// vanishing, and a file that cannot be read for another reason is not either.
func TestInventoryStillFailsWhenTheRootVanishes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	// The walk starts on the root: the call after the one for the repository.
	if _, err := capturingAround(t, root, 2, func() { _ = os.RemoveAll(root) }); err == nil {
		t.Fatal("Capture() error = nil, want a repository that vanished to fail")
	}
}

func TestInventoryStillFailsOnAFileItCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	root := t.TempDir()
	unreadable := filepath.Join(root, "a.go")
	if err := os.WriteFile(unreadable, []byte("package a"), 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(t.Context(), []workspace.Repository{{Name: "test", Path: root}}); err == nil {
		t.Fatal("Capture() error = nil, want an unreadable file to fail, not be skipped")
	}
}
