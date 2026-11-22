package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
)

func TestStampContentHashesRecordsOnlyWhatWasAnalysed(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string, modified time.Time) {
		t.Helper()
		filename := filepath.Join(root, name)
		if err := os.WriteFile(filename, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filename, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Now()
	write("old.go", "package old\n", started.Add(-time.Hour))
	// Edited while the pass ran: its digest would be of bytes nobody analysed.
	write("edited.go", "package edited\n", started.Add(time.Minute))
	write("exact.go", "package exact\n", started)
	write("kept.go", "package kept\n", started.Add(-time.Hour))
	if err := os.Mkdir(filepath.Join(root, "directory.go"), 0o700); err != nil {
		t.Fatal(err)
	}
	set := facts.Set{
		Repositories: []facts.Repository{{Key: "r", Name: "r", RootPath: root}, {Key: "nowhere", Name: "nowhere"}},
		Files: []facts.File{
			{Key: "old", RepositoryKey: "r", Path: "old.go"},
			{Key: "edited", RepositoryKey: "r", Path: "edited.go"},
			{Key: "exact", RepositoryKey: "r", Path: "exact.go"},
			{Key: "kept", RepositoryKey: "r", Path: "kept.go", ContentHash: "recorded-by-a-loader"},
			{Key: "missing", RepositoryKey: "r", Path: "missing.go"},
			{Key: "directory", RepositoryKey: "r", Path: "directory.go"},
			{Key: "rootless", RepositoryKey: "nowhere", Path: "old.go"},
		},
	}
	if err := stampContentHashes(context.Background(), &set, started); err != nil {
		t.Fatal(err)
	}

	digest := sha256.Sum256([]byte("package old\n"))
	want := map[string]string{
		"old": hex.EncodeToString(digest[:]), "edited": "", "exact": "", "kept": "recorded-by-a-loader",
		"missing": "", "directory": "", "rootless": "",
	}
	for _, file := range set.Files {
		if file.ContentHash != want[file.Key] {
			t.Errorf("%s: content hash = %q, want %q", file.Key, file.ContentHash, want[file.Key])
		}
	}
}

func TestStampContentHashesStopsWhenCancelled(t *testing.T) {
	root := t.TempDir()
	started := time.Now()
	filename := filepath.Join(root, "pending.go")
	if err := os.WriteFile(filename, []byte("package pending\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filename, started.Add(-time.Hour), started.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	set := facts.Set{
		Repositories: []facts.Repository{{Key: "r", Name: "r", RootPath: root}},
		Files:        []facts.File{{Key: "pending", RepositoryKey: "r", Path: "pending.go"}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := stampContentHashes(ctx, &set, started); !errors.Is(err, context.Canceled) {
		t.Fatalf("stampContentHashes() error = %v, want context.Canceled", err)
	}
	if hash := set.Files[0].ContentHash; hash != "" {
		t.Fatalf("cancelled pass recorded content hash %q", hash)
	}
}
