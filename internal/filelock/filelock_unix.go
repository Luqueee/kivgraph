//go:build unix

package filelock

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Lock is one process's exclusive claim on a path.
type Lock struct {
	file *os.File
}

// Acquire takes the lock without waiting. A false return is not an error: it
// means another process holds it, which is the answer a caller wants rather
// than a delay it did not ask for.
func Acquire(path string) (*Lock, bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, fmt.Errorf("create lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open lock file %q: %w", path, err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		closeErr := file.Close()
		if err == unix.EWOULDBLOCK {
			if closeErr != nil {
				return nil, false, fmt.Errorf("close lock file %q: %w", path, closeErr)
			}
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("lock %q: %w", path, err)
	}
	return &Lock{file: file}, true, nil
}

func (lock *Lock) Release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	// Closing the descriptor drops the flock; unlocking first keeps the
	// failure attributable when the close is what went wrong.
	unlockErr := unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	closeErr := lock.file.Close()
	lock.file = nil
	if unlockErr != nil {
		return fmt.Errorf("unlock: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close: %w", closeErr)
	}
	return nil
}
