//go:build unix

package generation

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// publishLock is the exclusive right to publish into one generation store.
//
// The store's mutex only orders the goroutines of one process, and a store is
// shared by every Ladygraph invocation over the same state directory: an
// `index --full` in a terminal, an `index_project` from a client, and the
// resynchroniser inside a running server all publish into it. The kernel
// releases this lock if its holder dies, which is the property that makes it
// safe to treat a candidate directory left behind as debris.
type publishLock struct {
	file *os.File
}

// acquirePublishLock takes the lock without waiting. A rebuild takes minutes,
// so blocking on one would look like a hang; a caller that loses gets
// ErrPublishInProgress and can say so.
func acquirePublishLock(path string) (*publishLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create publish lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open publish lock %q: %w", path, err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		closeErr := file.Close()
		if err == unix.EWOULDBLOCK {
			if closeErr != nil {
				return nil, fmt.Errorf("close publish lock %q: %w", path, closeErr)
			}
			return nil, ErrPublishInProgress
		}
		return nil, fmt.Errorf("lock %q: %w", path, err)
	}
	return &publishLock{file: file}, nil
}

func (lock *publishLock) release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	// Closing the descriptor drops the flock; unlocking first keeps the
	// failure attributable when the close is what went wrong.
	unlockErr := unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	closeErr := lock.file.Close()
	lock.file = nil
	if unlockErr != nil {
		return fmt.Errorf("unlock publish lock: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close publish lock: %w", closeErr)
	}
	return nil
}
