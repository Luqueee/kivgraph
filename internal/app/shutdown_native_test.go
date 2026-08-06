//go:build ladybug && cgo

package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
)

func TestLifecycleClosesLadybugDatabaseAndReleasesItsConnections(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "graph.db")
	set := facts.Set{
		Repositories: []facts.Repository{{Key: "repo:acme", Name: "acme", RootPath: "/repos/acme"}},
		Packages:     []facts.Package{{Key: "package:acme/widgets", RepositoryKey: "repo:acme", Language: facts.LanguageGo, Name: "widgets", RootPath: "/repos/acme"}},
		Files:        []facts.File{{Key: "file:acme/widgets:widgets.go", RepositoryKey: "repo:acme", PackageKey: "package:acme/widgets", Path: "widgets.go", Language: facts.LanguageGo}},
	}
	if _, err := ladybug.LoadCanonical(ctx, path, set, ladybug.CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "shutdown-test"}); err != nil {
		t.Fatalf("LoadCanonical() error = %v", err)
	}
	database, err := ladybug.Open(ctx, path, ladybug.DefaultConfig())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	// Health opens and closes a native connection internally. The lifecycle
	// owns the database that owns those connections; its Close implementation
	// also closes any reader/writer handles registered by future consumers.
	if err := database.Health(ctx); err != nil {
		database.Close()
		t.Fatalf("Health() error = %v", err)
	}

	lifecycle := NewLifecycle(ctx)
	if err := lifecycle.AddCloser("LadybugDB", database); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	reopened, err := ladybug.Open(ctx, path, ladybug.DefaultConfig())
	if err != nil {
		t.Fatalf("Open() after lifecycle shutdown error = %v", err)
	}
	if err := reopened.Health(ctx); err != nil {
		reopened.Close()
		t.Fatalf("Health() after lifecycle shutdown error = %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("Close() reopened database error = %v", err)
	}
}
