package watcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestContentHasherSkipsReindexForUnchangedContent(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	writeHashTestFile(t, file, "package main\n")
	hasher, err := NewContentHasher(nil)
	if err != nil {
		t.Fatalf("NewContentHasher() error = %v", err)
	}
	batch := Batch{Events: []Event{{Repository: "repo", Path: file, Operations: OperationWrite}}}

	first, err := hasher.Process(context.Background(), batch)
	if err != nil {
		t.Fatalf("Process(first) error = %v", err)
	}
	if len(first.Changed) != 1 || len(first.Unchanged) != 0 {
		t.Fatalf("first result = %#v, want one changed file", first)
	}
	wantFirstHash := hashTestContents("package main\n")
	if first.Changed[0].ContentHash != wantFirstHash {
		t.Fatalf("first hash = %q, want %q", first.Changed[0].ContentHash, wantFirstHash)
	}
	if got, ok := hasher.KnownHash("repo", file); !ok || got != wantFirstHash {
		t.Fatalf("KnownHash() = %q, %v, want %q, true", got, ok, wantFirstHash)
	}

	second, err := hasher.Process(context.Background(), batch)
	if err != nil {
		t.Fatalf("Process(second) error = %v", err)
	}
	if len(second.Changed) != 0 || len(second.Unchanged) != 1 {
		t.Fatalf("second result = %#v, want one unchanged file", second)
	}

	writeHashTestFile(t, file, "package main\n\nfunc changed() {}\n")
	third, err := hasher.Process(context.Background(), batch)
	if err != nil {
		t.Fatalf("Process(third) error = %v", err)
	}
	if len(third.Changed) != 1 || len(third.Unchanged) != 0 || third.Changed[0].ContentHash == wantFirstHash {
		t.Fatalf("third result = %#v, want one changed hash", third)
	}
}

func TestContentHasherRemovesKnownFilesAndSkipsDirectories(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "tracked.go")
	writeHashTestFile(t, file, "package tracked\n")
	hasher, err := NewContentHasher(nil)
	if err != nil {
		t.Fatalf("NewContentHasher() error = %v", err)
	}
	if _, err := hasher.Process(context.Background(), Batch{Events: []Event{{
		Repository: "repo", Path: file, Operations: OperationCreate,
	}}}); err != nil {
		t.Fatalf("Process(seed) error = %v", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatalf("Remove(%q): %v", file, err)
	}
	removed, err := hasher.Process(context.Background(), Batch{Events: []Event{{
		Repository: "repo", Path: file, Operations: OperationRemove,
	}}})
	if err != nil {
		t.Fatalf("Process(remove) error = %v", err)
	}
	if len(removed.Removed) != 1 || len(removed.Changed) != 0 {
		t.Fatalf("removed result = %#v, want one removed file", removed)
	}
	if _, ok := hasher.KnownHash("repo", file); ok {
		t.Fatal("KnownHash() still contains removed file")
	}

	directory := filepath.Join(root, "new-directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("Mkdir(%q): %v", directory, err)
	}
	skipped, err := hasher.Process(context.Background(), Batch{Events: []Event{{
		Repository: "repo", Path: directory, Operations: OperationCreate,
	}}})
	if err != nil {
		t.Fatalf("Process(directory) error = %v", err)
	}
	if len(skipped.Skipped) != 1 || len(skipped.Changed) != 0 {
		t.Fatalf("directory result = %#v, want one skipped path", skipped)
	}
}

func TestContentHasherCoalescesDuplicateEventsAndTracksReplacement(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "replacement.go")
	writeHashTestFile(t, file, "package replacement\n")
	hasher, err := NewContentHasher(nil)
	if err != nil {
		t.Fatalf("NewContentHasher() error = %v", err)
	}
	result, err := hasher.Process(context.Background(), Batch{Events: []Event{
		{Repository: "repo", Path: file, Operations: OperationWrite},
		{Repository: "repo", Path: file, Operations: OperationChmod},
	}})
	if err != nil {
		t.Fatalf("Process(duplicate events) error = %v", err)
	}
	if len(result.Changed) != 1 || result.Changed[0].Operations != OperationWrite|OperationChmod {
		t.Fatalf("duplicate result = %#v, want one merged change", result)
	}

	if err := os.Remove(file); err != nil {
		t.Fatalf("Remove(%q): %v", file, err)
	}
	if err := os.Mkdir(file, 0o700); err != nil {
		t.Fatalf("Mkdir(%q): %v", file, err)
	}
	replaced, err := hasher.Process(context.Background(), Batch{Events: []Event{{
		Repository: "repo", Path: file, Operations: OperationCreate,
	}}})
	if err != nil {
		t.Fatalf("Process(replacement) error = %v", err)
	}
	if len(replaced.Removed) != 1 || len(replaced.Skipped) != 0 {
		t.Fatalf("replacement result = %#v, want one removed file", replaced)
	}
}

func TestNewContentHasherValidatesKnownHashes(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "known.go")
	valid := hashTestContents("known")
	if _, err := NewContentHasher([]FileHash{{Repository: "repo", Path: file, ContentHash: valid}}); err != nil {
		t.Fatalf("NewContentHasher(valid) error = %v", err)
	}
	for _, invalid := range []string{"", "not-a-digest", "00"} {
		if _, err := NewContentHasher([]FileHash{{Repository: "repo", Path: file, ContentHash: invalid}}); err == nil {
			t.Fatalf("NewContentHasher(%q) unexpectedly succeeded", invalid)
		}
	}
	if _, err := NewContentHasher([]FileHash{{Repository: "", Path: file, ContentHash: valid}}); err == nil {
		t.Fatal("NewContentHasher(empty repository) unexpectedly succeeded")
	}
}

func TestContentHasherHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "canceled.go")
	writeHashTestFile(t, file, "package canceled\n")
	hasher, err := NewContentHasher(nil)
	if err != nil {
		t.Fatalf("NewContentHasher() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := hasher.Process(ctx, Batch{Events: []Event{{Repository: "repo", Path: file, Operations: OperationWrite}}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Process(canceled) error = %v, want context.Canceled", err)
	}
	if _, ok := hasher.KnownHash("repo", file); ok {
		t.Fatal("canceled Process() partially updated known hashes")
	}
}

func writeHashTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func hashTestContents(contents string) string {
	digest := sha256.Sum256([]byte(contents))
	return hex.EncodeToString(digest[:])
}
