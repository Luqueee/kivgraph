package durable

import (
	"errors"
	"os"
)

// File flushes a file the caller is no longer holding open.
//
// It opens for reading *and* writing even though it writes nothing. On POSIX
// an fsync is legal on a descriptor opened read-only, and every version of
// this that reached for os.Open worked there; Windows resolves the same call
// to FlushFileBuffers, which needs a handle with write access and answers
// ERROR_ACCESS_DENIED without one. Asking for the access the flush needs costs
// nothing on the platform that did not require it.
//
// The consequence for a caller is that a file it cannot write, it cannot
// flush, which is honest: a read-only file has nothing of the caller's left in
// the operating system's buffers to lose.
func File(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(syncErr, closeErr)
}
