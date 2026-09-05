package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/indexing"
	"github.com/Luqueee/kivgraph/internal/invalidation"
	"github.com/Luqueee/kivgraph/internal/logging"
	"github.com/Luqueee/kivgraph/internal/sourceobservation"
	"github.com/Luqueee/kivgraph/internal/version"
	"github.com/Luqueee/kivgraph/internal/watcher"
)

const (
	// profileInvalidationAttempts bounds one unchanged source failure. A
	// later filesystem event resets the budget, so a fix or another edit is
	// always eligible for a fresh set of attempts.
	profileInvalidationAttempts = 5
	profileInvalidationRetry    = 2 * time.Second
)

// invalidationScheduler drains stale profiles one at a time. Full indexing
// already serializes analyzer targets in profileProjectIndexer; this queue
// adds durable stale-state awareness and prevents several profile watchers
// from requesting the same shared-source rebuild repeatedly.
type invalidationScheduler struct {
	manager *invalidation.Manager
	indexer profileReindexer
	logger  *slog.Logger

	ctx     context.Context
	cancel  context.CancelFunc
	wake    chan struct{}
	done    chan struct{}
	retries sync.WaitGroup

	mu       sync.Mutex
	pending  map[string]struct{}
	attempts map[string]int
}

type profileReindexer interface {
	ReindexProfile(context.Context, string) error
}

func newInvalidationScheduler(
	parent context.Context,
	manager *invalidation.Manager,
	indexer profileReindexer,
	logger *slog.Logger,
) *invalidationScheduler {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	if logger == nil {
		logger = logging.New(nil)
	}
	scheduler := &invalidationScheduler{
		manager:  manager,
		indexer:  indexer,
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
		wake:     make(chan struct{}, 1),
		done:     make(chan struct{}),
		pending:  make(map[string]struct{}),
		attempts: make(map[string]int),
	}
	go scheduler.run()
	return scheduler
}

func (scheduler *invalidationScheduler) enqueueStale() {
	if scheduler == nil || scheduler.manager == nil {
		return
	}
	state := scheduler.manager.Snapshot()
	profiles := make([]string, 0, len(state.Profiles))
	for _, profile := range state.Profiles {
		if profile.Stale {
			profiles = append(profiles, profile.Profile)
		}
	}
	sort.Strings(profiles)
	scheduler.enqueue(profiles, true)
}

func (scheduler *invalidationScheduler) enqueue(profiles []string, resetAttempts bool) {
	if scheduler == nil {
		return
	}
	scheduler.mu.Lock()
	for _, profile := range profiles {
		if profile == "" {
			continue
		}
		if resetAttempts {
			delete(scheduler.attempts, profile)
		}
		scheduler.pending[profile] = struct{}{}
	}
	hasPending := len(scheduler.pending) > 0
	scheduler.mu.Unlock()
	if hasPending {
		select {
		case scheduler.wake <- struct{}{}:
		default:
		}
	}
}

func (scheduler *invalidationScheduler) next() string {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	profiles := make([]string, 0, len(scheduler.pending))
	for profile := range scheduler.pending {
		profiles = append(profiles, profile)
	}
	if len(profiles) == 0 {
		return ""
	}
	sort.Strings(profiles)
	profile := profiles[0]
	delete(scheduler.pending, profile)
	return profile
}

func (scheduler *invalidationScheduler) run() {
	defer close(scheduler.done)
	for {
		select {
		case <-scheduler.ctx.Done():
			return
		case <-scheduler.wake:
		}
		for {
			profile := scheduler.next()
			if profile == "" {
				break
			}
			scheduler.reindex(profile)
			if scheduler.ctx.Err() != nil {
				return
			}
		}
	}
}

