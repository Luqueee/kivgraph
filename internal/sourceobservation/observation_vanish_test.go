package sourceobservation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// vanishingContext runs act the moment its Err method is called for the at-th
// time. TreeDigest asks its context at the start of every entry, so this
// removes a path after the walk listed it and before it opened it, which is what
// a build writing in the tree does, with no seam in the walk.
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

// The walk asks once for the root and then once per entry, so the lone entry
// under a root is the second call.
const entryCall = 2

func TestTreeDigestIgnoresADirectoryThatVanishesMidWalk(t *testing.T) {
	root := testsupport.TempDir(t)
	if err := os.MkdirAll(filepath.Join(root, "gone"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gone", "x.go"), []byte("package gone"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := TreeDigest(&vanishingContext{
		Context: context.Background(), at: entryCall,
		act: func() { _ = os.RemoveAll(filepath.Join(root, "gone")) },
	}, root)
	if err != nil {
		t.Fatalf("TreeDigest() error = %v, want a vanished directory skipped", err)
	}
	// An entry that vanished contributes exactly what one that was never there does.
	want, err := TreeDigest(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("a vanished directory left a trace in the digest")
	}
}

func TestTreeDigestIgnoresAFileThatVanishesMidWalk(t *testing.T) {
	root := testsupport.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := TreeDigest(&vanishingContext{
		Context: context.Background(), at: entryCall,
		act: func() { _ = os.Remove(filepath.Join(root, "a.go")) },
	}, root)
	if err != nil {
		t.Fatalf("TreeDigest() error = %v, want a vanished file skipped", err)
	}
	want, err := TreeDigest(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("a vanished file left a trace in the digest")
	}
}

// The tree itself going away is not an entry vanishing: a digest of nothing would
// read as an empty repository.
func TestTreeDigestStillFailsWhenTheRootVanishes(t *testing.T) {
	root := testsupport.TempDir(t)
	if _, err := TreeDigest(&vanishingContext{
		Context: context.Background(), at: 1,
		act: func() { _ = os.RemoveAll(root) },
	}, root); err == nil {
		t.Fatal("TreeDigest() error = nil, want a root that vanished to fail")
	}
}
