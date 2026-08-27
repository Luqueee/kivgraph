//go:build unix

package durable

import (
	"errors"
	"os"
)

// Directory flushes the directory entry a rename or a create just changed.
//
// Writing a file durably is two operations on POSIX, not one: the file's own
// fsync persists its contents, and the parent directory's persists the name
// that reaches them. A crash between the two leaves a file that is complete
// and unreachable, which is precisely the state the generation store spends
// its rollback paths avoiding.
func Directory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	return errors.Join(syncErr, closeErr)
}
