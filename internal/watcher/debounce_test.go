package watcher

import (
	"context"
	"testing"
	"time"
)

func TestNewBatcherValidatesDurations(t *testing.T) {
	for _, test := range []struct {
		name     string
		debounce time.Duration
		maximum  time.Duration
	}{
		{name: "zero debounce", debounce: 0, maximum: time.Second},
		{name: "negative debounce", debounce: -time.Millisecond, maximum: time.Second},
		{name: "maximum below debounce", debounce: time.Second, maximum: 500 * time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewBatcher(test.debounce, test.maximum); err == nil {
				t.Fatal("NewBatcher() unexpectedly succeeded")
			}
		})
	}
	batcher, err := NewBatcher(DefaultDebounce, DefaultMaximumBatch)
	if err != nil {
		t.Fatalf("NewBatcher(defaults) error = %v", err)
	}
	if batcher.debounce != DefaultDebounce || batcher.maximum != DefaultMaximumBatch {
		t.Fatalf("durations = %s/%s, want %s/%s", batcher.debounce, batcher.maximum, DefaultDebounce, DefaultMaximumBatch)
	}
}

func TestBatcherDebouncesAndCoalescesInFirstSeenOrder(t *testing.T) {
	batcher, err := NewBatcher(25*time.Millisecond, 120*time.Millisecond)
	if err != nil {
		t.Fatalf("NewBatcher() error = %v", err)
	}
	input := make(chan Event)
	output := batcher.Run(context.Background(), input)
	first := Event{Repository: "repo", Path: "/repo/a.go", Operations: OperationCreate}
	input <- first
	input <- Event{Repository: "repo", Path: "/repo/b.go", Operations: OperationWrite}
	input <- Event{Repository: "repo", Path: "/repo/a.go", Operations: OperationWrite}

	batch := receiveBatch(t, output, 500*time.Millisecond)
	if len(batch.Events) != 2 {
		t.Fatalf("batch events = %#v, want two coalesced paths", batch.Events)
	}
	if batch.Events[0].Repository != first.Repository || batch.Events[0].Path != first.Path {
		t.Fatalf("first event = %#v, want %#v", batch.Events[0], first)
	}
	if want := OperationCreate | OperationWrite; batch.Events[0].Operations != want {
		t.Fatalf("coalesced operations = %s, want %s", batch.Events[0].Operations, want)
	}
	if batch.Events[1].Path != "/repo/b.go" || batch.Events[1].Operations != OperationWrite {
		t.Fatalf("second event = %#v, want b.go WRITE", batch.Events[1])
	}
	close(input)
	waitForClosedBatchStream(t, output)
}

func TestBatcherFlushesAtMaximumWhileEventsContinue(t *testing.T) {
	const (
		debounce = 35 * time.Millisecond
		maximum  = 100 * time.Millisecond
	)
	batcher, err := NewBatcher(debounce, maximum)
	if err != nil {
		t.Fatalf("NewBatcher() error = %v", err)
	}
	input := make(chan Event, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := batcher.Run(ctx, input)
	input <- Event{Repository: "repo", Path: "/repo/live.go", Operations: OperationWrite}
	started := time.Now()
	stop := make(chan struct{})
	senderDone := make(chan struct{})
	go func() {
		defer close(senderDone)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				select {
				case input <- Event{Repository: "repo", Path: "/repo/live.go", Operations: OperationWrite}:
				case <-stop:
					return
				}
			case <-stop:
				return
			}
		}
	}()

	batch := receiveBatch(t, output, 400*time.Millisecond)
	elapsed := time.Since(started)
	close(stop)
	<-senderDone
	cancel()
	waitForClosedBatchStream(t, output)

	if elapsed < 80*time.Millisecond {
		t.Fatalf("batch flushed after %s, before maximum deadline while events continued", elapsed)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("batch flushed after %s, beyond maximum deadline", elapsed)
	}
	if len(batch.Events) != 1 || batch.Events[0].Path != "/repo/live.go" {
		t.Fatalf("maximum batch = %#v, want one live.go event", batch.Events)
	}
}

func TestBatcherFlushesPendingEventsWhenInputCloses(t *testing.T) {
	batcher, err := NewBatcher(time.Second, 2*time.Second)
	if err != nil {
		t.Fatalf("NewBatcher() error = %v", err)
	}
	input := make(chan Event)
	output := batcher.Run(context.Background(), input)
	input <- Event{Repository: "repo", Path: "/repo/closed.go", Operations: OperationRemove}
	close(input)

	batch := receiveBatch(t, output, 100*time.Millisecond)
	if len(batch.Events) != 1 || batch.Events[0].Path != "/repo/closed.go" {
		t.Fatalf("closed-input batch = %#v", batch.Events)
	}
	waitForClosedBatchStream(t, output)
}

func TestBatcherCancellationStopsWithoutPendingBatch(t *testing.T) {
	batcher, err := NewBatcher(time.Second, 2*time.Second)
	if err != nil {
		t.Fatalf("NewBatcher() error = %v", err)
	}
	input := make(chan Event)
	ctx, cancel := context.WithCancel(context.Background())
	output := batcher.Run(ctx, input)
	input <- Event{Repository: "repo", Path: "/repo/canceled.go", Operations: OperationWrite}
	cancel()

	select {
	case batch, ok := <-output:
		if ok {
			t.Fatalf("received batch %#v after cancellation", batch)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("batcher did not stop after cancellation")
	}
}

func receiveBatch(t *testing.T, output <-chan Batch, timeout time.Duration) Batch {
	t.Helper()
	select {
	case batch, ok := <-output:
		if !ok {
			t.Fatal("batch output closed before a batch arrived")
		}
		return batch
	case <-time.After(timeout):
		t.Fatal("timed out waiting for batch")
		return Batch{}
	}
}

func waitForClosedBatchStream(t *testing.T, output <-chan Batch) {
	t.Helper()
	select {
	case _, ok := <-output:
		if ok {
			t.Fatal("received an unexpected second batch")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("batch output did not close")
	}
}
