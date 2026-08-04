//go:build linux

package generation

import (
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
		if err := syscall.Fallocate(int(file.Fd()), 0, 0, size); err != nil {
			return err
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
