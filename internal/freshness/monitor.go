package freshness

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/watcher"
	"github.com/Luqueee/kivgraph/internal/workspace"
	"github.com/fsnotify/fsnotify"
)

// Monitor invalidates a freshness cache from filesystem notifications. It
// never hashes on the notification path; the next full rebuild creates the
// next attestation, while graph_status remains a bounded cache read.
type Monitor struct {
	cache         *Cache
	watch         *watcher.Watcher
	cancel        context.CancelFunc
	done          chan struct{}
	ctx           context.Context
	verify        sync.WaitGroup
	registryWatch *fsnotify.Watcher
	registryPath  string
	ready         chan struct{}
}

// NewRegistryMonitor resolves the configured repositories once and then
// watches their runtime roots. Registry and Git inspection belongs at server
// setup, never in a graph_status call.
func NewRegistryMonitor(
	ctx context.Context,
	source config.RepositoriesFile,
	registryPath string,
	attestationRoot string,
	cache *Cache,
) (*Monitor, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cache == nil {
		return nil, errors.New("freshness monitor: cache is nil")
	}
	registry, err := workspace.NewRegistry(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("freshness monitor: resolve repository registry: %w", err)
	}
	registryWatch, resolvedRegistryPath, err := newRegistryWatcher(registryPath)
	if err != nil {
		return nil, err
	}
	monitor, err := newMonitor(ctx, registry.List(), cache, registryWatch, resolvedRegistryPath)
	if err != nil {
		_ = registryWatch.Close()
		return nil, err
	}
	initial := cache.Load()
	if initial.Generation > 0 && initial.State == "unverified" {
		monitor.verify.Add(1)
		go monitor.verifyInitial(attestationRoot, initial.Generation, registry.List())
	}
	return monitor, nil
}

// NewMonitor starts watching the registered repositories. The watcher's one
// initial directory walk happens at setup, outside graph_status; subsequent
// status calls only read the supplied cache.
func NewMonitor(
	ctx context.Context,
	repositories []workspace.Repository,
	cache *Cache,
) (*Monitor, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cache == nil {
		return nil, errors.New("freshness monitor: cache is nil")
	}
	return newMonitor(ctx, repositories, cache, nil, "")
}

func newMonitor(
	ctx context.Context,
	repositories []workspace.Repository,
	cache *Cache,
	registryWatch *fsnotify.Watcher,
	registryPath string,
) (*Monitor, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	watch, err := watcher.New(repositories)
	if err != nil {
		return nil, fmt.Errorf("freshness monitor: create watcher: %w", err)
	}
	monitorContext, cancel := context.WithCancel(ctx)
	monitor := &Monitor{
		cache:         cache,
		watch:         watch,
		cancel:        cancel,
		done:          make(chan struct{}),
		ctx:           monitorContext,
		registryWatch: registryWatch,
		registryPath:  registryPath,
		ready:         make(chan struct{}),
	}
	go monitor.run(monitorContext, repositories)
	return monitor, nil
}

func newRegistryWatcher(path string) (*fsnotify.Watcher, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, "", errors.New("freshness monitor: repository registry path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, "", fmt.Errorf("freshness monitor: resolve repository registry %q: %w", path, err)
	}
	registryWatch, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, "", fmt.Errorf("freshness monitor: create registry watcher: %w", err)
	}
	if err := registryWatch.Add(filepath.Dir(absolute)); err != nil {
		_ = registryWatch.Close()
		return nil, "", fmt.Errorf("freshness monitor: watch repository registry %q: %w", absolute, err)
	}
	return registryWatch, filepath.Clean(absolute), nil
}

// Close stops the monitor and waits until it no longer owns filesystem
// watches. It is safe to call on a nil monitor.
func (monitor *Monitor) Close() {
	if monitor == nil {
		return
	}
	monitor.cancel()
	<-monitor.done
	monitor.verify.Wait()
}

func (monitor *Monitor) verifyInitial(root string, generation uint64, repositories []workspace.Repository) {
	defer monitor.verify.Done()
	status := Check(monitor.ctx, root, generation, repositories)
	if monitor.ctx.Err() != nil {
		// A cancelled inventory is not an observation. Publishing it during
		// shutdown could overwrite state that a replacement monitor owns.
		return
	}
	monitor.cache.StoreIfUnverified(status)
}

func (monitor *Monitor) run(ctx context.Context, repositories []workspace.Repository) {
	defer close(monitor.done)
	if monitor.registryWatch != nil {
		defer monitor.registryWatch.Close()
	}
	runErr := make(chan error, 1)
	go func() { runErr <- monitor.watch.Run(ctx) }()

	byName := make(map[string]workspace.Repository, len(repositories))
	for _, repository := range repositories {
		byName[repository.Name] = repository
	}
	events := monitor.watch.Events()
	errorsCh := monitor.watch.Errors()
	var registryEvents <-chan fsnotify.Event
	var registryErrors <-chan error
	if monitor.registryWatch != nil {
		registryEvents = monitor.registryWatch.Events
		registryErrors = monitor.registryWatch.Errors
	}
	close(monitor.ready)
	for events != nil || errorsCh != nil || registryEvents != nil || registryErrors != nil {
		select {
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			repository, exists := byName[event.Repository]
			if exists && monitoredInput(repository, event.Path) {
				monitor.cache.MarkStale("registered source inventory changed: " + event.Path)
			}
		case err, ok := <-errorsCh:
			if !ok {
				errorsCh = nil
				continue
			}
			if err != nil {
				monitor.cache.MarkUnavailable("freshness monitor: " + err.Error())
			}
		case event, ok := <-registryEvents:
			if !ok {
				registryEvents = nil
				continue
			}
			if filepath.Clean(event.Name) == monitor.registryPath {
				monitor.cache.MarkStale("repository registry changed")
			}
		case err, ok := <-registryErrors:
			if !ok {
				registryErrors = nil
				continue
			}
			if err != nil {
				monitor.cache.MarkUnavailable("freshness monitor: " + err.Error())
			}
		case err := <-runErr:
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				monitor.cache.MarkUnavailable("freshness monitor: " + err.Error())
			}
			return
		}
	}
	if err := <-runErr; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		monitor.cache.MarkUnavailable("freshness monitor: " + err.Error())
	}
}

func monitoredInput(repository workspace.Repository, candidate string) bool {
	root, err := inventoryRoot(repository)
	if err != nil {
		return true
	}
	excluded, err := workspace.MatchesExclusion(root, candidate, repository.Exclusions)
	if err != nil {
		return true
	}
	if excluded {
		return false
	}
	manifests, err := resolveRepositoryPaths(root, repository.Manifests)
	if err != nil {
		return true
	}
	for _, manifest := range manifests {
		if filepath.Clean(candidate) == manifest {
			return true
		}
	}
	languages := repository.Languages
	if len(languages) == 0 {
		languages = config.SupportedLanguages()
	}
	return config.IsBuildConfigurationFile(candidate) ||
		config.HasSourceExtension(config.SourceExtensionSet(languages), candidate)
}
