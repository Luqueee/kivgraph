package indexing

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/filelock"
	"github.com/Luqueee/kivgraph/internal/sourceobservation"
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

// SourceObservationInterval is the fallback interval for detecting a source
// edit when a filesystem notification was dropped. Normal dirty edits are
// signalled by SourceEvents and are observed after the ordinary debounce; this
// slower pass is the correctness backstop and does not hash every tree on each
// HEAD poll.
const SourceObservationInterval = 10 * time.Minute

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
	// SourceRepositories are the effective providers whose mutable state is
	// observed. It may include analyzer-discovered providers that are not in the
	// registry polled through Repositories. When absent, Repositories is used.
	SourceRepositories []workspace.Repository
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
	// Attempts overrides ResyncAttempts. A value of 1 tries a batch exactly
	// once: the first failure is already the last attempt, and OnGaveUp fires
	// instead of a second try ever being scheduled.
	Attempts int

	// SourceManifest seeds source tracking from the last valid generation. When
	// absent, SourceObserver captures the baseline before the first poll.
	SourceManifest *sourceobservation.Manifest
	// SourceObserver captures the current mutable source state. It is called on
	// a source event and periodically as a dropped-event backstop.
	SourceObserver func(context.Context) (sourceobservation.Manifest, error)
	// SourceObservationInterval controls the dropped-event backstop.
	SourceObservationInterval time.Duration
	// SourceEvents carries a signal that one or more source paths changed. The
	// channel is deliberately only a wake-up; SourceObserver remains the
	// authority for the actual bytes and Git state.
	SourceEvents <-chan struct{}

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
	// OnSourceChanged reports the manifest that caused an invalidation and the
	// affected source movements. It is called before the debounce completes.
	OnSourceReady   func(sourceobservation.Manifest)
	OnSourceChanged func(sourceobservation.Manifest, []RepositoryMovement)
	// OnSourcePublished reports a source manifest after the corresponding
	// rebuild or safe skip completed.
	OnSourcePublished func(sourceobservation.Manifest)
	// OnSourceUnavailable reports a source that could not be observed. The last
	// valid generation remains active and the next observation retries it.
	OnSourceUnavailable func(error)
	// OnError receives a failure the loop absorbed. A resynchroniser never
	// stops on the loop's own account: the published graph keeps answering
	// and, unless the batch has now used up Attempts, the next tick tries
	// again. At Attempts 1 the first failure is also the last one: OnGaveUp
	// fires right after this, on the same tick.
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
	Repository        workspace.Repository
	From              string
	To                string
	Branch            string
	Reason            string
	ProfileScoped     bool
	SourceObservation string
}

