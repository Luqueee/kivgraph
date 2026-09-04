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
	freshnessMu    sync.RWMutex
	freshness      map[string]*freshness.Cache
	defaultProfile string
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
	return &profileProjectIndexer{
		gate:           make(chan struct{}, 1),
		configPath:     configPath,
		store:          store,
		freshness:      make(map[string]*freshness.Cache),
		defaultProfile: "default",
	}
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
	result, err := service.IndexProjects(ctx, projects, progress)
	if err == nil {
		cache := indexer.freshnessCache(profile, profileStore)
		cache.Store(service.ContentFreshness(ctx))
		watchLoaded, watchErr := config.LoadProfile(indexer.configPath, profile)
		if watchErr != nil {
			cache.MarkUnavailable(
				fmt.Sprintf("refresh content-freshness registry: %v", watchErr))
		} else {
			indexer.watchProfile(profile, watchLoaded, profileStore)
		}
	}
	return result, err
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
	service := indexing.NewService(loaded, profileStore, version.Value, "")
	if err := service.Reindex(ctx); err != nil {
		return err
	}
	indexer.freshnessCache(profile, profileStore).Store(service.ContentFreshness(ctx))
	return nil
}

func (indexer *profileProjectIndexer) setDefaultProfile(profile string) {
	indexer.freshnessMu.Lock()
	if profile != "" {
		indexer.defaultProfile = profile
	}
	indexer.freshnessMu.Unlock()
}

func (indexer *profileProjectIndexer) freshnessCache(profile string, store *hotsnapshot.SnapshotStore) *freshness.Cache {
	indexer.freshnessMu.Lock()
	defer indexer.freshnessMu.Unlock()
	if indexer.freshness == nil {
		indexer.freshness = make(map[string]*freshness.Cache)
	}
	if cache := indexer.freshness[profile]; cache != nil {
		return cache
	}
	status := freshness.Status{State: "unverified", Detail: "no published generation"}
	if store != nil {
		if generation, known := store.ActiveID(); known {
			status.Generation = generation
			status.Detail = "content freshness is not cached for this server"
		}
	}
	cache := freshness.NewCache(status)
	indexer.freshness[profile] = cache
	return cache
}

// ContentFreshness is the MCP host-status fast path. It compares two
// in-memory generation numbers and returns the last cached observation; it
// never opens the registry or scans a repository during graph_status.
func (indexer *profileProjectIndexer) ContentFreshness(_ context.Context) freshness.Status {
	if indexer == nil || indexer.store == nil {
		return freshness.Status{State: "unverified", Detail: "no snapshot store"}
	}
	indexer.freshnessMu.RLock()
	profile := indexer.defaultProfile
	indexer.freshnessMu.RUnlock()
	cache := indexer.freshnessCache(profile, indexer.store)
	generation, known := indexer.store.ActiveID()
	if !known {
		return freshness.Status{State: "unverified", Detail: "no published generation"}
	}
	status := cache.Load()
	if status.Generation != generation {
		return freshness.Status{
			Generation: generation,
			State:      "unverified",
			Detail:     "cached content freshness belongs to another generation",
		}
	}
	return status
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
