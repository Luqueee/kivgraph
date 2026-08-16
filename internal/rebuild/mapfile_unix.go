//go:build unix

package rebuild

import (
	"fmt"
	"os"
	"syscall"
)

// mapFile maps a file read-only and returns the bytes with the call that
// releases them.
//
// It replaces reading the file into the heap, which for a published snapshot is
// a 73 MB allocation that exists only to be decoded and thrown away. The
// mapping is MAP_SHARED, so two processes reading the same generation read the
// same physical pages while the decode runs -- the beginning of what phase 2b of
// ADR 0045 would keep instead of copying.
//
// Nothing may outlive release. That is safe here for a reason that has to stay
// true: every decoder in the snapshot format copies -- a record into a struct, a
// string through a conversion that allocates -- so the snapshot a reader ends up
// with shares nothing with these bytes. A decoder that started handing out a
// view into them would turn this into a use-after-free that answers queries.
func mapFile(path string) ([]byte, func(), error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("stat %s: %w", path, err)
	}
	size := info.Size()
	if size <= 0 {
		return nil, nil, fmt.Errorf("%s holds no bytes", path)
	}
	if int64(int(size)) != size {
		return nil, nil, fmt.Errorf("%s is %d bytes, too large to map on this platform", path, size)
	}
	data, err := syscall.Mmap(int(file.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, nil, fmt.Errorf("map %s: %w", path, err)
	}
	release := func() {
		if data != nil {
			_ = syscall.Munmap(data)
			data = nil
		}
	}
	return data, release, nil
}
