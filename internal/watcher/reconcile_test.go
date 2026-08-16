package watcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestReconcilerDetectsChangesRemovalsRenamesAndManifests(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "src")
	if err := os.MkdirAll(sourceRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q): %v", sourceRoot, err)
	}
	unchanged := filepath.Join(sourceRoot, "unchanged.go")
	modified := filepath.Join(sourceRoot, "modified.go")
	removed := filepath.Join(sourceRoot, "removed.go")
	oldName := filepath.Join(sourceRoot, "old.go")
	newName := filepath.Join(sourceRoot, "new.go")
	added := filepath.Join(sourceRoot, "added.ts")
	manifest := filepath.Join(root, "package.json")
	customManifest := filepath.Join(root, "project.manifest")
	writeTestFile(t, unchanged, "package unchanged\n")
	writeTestFile(t, modified, "package before\n")
	writeTestFile(t, removed, "package removed\n")
	writeTestFile(t, oldName, "package renamed\n")
	writeTestFile(t, manifest, "{\"name\":\"before\"}\n")
	writeTestFile(t, customManifest, "manifest-before\n")
	oldHash := seedFileHash(t, "repo", oldName)
	known := []FileHash{
		seedFileHash(t, "repo", unchanged),
		seedFileHash(t, "repo", modified),
		seedFileHash(t, "repo", removed),
		oldHash,
		seedFileHash(t, "repo", manifest),
		seedFileHash(t, "repo", customManifest),
	}
	hasher, err := NewContentHasher(known)
	if err != nil {
		t.Fatalf("NewContentHasher() error = %v", err)
	}
	reconciler, err := NewReconciler([]workspace.Repository{{
		Name:       "repo",
		RealPath:   root,
		Languages:  []string{"go", "typescript"},
		Manifests:  []string{customManifest},
		Roots:      []string{sourceRoot},
		Exclusions: []string{"ignored/**"},
	}}, hasher)
	if err != nil {
		t.Fatalf("NewReconciler() error = %v", err)
	}
	if err := os.WriteFile(modified, []byte("package after\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", modified, err)
	}
	if err := os.WriteFile(manifest, []byte("{\"name\":\"after\"}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", manifest, err)
	}
	if err := os.WriteFile(customManifest, []byte("manifest-after\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", customManifest, err)
	}
	if err := os.Remove(removed); err != nil {
		t.Fatalf("Remove(%q): %v", removed, err)
	}
	if err := os.Rename(oldName, newName); err != nil {
		t.Fatalf("Rename(%q, %q): %v", oldName, newName, err)
	}
	writeTestFile(t, added, "export const added = true\n")
	writeTestFile(t, filepath.Join(root, "node_modules", "ignored.go"), "package ignored\n")

	result, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	assertStatePaths(t, "added", result.Added, added, newName)
	assertStatePaths(t, "modified", result.Modified, modified, manifest, customManifest)
	assertStatePaths(t, "unchanged", result.Unchanged, unchanged)
	assertStatePaths(t, "removed", result.Removed, removed, oldName)
	if len(result.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want no skipped paths", result.Skipped)
	}
	if len(result.Renamed) != 1 {
		t.Fatalf("Renamed = %v, want one rename", result.Renamed)
	}
	if got, want := result.Renamed[0].From.Path, oldName; got != want {
		t.Fatalf("rename source = %q, want %q", got, want)
	}
	if got, want := result.Renamed[0].To.Path, newName; got != want {
		t.Fatalf("rename destination = %q, want %q", got, want)
	}
	if got, want := result.Renamed[0].From.ContentHash, oldHash.ContentHash; got != want {
		t.Fatalf("rename source hash = %q, want %q", got, want)
	}
	assertStatePaths(t, "manifest changes", result.ManifestChanges, manifest, customManifest)
	if _, exists := hasher.KnownHash("repo", oldName); exists {
		t.Fatal("old rename path remains in hash cache")
	}
	if _, exists := hasher.KnownHash("repo", newName); !exists {
		t.Fatal("new rename path is missing from hash cache")
	}
}

func TestReconcilerDoesNotGuessAmbiguousRenames(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.go")
	second := filepath.Join(root, "second.go")
	destination := filepath.Join(root, "destination.go")
	for _, path := range []string{first, second} {
		writeTestFile(t, path, "package duplicate\n")
	}
	hasher, err := NewContentHasher([]FileHash{
		seedFileHash(t, "repo", first),
		seedFileHash(t, "repo", second),
	})
	if err != nil {
		t.Fatalf("NewContentHasher() error = %v", err)
	}
	reconciler, err := NewReconciler([]workspace.Repository{{Name: "repo", RealPath: root, Languages: []string{"go"}}}, hasher)
	if err != nil {
		t.Fatalf("NewReconciler() error = %v", err)
	}
	if err := os.Remove(first); err != nil {
		t.Fatalf("Remove(%q): %v", first, err)
	}
	if err := os.Remove(second); err != nil {
		t.Fatalf("Remove(%q): %v", second, err)
	}
	writeTestFile(t, destination, "package duplicate\n")

	result, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(result.Renamed) != 0 {
		t.Fatalf("Renamed = %v, want no ambiguous rename", result.Renamed)
	}
	assertStatePaths(t, "added", result.Added, destination)
	assertStatePaths(t, "removed", result.Removed, first, second)
}