func (scheduler *invalidationScheduler) reindex(profile string) {
	if err := scheduler.manager.Refresh(scheduler.ctx); err != nil {
		scheduler.logger.Error("could not refresh source invalidation state", "profile", profile, "error", err)
		scheduler.retry(profile, err)
		return
	}
	if !profileIsStale(scheduler.manager.Snapshot(), profile) {
		scheduler.clearAttempts(profile)
		return
	}
	err := scheduler.indexer.ReindexProfile(scheduler.ctx, profile)
	if refreshErr := scheduler.manager.Refresh(scheduler.ctx); refreshErr != nil {
		scheduler.logger.Error("could not refresh source invalidation state after rebuild", "profile", profile, "error", refreshErr)
		scheduler.retry(profile, refreshErr)
		return
	}
	if !profileIsStale(scheduler.manager.Snapshot(), profile) {
		scheduler.clearAttempts(profile)
		return
	}
	if err == nil {
		err = errors.New("rebuild completed without clearing stale source state")
	}
	scheduler.retry(profile, err)
}

func (scheduler *invalidationScheduler) clearAttempts(profile string) {
	scheduler.mu.Lock()
	delete(scheduler.attempts, profile)
	scheduler.mu.Unlock()
}

func (scheduler *invalidationScheduler) retry(profile string, err error) {
	scheduler.mu.Lock()
	scheduler.attempts[profile]++
	attempt := scheduler.attempts[profile]
	scheduler.mu.Unlock()
	if attempt >= profileInvalidationAttempts {
		scheduler.logger.Error("gave up rebuilding a stale profile",
			"profile", profile, "attempts", attempt, "error", err,
			"remedy", "fix the source or analyzer failure and run `kivgraph index --full`")
		return
	}
	scheduler.logger.Error("could not rebuild stale profile",
		"profile", profile, "attempt", attempt, "error", err)
	scheduler.retries.Add(1)
	go func() {
		defer scheduler.retries.Done()
		timer := time.NewTimer(profileInvalidationRetry)
		defer timer.Stop()
		select {
		case <-scheduler.ctx.Done():
		case <-timer.C:
			if scheduler.ctx.Err() == nil {
				scheduler.enqueue([]string{profile}, false)
			}
		}
	}()
}

func (scheduler *invalidationScheduler) Close() {
	if scheduler == nil {
		return
	}
	scheduler.cancel()
	<-scheduler.done
	scheduler.retries.Wait()
}

func profileIsStale(state invalidation.State, profile string) bool {
	for _, record := range state.Profiles {
		if record.Profile == profile {
			return record.Stale
		}
	}
	return false
}

