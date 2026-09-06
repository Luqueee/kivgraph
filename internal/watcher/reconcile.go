package watcher

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// Rename is an unambiguous same-content path move found during reconciliation.
type Rename struct {
	From FileState
	To   FileState
}

// ReconciliationResult is the filesystem state recovered independently of
// fsnotify delivery.
type ReconciliationResult struct {
	Added           []FileState
	Modified        []FileState
	Unchanged       []FileState
	Removed         []FileState
	Skipped         []FileState
	Renamed         []Rename
	ManifestChanges []FileState
}

// Reconciler periodically scans registered repositories and feeds the observed
// paths through a ContentHasher. It is the recovery path for dropped or missed
// filesystem notifications.
type Reconciler struct {
	repositories []reconciliationRepository
	hasher       *ContentHasher
}

type reconciliationRepository struct {
	name      string
	root      string
	ignored   ignoreMatcher
	languages map[string]struct{}
	// extensions is what those languages are written in, resolved once
	// because isSource is asked about every file of every scan.
	extensions  map[string]struct{}
	sourceRoots []string
	manifests   map[string]struct{}
}

// NewReconciler creates a reconciler for registered repositories and an
// existing content-hash cache. Roots that do not exist yet are retained and
// treated as empty during a scan so deletions can still be detected.
func NewReconciler(repositories []workspace.Repository, hasher *ContentHasher) (*Reconciler, error) {
	if hasher == nil {
		return nil, errors.New("reconciler requires a content hasher")
	}
	reconciler := &Reconciler{
		repositories: make([]reconciliationRepository, 0, len(repositories)),
		hasher:       hasher,
	}
	seenNames := make(map[string]struct{}, len(repositories))
	seenRoots := make(map[string]struct{}, len(repositories))
	for index, repository := range repositories {
		name := strings.TrimSpace(repository.Name)
		if name == "" {
			return nil, fmt.Errorf("repository[%d]: name must not be empty", index)
		}
		if _, exists := seenNames[name]; exists {
			return nil, fmt.Errorf("repository[%d] %q: duplicate name", index, name)
		}
		root := strings.TrimSpace(repository.RealPath)
		if root == "" {
			root = strings.TrimSpace(repository.Path)
		}
		if root == "" {
			return nil, fmt.Errorf("repository[%d] %q: root path must not be empty", index, name)
		}
		absoluteRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("repository[%d] %q: make root absolute: %w", index, name, err)
		}
		root = filepath.Clean(absoluteRoot)
		if info, err := os.Lstat(root); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return nil, fmt.Errorf("repository[%d] %q: root %q is not a regular directory", index, name, root)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("repository[%d] %q: inspect root %q: %w", index, name, root, err)
		}
		if _, exists := seenRoots[root]; exists {
			return nil, fmt.Errorf("repository[%d] %q: duplicate root %q", index, name, root)
		}
		ignored, err := newIgnoreMatcher(root, repository.Exclusions)
		if err != nil {
			return nil, fmt.Errorf("repository[%d] %q: %w", index, name, err)
		}
		sourceRoots, err := reconciliationSourceRoots(root, repository.Roots)
		if err != nil {
			return nil, fmt.Errorf("repository[%d] %q: %w", index, name, err)
		}
		manifests, err := reconciliationManifestPaths(root, repository.Manifests)
		if err != nil {
			return nil, fmt.Errorf("repository[%d] %q: %w", index, name, err)
		}
		languages := make(map[string]struct{}, len(repository.Languages))
		for _, language := range repository.Languages {
			language = strings.ToLower(strings.TrimSpace(language))
			if language != "" {
				languages[language] = struct{}{}
			}
		}
		reconciler.repositories = append(reconciler.repositories, reconciliationRepository{
			name:        name,
			root:        root,
			ignored:     ignored,
			languages:   languages,
			extensions:  config.SourceExtensionSet(repository.Languages),
			sourceRoots: sourceRoots,
			manifests:   manifests,
		})
		seenNames[name] = struct{}{}
		seenRoots[root] = struct{}{}
	}
	sort.Slice(reconciler.repositories, func(left, right int) bool {
		return reconciler.repositories[left].name < reconciler.repositories[right].name
	})
	return reconciler, nil
}

