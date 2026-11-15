package main

import "sync"

// profileWatchers holds the watchers of each profile that depend on its
// repository registry, and replaces them when the profile is registered again.
//
// A watcher reads the registry once, when it starts. A profile `index_project`
// creates starts with an empty one, and a profile that gains a repository keeps
// the list it had: started once, neither would ever see the repositories that
// arrived later, and their edits and checkouts would never reach the graph
// until the process restarted.
type profileWatchers struct {
	mu     sync.Mutex
	closed bool
	stops  map[string]func()
	// retiring counts the replaced watchers still on their way out, so close
	// can promise that nothing it started outlives it.
	retiring sync.WaitGroup
}

// replace starts the watchers of one profile and retires the ones it had.
//
// The previous watchers are stopped on their own goroutine. A stop waits for
// its watcher to return, and a registration can be reached from inside one --
// a resynchroniser that reindexes a profile registers it again -- so waiting
// here would be a watcher waiting for itself.
func (watchers *profileWatchers) replace(name string, start func() func()) {
	watchers.mu.Lock()
	if watchers.closed {
		watchers.mu.Unlock()
		return
	}
	previous := watchers.stops[name]
	delete(watchers.stops, name)
	if previous != nil {
		watchers.retiring.Add(1)
	}
	watchers.mu.Unlock()
	if previous != nil {
		go func() {
			defer watchers.retiring.Done()
			previous()
		}()
	}

	stop := start()
	watchers.mu.Lock()
	if watchers.closed {
		watchers.mu.Unlock()
		stop()
		return
	}
	if watchers.stops == nil {
		watchers.stops = make(map[string]func())
	}
	// Two registrations of one profile can race here; the loser's watchers
	// are the ones to retire.
	raced := watchers.stops[name]
	watchers.stops[name] = stop
	watchers.mu.Unlock()
	if raced != nil {
		raced()
	}
}

// close stops every watcher and waits for the retired ones too.
func (watchers *profileWatchers) close() {
	watchers.mu.Lock()
	if watchers.closed {
		watchers.mu.Unlock()
		return
	}
	watchers.closed = true
	stops := watchers.stops
	watchers.stops = nil
	watchers.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
	watchers.retiring.Wait()
}
