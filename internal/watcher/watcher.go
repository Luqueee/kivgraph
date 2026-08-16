// Package watcher turns fsnotify notifications for registered repositories into
// repository-qualified filesystem events. It intentionally does not debounce,
// hash, classify, or invalidate events; those responsibilities belong to later
// incremental stages.
//
// Event granularity follows the platform backend. inotify reports the write
// that fills a file created moments earlier; kqueue sees only the directory
// change, because the new file is not watched until it already exists, so the
// same sequence arrives as a lone Create. A consumer therefore treats Create
// as a content change, and Reconciler stays the recovery path for whatever a
// backend does not report. Modifying a file that existed when the watch was
// installed is a Write on every backend.
//
// On kqueue platforms - macOS and the BSDs - the backend holds one descriptor
// per watched file and directory: 787 for the Kivgraph checkout itself, which
// has 659 files in 152 watched directories, against a per-process ceiling of
// kern.maxfilesperproc, 92160 on macOS 26. A tree past that ceiling fails in
// New with an explicit descriptor error rather than watching a subset of it.
package watcher

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/Luqueee/kivgraph/internal/workspace"
	"github.com/fsnotify/fsnotify"
)

var (
	// ErrAlreadyRunning indicates that Run was called more than once.
	ErrAlreadyRunning = errors.New("watcher is already running or has run")
)

// Operation is a portable bitmask describing a filesystem change.
type Operation uint8

const (
	// OperationCreate reports a newly created path.
	OperationCreate Operation = 1 << iota
	// OperationWrite reports a path whose contents or size may have changed.
	OperationWrite
	// OperationRemove reports a removed path.
	OperationRemove
	// OperationRename reports a renamed path.
	OperationRename
	// OperationChmod reports an attribute change. On Linux it can also be the
	// first notification for a removed path, so consumers must not treat it as
	// proof that the path still exists.
	OperationChmod
)

// Has reports whether the operation contains flag.
func (operation Operation) Has(flag Operation) bool {
	return operation&flag != 0
}

// String returns the stable, pipe-separated operation names.
func (operation Operation) String() string {
	var names []string
	for _, value := range []struct {
		flag Operation
		name string
	}{
		{OperationCreate, "CREATE"},
		{OperationWrite, "WRITE"},
		{OperationRemove, "REMOVE"},
		{OperationRename, "RENAME"},
		{OperationChmod, "CHMOD"},
	} {
		if operation.Has(value.flag) {
			names = append(names, value.name)
		}
	}
	if len(names) == 0 {
		return "[no operations]"
	}
	return strings.Join(names, "|")
}

// Event is a repository-qualified filesystem notification.
type Event struct {
	// Repository is the registered repository name owning Path.
	Repository string
	// Path is an absolute, cleaned path. It is not resolved through symlinks.
	Path string
	// Operations contains one or more portable filesystem operations.
	Operations Operation
}

type repositoryRoot struct {
	name    string
	path    string
	ignored ignoreMatcher
}

// Watcher recursively watches registered repository directories.
//
// A Watcher must be run by exactly one call to Run. Run owns delivery on Events
// and Errors and closes both channels before returning. The watcher reports raw
// notifications; it deliberately performs no debounce or content hashing.
type Watcher struct {
	backend *fsnotify.Watcher
	events  chan Event
	errors  chan error
	done    chan struct{}

	mu       sync.Mutex
	closed   bool
	closeErr error
	watched  map[string]struct{}
	roots    []repositoryRoot

	runMu    sync.Mutex
	runStart bool
}

