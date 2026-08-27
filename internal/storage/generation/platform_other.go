//go:build !linux && !darwin && !windows

package generation

import (
	"errors"
	"os"
)

func filesystemCapacity(string) (uint64, uint64, error) {
	return 0, 0, errors.New("generation filesystem capacity is unsupported on this platform")
}

func preallocate(path string, size int64) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
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
	buffer := make([]byte, 1<<20)
	for remaining := size; remaining > 0; {
		chunk := int64(len(buffer))
		if remaining < chunk {
			chunk = remaining
		}
		if _, err := file.Write(buffer[:chunk]); err != nil {
			return err
		}
		remaining -= chunk
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
