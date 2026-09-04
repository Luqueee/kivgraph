package indexing

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/freshness"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestContentFreshnessUsesCachedGenerationWithoutFilesystemScan(t *testing.T) {
	repositoryRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repositoryRoot, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repository := workspace.Repository{Name: "test", Path: repositoryRoot, Languages: []string{"go"}}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(
		hotsnapshot.LadybugSnapshotRows{}, 42, time.Unix(1, 0).UTC(), 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(config.Loaded{
		Config: config.DefaultConfig(),
		Repositories: config.RepositoriesFile{
			Version:      config.CurrentSchemaVersion,
			Repositories: []config.Repository{{Name: repository.Name, Path: repository.Path, Languages: repository.Languages}},
		},
	}, hotsnapshot.NewSnapshotStore(snapshot), "resolver-v1", "")
	service.freshnessCache.Store(freshness.Status{Generation: 42, State: "fresh"})
	if err := os.WriteFile(filepath.Join(repositoryRoot, "main.go"), []byte("package changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := service.ContentFreshness(t.Context())
	if got.Generation != 42 || got.State != "fresh" {
		t.Fatalf("edited input %q: content freshness = %+v, want the cached generation 42", filepath.Join(repositoryRoot, "main.go"), got)
	}
}

func TestContentFreshnessReturnsCachedDeferredGeneration(t *testing.T) {
	store := hotsnapshot.NewDeferredSnapshotStore(42, func() (*hotsnapshot.GraphSnapshot, error) {
		return nil, errors.New("deferred loader must not be called")
	})
	service := NewService(config.Loaded{}, store, "resolver-v1", "")
	service.freshnessCache.Store(freshness.Status{Generation: 42, State: "fresh"})

	got := service.ContentFreshness(t.Context())
	if got.Generation != 42 || got.State != "fresh" {
		t.Fatalf("content freshness = %+v, want the cached deferred generation 42", got)
	}
}
