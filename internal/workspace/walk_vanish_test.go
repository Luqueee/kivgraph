package workspace

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// vanishingContext runs act the moment its Err method is called for the at-th
// time. Every discovery walk asks its context before it lists a directory and
// before it visits an entry, so this removes a path after its parent was listed
// and before it was read -- what a build writing in the tree does -- with no seam
// in the walk.
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

// Each discovery asks once on entry, once for the root directory it is about to
// list, and then once per entry: with a lone entry under the root, that entry is
// the third call and the listing of the root is the second.
const (
	rootCall  = 2
	entryCall = 3
)

type discoverer struct {
	name     string
	manifest string
	discover func(context.Context, string) (any, error)
}

func discoverers() []discoverer {
	return []discoverer{
		{"go", "go.mod", func(ctx context.Context, root string) (any, error) {
			return DiscoverGo(ctx, Repository{Path: root})
		}},
		{"typescript", "package.json", func(ctx context.Context, root string) (any, error) {
			return DiscoverTypeScript(ctx, Repository{Path: root})
		}},
		{"cargo", "Cargo.toml", func(ctx context.Context, root string) (any, error) {
			return DiscoverCargo(ctx, Repository{Path: root})
		}},
	}
}

func discoverAround(t *testing.T, d discoverer, root string, at int, act func()) (any, *vanishingContext, error) {
	t.Helper()
	ctx := &vanishingContext{Context: t.Context(), at: at, act: act}
	got, err := d.discover(ctx, root)
	return got, ctx, err
}

// TestDiscoveryIgnoresAnEntryThatVanishesMidWalk is the failure of a pass that
// ran while `cargo` was writing in a registered repository: a directory existed
// when its parent was listed and not when it was read, and the whole pass
// stopped on it. A vanished entry has to leave the discovery exactly as if it had
// never been there.
func TestDiscoveryIgnoresAnEntryThatVanishesMidWalk(t *testing.T) {
	for _, d := range discoverers() {
		t.Run(d.name+" directory", func(t *testing.T) {
			root := testsupport.TempDir(t)
			writeDiscoveryFile(t, filepath.Join(root, "gone", d.manifest), "x")

			got, ctx, err := discoverAround(t, d, root, entryCall, func() { _ = os.RemoveAll(filepath.Join(root, "gone")) })
			if ctx.calls < entryCall {
				t.Fatalf("the walk asked its context %d times, so nothing vanished: the call count this test relies on moved", ctx.calls)
			}
			if err != nil {
				t.Fatalf("discovery error = %v, want a vanished directory skipped", err)
			}
			want, err := d.discover(t.Context(), root)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("a vanished directory left a trace: got %#v, want %#v", got, want)
			}
		})
		t.Run(d.name+" file", func(t *testing.T) {
			root := testsupport.TempDir(t)
			writeDiscoveryFile(t, filepath.Join(root, d.manifest), "x")

			got, ctx, err := discoverAround(t, d, root, entryCall, func() { _ = os.Remove(filepath.Join(root, d.manifest)) })
			if ctx.calls < entryCall {
				t.Fatalf("the walk asked its context %d times, so nothing vanished: the call count this test relies on moved", ctx.calls)
			}
			if err != nil {
				t.Fatalf("discovery error = %v, want a vanished file skipped", err)
			}
			want, err := d.discover(t.Context(), root)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("a vanished file left a trace: got %#v, want %#v", got, want)
			}
		})
	}
}

// The repository itself going away is not an entry vanishing: a discovery of
// nothing would read as a repository with no modules.
func TestDiscoveryStillFailsWhenTheRootVanishes(t *testing.T) {
	for _, d := range discoverers() {
		t.Run(d.name, func(t *testing.T) {
			root := filepath.Join(testsupport.TempDir(t), "repo")
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			if _, _, err := discoverAround(t, d, root, rootCall, func() { _ = os.RemoveAll(root) }); err == nil {
				t.Fatal("discovery error = nil, want a repository that vanished to fail")
			}
		})
	}
}
