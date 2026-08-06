package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	// ErrLifecycleClosed reports an attempt to add work after shutdown began.
	ErrLifecycleClosed = errors.New("application lifecycle is closed")
	// ErrLifecycleStarted reports an attempt to add work after waiting began.
	ErrLifecycleStarted = errors.New("application lifecycle has already started")
)

// RunFunc is one long-lived application loop. Its context is cancelled before
// shutdown resources are closed.
type RunFunc func(context.Context) error

// Lifecycle coordinates long-lived loops and their owned resources. A caller
// registers resources in shutdown order: MCP ingress, watcher, worker,
// connections, and LadybugDB/storage. Run loops are stopped by cancelling
// Context and are waited after resource close.
type Lifecycle struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	resources []Resource
	closed    bool
	waiting   bool

	runners sync.WaitGroup
	runDone chan struct{}
	runErrs []error

	shutdownStarted bool
	shutdownDone    chan struct{}
	shutdownErr     error
}

// NewLifecycle creates an application lifecycle rooted at parent. A nil
// parent is treated as context.Background.
func NewLifecycle(parent context.Context) *Lifecycle {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &Lifecycle{
		ctx:          ctx,
		cancel:       cancel,
		shutdownDone: make(chan struct{}),
	}
}

// Context is the cancellation context shared by all long-lived loops.
func (lifecycle *Lifecycle) Context() context.Context {
	if lifecycle == nil {
		return context.Background()
	}
	return lifecycle.ctx
}

// Add registers a close operation. Resources must be added in the order they
// must close, not in reverse construction order.
func (lifecycle *Lifecycle) Add(resource Resource) error {
	if lifecycle == nil {
		return ErrLifecycleClosed
	}
	if strings.TrimSpace(resource.Name) == "" {
		return errors.New("lifecycle resource name must not be empty")
	}
	if resource.Close == nil {
		return fmt.Errorf("lifecycle resource %q has no close function", resource.Name)
	}
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	if lifecycle.closed {
		return ErrLifecycleClosed
	}
	if lifecycle.waiting {
		return ErrLifecycleStarted
	}
	lifecycle.resources = append(lifecycle.resources, resource)
	return nil
}

// AddCloser registers a conventional Close() error resource.
func (lifecycle *Lifecycle) AddCloser(name string, closer interface{ Close() error }) error {
	if closer == nil {
		return lifecycle.Add(Resource{Name: name, Close: nil})
	}
	return lifecycle.Add(Resource{
		Name: name,
		Close: func(context.Context) error {
			return closer.Close()
		},
	})
}

// AddContextCloser registers a resource whose close operation accepts a
// context, such as tsworker.Supervisor.
func (lifecycle *Lifecycle) AddContextCloser(name string, closer interface {
	Close(context.Context) error
}) error {
	if closer == nil {
		return lifecycle.Add(Resource{Name: name, Close: nil})
	}
	return lifecycle.Add(Resource{Name: name, Close: closer.Close})
}

// Go starts one long-lived loop. Calls after Wait or Shutdown are rejected so
// the wait group cannot be extended while it is being drained.
func (lifecycle *Lifecycle) Go(name string, run RunFunc) error {
	if lifecycle == nil {
		return ErrLifecycleClosed
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("lifecycle runner name must not be empty")
	}
	if run == nil {
		return fmt.Errorf("lifecycle runner %q has no run function", name)
	}
	lifecycle.mu.Lock()
	if lifecycle.closed {
		lifecycle.mu.Unlock()
		return ErrLifecycleClosed
	}
	if lifecycle.waiting {
		lifecycle.mu.Unlock()
		return ErrLifecycleStarted
	}
	lifecycle.runners.Add(1)
	lifecycle.mu.Unlock()

	go func() {
		defer lifecycle.runners.Done()
		err := run(lifecycle.ctx)
		if err == nil || lifecycle.expectedCancellation(err) {
			return
		}
		lifecycle.mu.Lock()
		lifecycle.runErrs = append(lifecycle.runErrs, fmt.Errorf("%s: %w", name, err))
		lifecycle.mu.Unlock()
	}()
	return nil
}

// Wait waits for all registered loops and returns their unexpected failures.
// It does not close resources; Shutdown must be used by the owner.
func (lifecycle *Lifecycle) Wait() error {
	if lifecycle == nil {
		return nil
	}
	return lifecycle.waitRunners(context.Background())
}

// Shutdown cancels all loops, closes resources in registration order, and
// waits for the loops to exit. It is idempotent and safe for concurrent callers.
// The first caller supplies the context used by close operations; later callers
// may use a different context to bound their own wait for that same shutdown.
func (lifecycle *Lifecycle) Shutdown(ctx context.Context) error {
	if lifecycle == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	lifecycle.mu.Lock()
	if lifecycle.shutdownStarted {
		done := lifecycle.shutdownDone
		lifecycle.mu.Unlock()
		select {
		case <-done:
			lifecycle.mu.Lock()
			err := lifecycle.shutdownErr
			lifecycle.mu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	lifecycle.shutdownStarted = true
	lifecycle.closed = true
	resources := append([]Resource(nil), lifecycle.resources...)
	lifecycle.mu.Unlock()

	// Cancellation is the MCP close signal for Server.Run and also stops a
	// watcher loop that is blocked in its event select.
	lifecycle.cancel()
	closeErr := Shutdown(ctx, resources...)
	waitErr := lifecycle.waitRunners(ctx)
	shutdownErr := errors.Join(closeErr, waitErr)

	lifecycle.mu.Lock()
	lifecycle.shutdownErr = shutdownErr
	close(lifecycle.shutdownDone)
	lifecycle.mu.Unlock()
	return shutdownErr
}

func (lifecycle *Lifecycle) waitRunners(ctx context.Context) error {
	lifecycle.mu.Lock()
	if !lifecycle.waiting {
		lifecycle.waiting = true
		lifecycle.runDone = make(chan struct{})
		done := lifecycle.runDone
		go func() {
			lifecycle.runners.Wait()
			close(done)
		}()
	}
	done := lifecycle.runDone
	lifecycle.mu.Unlock()

	select {
	case <-done:
		lifecycle.mu.Lock()
		err := errors.Join(lifecycle.runErrs...)
		lifecycle.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (lifecycle *Lifecycle) expectedCancellation(err error) bool {
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return lifecycle.ctx.Err() != nil
}
