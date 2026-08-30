package indexing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/filelock"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// fakeClock lets a test decide that the debounce has elapsed instead of
// sleeping through it.
type fakeClock struct {
	mutex sync.Mutex
	now   time.Time
}

func (clock *fakeClock) Now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.now
}

func (clock *fakeClock) Advance(by time.Duration) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.now = clock.now.Add(by)
}

// gitFixture writes the smallest layout ReadGitHead accepts. Running the git
// binary would make these tests slower and depend on the host for no gain.
func gitFixture(t *testing.T, name, branch, commit string) workspace.Repository {
	t.Helper()
	root := testsupport.TempDir(t)
	writeGitFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/"+branch+"\n")
	writeGitFile(t, filepath.Join(root, ".git", "refs", "heads", branch), commit+"\n")
	return workspace.Repository{Name: name, Path: root, RealPath: root}
}

func moveHead(t *testing.T, repository workspace.Repository, branch, commit string) {
	t.Helper()
	writeGitFile(t, filepath.Join(repository.RealPath, ".git", "HEAD"), "ref: refs/heads/"+branch+"\n")
	writeGitFile(t, filepath.Join(repository.RealPath, ".git", "refs", "heads", branch), commit+"\n")
}

func writeGitFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

// resyncHarness runs the loop in the background and records what it asked for.
type resyncHarness struct {
	mutex         sync.Mutex
	batches       [][]string
	observedMoves int
	attempts      int
	fail          error
	// failuresLeft is how many more attempts fail: -1 for every one of them,
	// and a positive count for a failure that heals on its own. The count is
	// what makes "it failed twice and then worked" a fact of the fake rather
	// than a race the test has to win.
	failuresLeft int

	clock  *fakeClock
	cancel context.CancelFunc
	done   chan struct{}
}

