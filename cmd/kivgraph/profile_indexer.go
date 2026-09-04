package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/freshness"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/indexing"
	"github.com/Luqueee/kivgraph/internal/version"
)

// profileProjectIndexer serializes full rebuilds across profiles and creates a
// named profile before its first pass. Analyzer processes are shared by the
// installation, so two profile rebuilds must not compete for them.
type profileProjectIndexer struct {
	gate           chan struct{}
	configPath     string
	store          *hotsnapshot.SnapshotStore
	watchMu        sync.RWMutex
	watch          func(string, config.Loaded, *hotsnapshot.SnapshotStore)
	freshnessRoot  string
	freshnessError error
}

func (indexer *profileProjectIndexer) setProfileWatcher(watch func(string, config.Loaded, *hotsnapshot.SnapshotStore)) {
	indexer.watchMu.Lock()
	defer indexer.watchMu.Unlock()
	indexer.watch = watch
}

func (indexer *profileProjectIndexer) watchProfile(name string, loaded config.Loaded, store *hotsnapshot.SnapshotStore) {
	indexer.watchMu.RLock()
	watch := indexer.watch
	indexer.watchMu.RUnlock()
	if watch != nil {
		watch(name, loaded, store)
	}
}

type namedProfileReindexer struct {
	indexer *profileProjectIndexer
	profile string
}

func (indexer namedProfileReindexer) Reindex(ctx context.Context) error {
	return indexer.indexer.ReindexProfile(ctx, indexer.profile)
}

func newProfileProjectIndexer(configPath string, store *hotsnapshot.SnapshotStore) *profileProjectIndexer {
	indexer := &profileProjectIndexer{gate: make(chan struct{}, 1), configPath: configPath, store: store}
	loaded, err := config.ReadProfile(configPath, store.DefaultProfileName())
	if err != nil {
		indexer.freshnessError = err
		return indexer
	}
	indexer.freshnessRoot = loaded.Config.Storage.DatabasePath
	return indexer
}

// ContentFreshness attests only the default this server actually serves. The
// on-disk default can change without changing this running store's identity.
// Unlike loadProfileStore, this read must never create or open another graph.
func (indexer *profileProjectIndexer) ContentFreshness(ctx context.Context) freshness.Status {
	if indexer == nil || indexer.store == nil {
		return freshness.Status{State: "unverified", Detail: "no snapshot store"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if indexer.freshnessError != nil {
		return freshness.Status{State: "unavailable", Detail: indexer.freshnessError.Error()}
	}
	loaded, err := config.ReadProfile(indexer.configPath, indexer.store.DefaultProfileName())
	if err != nil {
		return freshness.Status{State: "unavailable", Detail: err.Error()}
	}
	if loaded.Config.Storage.DatabasePath != indexer.freshnessRoot {
		return freshness.Status{State: "unavailable", Detail: "profile storage changed; restart the server before checking freshness"}
	}
	selected, err := indexer.store.ResolveProfiles(nil)
	if err != nil {
		return freshness.Status{State: "unavailable", Detail: err.Error()}
	}
	return indexing.NewService(loaded, selected[0].Store, version.Value, "").ContentFreshness(ctx)
}

func (indexer *profileProjectIndexer) IndexProjects(
	ctx context.Context,
	projects []indexing.Project,
	progress func(indexing.ProjectProgress),
) (indexing.ProjectResult, error) {
	loaded, err := config.Load(indexer.configPath)
	if err != nil {
		return indexing.ProjectResult{}, err
	}
	return indexer.IndexProjectsInProfile(ctx, loaded.Config.Profiles.Default, projects, progress)
}

func (indexer *profileProjectIndexer) IndexProjectsInProfile(
	ctx context.Context,
	profile string,
	projects []indexing.Project,
	progress func(indexing.ProjectProgress),
) (indexing.ProjectResult, error) {
	if indexer == nil || indexer.store == nil {
		return indexing.ProjectResult{}, errors.New("profile project indexer is not configured")
	}
	if err := config.ValidateProfileName(profile); err != nil {
		return indexing.ProjectResult{}, fmt.Errorf("profile name: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case indexer.gate <- struct{}{}:
		defer func() { <-indexer.gate }()
	case <-ctx.Done():
		return indexing.ProjectResult{}, ctx.Err()
	}

	loaded, profileStore, err := indexer.loadProfileStore(ctx, profile, true)
	if err != nil {
		return indexing.ProjectResult{}, err
	}
	service := indexing.NewService(loaded, profileStore, version.Value, "")
	return service.IndexProjects(ctx, projects, progress)
}

func (indexer *profileProjectIndexer) ReindexProfile(ctx context.Context, profile string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case indexer.gate <- struct{}{}:
		defer func() { <-indexer.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	loaded, profileStore, err := indexer.loadProfileStore(ctx, profile, false)
	if err != nil {
		return err
	}
	return indexing.NewService(loaded, profileStore, version.Value, "").Reindex(ctx)
}

func (indexer *profileProjectIndexer) loadProfileStore(
	ctx context.Context,
	profile string,
	create bool,
) (config.Loaded, *hotsnapshot.SnapshotStore, error) {
	loaded, err := config.LoadProfile(indexer.configPath, profile)
	if errors.Is(err, config.ErrProfileNotFound) {
		if !create {
			return config.Loaded{}, nil, err
		}
		if err := config.CreateProfile(indexer.configPath, profile); err != nil {
			return config.Loaded{}, nil, err
		}
		loaded, err = config.LoadProfile(indexer.configPath, profile)
	}
	if err != nil {
		return config.Loaded{}, nil, err
	}

	selected, err := indexer.store.ResolveProfiles([]string{profile})
	if err != nil {
		profileStore, openErr := openConfiguredSnapshot(ctx, loaded)
		if openErr != nil {
			return config.Loaded{}, nil, openErr
		}
		if addErr := indexer.store.AddProfile(profile, profileStore); addErr != nil {
			profileStore.Close()
			return config.Loaded{}, nil, addErr
		}
		indexer.watchProfile(profile, loaded, profileStore)
		selected = []hotsnapshot.ProfileStore{{Name: profile, Store: profileStore}}
	}
	return loaded, selected[0].Store, nil
}
