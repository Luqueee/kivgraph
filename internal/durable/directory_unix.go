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
	// The name is checked on both platforms or on neither. Off Unix this has
	// to look, because there is no directory flush to attempt and a caller
	// naming a file would otherwise be told its write was made durable by a
	// call that did nothing. Here the flush would succeed on a regular file
	// and mean something else, which is the same lie one layer down.
	info, statErr := directory.Stat()
	if statErr != nil {
		return errors.Join(statErr, directory.Close())
	}
	if !info.IsDir() {
		return errors.Join(&os.PathError{Op: "syncdir", Path: path, Err: os.ErrInvalid}, directory.Close())
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	return errors.Join(syncErr, closeErr)
}
