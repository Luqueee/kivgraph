//go:build windows

package generation

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// filesystemCapacity reports what the volume holding path has, and what this
// caller may still use of it.
//
// The second number is deliberately the caller's and not the volume's.
// GetDiskFreeSpaceEx returns both, and they differ wherever a disk quota
// applies: the store is deciding whether its own next generation will fit, so
// space that exists and that it may not have is not space. That is the same
// distinction statfs draws between f_bfree and f_bavail, and the Unix files
// here read f_bavail for the same reason.
func filesystemCapacity(path string) (total, available uint64, err error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, fmt.Errorf("stat filesystem: %w", err)
	}
	var availableToCaller, totalBytes, freeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(pointer, &availableToCaller, &totalBytes, &freeBytes); err != nil {
		return 0, 0, fmt.Errorf("stat filesystem: %w", err)
	}
	return totalBytes, availableToCaller, nil
}

// preallocate claims the space a publication will need before it needs it.
//
// Truncate is the whole of it here, and it is not the weaker thing it looks
// like. It resolves to SetEndOfFile, and NTFS allocates the clusters of a file
// that has not been marked sparse -- so the extent is really taken, which is
// the property the reserve exists for, and no byte of it has to be written to
// take it. That is what fallocate buys on Linux.
//
// The generic fallback this replaces did write them, a megabyte at a time.
// It was correct and it cost the size of the reserve in I/O on every publish.
func preallocate(path string, size int64) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if size > 0 {
		if err := file.Truncate(size); err != nil {
			return err
		}
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}
