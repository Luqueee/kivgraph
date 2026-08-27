//go:build !unix

package durable

import "os"

// Directory reports that the entry is as durable as this platform will make
// it, having done nothing to it.
//
// This is a declared limitation and not a silence. Windows has no fsync for a
// directory at all -- FlushFileBuffers on a directory handle answers
// ERROR_ACCESS_DENIED, so the operation cannot be attempted, let alone
// skipped for cost. What it offers instead is narrower: NTFS journals the
// metadata, and MoveFileEx can be asked for write-through on one rename, which
// is not the same guarantee and is not reachable through os.Rename.
//
// So a caller here gets the file flush and not the directory flush, and the
// window this leaves -- a crash after the contents are persisted but before
// the name that reaches them is -- stays open. Returning an error instead
// would be worse: it would refuse to write anything at all on a platform
// where the write mostly survives, and every caller would have to special-case
// the refusal. The path is still checked, so a caller that names a directory
// that is not there is still told.
func Directory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return &os.PathError{Op: "syncdir", Path: path, Err: os.ErrInvalid}
	}
	return nil
}