// Reconcile scans manifests and configured source files once. It detects
// additions, modifications, removals, unchanged files and unambiguous renames
// without relying on an fsnotify event having arrived.
func (reconciler *Reconciler) Reconcile(ctx context.Context) (ReconciliationResult, error) {
	if reconciler == nil {
		return ReconciliationResult{}, errors.New("reconcile nil reconciler")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	knownRecords := reconciler.hasher.KnownFiles()
	known := make(map[FileKey]string, len(knownRecords))
	for _, record := range knownRecords {
		key, err := newFileKey(record.Repository, record.Path)
		if err != nil {
			return ReconciliationResult{}, fmt.Errorf("known hash: %w", err)
		}
		known[key] = record.ContentHash
	}

	files, err := reconciler.scan(ctx)
	if err != nil {
		return ReconciliationResult{}, err
	}
	current := make(map[FileKey]struct{}, len(files))
	events := make([]Event, 0, len(files)+len(known))
	for _, file := range files {
		key := FileKey{Repository: file.repository, Path: file.path}
		if _, exists := current[key]; exists {
			continue
		}
		current[key] = struct{}{}
		operation := OperationWrite
		if _, exists := known[key]; !exists {
			operation = OperationCreate
		}
		events = append(events, Event{Repository: file.repository, Path: file.path, Operations: operation})
	}
	for _, record := range knownRecords {
		key, err := newFileKey(record.Repository, record.Path)
		if err != nil {
			return ReconciliationResult{}, fmt.Errorf("known hash: %w", err)
		}
		if !reconciler.owns(key) {
			continue
		}
		if _, exists := current[key]; !exists {
			events = append(events, Event{Repository: key.Repository, Path: key.Path, Operations: OperationRemove})
		}
	}

	hashes, err := reconciler.hasher.Process(ctx, Batch{Events: events})
	if err != nil {
		return ReconciliationResult{}, fmt.Errorf("hash reconciliation: %w", err)
	}
	return reconciler.resultFromHashes(hashes, known), nil
}

// Process handles notifications from Watcher without scanning the complete
// repository tree. It filters paths using the same source and manifest policy
// as Reconcile, then compares their content against the reconciliation cache.
// A caller can therefore use filesystem events for low latency and keep
// Reconcile as the periodic recovery path for missed notifications.
func (reconciler *Reconciler) Process(ctx context.Context, batch Batch) (ReconciliationResult, error) {
	if reconciler == nil {
		return ReconciliationResult{}, errors.New("process nil reconciler")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	events, err := normalizeHashEvents(batch.Events)
	if err != nil {
		return ReconciliationResult{}, fmt.Errorf("normalize watcher events: %w", err)
	}
	filtered := make([]Event, 0, len(events))
	known := make(map[FileKey]string, len(events))
	for _, event := range events {
		key := FileKey{Repository: event.Repository, Path: event.Path}
		if !reconciler.isTrackedPath(key.Repository, key.Path) {
			continue
		}
		if hash, exists := reconciler.hasher.KnownHash(key.Repository, key.Path); exists {
			known[key] = hash
		}
		filtered = append(filtered, event)
	}
	hashes, err := reconciler.hasher.Process(ctx, Batch{Events: filtered})
	if err != nil {
		return ReconciliationResult{}, fmt.Errorf("hash watcher events: %w", err)
	}
	return reconciler.resultFromHashes(hashes, known), nil
}

func (reconciler *Reconciler) resultFromHashes(hashes HashResult, known map[FileKey]string) ReconciliationResult {
	result := ReconciliationResult{
		Unchanged: append([]FileState(nil), hashes.Unchanged...),
		Removed:   append([]FileState(nil), hashes.Removed...),
		Skipped:   append([]FileState(nil), hashes.Skipped...),
	}
	for _, state := range hashes.Changed {
		key := FileKey{Repository: state.Repository, Path: state.Path}
		if _, existed := known[key]; existed {
			result.Modified = append(result.Modified, state)
		} else {
			result.Added = append(result.Added, state)
		}
	}
	result.Renamed = detectRenames(result.Added, result.Removed, known)
	result.ManifestChanges = reconciler.manifestChanges(result.Added, result.Modified, result.Removed)
	sortReconciliationResult(&result)
	return result
}

// Run performs one reconciliation immediately and repeats it at interval until
// ctx is cancelled. The sink is called synchronously, so a failed sink or scan
// is returned instead of being hidden behind a background goroutine.
func (reconciler *Reconciler) Run(ctx context.Context, interval time.Duration, sink func(ReconciliationResult) error) error {
	if reconciler == nil {
		return errors.New("run nil reconciler")
	}
	if interval <= 0 {
		return fmt.Errorf("reconciliation interval must be positive, got %s", interval)
	}
	if sink == nil {
		return errors.New("reconciliation sink must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		result, err := reconciler.Reconcile(ctx)
		if err != nil {
			return err
		}
		if err := sink(result); err != nil {
			return fmt.Errorf("reconciliation sink: %w", err)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type scannedFile struct {
	repository string
	path       string
}

func (reconciler *Reconciler) scan(ctx context.Context) ([]scannedFile, error) {
	files := make([]scannedFile, 0)
	for _, repository := range reconciler.repositories {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := filepath.WalkDir(repository.root, func(path string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				if errors.Is(walkErr, fs.ErrNotExist) {
					return nil
				}
				return fmt.Errorf("walk %q: %w", path, walkErr)
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if path != repository.root && repository.ignored.ignored(path) {
					return filepath.SkipDir
				}
				return nil
			}
			if repository.ignored.ignored(path) {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return fmt.Errorf("inspect %q: %w", path, err)
			}
			if !info.Mode().IsRegular() || (!repository.isManifest(path) && !repository.isSource(path)) {
				return nil
			}
			files = append(files, scannedFile{repository: repository.name, path: filepath.Clean(path)})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scan repository %q: %w", repository.name, err)
		}
	}
	sort.Slice(files, func(left, right int) bool {
		if files[left].repository != files[right].repository {
			return files[left].repository < files[right].repository
		}
		return files[left].path < files[right].path
	})
	return files, nil
}

func reconciliationSourceRoots(root string, configured []string) ([]string, error) {
	if len(configured) == 0 {
		return []string{root}, nil
	}
	roots := make([]string, 0, len(configured))
	seen := make(map[string]struct{}, len(configured))
	for index, configuredRoot := range configured {
		absolute, err := filepath.Abs(strings.TrimSpace(configuredRoot))
		if err != nil {
			return nil, fmt.Errorf("roots[%d]: make absolute: %w", index, err)
		}
		absolute = filepath.Clean(absolute)
		if !pathWithin(root, absolute) {
			return nil, fmt.Errorf("roots[%d] %q escapes repository root", index, configuredRoot)
		}
		if info, err := os.Lstat(absolute); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return nil, fmt.Errorf("roots[%d] %q is not a regular directory", index, absolute)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("roots[%d] %q: inspect: %w", index, absolute, err)
		}
		if _, exists := seen[absolute]; !exists {
			roots = append(roots, absolute)
			seen[absolute] = struct{}{}
		}
	}
	return roots, nil
}

func reconciliationManifestPaths(root string, configured []string) (map[string]struct{}, error) {
	paths := make(map[string]struct{}, len(configured))
	for index, configuredPath := range configured {
		configuredPath = strings.TrimSpace(configuredPath)
		if configuredPath == "" {
			return nil, fmt.Errorf("manifests[%d]: path must not be empty", index)
		}
		absolute, err := filepath.Abs(configuredPath)
		if err != nil {
			return nil, fmt.Errorf("manifests[%d]: make absolute: %w", index, err)
		}
		absolute = filepath.Clean(absolute)
		if !pathWithin(root, absolute) {
			return nil, fmt.Errorf("manifests[%d] %q escapes repository root", index, configuredPath)
		}
		paths[absolute] = struct{}{}
	}
	return paths, nil
}

func (repository reconciliationRepository) isManifest(path string) bool {
	if _, configured := repository.manifests[filepath.Clean(path)]; configured {
		return true
	}
	return isManifestPath(path)
}

func (repository reconciliationRepository) isSource(path string) bool {
	insideSourceRoot := false
	for _, root := range repository.sourceRoots {
		if pathWithin(root, path) {
			insideSourceRoot = true
			break
		}
	}
	if !insideSourceRoot {
		return false
	}
	return config.HasSourceExtension(repository.extensions, path)
}

func (reconciler *Reconciler) owns(key FileKey) bool {
	for _, repository := range reconciler.repositories {
		if repository.name == key.Repository && pathWithin(repository.root, key.Path) {
			return true
		}
	}
	return false
}

func isManifestPath(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	switch name {
	case "package.json", "package-lock.json", "pnpm-workspace.yaml", "pnpm-workspace.yml", "pnpm-lock.yaml", "yarn.lock", "go.mod", "go.sum", "go.work", "cargo.toml", "cargo.lock", "build.rs":
		return true
	}
	return name == "tsconfig.json" || (strings.HasPrefix(name, "tsconfig.") && strings.HasSuffix(name, ".json"))
}

func detectRenames(added, removed []FileState, known map[FileKey]string) []Rename {
	removedByHash := make(map[string][]FileState)
	for _, state := range removed {
		hash, exists := known[FileKey{Repository: state.Repository, Path: state.Path}]
		if exists {
			state.ContentHash = hash
			removedByHash[hash] = append(removedByHash[hash], state)
		}
	}
	addedByHash := make(map[string][]FileState)
	for _, state := range added {
		if state.ContentHash != "" {
			addedByHash[state.ContentHash] = append(addedByHash[state.ContentHash], state)
		}
	}
	renames := make([]Rename, 0)
	for hash, from := range removedByHash {
		to := addedByHash[hash]
		if len(from) == 1 && len(to) == 1 {
			renames = append(renames, Rename{From: from[0], To: to[0]})
		}
	}
	sort.Slice(renames, func(left, right int) bool {
		if renames[left].From.Repository != renames[right].From.Repository {
			return renames[left].From.Repository < renames[right].From.Repository
		}
		return renames[left].From.Path < renames[right].From.Path
	})
	return renames
}

func (reconciler *Reconciler) manifestChanges(groups ...[]FileState) []FileState {
	changes := make([]FileState, 0)
	for _, group := range groups {
		for _, state := range group {
			if reconciler.isManifest(state.Repository, state.Path) {
				changes = append(changes, state)
			}
		}
	}
	return changes
}

func (reconciler *Reconciler) isManifest(repositoryName, path string) bool {
	for _, repository := range reconciler.repositories {
		if repository.name == repositoryName {
			return repository.isManifest(path)
		}
	}
	return isManifestPath(path)
}

func (reconciler *Reconciler) isTrackedPath(repositoryName, path string) bool {
	for _, repository := range reconciler.repositories {
		if repository.name != repositoryName {
			continue
		}
		if repository.ignored.ignored(path) {
			return false
		}
		return repository.isManifest(path) || repository.isSource(path)
	}
	return false
}

func sortReconciliationResult(result *ReconciliationResult) {
	stateGroups := [][]FileState{result.Added, result.Modified, result.Unchanged, result.Removed, result.Skipped, result.ManifestChanges}
	for _, group := range stateGroups {
		sort.Slice(group, func(left, right int) bool {
			if group[left].Repository != group[right].Repository {
				return group[left].Repository < group[right].Repository
			}
			return group[left].Path < group[right].Path
		})
	}
}
