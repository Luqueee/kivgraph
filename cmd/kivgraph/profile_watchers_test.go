package main

import (
	"sync"
	"sync/atomic"
	"testing"
)

// recordedWatcher counts how often one generation of watchers was started and
// stopped, which is all a replacement can get wrong.
type recordedWatcher struct {
	started, stopped atomic.Int32
}

func (watcher *recordedWatcher) start() func() {
	watcher.started.Add(1)
	return func() { watcher.stopped.Add(1) }
}

func counts(watcher *recordedWatcher) [2]int32 {
	return [2]int32{watcher.started.Load(), watcher.stopped.Load()}
}

// Registering a profile again is how its watchers learn about a repository
// that arrived after startup: the old ones are stopped once, the new ones run,
// and another profile's are left alone.
func TestProfileWatchersReplaceOnlyTheRegisteredProfile(t *testing.T) {
	var watchers profileWatchers
	var first, second, other recordedWatcher
	watchers.replace("alpha", first.start)
	watchers.replace("beta", other.start)
	watchers.replace("alpha", second.start)
	// The retired watchers stop on their own goroutine; close waits for them.
	watchers.close()

	if got := counts(&first); got != [2]int32{1, 1} {
		t.Fatalf("replaced watchers started/stopped = %v, want started once and stopped once", got)
	}
	if got := counts(&second); got != [2]int32{1, 1} {
		t.Fatalf("current watchers started/stopped = %v, want stopped once by close", got)
	}
	if got := counts(&other); got != [2]int32{1, 1} {
		t.Fatalf("another profile's watchers started/stopped = %v, want untouched until close", got)
	}
}

func TestProfileWatchersStartNothingAfterClose(t *testing.T) {
	var watchers profileWatchers
	var late recordedWatcher
	watchers.close()
	watchers.close()
	watchers.replace("alpha", late.start)
	if got := counts(&late); got != [2]int32{0, 0} {
		t.Fatalf("watchers registered after close started/stopped = %v, want never started", got)
	}
}

// A close that lands while a registration is starting its watchers must not
// leave them running: nothing would ever stop them.
func TestProfileWatchersStopWhatStartedDuringClose(t *testing.T) {
	var watchers profileWatchers
	var racing recordedWatcher
	watchers.replace("alpha", func() func() {
		stop := racing.start()
		watchers.close()
		return stop
	})
	if got := counts(&racing); got != [2]int32{1, 1} {
		t.Fatalf("watchers started during close started/stopped = %v, want stopped", got)
	}
}

// A stop waits for its watcher, and a watcher can be what registers the
// profile again. Stopping the previous watchers inline would wait for the
// caller itself.
func TestProfileWatchersReplaceFromInsideTheWatcherBeingReplaced(t *testing.T) {
	var watchers profileWatchers
	running := make(chan struct{})
	var replaced recordedWatcher
	watchers.replace("alpha", func() func() {
		return func() { <-running }
	})
	// This call stands for the old watcher's own goroutine: it only ends, and
	// lets the stop above return, after the registration it made has returned.
	watchers.replace("alpha", replaced.start)
	close(running)
	watchers.close()
	if got := counts(&replaced); got != [2]int32{1, 1} {
		t.Fatalf("watchers registered from inside a watcher started/stopped = %v", got)
	}
}

// Two registrations of one profile at once leave exactly one set running.
func TestProfileWatchersConcurrentRegistrationsLeaveOneRunning(t *testing.T) {
	var watchers profileWatchers
	const registrations = 32
	recorded := make([]recordedWatcher, registrations)
	var group sync.WaitGroup
	for index := range recorded {
		group.Add(1)
		go func() {
			defer group.Done()
			watchers.replace("alpha", recorded[index].start)
		}()
	}
	group.Wait()
	running := 0
	for index := range recorded {
		got := counts(&recorded[index])
		if got[0] != 1 {
			t.Fatalf("registration %d started %d times", index, got[0])
		}
		if got[1] == 0 {
			running++
		}
	}
	// The retired ones stop asynchronously, so only an upper bound holds before
	// close and the exact count holds after it.
	watchers.close()
	for index := range recorded {
		if got := counts(&recorded[index]); got != [2]int32{1, 1} {
			t.Fatalf("registration %d started/stopped = %v after close", index, got)
		}
	}
	if running < 1 {
		t.Fatal("no watchers were left running before close")
	}
}
