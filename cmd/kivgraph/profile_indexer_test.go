package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

func TestProfileProjectIndexerRejectsInvalidRoutingBeforeIndexing(t *testing.T) {
	var nilIndexer *profileProjectIndexer
	if _, err := nilIndexer.IndexProjectsInProfile(context.Background(), "default", nil, nil); err == nil {
		t.Fatal("nil IndexProjectsInProfile() error = nil")
	}
	indexer := newProfileProjectIndexer("missing-config.yaml", hotsnapshot.NewSnapshotStore(nil))
	if _, err := indexer.IndexProjectsInProfile(context.Background(), "../other", nil, nil); err == nil {
		t.Fatal("invalid profile IndexProjectsInProfile() error = nil")
	}
	if _, err := indexer.IndexProjects(context.Background(), nil, nil); err == nil {
		t.Fatal("IndexProjects() with missing configuration error = nil")
	}
}

func TestProfileProjectIndexerCreatesAndRoutesProfileBeforeValidatingBatch(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}
	defaultStore := hotsnapshot.NewSnapshotStore(nil)
	aggregate, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{
		"default": defaultStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	indexer := newProfileProjectIndexer(configPath, aggregate)
	var watched string
	indexer.setProfileWatcher(func(name string, _ config.Loaded, _ *hotsnapshot.SnapshotStore) { watched = name })

	if _, err := indexer.IndexProjectsInProfile(context.Background(), "other", nil, nil); err == nil {
		t.Fatalf("IndexProjectsInProfile() error = %v, want empty-batch refusal", err)
	}
	if _, err := aggregate.ResolveProfiles([]string{"other"}); err != nil {
		t.Fatalf("created profile is not queryable: %v", err)
	}
	if _, err := config.LoadProfile(configPath, "other"); err != nil {
		t.Fatalf("created profile is not durable: %v", err)
	}
	if watched != "other" {
		t.Fatalf("watched profile = %q, want other", watched)
	}
	if _, err := indexer.IndexProjects(context.Background(), nil, nil); err == nil {
		t.Fatalf("default IndexProjects() error = %v, want empty-batch refusal", err)
	}
}

func TestProfileReindexerHonoursCancellationWhileAnotherProfileOwnsTheGate(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}
	aggregate, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{
		"default": hotsnapshot.NewSnapshotStore(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	indexer := newProfileProjectIndexer(configPath, aggregate)
	indexer.gate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (namedProfileReindexer{indexer: indexer, profile: "default"}).Reindex(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Reindex() error = %v, want context cancellation", err)
	}
	<-indexer.gate
}
