//go:build unix

package indexing

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// writerLock is the exclusive right to rebuild the graph of one state
// directory.
//
// It is an advisory lock on a file, not a daemon electing a leader: the
// publisher/follower split already exists, so whoever holds this rebuilds and
// everyone else notices through the CURRENT pointer they were following
// anyway. The kernel releases it if the holder dies, which is the property a
// pid file cannot offer.
type writerLock struct {
	file *os.File
}

// acquireWriterLock takes the lock without waiting. A false return is not an
// error: it means another process is already rebuilding the same graph.
func acquireWriterLock(path string) (*writerLock, bool, error) {
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
	return &writerLock{file: file}, true, nil
}

func (lock *writerLock) release() error {
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
