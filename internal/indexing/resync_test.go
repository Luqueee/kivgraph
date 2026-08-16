package indexing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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
	fail          error

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
		if harness.fail != nil {
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
	held, acquired, err := acquireWriterLock(lockPath)
	if err != nil || !acquired {
		t.Fatalf("acquireWriterLock() = %v, %t, %v", held, acquired, err)
	}
	t.Cleanup(func() { _ = held.release() })

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

	if err := held.release(); err != nil {
		t.Fatalf("release() error = %v", err)
	}
}

func TestAcquireWriterLockIsExclusiveAndReleasable(t *testing.T) {
	path := filepath.Join(testsupport.TempDir(t), "resync.lock")
	first, acquired, err := acquireWriterLock(path)
	if err != nil || !acquired {
		t.Fatalf("first acquireWriterLock() = %t, %v", acquired, err)
	}
	if _, second, err := acquireWriterLock(path); err != nil || second {
		t.Fatalf("second acquireWriterLock() = %t, %v, want refused without error", second, err)
	}
	if err := first.release(); err != nil {
		t.Fatalf("release() error = %v", err)
	}
	third, acquired, err := acquireWriterLock(path)
	if err != nil || !acquired {
		t.Fatalf("acquireWriterLock() after release = %t, %v", acquired, err)
	}
	if err := third.release(); err != nil {
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
