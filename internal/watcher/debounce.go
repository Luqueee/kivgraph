package watcher

import (
	"context"
	"fmt"
	"time"
)

const (
	// DefaultDebounce is the quiet period after the last raw event.
	DefaultDebounce = 150 * time.Millisecond
	// DefaultMaximumBatch is the upper bound from the first event to a flush.
	DefaultMaximumBatch = 500 * time.Millisecond
	batchOutputBuffer   = 16
)

// Batch is an ordered set of coalesced filesystem events.
type Batch struct {
	// Events preserves first-seen order. Events with the same repository and
	// path are merged by OR-ing their Operations.
	Events []Event
}

// Batcher coalesces raw events using a quiet-period debounce and a hard batch
// deadline. It does not inspect the filesystem or calculate content hashes.
type Batcher struct {
	debounce time.Duration
	maximum  time.Duration
}

// NewBatcher creates a Batcher with the supplied durations. The maximum batch
// duration must be at least the debounce duration, matching watcher config
// validation. Use DefaultDebounce and DefaultMaximumBatch for project defaults.
func NewBatcher(debounce, maximum time.Duration) (*Batcher, error) {
	if debounce <= 0 {
		return nil, fmt.Errorf("debounce must be positive, got %s", debounce)
	}
	if maximum < debounce {
		return nil, fmt.Errorf("maximum batch must be at least debounce (%s), got %s", debounce, maximum)
	}
	return &Batcher{debounce: debounce, maximum: maximum}, nil
}

// Run consumes raw events and returns batches until input closes or ctx is
// cancelled. Input closure flushes the pending batch immediately. Context
// cancellation stops without emitting a pending batch because the caller has
// explicitly abandoned the update cycle.
func (batcher *Batcher) Run(ctx context.Context, input <-chan Event) <-chan Batch {
	output := make(chan Batch, batchOutputBuffer)
	if batcher == nil {
		close(output)
		return output
	}
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		defer close(output)
		pending := make(map[eventKey]Event)
		order := make([]eventKey, 0, 8)
		var debounceTimer *time.Timer
		var maximumTimer *time.Timer
		var debounceEvents <-chan time.Time
		var maximumEvents <-chan time.Time

		stopTimers := func() {
			stopTimer(debounceTimer)
			stopTimer(maximumTimer)
			debounceTimer = nil
			maximumTimer = nil
			debounceEvents = nil
			maximumEvents = nil
		}
		resetDebounce := func() {
			if debounceTimer == nil {
				debounceTimer = time.NewTimer(batcher.debounce)
			} else {
				stopTimer(debounceTimer)
				debounceTimer.Reset(batcher.debounce)
			}
			debounceEvents = debounceTimer.C
		}
		startMaximum := func() {
			maximumTimer = time.NewTimer(batcher.maximum)
			maximumEvents = maximumTimer.C
		}
		flush := func() {
			if len(order) == 0 {
				stopTimers()
				return
			}
			events := make([]Event, len(order))
			for index, key := range order {
				events[index] = pending[key]
			}
			output <- Batch{Events: events}
			pending = make(map[eventKey]Event)
			order = order[:0]
			stopTimers()
		}
		defer stopTimers()

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-input:
				if !ok {
					flush()
					return
				}
				key := eventKey{repository: event.Repository, path: event.Path}
				if previous, exists := pending[key]; exists {
					previous.Operations |= event.Operations
					pending[key] = previous
				} else {
					pending[key] = event
					order = append(order, key)
					if len(order) == 1 {
						startMaximum()
					}
				}
				resetDebounce()
			case <-debounceEvents:
				flush()
			case <-maximumEvents:
				flush()
			}
		}
	}()
	return output
}

type eventKey struct {
	repository string
	path       string
}

func stopTimer(timer *time.Timer) {
	if timer == nil || timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}
