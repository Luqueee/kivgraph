//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// mapSnapshotFile maps the published snapshot read-only.
//
// The mapping is the measurement here: this benchmark exists to report what
// the snapshot costs when it is mapped rather than read, so on the platforms
// that can map it, it maps it.
func mapSnapshotFile(path string) ([]byte, func(), error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open the published snapshot: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("stat the published snapshot: %w", err)
	}
	if info.Size() == 0 {
		return nil, nil, errors.New("the published snapshot is empty")
	}
	data, err := syscall.Mmap(int(file.Fd()), 0, int(info.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, nil, fmt.Errorf("map the published snapshot: %w", err)
	}
	return data, func() { _ = syscall.Munmap(data) }, nil
}