// New creates a recursive watcher for the supplied immutable repository
// metadata. Each repository root is watched immediately, including directories
// that already exist below it. Symlinked entries and default dependency trees
// are not watched; configured exclusions use the same path-segment and `**`
// semantics as workspace discovery.
func New(repositories []workspace.Repository) (*Watcher, error) {
	backend, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}
	watcher := &Watcher{
		backend: backend,
		events:  make(chan Event, 64),
		errors:  make(chan error, 32),
		done:    make(chan struct{}),
		watched: make(map[string]struct{}),
		roots:   make([]repositoryRoot, 0, len(repositories)),
	}

	seenNames := make(map[string]struct{}, len(repositories))
	seenPaths := make(map[string]struct{}, len(repositories))
	for index, repository := range repositories {
		name := strings.TrimSpace(repository.Name)
		if name == "" {
			_ = watcher.Close()
			return nil, fmt.Errorf("repository[%d]: name must not be empty", index)
		}
		if _, exists := seenNames[name]; exists {
			_ = watcher.Close()
			return nil, fmt.Errorf("repository[%d] %q: duplicate name", index, name)
		}
		root := strings.TrimSpace(repository.RealPath)
		if root == "" {
			root = strings.TrimSpace(repository.Path)
		}
		if root == "" {
			_ = watcher.Close()
			return nil, fmt.Errorf("repository[%d] %q: root path must not be empty", index, name)
		}
		root, err = filepath.Abs(root)
		if err != nil {
			_ = watcher.Close()
			return nil, fmt.Errorf("repository[%d] %q: make root absolute: %w", index, name, err)
		}
		root = filepath.Clean(root)
		info, err := os.Lstat(root)
		if err != nil {
			_ = watcher.Close()
			return nil, fmt.Errorf("repository[%d] %q: inspect root %q: %w", index, name, root, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			_ = watcher.Close()
			return nil, fmt.Errorf("repository[%d] %q: root %q is not a regular directory", index, name, root)
		}
		if _, exists := seenPaths[root]; exists {
			_ = watcher.Close()
			return nil, fmt.Errorf("repository[%d] %q: duplicate root %q", index, name, root)
		}
		ignored, err := newIgnoreMatcher(root, repository.Exclusions)
		if err != nil {
			_ = watcher.Close()
			return nil, fmt.Errorf("repository[%d] %q: %w", index, name, err)
		}
		seenNames[name] = struct{}{}
		seenPaths[root] = struct{}{}
		watcher.roots = append(watcher.roots, repositoryRoot{name: name, path: root, ignored: ignored})
	}
	// Longest roots win if a caller constructs a registry that contains nested
	// roots. The validated workspace registry normally rejects that topology.
	sort.Slice(watcher.roots, func(left, right int) bool {
		if len(watcher.roots[left].path) != len(watcher.roots[right].path) {
			return len(watcher.roots[left].path) > len(watcher.roots[right].path)
		}
		return watcher.roots[left].name < watcher.roots[right].name
	})
	for _, root := range watcher.roots {
		if err := watcher.addTree(root, root.path); err != nil {
			_ = watcher.Close()
			return nil, fmt.Errorf("watch repository %q: %w", root.name, err)
		}
	}
	return watcher, nil
}

// Events returns raw repository-qualified filesystem notifications.
func (watcher *Watcher) Events() <-chan Event {
	if watcher == nil {
		return nil
	}
	return watcher.events
}

// Errors returns backend and dynamic-watch errors. Errors are never silently
// discarded; callers should drain this channel while consuming Events.
func (watcher *Watcher) Errors() <-chan error {
	if watcher == nil {
		return nil
	}
	return watcher.errors
}

// WatchedPaths returns the currently registered directory watches in sorted
// order. It is intended for diagnostics and tests.
func (watcher *Watcher) WatchedPaths() []string {
	if watcher == nil {
		return nil
	}
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	paths := make([]string, 0, len(watcher.watched))
	for path := range watcher.watched {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// Run forwards notifications until ctx is cancelled or Close is called.
// Context cancellation is returned to the caller; Close is a clean nil return.
func (watcher *Watcher) Run(ctx context.Context) error {
	if watcher == nil {
		return errors.New("run nil watcher")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	watcher.runMu.Lock()
	if watcher.runStart {
		watcher.runMu.Unlock()
		return ErrAlreadyRunning
	}
	watcher.runStart = true
	watcher.runMu.Unlock()
	defer func() {
		_ = watcher.Close()
		close(watcher.events)
		close(watcher.errors)
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-watcher.done:
			return nil
		case rawEvent, ok := <-watcher.backend.Events:
			if !ok {
				return nil
			}
			if err := watcher.processEvent(ctx, rawEvent); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				if err := watcher.reportError(ctx, err); err != nil {
					return err
				}
			}
		case backendError, ok := <-watcher.backend.Errors:
			if !ok {
				return nil
			}
			if backendError == nil {
				continue
			}
			if err := watcher.reportError(ctx, fmt.Errorf("fsnotify: %w", backendError)); err != nil {
				return err
			}
		}
	}
}

// Close stops all filesystem watches. It is safe to call repeatedly and from a
// goroutine concurrent with Run.
func (watcher *Watcher) Close() error {
	if watcher == nil {
		return nil
	}
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	if watcher.closed {
		return watcher.closeErr
	}
	watcher.closed = true
	close(watcher.done)
	watcher.closeErr = watcher.backend.Close()
	return watcher.closeErr
}

func (watcher *Watcher) addTree(root repositoryRoot, start string) error {
	if root.ignored.ignored(start) {
		return nil
	}
	return filepath.WalkDir(start, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if path != start && root.ignored.ignored(path) {
			return filepath.SkipDir
		}
		if err := watcher.addDirectory(path); err != nil {
			if errors.Is(err, fsnotify.ErrClosed) {
				return err
			}
			return fmt.Errorf("add directory %q: %w", path, err)
		}
		return nil
	})
}

