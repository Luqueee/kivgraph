//go:build windows

package generation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// publishLock is the exclusive right to publish into one generation store.
//
// This is the Windows counterpart of the flock in publish_lock_unix.go, and it
// is an implementation rather than a fallback because the two properties the
// store leans on both survive the change of primitive: LockFileEx is exclusive
// across processes, and Windows releases the lock when the last handle on the
// file closes, which it does when a holder dies. That second one is what makes
// a candidate directory left behind safe to treat as debris. A stub that
// always succeeded would keep the code compiling and turn two concurrent
// publications into corruption that only appears under load.
type publishLock struct {
	file *os.File
}

// acquirePublishLock takes the lock without waiting. A rebuild takes minutes,
// so blocking on one would look like a hang; a caller that loses gets
// ErrPublishInProgress and can say so.
func acquirePublishLock(path string) (*publishLock, error) {
	file, err := openLockFile(path, "publish lock")
	if err != nil {
		return nil, err
	}
	if err := lockRegion(file); err != nil {
		closeErr := file.Close()
		// LOCKFILE_FAIL_IMMEDIATELY reports a range somebody else holds as a
		// violation instead of waiting for it, so this is contention and not a
		// failure to lock.
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
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
	// Closing the handle drops the lock; unlocking first keeps the failure
	// attributable when the close is what went wrong.
	unlockErr := unlockRegion(lock.file)
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

// lockedRegionBytes is the range every lock in this package takes.
//
// LockFileEx locks a range of a file rather than the file, so two processes
// exclude each other only if they name the same one. The lock file stays
// empty, which makes the choice nominal -- one byte at offset zero, asked for
// by every holder and read by none -- but it has to be stated somewhere, and a
// range that is locked past the end of the file is legal precisely so that
// this works.
const lockedRegionBytes = 1

func openLockFile(path string, what string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create %s directory: %w", what, err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open %s %q: %w", what, path, err)
	}
	return file, nil
}

func lockRegion(file *os.File) error {
	return windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, lockedRegionBytes, 0, new(windows.Overlapped),
	)
}

func unlockRegion(file *os.File) error {
	return windows.UnlockFileEx(
		windows.Handle(file.Fd()),
		0, lockedRegionBytes, 0, new(windows.Overlapped),
	)
}
