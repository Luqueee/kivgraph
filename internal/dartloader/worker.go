package dartloader

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const stderrLimit = 16 << 10

type diagnosticTail struct {
	mu    sync.Mutex
	bytes []byte
}

func (b *diagnosticTail) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if n >= stderrLimit {
		b.bytes = append(b.bytes[:0], p[n-stderrLimit:]...)
	} else {
		b.bytes = append(b.bytes, p...)
		if len(b.bytes) > stderrLimit {
			b.bytes = append([]byte(nil), b.bytes[len(b.bytes)-stderrLimit:]...)
		}
	}
	return n, nil
}
func (b *diagnosticTail) text() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.bytes))
}

// workerProcess owns one analyzer and observes Wait exactly once. The exit
// channel also releases protocol readers whose consumer has already failed.
type workerProcess struct {
	ctx    context.Context
	cancel context.CancelFunc
	cmd    *exec.Cmd
	in     io.WriteCloser
	stderr diagnosticTail
	done   chan struct{}
	exit   error
	once   sync.Once
}

func launchWorker(ctx context.Context, executable string, args []string, root string) (*workerProcess, io.Reader, error) {
	lifetime, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(lifetime, executable, args...)
	cmd.Dir = root
	cmd.WaitDelay = 250 * time.Millisecond
	w := &workerProcess{ctx: ctx, cancel: cancel, cmd: cmd, done: make(chan struct{})}
	cmd.Stderr = &w.stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, nil, err
	}
	w.in = in
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		cancel()
		return nil, nil, err
	}
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		cancel()
		return nil, nil, err
	}
	go func() { w.exit = cmd.Wait(); close(w.done) }()
	return w, out, nil
}

func (w *workerProcess) failure(phase string, cause error) error {
	if w == nil {
		return fmt.Errorf("Dart analyzer %s: %w", phase, cause)
	}
	if err := w.ctx.Err(); err != nil {
		return fmt.Errorf("Dart analyzer %s: %w", phase, err)
	}
	select {
	case <-w.done:
		return fmt.Errorf("Dart analyzer %s: %w; process exit: %v; stderr: %s", phase, cause, w.exit, w.stderr.text())
	case <-time.After(100 * time.Millisecond):
		return fmt.Errorf("Dart analyzer %s: %w; stderr: %s", phase, cause, w.stderr.text())
	}
}

func (w *workerProcess) stopped(phase string) error {
	if w == nil {
		return nil
	}
	select {
	case <-w.done:
		return w.failure(phase, fmt.Errorf("worker terminated"))
	default:
		return nil
	}
}

func (w *workerProcess) close() {
	if w == nil {
		return
	}
	w.once.Do(func() {
		_ = w.in.Close()
		select {
		case <-w.done:
		case <-time.After(250 * time.Millisecond):
			w.cancel()
			<-w.done
		}
		w.cancel()
	})
}
