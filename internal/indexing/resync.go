package indexing

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Luqueee/ladygraph/internal/watcher"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

// ResyncInterval is how often the resynchroniser asks each registered
// repository where its HEAD is.
//
// The question is two small file reads per repository and no subprocess, and
// its answer only changes when somebody checks out, so polling costs nothing
// worth measuring and avoids what watching would cost: git rewrites HEAD and
// refs with an atomic rename, so a descriptor held on either goes on pointing
// at the old inode, and under kqueue every watched path is a descriptor the
// source-tree watcher already needs.
const ResyncInterval = 2 * time.Second

// ResyncDebounce is how long HEAD must hold still before a move is acted on.
// A checkout is not atomic seen from outside: HEAD moves, then thousands of
// files are rewritten, and a rebase moves HEAD once per replayed commit.
const ResyncDebounce = 3 * time.Second

// ResyncOptions configures Resync.
type ResyncOptions struct {
	// Repositories are the registered repositories to follow. A repository
	// whose HEAD cannot be read is reported once and skipped, never guessed.
	Repositories []workspace.Repository
	// LockPath is the file that elects the single writer. Several servers
	// may follow the same repositories; only one may rebuild them.
	LockPath string
	// Resync performs the rebuild for the repositories whose HEAD moved. It
	// is injected because the loop decides *when*, never *how*: the route
	// belongs to the indexer.
	Resync func(context.Context, []workspace.Repository) error
	// Interval and Debounce override the constants above.
	Interval time.Duration
	Debounce time.Duration

	// ContentUnchanged, when set, is asked whether the bytes the graph
	// describes are still the ones on disk for the repositories that moved.
	//
	// HEAD moving is cheap to observe but says nothing about content: a
	// commit moves it and rewrites no file. Without this the loop would
	// rebuild the whole corpus after every commit to produce a graph
	// identical to the one it already had.
	ContentUnchanged func(context.Context, []RepositoryMovement) (bool, error)
	// OnMoved reports the repositories a tick observed moving, before the
	// debounce. It must not block.
	OnMoved func([]RepositoryMovement)
	// OnResynced reports a completed rebuild.
	OnResynced func([]RepositoryMovement)
	// OnError receives a failure the loop absorbed. A resynchroniser never
	// stops on one: the published graph keeps answering and the next tick
	// tries again.
	OnError func(error)
	// OnSkipped reports a movement that needed no rebuild: the content is
	// what the graph already describes, or another process is rebuilding it.
	OnSkipped func([]RepositoryMovement)

	// Now overrides the clock, for tests.
	Now func() time.Time
}

// RepositoryMovement is one repository whose working tree left the commit the
// graph was built from.
type RepositoryMovement struct {
	Repository workspace.Repository
	From       string
	To         string
	Branch     string
}

// Resync keeps the published graph on the code that is actually checked out.
//
// It observes one thing -- HEAD moving -- because that one thing covers every
// way a tree changes wholesale: checkout, pull, merge, rebase, reset, and
// commit. A push is not among them: it moves no local ref and rewrites no
// file, so the code the graph describes is identical before and after.
//
// Debouncing is per workspace and not per repository on purpose. A pull across
// thirty-three repositories must produce one rebuild: a rebuild costs the
// whole corpus, so paying it once per repository would cost minutes to reflect
// one afternoon's fetch.
//
// Resync blocks until ctx is done.
func Resync(ctx context.Context, options ResyncOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.Resync == nil {
		return errors.New("resync: a resync function is required")
	}
	if options.LockPath == "" {
		return errors.New("resync: a lock path is required")
	}
	interval := options.Interval
	if interval <= 0 {
		interval = ResyncInterval
	}
	debounce := options.Debounce
	if debounce <= 0 {
		debounce = ResyncDebounce
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}

	tracker := newHeadTracker(options.Repositories, options.OnError)
	tracker.prime()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pending := map[string]RepositoryMovement{}
	var quietSince time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			moved := tracker.poll()
			if len(moved) > 0 {
				for _, movement := range moved {
					// A repository that moves twice keeps the commit it
					// started from, so the report describes the whole
					// journey rather than its last leg.
					if previous, seen := pending[movement.Repository.Name]; seen {
						movement.From = previous.From
					}
					pending[movement.Repository.Name] = movement
				}
				quietSince = now()
				reportMovements(options.OnMoved, sortedMovements(pending))
				continue
			}
			if len(pending) == 0 {
				continue
			}
			if now().Sub(quietSince) < debounce {
				continue
			}
			// Git is still inside the operation while its index lock
			// exists. Rebuilding now would index a half-written tree.
			if tracker.anyBusy(pending) {
				quietSince = now()
				continue
			}

			batch := sortedMovements(pending)
			pending = map[string]RepositoryMovement{}
			rebuilt, err := resyncBatch(ctx, options, batch)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				report(options.OnError, err)
				// The tree is still where it moved to, so the next tick
				// must not conclude that nothing happened.
				tracker.rewind(batch)
				continue
			}
			if rebuilt {
				reportMovements(options.OnResynced, batch)
				continue
			}
			// Nothing was rebuilt: either the content is what the graph
			// already describes, or another process holds the lock and is
			// rebuilding it. Both leave this loop with nothing to say.
			reportMovements(options.OnSkipped, batch)
		}
	}
}

