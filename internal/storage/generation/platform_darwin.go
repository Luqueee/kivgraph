//go:build darwin

package generation

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func filesystemCapacity(path string) (total, available uint64, err error) {
	var status syscall.Statfs_t
	if err := syscall.Statfs(path, &status); err != nil {
		return 0, 0, fmt.Errorf("stat filesystem: %w", err)
	}
	blockSize := uint64(status.Bsize)
	return status.Blocks * blockSize, status.Bavail * blockSize, nil
}

// preallocate reserves size bytes for path. APFS honours ftruncate with a
// sparse file, which reserves nothing, so the space is claimed with
// F_PREALLOCATE before the file is grown. F_ALLOCATEALL fails instead of
// reserving a fragment: a partial reserve is not a reserve.
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
		store := unix.Fstore_t{
			Flags:   unix.F_ALLOCATEALL,
			Posmode: unix.F_PEOFPOSMODE,
			Offset:  0,
			Length:  size,
		}
		if err := unix.FcntlFstore(file.Fd(), unix.F_PREALLOCATE, &store); err != nil {
			return fmt.Errorf("preallocate %d bytes: %w", size, err)
		}
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