func (watcher *Watcher) addDirectory(path string) error {
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	if watcher.closed {
		return fsnotify.ErrClosed
	}
	path = filepath.Clean(path)
	if _, exists := watcher.watched[path]; exists {
		return nil
	}
	if err := watcher.backend.Add(path); err != nil {
		return descriptorLimit(err)
	}
	watcher.watched[path] = struct{}{}
	return nil
}

// descriptorLimit names the one failure an operator can act on. A kqueue
// backend needs a descriptor for every watched file, so a large repository
// exhausts the per-process limit long before anything else goes wrong, and
// the bare "too many open files" does not say which limit to raise.
func descriptorLimit(err error) error {
	if !errors.Is(err, syscall.EMFILE) && !errors.Is(err, syscall.ENFILE) {
		return err
	}
	if runtime.GOOS == "darwin" {
		return fmt.Errorf("%w: the kqueue backend needs one descriptor per watched file; raise the open file limit (ulimit -n) or kern.maxfilesperproc, or exclude more of the tree", err)
	}
	return fmt.Errorf("%w: raise the open file limit (ulimit -n) or exclude more of the tree", err)
}

func (watcher *Watcher) processEvent(ctx context.Context, raw fsnotify.Event) error {
	operation := operationFrom(raw.Op)
	if operation == 0 {
		return nil
	}
	path, err := filepath.Abs(raw.Name)
	if err != nil {
		return fmt.Errorf("normalize event path %q: %w", raw.Name, err)
	}
	path = filepath.Clean(path)
	root, ok := watcher.repositoryFor(path)
	if !ok || root.ignored.ignored(path) {
		return nil
	}
	if operation.Has(OperationCreate) {
		info, statErr := os.Lstat(path)
		if statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			if err := watcher.addTree(root, path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("extend watch for %q: %w", path, err)
			}
		} else if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return fmt.Errorf("inspect created path %q: %w", path, statErr)
		}
	}
	if operation.Has(OperationRemove) || operation.Has(OperationRename) {
		if err := watcher.removeTree(path); err != nil {
			return err
		}
	}
	return watcher.emit(ctx, Event{Repository: root.name, Path: path, Operations: operation})
}

func (watcher *Watcher) repositoryFor(path string) (repositoryRoot, bool) {
	for _, root := range watcher.roots {
		if pathWithin(root.path, path) {
			return root, true
		}
	}
	return repositoryRoot{}, false
}

func (watcher *Watcher) removeTree(path string) error {
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	var failures []error
	for watchedPath := range watcher.watched {
		if !pathWithin(path, watchedPath) {
			continue
		}
		if err := watcher.backend.Remove(watchedPath); err != nil && !errors.Is(err, fsnotify.ErrNonExistentWatch) && !errors.Is(err, fsnotify.ErrClosed) {
			failures = append(failures, fmt.Errorf("remove directory watch %q: %w", watchedPath, err))
		}
		delete(watcher.watched, watchedPath)
	}
	return errors.Join(failures...)
}

func (watcher *Watcher) emit(ctx context.Context, event Event) error {
	select {
	case watcher.events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-watcher.done:
		return nil
	}
}

func (watcher *Watcher) reportError(ctx context.Context, err error) error {
	select {
	case watcher.errors <- err:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-watcher.done:
		return nil
	}
}

func operationFrom(operation fsnotify.Op) Operation {
	var result Operation
	if operation.Has(fsnotify.Create) {
		result |= OperationCreate
	}
	if operation.Has(fsnotify.Write) {
		result |= OperationWrite
	}
	if operation.Has(fsnotify.Remove) {
		result |= OperationRemove
	}
	if operation.Has(fsnotify.Rename) {
		result |= OperationRename
	}
	if operation.Has(fsnotify.Chmod) {
		result |= OperationChmod
	}
	return result
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
