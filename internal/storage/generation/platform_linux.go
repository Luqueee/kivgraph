//go:build linux

package generation

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func filesystemCapacity(path string) (total, available uint64, err error) {
	var status syscall.Statfs_t
	if err := syscall.Statfs(path, &status); err != nil {
		return 0, 0, fmt.Errorf("stat filesystem: %w", err)
	}
	blockSize := uint64(status.Bsize)
	return uint64(status.Blocks) * blockSize, uint64(status.Bavail) * blockSize, nil
}

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
		// A filesystem that cannot preallocate still has to hold the reserve,
		// so it gets the same bytes written one by one. Running out of space is
		// not that case and stays the error it is.
		if err := syscall.Fallocate(int(file.Fd()), 0, 0, size); err != nil {
			if !unsupportedFallocate(err) {
				return err
			}
			if err := writeZeros(file, size); err != nil {
				return err
			}
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

// unsupportedFallocate is the kernel saying this filesystem cannot preallocate,
// as opposed to saying it has no room: NFS before 4.2, 9p and most FUSE
// filesystems answer EOPNOTSUPP, and a kernel without the call answers ENOSYS.
func unsupportedFallocate(err error) bool {
	return errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, syscall.ENOSYS)
}
