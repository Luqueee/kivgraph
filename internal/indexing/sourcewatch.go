package indexing

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Luqueee/kivgraph/internal/watcher"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// DefaultReconciliationInterval is the recovery interval used when a source
// monitor is not given an explicit interval.
const DefaultReconciliationInterval = 10 * time.Minute

// SourceWatchRetryInterval is how often a failed change callback is retried.
const SourceWatchRetryInterval = time.Second

// ErrChangeHandlerRequired means WatchSources cannot report detected changes
// because the caller did not provide a change handler.
var ErrChangeHandlerRequired = errors.New("source watch change handler is required")

// SourceWatchOptions configures the low-latency source monitor used by a
// long-running server. WatchSources reports only source or provider-manifest
// changes; callers decide how to observe the new manifest and which profiles
// to rebuild.
type SourceWatchOptions struct {
	Repositories []workspace.Repository
	Debounce     time.Duration
	MaximumBatch time.Duration
	// ReconciliationInterval is the recovery interval for notifications that a
	// filesystem backend dropped or could not represent.
	ReconciliationInterval time.Duration
	// ReportInitialChanges asks the monitor to deliver the initial scan. This is
	// useful when a caller has a previously recorded manifest and needs to close
	// the gap between that observation and watcher startup.
	ReportInitialChanges bool

	// OnChange receives a coalesced source change. It should enqueue work rather
	// than perform a full rebuild synchronously, so the monitor remains able to
	// consume later filesystem events.
	OnChange func(context.Context, watcher.ReconciliationResult) error
	// OnError receives non-fatal watcher, hashing, and reconciliation errors.
	// The monitor continues so a transient unreadable file does not silently
	// disable future invalidation.
	OnError func(error)
}

// WatchSources observes configured source trees until ctx is cancelled.
// Filesystem events provide prompt notification and periodic reconciliation
// provides recovery from a dropped event. The first reconciliation seeds the
// content cache and is normally not reported as a change: starting a server
// must not rebuild every tracked profile merely because its cache was empty in
// this process. ReportInitialChanges lets a caller with an existing manifest
// close the observation-to-watcher startup gap without making that default
// expensive.
func WatchSources(ctx context.Context, options SourceWatchOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.OnChange == nil {
		return fmt.Errorf("source watch: %w", ErrChangeHandlerRequired)
	}
	debounce := options.Debounce
	if debounce <= 0 {
		debounce = watcher.DefaultDebounce
	}
	maximum := options.MaximumBatch
	if maximum <= 0 {
		maximum = watcher.DefaultMaximumBatch
	}
	reconciliationInterval := options.ReconciliationInterval
	if reconciliationInterval <= 0 {
		reconciliationInterval = DefaultReconciliationInterval
	}

	hasher, err := watcher.NewContentHasher(nil)
	if err != nil {
		return fmt.Errorf("source watch: create content cache: %w", err)
	}
	reconciler, err := watcher.NewReconciler(options.Repositories, hasher)
	if err != nil {
		return fmt.Errorf("source watch: create reconciler: %w", err)
	}
	filesystemWatcher, err := watcher.New(options.Repositories)
	if err != nil {
		return fmt.Errorf("source watch: create filesystem watcher: %w", err)
	}
	defer filesystemWatcher.Close()
	initial, err := reconciler.Reconcile(ctx)
	if err != nil {
		return fmt.Errorf("source watch: seed content cache: %w", err)
	}
	batcher, err := watcher.NewBatcher(debounce, maximum)
	if err != nil {
		return fmt.Errorf("source watch: create event batcher: %w", err)
	}

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchDone := make(chan error, 1)
	go func() {
		watchDone <- filesystemWatcher.Run(watchCtx)
	}()
	waitForWatcher := func() error {
		cancel()
		watchErr := <-watchDone
		if watchErr != nil && !errors.Is(watchErr, context.Canceled) {
			return fmt.Errorf("source watch: filesystem watcher stopped: %w", watchErr)
		}
		return nil
	}
	batches := batcher.Run(watchCtx, filesystemWatcher.Events())
	ticker := time.NewTicker(reconciliationInterval)
	defer ticker.Stop()
	watcherErrors := filesystemWatcher.Errors()
	retryTicker := time.NewTicker(SourceWatchRetryInterval)
	defer retryTicker.Stop()
	var pending *watcher.ReconciliationResult
	queuePending := func(result watcher.ReconciliationResult) {
		if pending == nil {
			pending = &result
			return
		}
		coalesced := coalesceReconciliationResults(*pending, result)
		pending = &coalesced
	}
	deliverPending := func() {
		if pending == nil {
			return
		}
		if err := options.OnChange(ctx, *pending); err != nil {
			reportSourceWatchError(options.OnError, fmt.Errorf("handle source change: %w", err))
			return
		}
		pending = nil
	}
	if options.ReportInitialChanges && hasSourceChanges(initial) {
		queuePending(initial)
		deliverPending()
	}

	for {
		select {
		case <-ctx.Done():
			if err := waitForWatcher(); err != nil {
				return err
			}
			return nil
		case err := <-watchDone:
			if ctx.Err() != nil {
				return nil
			}
			if err == nil || errors.Is(err, context.Canceled) {
				return errors.New("source watch: filesystem watcher stopped unexpectedly")
			}
			return fmt.Errorf("source watch: filesystem watcher stopped: %w", err)
		case err, ok := <-watcherErrors:
			if !ok {
				watcherErrors = nil
				continue
			}
			if err != nil {
				reportSourceWatchError(options.OnError, err)
			}
		case batch, ok := <-batches:
			if !ok {
				if err := waitForWatcher(); err != nil {
					return errors.Join(errors.New("source watch: event batcher stopped unexpectedly"), err)
				}
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("source watch: event batcher stopped unexpectedly")
			}
			result, processErr := reconciler.Process(ctx, batch)
			if processErr != nil {
				reportSourceWatchError(options.OnError, processErr)
				continue
			}
			if hasSourceChanges(result) {
				queuePending(result)
				deliverPending()
			}
		case <-ticker.C:
			result, reconcileErr := reconciler.Reconcile(ctx)
			if reconcileErr != nil {
				reportSourceWatchError(options.OnError, reconcileErr)
				continue
			}
			if hasSourceChanges(result) {
				queuePending(result)
				deliverPending()
			}
		case <-retryTicker.C:
			deliverPending()
		}
	}
}

