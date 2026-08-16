//go:build !unix

package rebuild

import "os"

// mapFile reads the file, because this platform has no mmap here.
//
// The contract is the same either way -- the bytes are valid until release is
// called -- so a caller cannot tell the two apart, and neither has to know which
// one it got. What differs is the cost: a read allocates the whole file.
func mapFile(path string) ([]byte, func(), error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return data, func() {}, nil
}
