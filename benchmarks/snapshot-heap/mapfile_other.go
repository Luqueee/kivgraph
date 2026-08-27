//go:build !unix

package main

import (
	"errors"
	"fmt"
	"os"
)

// mapSnapshotFile reads the published snapshot, because this platform has no
// mmap here -- the same trade internal/rebuild makes off Unix.
//
// The number this benchmark reports is then not comparable with one from a
// platform that mapped it: a read allocates the whole file, which is exactly
// the cost the mapping avoids. It builds and runs so the package is not a hole
// in a cross-platform build, not so the two results can be put side by side.
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
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read the published snapshot: %w", err)
	}
	return data, func() {}, nil
}
