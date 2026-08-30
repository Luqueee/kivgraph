package indexing

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/filelock"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/watcher"
	"github.com/Luqueee/kivgraph/internal/workspace"
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

// ResyncAttempts is how many times in a row the loop rebuilds one movement
// that keeps failing before it stops asking.
//
// A resynchroniser absorbs a failure and tries again, because most of them are
// transient: a rebuild that lost a race, a tree git was still writing. What it
// did not bound is the other kind. A daemon whose PATH cannot resolve `node`
// fails every rebuild of a TypeScript repository, identically, and at the
// intervals above that settles at one full `index --full` every six seconds,
// indefinitely -- measured at roughly ten attempts a minute for twenty minutes
// on a machine whose owner noticed the CPU rather than the log.
//
// Five to match StartLimitBurst in internal/supervisor, and for the same
// argument the unit already makes: a rebuild that failed five times in a row on
// a tree that has not moved again will not be fixed by a sixth. Backoff would
// soften the symptom and keep the loop, and a loop that cannot succeed should
// end rather than slow down.
const ResyncAttempts = 5

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
	// Attempts overrides ResyncAttempts.
	Attempts int

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
	// OnGaveUp reports the movement the loop stopped retrying, with the
	// failure that ended it. It fires once per abandoned batch and it is the
	// only place the give-up is stated: OnError has already reported each
	// attempt, and a reader counting those has no way to tell the difference
	// between a loop that stopped and one that is still going.
	OnGaveUp func([]RepositoryMovement, error)

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
	attempts := options.Attempts
	if attempts <= 0 {
		attempts = ResyncAttempts
	}

	tracker := newHeadTracker(options.Repositories, options.OnError)
	tracker.prime()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pending := map[string]RepositoryMovement{}
	var failures repeatedFailures
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
				if !failures.record(batch, attempts) {
					// Nothing is rewound, and that is the whole of
					// giving up: the tracker keeps the commit the tree
					// is actually on, so the next tick sees no movement
					// and this batch is never proposed again. Only the
					// tree moving somewhere new can produce work here,
					// which is the one event that could make a sixth
					// attempt different from the five that failed.
					reportGaveUp(options.OnGaveUp, batch, err)
					continue
				}
				// The tree is still where it moved to, so the next tick
				// must not conclude that nothing happened.
				tracker.rewind(batch)
				continue
			}
			failures.clear()
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

	lock, acquired, err := filelock.Acquire(options.LockPath)
	if err != nil {
		return false, fmt.Errorf("acquire resync lock: %w", err)
	}
	if !acquired {
		return false, nil
	}
	defer func() {
		if closeErr := lock.Release(); closeErr != nil {
			report(options.OnError, fmt.Errorf("release resync lock: %w", closeErr))
		}
	}()

	if err := options.Resync(ctx, repositories); err != nil {
		// Another process is publishing into the same store. That is what the
		// lock exists to produce, not a failure: whoever holds it rebuilds,
		// and this server installs the result through the CURRENT pointer it
		// follows anyway. Reporting it would put an ERROR in a server's log
		// for a system that is working.
		if errors.Is(err, generation.ErrPublishInProgress) {
			return false, nil
		}
		return false, fmt.Errorf("resync %d repository(ies): %w", len(repositories), err)
	}
	return true, nil
}

// repeatedFailures counts how many times in a row one batch failed.
//
// A batch is identified by what it describes -- which repositories moved, and
// between which commits -- and not by when it was proposed. A retry of an
// unchanged movement therefore carries the same fingerprint and adds to the
// count, while any genuinely new movement carries a different one and starts
// over. That is what makes the bound safe: it can only ever suppress a repeat
// of the identical failing work, never work nobody has attempted yet.
//
// The zero value counts nothing and matches no batch, which is what a loop that
// has not failed yet needs.
type repeatedFailures struct {
	fingerprint string
	count       int
}

// record counts one failure of a batch and reports whether it is worth
// retrying.
func (failures *repeatedFailures) record(batch []RepositoryMovement, limit int) bool {
	fingerprint := fingerprintOf(batch)
	if fingerprint != failures.fingerprint {
		failures.fingerprint = fingerprint
		failures.count = 0
	}
	failures.count++
	return failures.count < limit
}

// clear forgets the failing batch after a rebuild that did not fail. A batch
// that has succeeded once has earned the full count again if it ever fails.
func (failures *repeatedFailures) clear() {
	failures.fingerprint = ""
	failures.count = 0
}

// fingerprintOf describes a batch by its content. The batch arrives sorted by
// repository name, so the same movement always produces the same string, and
// the NUL separators keep two repositories from spelling one another's
// fingerprint between them.
func fingerprintOf(batch []RepositoryMovement) string {
	var fingerprint strings.Builder
	for _, movement := range batch {
		fingerprint.WriteString(movement.Repository.Name)
		fingerprint.WriteByte(0)
		fingerprint.WriteString(movement.From)
		fingerprint.WriteByte(0)
		fingerprint.WriteString(movement.To)
		fingerprint.WriteByte(0)
	}
	return fingerprint.String()
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

func reportGaveUp(sink func([]RepositoryMovement, error), batch []RepositoryMovement, err error) {
	if sink == nil || len(batch) == 0 {
		return
	}
	sink(batch, err)
}