// Resync keeps the published graph on the code that is actually checked out.
//
// It observes Git HEAD moving and, when configured, mutable source state. HEAD
// covers every way a tree changes wholesale: checkout, pull, merge, rebase,
// reset, and commit. A push is not among them: it moves no local ref and
// rewrites no file, so the code the graph describes is identical before and
// after. Source events cover the complementary case: dirty bytes can change
// while HEAD stays still.
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
	sourceInterval := options.SourceObservationInterval
	if sourceInterval <= 0 {
		sourceInterval = SourceObservationInterval
	}
	sources := newSourceStateTracker(options)
	sourceRepositories := options.SourceRepositories
	if len(sourceRepositories) == 0 {
		sourceRepositories = options.Repositories
	}
	if sources != nil {
		if err := sources.prime(ctx, now()); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if options.SourceManifest != nil {
				return fmt.Errorf("resync: invalid initial source observations: %w", err)
			}
			reportSourceUnavailable(options, err)
		}
		if sources.ready && options.OnSourceReady != nil {
			options.OnSourceReady(sources.baseline)
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pending := map[string]RepositoryMovement{}
	var failures repeatedFailures
	var quietSince time.Time
	sourceRequested := sources != nil && options.SourceManifest != nil
	sourceEvents := options.SourceEvents
	if sourceRequested {
		quietSince = now()
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-sourceEvents:
			if !ok {
				// A closed notification channel means the producer has gone
				// away. Disable this wake-up path and keep the periodic
				// reconciliation backstop alive.
				sourceEvents = nil
				continue
			}
			if sources == nil {
				continue
			}
			sourceRequested = true
			quietSince = now()
		case <-ticker.C:
			nowValue := now()
			moved := tracker.poll()
			if len(moved) > 0 {
				mergeMovements(pending, moved)
				quietSince = nowValue
				reportMovements(options.OnMoved, sortedMovements(pending))
				if sources != nil {
					sourceRequested = true
				}
			}

			if sources != nil {
				periodic := !sourceRequested && nowValue.Sub(sources.lastCheck) >= sourceInterval
				debouncedEvent := sourceRequested && !quietSince.IsZero() && nowValue.Sub(quietSince) >= debounce
				if periodic || debouncedEvent {
					sourceRequested = false
					wasReady := sources.ready
					changes, actual, err := sources.check(ctx, sourceRepositories)
					sources.lastCheck = nowValue
					if err != nil {
						reportSourceUnavailable(options, err)
						// Leave the retry to the periodic backstop. A source that is
						// absent must not make a daemon hash it every debounce window.
						quietSince = time.Time{}
					} else {
						if !wasReady && sources.ready && options.OnSourceReady != nil {
							options.OnSourceReady(actual)
						}
						if len(changes) > 0 {
							mergeMovements(pending, changes)
							quietSince = nowValue
							reportSourceChanged(options, actual, changes)
						}
					}
				}
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
			outcome, err := resyncBatch(ctx, options, batch)
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
					if sources != nil {
						sources.abandon()
					}
					continue
				}
				// The tree is still where it moved to, so the next tick
				// must not conclude that nothing happened.
				tracker.rewind(batch)
				if sources != nil {
					sourceRequested = true
					quietSince = nowValue
				}
				continue
			}
			if outcome == resyncBusy {
				// Another process is rebuilding the same workspace. Keep the
				// batch so this loop can observe and report the completion rather
				// than silently dropping the only source movement it saw.
				mergeMovements(pending, batch)
				quietSince = nowValue
				if sources != nil {
					sourceRequested = true
				}
				continue
			}
			failures.clear()
			if sources != nil {
				if published, ok := sources.publish(); ok {
					reportSourcePublished(options, published)
				}
			}
			if outcome == resyncBuilt {
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

// sourceStateTracker keeps the source manifest that the active generation was
// built from. A source observation is only committed after the matching
// rebuild succeeds; a failed rebuild therefore keeps detecting the same source
// movement and can use the existing bounded retry policy.
type sourceStateTracker struct {
	observer  func(context.Context) (sourceobservation.Manifest, error)
	baseline  sourceobservation.Manifest
	latest    sourceobservation.Manifest
	ready     bool
	lastCheck time.Time
}

func newSourceStateTracker(options ResyncOptions) *sourceStateTracker {
	if options.SourceObserver == nil {
		return nil
	}
	return &sourceStateTracker{observer: options.SourceObserver, baseline: manifestOrZero(options.SourceManifest)}
}

func manifestOrZero(manifest *sourceobservation.Manifest) sourceobservation.Manifest {
	if manifest == nil {
		return sourceobservation.Manifest{}
	}
	return *manifest
}

func (tracker *sourceStateTracker) prime(ctx context.Context, now time.Time) error {
	if tracker.baseline.Profile != "" {
		if err := tracker.baseline.Validate(); err != nil {
			return fmt.Errorf("validate initial source observations: %w", err)
		}
		tracker.ready = true
		tracker.lastCheck = now
		return nil
	}
	manifest, err := tracker.observer(ctx)
	tracker.lastCheck = now
	if err != nil {
		return fmt.Errorf("observe initial source state: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("validate initial source state: %w", err)
	}
	tracker.baseline = manifest
	tracker.ready = true
	return nil
}

func (tracker *sourceStateTracker) check(
	ctx context.Context,
	repositories []workspace.Repository,
) ([]RepositoryMovement, sourceobservation.Manifest, error) {
	manifest, err := tracker.observer(ctx)
	if err != nil {
		return nil, sourceobservation.Manifest{}, fmt.Errorf("observe current source state: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return nil, sourceobservation.Manifest{}, fmt.Errorf("validate current source state: %w", err)
	}
	if !tracker.ready {
		tracker.baseline = manifest
		tracker.ready = true
		return nil, manifest, nil
	}
	changes, err := sourceobservation.Diff(tracker.baseline, manifest)
	if err != nil {
		return nil, sourceobservation.Manifest{}, err
	}
	if len(changes) == 0 {
		tracker.latest = manifest
		return nil, manifest, nil
	}
	movements, err := sourceMovements(changes, repositories)
	if err != nil {
		return nil, sourceobservation.Manifest{}, err
	}
	tracker.latest = manifest
	return movements, manifest, nil
}

func (tracker *sourceStateTracker) publish() (sourceobservation.Manifest, bool) {
	if tracker == nil || tracker.latest.Profile == "" {
		return sourceobservation.Manifest{}, false
	}
	tracker.baseline = tracker.latest
	tracker.latest = sourceobservation.Manifest{}
	return tracker.baseline, true
}

func (tracker *sourceStateTracker) abandon() {
	if tracker == nil || tracker.latest.Profile == "" {
		return
	}
	tracker.baseline = tracker.latest
	tracker.latest = sourceobservation.Manifest{}
}

func sourceMovements(changes []sourceobservation.Change, repositories []workspace.Repository) ([]RepositoryMovement, error) {
	byName := make(map[string]workspace.Repository, len(repositories))
	for _, repository := range repositories {
		byName[repository.Name] = repository
	}
	movements := make([]RepositoryMovement, 0, len(changes))
	for _, change := range changes {
		if change.ProfileScoped && change.Before.Repository == "" && change.After.Repository == "" {
			movements = append(movements, RepositoryMovement{
				Reason:        change.Reason,
				ProfileScoped: true,
			})
			continue
		}
		repository, exists := byName[change.Repository]
		if !exists {
			if change.Before.Repository != "" && change.After.Repository == "" {
				// A removed provider cannot be looked up in the current effective
				// set. Its name is still enough for the full profile rebuild,
				// which receives the current provider set from the indexer.
				repository = workspace.Repository{Name: change.Repository}
			} else {
				return nil, fmt.Errorf("source change %q is not a registered repository", change.Repository)
			}
		}
		movement := RepositoryMovement{
			Repository:        repository,
			From:              change.Before.Observation.Commit,
			To:                change.After.Observation.Commit,
			Branch:            change.After.Observation.Branch,
			Reason:            change.Reason,
			ProfileScoped:     change.ProfileScoped,
			SourceObservation: sourceObservationID(change),
		}
		movements = append(movements, movement)
	}
	sort.Slice(movements, func(left, right int) bool {
		return movements[left].Repository.Name < movements[right].Repository.Name
	})
	return movements, nil
}

func sourceObservationID(change sourceobservation.Change) string {
	if change.After.Repository != "" {
		return string(change.After.Observation.ID)
	}
	return string(change.Before.Observation.ID)
}

func mergeMovements(pending map[string]RepositoryMovement, movements []RepositoryMovement) {
	for _, movement := range movements {
		if previous, seen := pending[movement.Repository.Name]; seen {
			movement.From = previous.From
			if movement.Reason == "" {
				movement.Reason = previous.Reason
			}
			if movement.SourceObservation == "" {
				movement.SourceObservation = previous.SourceObservation
			}
			if previous.ProfileScoped {
				movement.ProfileScoped = true
			}
		}
		pending[movement.Repository.Name] = movement
	}
}

func reportSourceChanged(options ResyncOptions, manifest sourceobservation.Manifest, movements []RepositoryMovement) {
	if options.OnSourceChanged != nil {
		options.OnSourceChanged(manifest, movements)
	}
}

func reportSourcePublished(options ResyncOptions, manifest sourceobservation.Manifest) {
	if options.OnSourcePublished != nil {
		options.OnSourcePublished(manifest)
	}
}

func reportSourceUnavailable(options ResyncOptions, err error) {
	if options.OnSourceUnavailable != nil {
		options.OnSourceUnavailable(err)
	}
	report(options.OnError, err)
}

// resyncBatch runs one rebuild under the writer lock.
//
// Losing the lock is not a failure: another process is already rebuilding the
// same repositories, and every server follows the CURRENT pointer, so this one
// will serve the result without having produced it.
type resyncOutcome uint8

const (
	resyncBuilt resyncOutcome = iota
	resyncSkipped
	resyncBusy
)

func resyncBatch(ctx context.Context, options ResyncOptions, batch []RepositoryMovement) (resyncOutcome, error) {
	repositories := make([]workspace.Repository, 0, len(batch))
	for _, movement := range batch {
		if movement.Repository.Name == "" {
			continue
		}
		repositories = append(repositories, movement.Repository)
	}

	// Asked before the lock and before any work: a commit is the common
	// case and it rewrites nothing, so the cheapest rebuild is the one that
	// does not happen.
	if options.ContentUnchanged != nil {
		unchanged, err := options.ContentUnchanged(ctx, batch)
		if err != nil {
			return resyncSkipped, fmt.Errorf("compare indexed content: %w", err)
		}
		if unchanged {
			return resyncSkipped, nil
		}
	}

	lock, acquired, err := filelock.Acquire(options.LockPath)
	if err != nil {
		return resyncBusy, fmt.Errorf("acquire resync lock: %w", err)
	}
	if !acquired {
		return resyncBusy, nil
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
			return resyncBusy, nil
		}
		return resyncBusy, fmt.Errorf("resync %d repository(ies): %w", len(repositories), err)
	}
	return resyncBuilt, nil
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
		fingerprint.WriteString(movement.Reason)
		fingerprint.WriteByte(0)
		fingerprint.WriteString(movement.SourceObservation)
		fingerprint.WriteByte(0)
		if movement.ProfileScoped {
			fingerprint.WriteByte('1')
		} else {
			fingerprint.WriteByte('0')
		}
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
		if movement.Repository.RealPath == "" {
			continue
		}
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