func startResync(t *testing.T, options ResyncOptions) *resyncHarness {
	t.Helper()
	harness := &resyncHarness{
		clock: &fakeClock{now: time.Unix(1_700_000_000, 0)},
		done:  make(chan struct{}),
	}
	options.Now = harness.clock.Now
	if options.Interval == 0 {
		options.Interval = time.Millisecond
	}
	if options.Debounce == 0 {
		options.Debounce = time.Minute
	}
	if options.LockPath == "" {
		options.LockPath = filepath.Join(testsupport.TempDir(t), "resync.lock")
	}
	// The loop stamps the quiet period from the clock the test controls, so
	// a test that advanced it before the move was observed would stamp the
	// debounce forward and wait forever. Counting observations lets a test
	// advance only once the loop has seen what it did.
	previousOnMoved := options.OnMoved
	options.OnMoved = func(batch []RepositoryMovement) {
		harness.mutex.Lock()
		harness.observedMoves++
		harness.mutex.Unlock()
		if previousOnMoved != nil {
			previousOnMoved(batch)
		}
	}
	options.Resync = func(_ context.Context, repositories []workspace.Repository) error {
		harness.mutex.Lock()
		defer harness.mutex.Unlock()
		harness.attempts++
		if harness.fail != nil && harness.failuresLeft != 0 {
			if harness.failuresLeft > 0 {
				harness.failuresLeft--
			}
			return harness.fail
		}
		names := make([]string, 0, len(repositories))
		for _, repository := range repositories {
			names = append(names, repository.Name)
		}
		harness.batches = append(harness.batches, names)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	harness.cancel = cancel
	go func() {
		defer close(harness.done)
		if err := Resync(ctx, options); err != nil {
			t.Errorf("Resync() error = %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-harness.done
	})
	return harness
}

// awaitMoved waits until the loop has observed count movement reports. A test
// must not advance the clock before that: the loop stamps the quiet period
// with the same clock, and an early advance moves the deadline with it.
func (harness *resyncHarness) awaitMoved(t *testing.T, count int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		harness.mutex.Lock()
		seen := harness.observedMoves
		harness.mutex.Unlock()
		if seen >= count {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d movement report(s)", count)
}

func (harness *resyncHarness) setFailure(err error) {
	harness.mutex.Lock()
	defer harness.mutex.Unlock()
	harness.fail = err
	harness.failuresLeft = 0
	if err != nil {
		harness.failuresLeft = -1
	}
}

// failNext makes the next times attempts fail and every one after them
// succeed.
func (harness *resyncHarness) failNext(err error, times int) {
	harness.mutex.Lock()
	defer harness.mutex.Unlock()
	harness.fail = err
	harness.failuresLeft = times
}

// attempted is how many rebuilds the loop has asked for, failed or not.
func (harness *resyncHarness) attempted() int {
	harness.mutex.Lock()
	defer harness.mutex.Unlock()
	return harness.attempts
}

func (harness *resyncHarness) observed() [][]string {
	harness.mutex.Lock()
	defer harness.mutex.Unlock()
	return append([][]string(nil), harness.batches...)
}

// awaitNoBatch gives the loop time to do the wrong thing.
// awaitRebuild advances the clock while waiting. Stamping the quiet period is
// the loop's business and it does it on every tick that sees movement, so a
// test that advanced once could always be beaten by one more observation.
// Advancing repeatedly cannot make the loop fire early: it still refuses while
// anything moved on the tick, or while git holds the index lock.
func (harness *resyncHarness) awaitRebuild(t *testing.T, count int) [][]string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		harness.clock.Advance(time.Minute)
		if got := harness.observed(); len(got) >= count {
			return got
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d rebuild(s), saw %#v", count, harness.observed())
	return nil
}

func (harness *resyncHarness) awaitNoBatch(t *testing.T) {
	t.Helper()
	time.Sleep(60 * time.Millisecond)
	if got := harness.observed(); len(got) != 0 {
		t.Fatalf("loop rebuilt when it should not have: %#v", got)
	}
}

func TestResyncRebuildsOnlyAfterHeadMovesAndSettles(t *testing.T) {
	repository := gitFixture(t, "alpha", "main", "1111111111111111111111111111111111111111")
	harness := startResync(t, ResyncOptions{
		Repositories: []workspace.Repository{repository},
		Debounce:     30 * time.Second,
	})

	// Priming must not rebuild: the graph was built from what is checked
	// out now.
	harness.awaitNoBatch(t)

	moveHead(t, repository, "feature/x", "2222222222222222222222222222222222222222")
	harness.awaitMoved(t, 1)
	// The move alone is not enough; the debounce has not elapsed.
	harness.awaitNoBatch(t)

	batches := harness.awaitRebuild(t, 1)
	if len(batches[0]) != 1 || batches[0][0] != "alpha" {
		t.Fatalf("rebuild batch = %#v, want just alpha", batches[0])
	}
}

// TestResyncCoalescesAWholeWorkspaceIntoOneRebuild is the property that keeps
// a multi-repository pull affordable: a rebuild costs the whole corpus, so
// paying it once per repository would cost minutes.
func TestResyncCoalescesAWholeWorkspaceIntoOneRebuild(t *testing.T) {
	first := gitFixture(t, "alpha", "main", "1111111111111111111111111111111111111111")
	second := gitFixture(t, "beta", "main", "3333333333333333333333333333333333333333")
	third := gitFixture(t, "gamma", "main", "5555555555555555555555555555555555555555")
	harness := startResync(t, ResyncOptions{
		Repositories: []workspace.Repository{first, second, third},
		Debounce:     30 * time.Second,
	})
	harness.awaitNoBatch(t)

	moveHead(t, first, "main", "2222222222222222222222222222222222222222")
	moveHead(t, second, "main", "4444444444444444444444444444444444444444")
	harness.awaitMoved(t, 1)

	batches := harness.awaitRebuild(t, 1)
	if len(batches) != 1 {
		t.Fatalf("rebuilds = %#v, want exactly one", batches)
	}
	if len(batches[0]) != 2 || batches[0][0] != "alpha" || batches[0][1] != "beta" {
		t.Fatalf("batch = %#v, want alpha and beta, sorted, without gamma", batches[0])
	}
}

// TestResyncWaitsWhileGitHoldsTheIndexLock keeps the loop from indexing a
// half-written tree: HEAD moves early in a checkout and the files follow.
func TestResyncWaitsWhileGitHoldsTheIndexLock(t *testing.T) {
	repository := gitFixture(t, "alpha", "main", "1111111111111111111111111111111111111111")
	harness := startResync(t, ResyncOptions{
		Repositories: []workspace.Repository{repository},
		Debounce:     30 * time.Second,
	})
	harness.awaitNoBatch(t)

	lockPath := filepath.Join(repository.RealPath, ".git", "index.lock")
	writeGitFile(t, lockPath, "")
	moveHead(t, repository, "main", "2222222222222222222222222222222222222222")
	harness.awaitMoved(t, 1)
	harness.clock.Advance(time.Minute)
	harness.awaitNoBatch(t)

	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("Remove(index.lock) error = %v", err)
	}
	harness.awaitRebuild(t, 1)
}

// TestResyncRetriesAfterAFailedRebuild guards the bookkeeping: a loop that
// remembered the new commit after failing would conclude on the next tick
// that the tree never moved, and never try again.
func TestResyncRetriesAfterAFailedRebuild(t *testing.T) {
	repository := gitFixture(t, "alpha", "main", "1111111111111111111111111111111111111111")
	var absorbed []error
	var mutex sync.Mutex
	harness := startResync(t, ResyncOptions{
		Repositories: []workspace.Repository{repository},
		Debounce:     30 * time.Second,
		OnError: func(err error) {
			mutex.Lock()
			defer mutex.Unlock()
			absorbed = append(absorbed, err)
		},
	})
	harness.awaitNoBatch(t)

	harness.setFailure(errors.New("rebuild exploded"))
	moveHead(t, repository, "main", "2222222222222222222222222222222222222222")
	harness.awaitMoved(t, 1)
	harness.clock.Advance(time.Minute)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mutex.Lock()
		seen := len(absorbed)
		mutex.Unlock()
		if seen > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	mutex.Lock()
	seen := len(absorbed)
	mutex.Unlock()
	if seen == 0 {
		t.Fatal("a failed rebuild was not reported")
	}

	// The rewind puts the tracker back before the move, so the loop observes
	// it a second time and stamps the quiet period again. Advancing before
	// that would move the deadline with it and the retry would never fire.
	harness.setFailure(nil)
	batches := harness.awaitRebuild(t, 1)
	if batches[0][0] != "alpha" {
		t.Fatalf("retry batch = %#v", batches[0])
	}
}

// TestResyncYieldsToAnotherWriter: two servers may follow the same
// repositories, only one may rebuild them. The loser does not fail; it will
// serve the winner's generation through the CURRENT pointer it already
// follows.
// TestResyncSkipsWhenTheContentIsUnchanged is the commit case: HEAD moves and
// nothing is rewritten, so rebuilding the corpus would spend seconds to
// produce the graph that is already published.
func TestResyncSkipsWhenTheContentIsUnchanged(t *testing.T) {
	repository := gitFixture(t, "alpha", "main", "1111111111111111111111111111111111111111")
	var skipped int
	var mutex sync.Mutex
	harness := startResync(t, ResyncOptions{
		Repositories: []workspace.Repository{repository},
		Debounce:     30 * time.Second,
		ContentUnchanged: func(context.Context, []RepositoryMovement) (bool, error) {
			return true, nil
		},
		OnSkipped: func([]RepositoryMovement) {
			mutex.Lock()
			skipped++
			mutex.Unlock()
		},
	})
	harness.awaitNoBatch(t)

	moveHead(t, repository, "main", "2222222222222222222222222222222222222222")
	harness.awaitMoved(t, 1)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		harness.clock.Advance(time.Minute)
		mutex.Lock()
		seen := skipped
		mutex.Unlock()
		if seen > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	mutex.Lock()
	seen := skipped
	mutex.Unlock()
	if seen == 0 {
		t.Fatal("an unchanged tree was never reported as skipped")
	}
	if got := harness.observed(); len(got) != 0 {
		t.Fatalf("an unchanged tree was rebuilt anyway: %#v", got)
	}

	// And the tracker moved on: a later, real change still rebuilds.
	harness.clock.Advance(time.Minute)
	if got := harness.observed(); len(got) != 0 {
		t.Fatalf("the skip repeated into a rebuild: %#v", got)
	}
}

func TestResyncYieldsToAnotherWriter(t *testing.T) {
	lockPath := filepath.Join(testsupport.TempDir(t), "resync.lock")
	held, acquired, err := filelock.Acquire(lockPath)
	if err != nil || !acquired {
		t.Fatalf("filelock.Acquire() = %v, %t, %v", held, acquired, err)
	}
	t.Cleanup(func() { _ = held.Release() })

	repository := gitFixture(t, "alpha", "main", "1111111111111111111111111111111111111111")
	harness := startResync(t, ResyncOptions{
		Repositories: []workspace.Repository{repository},
		Debounce:     30 * time.Second,
		LockPath:     lockPath,
	})
	harness.awaitNoBatch(t)

	moveHead(t, repository, "main", "2222222222222222222222222222222222222222")
	harness.awaitMoved(t, 1)
	harness.clock.Advance(time.Minute)
	harness.awaitNoBatch(t)

	if err := held.Release(); err != nil {
		t.Fatalf("release() error = %v", err)
	}
}

func TestResyncRejectsAnIncompleteRequest(t *testing.T) {
	if err := Resync(context.Background(), ResyncOptions{LockPath: "x"}); err == nil {
		t.Fatal("Resync() without a resync function must fail")
	}
	if err := Resync(context.Background(), ResyncOptions{
		Resync: func(context.Context, []workspace.Repository) error { return nil },
	}); err == nil {
		t.Fatal("Resync() without a lock path must fail")
	}
}

// awaitAttempts waits until the loop has asked for count rebuilds, failed or
// not, advancing the clock so the debounce cannot hold it back. It is the
// counting counterpart of awaitRebuild, which only sees the ones that worked.
func (harness *resyncHarness) awaitAttempts(t *testing.T, count int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		harness.clock.Advance(time.Minute)
		if harness.attempted() >= count {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d rebuild attempt(s), saw %d", count, harness.attempted())
}

// gaveUpRecorder collects what the loop abandoned.
type gaveUpRecorder struct {
	mutex    sync.Mutex
	batches  [][]string
	failures []error
}

func (recorder *gaveUpRecorder) record(batch []RepositoryMovement, err error) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	names := make([]string, 0, len(batch))
	for _, movement := range batch {
		names = append(names, movement.Repository.Name)
	}
	recorder.batches = append(recorder.batches, names)
	recorder.failures = append(recorder.failures, err)
}

func (recorder *gaveUpRecorder) observed() ([][]string, []error) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return append([][]string(nil), recorder.batches...), append([]error(nil), recorder.failures...)
}

// await waits for count give-ups while the harness drives the clock.
func (recorder *gaveUpRecorder) await(t *testing.T, harness *resyncHarness, count int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		harness.clock.Advance(time.Minute)
		if batches, _ := recorder.observed(); len(batches) >= count {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	batches, _ := recorder.observed()
	t.Fatalf("timed out waiting for %d give-up(s), saw %#v", count, batches)
}

// TestResyncStopsRetryingAMovementThatKeepsFailing is the bound. Without it a
// daemon whose PATH cannot resolve the TypeScript worker rebuilds the whole
// corpus every six seconds for as long as the machine is on: the tree has
// really moved, the rewind is right, and nothing else ever says stop.
//
// The retry itself is TestResyncRetriesAfterAFailedRebuild's contract and it
// still holds; what this pins is where the retrying ends.
func TestResyncStopsRetryingAMovementThatKeepsFailing(t *testing.T) {
	repository := gitFixture(t, "alpha", "main", "1111111111111111111111111111111111111111")
	var recorder gaveUpRecorder
	failure := errors.New("exec: node: not found")
	harness := startResync(t, ResyncOptions{
		Repositories: []workspace.Repository{repository},
		Debounce:     30 * time.Second,
		Attempts:     3,
		OnGaveUp:     recorder.record,
	})
	harness.awaitNoBatch(t)

	harness.setFailure(failure)
	moveHead(t, repository, "main", "2222222222222222222222222222222222222222")
	harness.awaitMoved(t, 1)
	recorder.await(t, harness, 1)

	batches, failures := recorder.observed()
	if len(batches) != 1 || len(batches[0]) != 1 || batches[0][0] != "alpha" {
		t.Fatalf("give-up batches = %#v, want one naming alpha", batches)
	}
	if !errors.Is(failures[0], failure) {
		t.Fatalf("give-up error = %v, want the failure that ended it", failures[0])
	}
	if attempted := harness.attempted(); attempted != 3 {
		t.Fatalf("the loop attempted %d rebuild(s), want exactly the 3 it was allowed", attempted)
	}

	// And it stays stopped. A rebuild that would now succeed changes
	// nothing, because the loop is no longer proposing the batch: only the
	// tree moving somewhere new can produce work here again.
	harness.setFailure(nil)
	for range 50 {
		harness.clock.Advance(time.Minute)
		time.Sleep(time.Millisecond)
	}
	if attempted := harness.attempted(); attempted != 3 {
		t.Fatalf("the loop attempted %d rebuild(s) after giving up, want the 3 it stopped at", attempted)
	}
	if got := harness.observed(); len(got) != 0 {
		t.Fatalf("an abandoned batch was rebuilt after all: %#v", got)
	}

	// A genuinely new movement is new work and is tried immediately.
	moveHead(t, repository, "main", "3333333333333333333333333333333333333333")
	harness.awaitMoved(t, 2)
	if rebuilt := harness.awaitRebuild(t, 1); rebuilt[0][0] != "alpha" {
		t.Fatalf("rebuild after a new movement = %#v", rebuilt)
	}
}

// TestANewMovementBuysABatchTheFullCountAgain is the other half of the bound,
// and the one that keeps it from becoming a daemon that resynchronises once
// and then never again. The count follows the batch's content, so a tree that
// moved somewhere new is work nobody has attempted yet.
func TestANewMovementBuysABatchTheFullCountAgain(t *testing.T) {
	repository := gitFixture(t, "alpha", "main", "1111111111111111111111111111111111111111")
	var recorder gaveUpRecorder
	harness := startResync(t, ResyncOptions{
		Repositories: []workspace.Repository{repository},
		Debounce:     30 * time.Second,
		Attempts:     2,
		OnGaveUp:     recorder.record,
	})
	harness.awaitNoBatch(t)
	harness.setFailure(errors.New("exec: node: not found"))

	moveHead(t, repository, "main", "2222222222222222222222222222222222222222")
	harness.awaitMoved(t, 1)
	recorder.await(t, harness, 1)

	moveHead(t, repository, "main", "3333333333333333333333333333333333333333")
	recorder.await(t, harness, 2)

	if attempted := harness.attempted(); attempted != 4 {
		t.Fatalf("the loop attempted %d rebuild(s), want 2 for each of the two movements", attempted)
	}
}

// TestARebuildThatWorkedForgetsTheFailuresBeforeIt covers the reset a
// transient failure needs: an unlucky rebuild followed by a good one must not
// leave the next real failure with a shorter rope than the first one had.
func TestARebuildThatWorkedForgetsTheFailuresBeforeIt(t *testing.T) {
	repository := gitFixture(t, "alpha", "main", "1111111111111111111111111111111111111111")
	var recorder gaveUpRecorder
	harness := startResync(t, ResyncOptions{
		Repositories: []workspace.Repository{repository},
		Debounce:     30 * time.Second,
		Attempts:     2,
		OnGaveUp:     recorder.record,
	})
	harness.awaitNoBatch(t)

	// One failure, then a rebuild that works: two attempts for one movement.
	harness.failNext(errors.New("the tree was still being written"), 1)
	moveHead(t, repository, "main", "2222222222222222222222222222222222222222")
	harness.awaitMoved(t, 1)
	harness.awaitRebuild(t, 1)
	if attempted := harness.attempted(); attempted != 2 {
		t.Fatalf("the loop attempted %d rebuild(s), want one failure and one success", attempted)
	}

	// The next movement fails for good, and it is owed the whole count.
	harness.setFailure(errors.New("exec: node: not found"))
	moveHead(t, repository, "main", "3333333333333333333333333333333333333333")
	recorder.await(t, harness, 1)
	if attempted := harness.attempted(); attempted != 4 {
		t.Fatalf("the loop attempted %d rebuild(s), want the 2 above plus a full count of 2", attempted)
	}
}

// TestAFingerprintSeparatesBatchesThatWouldSpellTheSame is the corrupt shape
// nothing can build today: the fingerprint decides whether two failures are
// the same failure, so two batches it could not tell apart would spend one
// movement's retries on another's.
func TestAFingerprintSeparatesBatchesThatWouldSpellTheSame(t *testing.T) {
	movement := func(name, from, to string) RepositoryMovement {
		return RepositoryMovement{Repository: workspace.Repository{Name: name}, From: from, To: to}
	}
	for name, pair := range map[string][2][]RepositoryMovement{
		"a name that carries the next field": {
			{movement("alpha", "1111", "2222")},
			{movement("alpha1111", "", "2222")},
		},
		"one repository against two": {
			{movement("alpha", "1111", "2222")},
			{movement("alpha", "1111", "2222"), movement("beta", "3333", "4444")},
		},
		"the same repositories moving elsewhere": {
			{movement("alpha", "1111", "2222")},
			{movement("alpha", "1111", "3333")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if first, second := fingerprintOf(pair[0]), fingerprintOf(pair[1]); first == second {
				t.Fatalf("two different batches share the fingerprint %q", first)
			}
		})
	}
	// And the retry of one movement is the same batch, or the count would
	// never reach its limit and the loop would run forever after all.
	repeated := []RepositoryMovement{movement("alpha", "1111", "2222")}
	if fingerprintOf(repeated) != fingerprintOf([]RepositoryMovement{movement("alpha", "1111", "2222")}) {
		t.Fatal("one movement produced two fingerprints, so a retry would never be counted as one")
	}
}

// TestTheBoundDoesNotDependOnSomebodyListening: OnGaveUp is optional, like
// every other sink here. A loop that only stopped when somebody had asked to
// hear about it would go on forever in exactly the configurations that did not
// want the report.
func TestTheBoundDoesNotDependOnSomebodyListening(t *testing.T) {
	repository := gitFixture(t, "alpha", "main", "1111111111111111111111111111111111111111")
	harness := startResync(t, ResyncOptions{
		Repositories: []workspace.Repository{repository},
		Debounce:     30 * time.Second,
		Attempts:     2,
	})
	harness.awaitNoBatch(t)

	harness.setFailure(errors.New("exec: node: not found"))
	moveHead(t, repository, "main", "2222222222222222222222222222222222222222")
	harness.awaitMoved(t, 1)
	harness.awaitAttempts(t, 2)

	for range 50 {
		harness.clock.Advance(time.Minute)
		time.Sleep(time.Millisecond)
	}
	if attempted := harness.attempted(); attempted != 2 {
		t.Fatalf("the loop attempted %d rebuild(s) with no OnGaveUp set, want the 2 it was allowed", attempted)
	}
}
