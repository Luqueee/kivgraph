package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLifecycleShutdownCancelsRunnersClosesEveryResourceInOrderAndIsIdempotent(t *testing.T) {
	lifecycle := NewLifecycle(context.Background())
	var mu sync.Mutex
	var closed []string
	record := func(name string) {
		mu.Lock()
		closed = append(closed, name)
		mu.Unlock()
	}

	for _, name := range []string{"watcher", "worker", "connection", "ladybug"} {
		name := name
		if err := lifecycle.Add(Resource{
			Name: name,
			Close: func(context.Context) error {
				record(name)
				return nil
			},
		}); err != nil {
			t.Fatalf("Add(%q) error = %v", name, err)
		}
	}

	runnerStopped := make(chan struct{})
	if err := lifecycle.Go("mcp", func(ctx context.Context) error {
		<-ctx.Done()
		close(runnerStopped)
		return ctx.Err()
	}); err != nil {
		t.Fatalf("Go() error = %v", err)
	}

	if err := lifecycle.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case <-runnerStopped:
	default:
		t.Fatal("Shutdown() returned before the MCP runner stopped")
	}

	mu.Lock()
	got := append([]string(nil), closed...)
	mu.Unlock()
	want := []string{"watcher", "worker", "connection", "ladybug"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("close order = %v, want %v", got, want)
	}
	if err := lifecycle.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(closed, ",") != strings.Join(want, ",") {
		t.Fatalf("second Shutdown() changed close order/count: %v", closed)
	}
}

func TestLifecycleClosesResourcesBeforeWaitingForDependentRunner(t *testing.T) {
	lifecycle := NewLifecycle(context.Background())
	released := make(chan struct{})
	if err := lifecycle.Add(Resource{
		Name: "worker",
		Close: func(context.Context) error {
			close(released)
			return nil
		},
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := lifecycle.Go("dependent loop", func(context.Context) error {
		<-released
		return nil
	}); err != nil {
		t.Fatalf("Go() error = %v", err)
	}
	if err := lifecycle.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestLifecycleShutdownContinuesAfterCloseFailure(t *testing.T) {
	lifecycle := NewLifecycle(context.Background())
	first := errors.New("watcher close failed")
	var closed []string
	for _, resource := range []Resource{
		{Name: "watcher", Close: func(context.Context) error { closed = append(closed, "watcher"); return first }},
		{Name: "worker", Close: func(context.Context) error { closed = append(closed, "worker"); return nil }},
		{Name: "ladybug", Close: func(context.Context) error { closed = append(closed, "ladybug"); return nil }},
	} {
		if err := lifecycle.Add(resource); err != nil {
			t.Fatalf("Add(%q) error = %v", resource.Name, err)
		}
	}

	err := lifecycle.Shutdown(context.Background())
	if !errors.Is(err, first) {
		t.Fatalf("Shutdown() error = %v, want the watcher failure", err)
	}
	if got, want := strings.Join(closed, ","), "watcher,worker,ladybug"; got != want {
		t.Fatalf("closed resources = %q, want %q", got, want)
	}
}

func TestLifecycleRejectsRegistrationAfterWaitBegins(t *testing.T) {
	lifecycle := NewLifecycle(context.Background())
	finished := make(chan struct{})
	if err := lifecycle.Go("loop", func(context.Context) error {
		close(finished)
		return nil
	}); err != nil {
		t.Fatalf("Go() error = %v", err)
	}
	if err := lifecycle.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	<-finished
	if err := lifecycle.Add(Resource{Name: "late", Close: func(context.Context) error { return nil }}); !errors.Is(err, ErrLifecycleStarted) {
		t.Fatalf("Add() error = %v, want ErrLifecycleStarted", err)
	}
	if err := lifecycle.Go("late", func(context.Context) error { return nil }); !errors.Is(err, ErrLifecycleStarted) {
		t.Fatalf("Go() error = %v, want ErrLifecycleStarted", err)
	}
}

func TestLifecycleShutdownHonorsCallerDeadline(t *testing.T) {
	lifecycle := NewLifecycle(context.Background())
	blocked := make(chan struct{})
	if err := lifecycle.Add(Resource{Name: "blocked", Close: func(ctx context.Context) error {
		close(blocked)
		<-ctx.Done()
		return ctx.Err()
	}}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := lifecycle.Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown() error = %v, want context.DeadlineExceeded", err)
	}
	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not start the close operation")
	}
}