// watchProfileSources starts the source watcher for one profile. The initial
// observation catches edits made before the server started; the watcher then
// handles low-latency events and periodic reconciliation handles dropped
// notifications.
func watchProfileSources(
	ctx context.Context,
	loaded config.Loaded,
	manager *invalidation.Manager,
	scheduler *invalidationScheduler,
	command string,
) func() {
	logger := logging.New(os.Stderr)
	watchCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		options := indexing.OptionsFromConfig(loaded.Config)
		options.Profile = loaded.Profile
		options.Repositories = nil
		options.ResolverVersion = version.Value
		registry, err := registryForProfile(watchCtx, loaded)
		if err != nil {
			logger.Error("could not prepare source watcher", "command", command, "profile", loaded.Profile, "error", err)
			return
		}
		options.Repositories = registry.List()
		repositories := options.Repositories
		manifest, observedRepositories, err := indexing.ObserveSources(watchCtx, options)
		if err != nil {
			if stateErr := handleUnavailableProfileSources(watchCtx, manager, loaded.Profile, err, logger, command); stateErr != nil {
				logger.Error("could not synchronize unavailable source state", "command", command, "profile", loaded.Profile, "error", stateErr)
			}
			scheduler.enqueueStale()
		} else {
			repositories = observedRepositories
			if err := invalidateProfile(watchCtx, manager, loaded.Profile, manifest); err != nil &&
				!errors.Is(err, invalidation.ErrProfileNotTracked) {
				logger.Error("could not synchronize source invalidation state", "command", command, "profile", loaded.Profile, "error", err)
			}
			scheduler.enqueueStale()
		}
		if len(repositories) == 0 {
			return
		}

		err = indexing.WatchSources(watchCtx, indexing.SourceWatchOptions{
			Repositories:           repositories,
			Debounce:               time.Duration(loaded.Config.Watcher.DebounceMilliseconds) * time.Millisecond,
			MaximumBatch:           time.Duration(loaded.Config.Watcher.MaximumBatchMilliseconds) * time.Millisecond,
			ReconciliationInterval: time.Duration(loaded.Config.Watcher.ReconciliationInterval),
			ReportInitialChanges:   true,
			OnChange: func(changeCtx context.Context, _ watcher.ReconciliationResult) error {
				return refreshInvalidationForProfile(changeCtx, loaded, manager, scheduler, logger, command)
			},
			OnError: func(err error) {
				logger.Error("source watcher error", "command", command, "profile", loaded.Profile, "error", err)
			},
		})
		if err != nil && watchCtx.Err() == nil {
			logger.Error("source watcher stopped", "command", command, "profile", loaded.Profile, "error", err)
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func refreshInvalidationForProfile(
	ctx context.Context,
	loaded config.Loaded,
	manager *invalidation.Manager,
	scheduler *invalidationScheduler,
	logger *slog.Logger,
	command string,
) error {
	options := indexing.OptionsFromConfig(loaded.Config)
	options.Profile = loaded.Profile
	options.ResolverVersion = version.Value
	registry, err := registryForProfile(ctx, loaded)
	if err != nil {
		if stateErr := handleUnavailableProfileSources(ctx, manager, loaded.Profile, err, logger, command); stateErr != nil {
			return stateErr
		}
		scheduler.enqueueStale()
		return nil
	}
	options.Repositories = registry.List()
	manifest, _, err := indexing.ObserveSources(ctx, options)
	if err != nil {
		if stateErr := handleUnavailableProfileSources(ctx, manager, loaded.Profile, err, logger, command); stateErr != nil {
			return stateErr
		}
		scheduler.enqueueStale()
		return nil
	}
	if err := invalidateProfile(ctx, manager, loaded.Profile, manifest); err != nil &&
		!errors.Is(err, invalidation.ErrProfileNotTracked) {
		return err
	}
	scheduler.enqueueStale()
	return nil
}

func invalidateProfile(
	ctx context.Context,
	manager *invalidation.Manager,
	profile string,
	manifest sourceobservation.Manifest,
) error {
	if err := manager.Refresh(ctx); err != nil {
		return fmt.Errorf("refresh invalidation state: %w", err)
	}
	return manager.Invalidate(ctx, profile, manifest)
}

func handleUnavailableProfileSources(
	ctx context.Context,
	manager *invalidation.Manager,
	profile string,
	detail error,
	logger *slog.Logger,
	command string,
) error {
	if err := manager.Refresh(ctx); err != nil {
		logger.Error("could not refresh source invalidation state", "command", command, "profile", profile, "error", err)
		return fmt.Errorf("refresh unavailable source state: %w", err)
	}
	state := manager.Snapshot()
	var stateErr error
	for _, record := range state.Profiles {
		if record.Profile != profile {
			continue
		}
		for _, source := range record.Manifest.Sources {
			if err := manager.MarkStale(ctx, source.Observation.Worktree, source.Repository,
				invalidation.ReasonSourceUnavailable, detail.Error()); err != nil {
				logger.Error("could not mark unavailable source stale", "command", command,
					"profile", profile, "repository", source.Repository, "error", err)
				stateErr = errors.Join(stateErr, fmt.Errorf("mark %s unavailable: %w", source.Repository, err))
			}
		}
	}
	logger.Error("could not observe profile sources", "command", command, "profile", profile, "error", detail)
	return stateErr
}
