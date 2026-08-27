//go:build windows

package indexing

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// writerLock is the exclusive right to rebuild the graph of one state
// directory.
//
// This is the Windows counterpart of the flock in resync_lock_unix.go. What
// carries over is the whole reason the lock exists: LockFileEx excludes other
// processes, and Windows drops the lock when the holder's last handle closes,
// so a crashed rebuild does not leave the graph locked forever. That is the
// property the comment on the unix side says a pid file cannot offer, and it
// is why this is written against the platform's own primitive instead of being
// stubbed out.
type writerLock struct {
	file *os.File
}

// lockedRegionBytes is the range this lock takes.
//
// LockFileEx locks a range of a file rather than the file, so two processes
// exclude each other only if they name the same one. The lock file stays
// empty, which makes the choice nominal -- one byte at offset zero -- but
// locking past the end of a file is legal exactly so that this works.
const lockedRegionBytes = 1

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
	lockErr := windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, lockedRegionBytes, 0, new(windows.Overlapped),
	)
	if lockErr != nil {
		closeErr := file.Close()
		// LOCKFILE_FAIL_IMMEDIATELY reports a range somebody else holds as a
		// violation instead of waiting for it, so this is the "already
		// rebuilding" answer and not a failure to lock.
		if errors.Is(lockErr, windows.ERROR_LOCK_VIOLATION) {
			if closeErr != nil {
				return nil, false, fmt.Errorf("close lock file %q: %w", path, closeErr)
			}
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("lock %q: %w", path, lockErr)
	}
	return &writerLock{file: file}, true, nil
}

func (lock *writerLock) release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	// Closing the handle drops the lock; unlocking first keeps the failure
	// attributable when the close is what went wrong.
	unlockErr := windows.UnlockFileEx(
		windows.Handle(lock.file.Fd()),
		0, lockedRegionBytes, 0, new(windows.Overlapped),
	)
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