func TestReconcilerRunRepeatsUntilCancellation(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\n")
	hasher, err := NewContentHasher(nil)
	if err != nil {
		t.Fatalf("NewContentHasher() error = %v", err)
	}
	reconciler, err := NewReconciler([]workspace.Repository{{Name: "repo", RealPath: root, Languages: []string{"go"}}}, hasher)
	if err != nil {
		t.Fatalf("NewReconciler() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	err = reconciler.Run(ctx, 10*time.Millisecond, func(result ReconciliationResult) error {
		calls++
		if calls == 2 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if calls != 2 {
		t.Fatalf("sink calls = %d, want two immediate/periodic reconciliations", calls)
	}
}

func TestReconcilerValidatesRunArguments(t *testing.T) {
	hasher, err := NewContentHasher(nil)
	if err != nil {
		t.Fatalf("NewContentHasher() error = %v", err)
	}
	reconciler, err := NewReconciler(nil, hasher)
	if err != nil {
		t.Fatalf("NewReconciler() error = %v", err)
	}
	if err := reconciler.Run(context.Background(), 0, func(ReconciliationResult) error { return nil }); err == nil {
		t.Fatal("Run() with zero interval returned nil error")
	}
	if err := reconciler.Run(context.Background(), time.Second, nil); err == nil {
		t.Fatal("Run() with nil sink returned nil error")
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func seedFileHash(t *testing.T, repository, path string) FileHash {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	digest := sha256.Sum256(content)
	return FileHash{Repository: repository, Path: path, ContentHash: hex.EncodeToString(digest[:])}
}

func assertStatePaths(t *testing.T, label string, states []FileState, want ...string) {
	t.Helper()
	if len(states) != len(want) {
		t.Fatalf("%s count = %d (%v), want %d paths", label, len(states), states, len(want))
	}
	got := make(map[string]struct{}, len(states))
	for _, state := range states {
		got[state.Path] = struct{}{}
	}
	for _, path := range want {
		if _, exists := got[path]; !exists {
			t.Fatalf("%s = %v, missing %q", label, states, path)
		}
	}
}

// TestReconcilerSeesRustSourcesAndManifests is what makes the Rust
// invalidation plan reachable: the classifier has a branch for a manifest and
// for a build script, and neither ever fires if the scan does not report them.
func TestReconcilerSeesRustSourcesAndManifests(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "Cargo.toml"), "[package]\nname = \"probe\"\nversion = \"0.1.0\"\n")
	writeTestFile(t, filepath.Join(root, "Cargo.lock"), "version = 4\n")
	writeTestFile(t, filepath.Join(root, "build.rs"), "fn main() {}\n")
	writeTestFile(t, filepath.Join(root, "src", "lib.rs"), "pub fn run() {}\n")
	writeTestFile(t, filepath.Join(root, "notes.md"), "not a source\n")

	hasher, err := NewContentHasher(nil)
	if err != nil {
		t.Fatalf("NewContentHasher() error = %v", err)
	}
	reconciler, err := NewReconciler([]workspace.Repository{{
		Name: "probe", Path: root, RealPath: root, Languages: []string{"rust"},
	}}, hasher)
	if err != nil {
		t.Fatalf("NewReconciler() error = %v", err)
	}
	result, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	seen := make(map[string]bool, len(result.Added))
	for _, state := range result.Added {
		seen[filepath.Base(state.Path)] = true
	}
	for _, want := range []string{"Cargo.toml", "Cargo.lock", "build.rs", "lib.rs"} {
		if !seen[want] {
			t.Fatalf("%q was not scanned: %#v", want, result.Added)
		}
	}
	if seen["notes.md"] {
		t.Fatal("a file no language declares must not be scanned")
	}
	manifests := make(map[string]bool, len(result.ManifestChanges))
	for _, state := range result.ManifestChanges {
		manifests[filepath.Base(state.Path)] = true
	}
	for _, want := range []string{"Cargo.toml", "Cargo.lock", "build.rs"} {
		if !manifests[want] {
			t.Fatalf("%q is not reported as a manifest change: %#v", want, result.ManifestChanges)
		}
	}
}