// resyncBatch runs one rebuild under the writer lock.
//
// Losing the lock is not a failure: another process is already rebuilding the
// same repositories, and every server follows the CURRENT pointer, so this one
// will serve the result without having produced it.
func resyncBatch(ctx context.Context, options ResyncOptions, batch []RepositoryMovement) (bool, error) {
	repositories := make([]workspace.Repository, 0, len(batch))
	for _, movement := range batch {
		repositories = append(repositories, movement.Repository)
	}

	// Asked before the lock and before any work: a commit is the common
	// case and it rewrites nothing, so the cheapest rebuild is the one that
	// does not happen.
	if options.ContentUnchanged != nil {
		unchanged, err := options.ContentUnchanged(ctx, batch)
		if err != nil {
			return false, fmt.Errorf("compare indexed content: %w", err)
		}
		if unchanged {
			return false, nil
		}
	}

	lock, acquired, err := acquireWriterLock(options.LockPath)
	if err != nil {
		return false, fmt.Errorf("acquire resync lock: %w", err)
	}
	if !acquired {
		return false, nil
	}
	defer func() {
		if closeErr := lock.release(); closeErr != nil {
			report(options.OnError, fmt.Errorf("release resync lock: %w", closeErr))
		}
	}()

	if err := options.Resync(ctx, repositories); err != nil {
		return false, fmt.Errorf("resync %d repository(ies): %w", len(repositories), err)
	}
	return true, nil
}

// headTracker remembers where each repository's HEAD was last seen.
type headTracker struct {
	repositories []workspace.Repository
	heads        map[string]string
	unreadable   map[string]struct{}
	onError      func(error)
}

func newHeadTracker(repositories []workspace.Repository, onError func(error)) *headTracker {
	return &headTracker{
		repositories: repositories,
		heads:        make(map[string]string, len(repositories)),
		unreadable:   make(map[string]struct{}),
		onError:      onError,
	}
}

// prime records the current position of every HEAD without reporting a move.
// The graph was built from what is checked out now; a first tick that called
// all of it new would rebuild the corpus on startup.
func (tracker *headTracker) prime() {
	for _, repository := range tracker.repositories {
		if head, err := watcher.ReadGitHead(repository.RealPath); err == nil {
			tracker.heads[repository.Name] = head.Commit
		}
	}
}

// poll returns the repositories whose HEAD left the commit last seen.
func (tracker *headTracker) poll() []RepositoryMovement {
	moved := make([]RepositoryMovement, 0)
	for _, repository := range tracker.repositories {
		head, err := watcher.ReadGitHead(repository.RealPath)
		if err != nil {
			// Reported once. A directory that is not a checkout is not a
			// failure worth repeating every two seconds.
			if _, seen := tracker.unreadable[repository.Name]; !seen {
				tracker.unreadable[repository.Name] = struct{}{}
				report(tracker.onError, fmt.Errorf("read HEAD of %q: %w", repository.Name, err))
			}
			continue
		}
		delete(tracker.unreadable, repository.Name)
		previous, known := tracker.heads[repository.Name]
		tracker.heads[repository.Name] = head.Commit
		if !known || previous == head.Commit {
			continue
		}
		moved = append(moved, RepositoryMovement{
			Repository: repository,
			From:       previous,
			To:         head.Commit,
			Branch:     head.Branch,
		})
	}
	return moved
}

// anyBusy reports whether git still holds the index lock of any repository in
// the batch.
func (tracker *headTracker) anyBusy(pending map[string]RepositoryMovement) bool {
	for _, movement := range pending {
		if watcher.GitOperationInProgress(movement.Repository.RealPath) {
			return true
		}
	}
	return false
}

// rewind restores the commit a failed batch started from, so the next poll
// sees the move again. Dropping the entry instead would re-prime it as if the
// repository had just been registered, and the retry would never come.
func (tracker *headTracker) rewind(batch []RepositoryMovement) {
	for _, movement := range batch {
		tracker.heads[movement.Repository.Name] = movement.From
	}
}

func sortedMovements(pending map[string]RepositoryMovement) []RepositoryMovement {
	batch := make([]RepositoryMovement, 0, len(pending))
	for _, movement := range pending {
		batch = append(batch, movement)
	}
	sort.Slice(batch, func(left, right int) bool {
		return batch[left].Repository.Name < batch[right].Repository.Name
	})
	return batch
}

func reportMovements(sink func([]RepositoryMovement), batch []RepositoryMovement) {
	if sink == nil || len(batch) == 0 {
		return
	}
	sink(batch)
}
