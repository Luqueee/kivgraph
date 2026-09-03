package freshness

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestInventoryRejectsMissingRootsAndCancellation(t *testing.T) {
	if _, err := Capture(t.Context(), []workspace.Repository{{Name: "missing", Path: filepath.Join(t.TempDir(), "absent")}}); err == nil {
		t.Fatal("missing root accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Capture(ctx, []workspace.Repository{{Name: "x", Path: t.TempDir()}}); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestInventoryTracksEditsAdditionsDeletionsAndExclusions(t *testing.T) {
	root := t.TempDir()
	repos := []workspace.Repository{{Name: "test", Path: root, Exclusions: []string{"build"}}}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	capture := func() string {
		t.Helper()
		value, err := Capture(t.Context(), repos)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	write("main.go", "package main")
	initial := capture()
	write("main.go", "package changed")
	if capture() == initial {
		t.Fatal("edit invisible")
	}
	write("main.go", "package main")
	if capture() != initial {
		t.Fatal("restored content differs")
	}
	write("new.go", "package main")
	if capture() == initial {
		t.Fatal("new file invisible")
	}
	if err := os.Remove(filepath.Join(root, "new.go")); err != nil {
		t.Fatal(err)
	}
	if capture() != initial {
		t.Fatal("deletion not tracked")
	}
	if err := os.Mkdir(filepath.Join(root, "build"), 0700); err != nil {
		t.Fatal(err)
	}
	write("build/generated.go", "ignored")
	if capture() != initial {
		t.Fatal("excluded output changed inventory")
	}
	repos[0].Exclusions = nil
	if capture() == initial {
		t.Fatal("registry exclusions not fingerprinted")
	}
}

func TestAttestationIsGenerationBoundAndFailsClosed(t *testing.T) {
	root := t.TempDir()
	source := t.TempDir()
	repos := []workspace.Repository{{Name: "test", Path: source}}
	if got := Check(t.Context(), root, 1, repos); got.State != "unverified" {
		t.Fatal(got)
	}
	digest, err := Capture(t.Context(), repos)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(root, 1, digest); err != nil {
		t.Fatal(err)
	}
	if got := Check(t.Context(), root, 1, repos); got.State != "fresh" {
		t.Fatal(got)
	}
	if got := Check(t.Context(), root, 2, repos); got.State != "unverified" {
		t.Fatal(got)
	}
	if err := os.WriteFile(filepath.Join(source, "new.go"), []byte("package main"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := Check(t.Context(), root, 1, repos); got.State != "stale" {
		t.Fatal(got)
	}
	repos[0].Path = filepath.Join(source, "missing")
	if got := Check(t.Context(), root, 1, repos); got.State != "unavailable" {
		t.Fatal(got)
	}
	if err := os.WriteFile(recordPath(root, 1), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := Check(t.Context(), root, 1, repos); got.State != "unverified" {
		t.Fatal(got)
	}
}

func TestSourceSymlinksAreNotFollowed(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside.go"), filepath.Join(root, "link.go")); err != nil {
		t.Skip(err)
	}
	if _, err := Capture(t.Context(), []workspace.Repository{{Name: "test", Path: root}}); err == nil {
		t.Fatal("source symlink accepted")
	}
}

// Remaining filesystem-error branches require failed reads/writes/renames after
// successful opens; no production fault-injection seam is added for these.