func hasSourceChanges(result watcher.ReconciliationResult) bool {
	return len(result.Added) > 0 || len(result.Modified) > 0 || len(result.Removed) > 0
}

// coalesceReconciliationResults keeps one bounded pending result while a
// change handler is unavailable. Repeated paths retain their latest state,
// while distinct changes remain available to the next successful delivery.
func coalesceReconciliationResults(
	previous, next watcher.ReconciliationResult,
) watcher.ReconciliationResult {
	type stateCategory uint8
	const (
		addedCategory stateCategory = iota
		modifiedCategory
		unchangedCategory
		removedCategory
		skippedCategory
	)
	type pendingState struct {
		category stateCategory
		state    watcher.FileState
	}
	states := make(map[watcher.FileKey]pendingState)
	merge := func(category stateCategory, group []watcher.FileState) {
		for _, state := range group {
			key := watcher.FileKey{Repository: state.Repository, Path: state.Path}
			states[key] = pendingState{category: category, state: state}
		}
	}
	mergeResult := func(result watcher.ReconciliationResult) {
		merge(addedCategory, result.Added)
		merge(modifiedCategory, result.Modified)
		merge(unchangedCategory, result.Unchanged)
		merge(removedCategory, result.Removed)
		merge(skippedCategory, result.Skipped)
	}
	mergeResult(previous)
	mergeResult(next)

	result := watcher.ReconciliationResult{}
	for _, pending := range states {
		switch pending.category {
		case addedCategory:
			result.Added = append(result.Added, pending.state)
		case modifiedCategory:
			result.Modified = append(result.Modified, pending.state)
		case unchangedCategory:
			result.Unchanged = append(result.Unchanged, pending.state)
		case removedCategory:
			result.Removed = append(result.Removed, pending.state)
		case skippedCategory:
			result.Skipped = append(result.Skipped, pending.state)
		}
	}
	sortFileStateGroups(&result)
	result.Renamed = coalesceRenames(previous.Renamed, next.Renamed)
	result.ManifestChanges = coalesceFileStates(previous.ManifestChanges, next.ManifestChanges)
	return result
}

func sortFileStateGroups(result *watcher.ReconciliationResult) {
	groups := [][]watcher.FileState{
		result.Added,
		result.Modified,
		result.Unchanged,
		result.Removed,
		result.Skipped,
	}
	for _, group := range groups {
		sort.Slice(group, func(left, right int) bool {
			if group[left].Repository != group[right].Repository {
				return group[left].Repository < group[right].Repository
			}
			return group[left].Path < group[right].Path
		})
	}
}

func coalesceFileStates(previous, next []watcher.FileState) []watcher.FileState {
	result := append([]watcher.FileState(nil), previous...)
	indices := make(map[watcher.FileKey]int, len(result))
	for index, state := range result {
		indices[watcher.FileKey{Repository: state.Repository, Path: state.Path}] = index
	}
	for _, state := range next {
		key := watcher.FileKey{Repository: state.Repository, Path: state.Path}
		if index, exists := indices[key]; exists {
			result[index] = state
			continue
		}
		indices[key] = len(result)
		result = append(result, state)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Repository != result[right].Repository {
			return result[left].Repository < result[right].Repository
		}
		return result[left].Path < result[right].Path
	})
	return result
}

func coalesceRenames(previous, next []watcher.Rename) []watcher.Rename {
	type renameKey struct {
		from watcher.FileKey
		to   watcher.FileKey
	}
	result := append([]watcher.Rename(nil), previous...)
	indices := make(map[renameKey]int, len(result))
	for index, rename := range result {
		indices[renameKey{
			from: watcher.FileKey{Repository: rename.From.Repository, Path: rename.From.Path},
			to:   watcher.FileKey{Repository: rename.To.Repository, Path: rename.To.Path},
		}] = index
	}
	for _, rename := range next {
		key := renameKey{
			from: watcher.FileKey{Repository: rename.From.Repository, Path: rename.From.Path},
			to:   watcher.FileKey{Repository: rename.To.Repository, Path: rename.To.Path},
		}
		if index, exists := indices[key]; exists {
			result[index] = rename
			continue
		}
		indices[key] = len(result)
		result = append(result, rename)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].From.Repository != result[right].From.Repository {
			return result[left].From.Repository < result[right].From.Repository
		}
		if result[left].From.Path != result[right].From.Path {
			return result[left].From.Path < result[right].From.Path
		}
		return result[left].To.Path < result[right].To.Path
	})
	return result
}

func reportSourceWatchError(sink func(error), err error) {
	if sink != nil && err != nil {
		sink(err)
	}
}
