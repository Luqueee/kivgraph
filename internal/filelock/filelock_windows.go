//go:build windows

package filelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Lock is one process's exclusive claim on a path.
//
// This is the Windows counterpart of the flock in filelock_unix.go. What
// carries over is the whole reason the lock exists: LockFileEx excludes other
// processes, and Windows drops it when the holder's last handle closes, so a
// crashed holder does not leave the path locked forever. That is the property
// the package doc says a pid file cannot offer, and it is why this is written
// against the platform's own primitive instead of being stubbed out.
type Lock struct {
	file *os.File
}

// lockedRegionBytes is the range this lock takes.
//
// LockFileEx locks a range of a file rather than the file, so two processes
// exclude each other only if they name the same one. The lock file stays
// empty, which makes the choice nominal -- one byte at offset zero -- but
// locking past the end of a file is legal exactly so that this works.
const lockedRegionBytes = 1

// Acquire takes the lock without waiting. A false return is not an error: it
// means another process holds it.
func Acquire(path string) (*Lock, bool, error) {
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
		// violation instead of waiting for it, so this is the "somebody else
		// has it" answer and not a failure to lock.
		if errors.Is(lockErr, windows.ERROR_LOCK_VIOLATION) {
			if closeErr != nil {
				return nil, false, fmt.Errorf("close lock file %q: %w", path, closeErr)
			}
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("lock %q: %w", path, lockErr)
	}
	return &Lock{file: file}, true, nil
}

func (lock *Lock) Release() error {
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
