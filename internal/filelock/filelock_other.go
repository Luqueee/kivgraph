//go:build !unix && !windows

package filelock

import "errors"

// Lock has no primitive to stand on here.
//
// The two answers Acquire can give -- "you hold it" and "somebody else does"
// -- are both claims about other processes, and without a cross-process lock
// neither can be made. Reporting the second would be the worse lie: it reads
// as a healthy machine already doing the work, so the work would never happen.
// It refuses and names why, and each caller decides what that means for it.
type Lock struct{}

// Acquire refuses on a platform with no cross-process file lock.
func Acquire(string) (*Lock, bool, error) {
	return nil, false, errors.New("this platform has no cross-process file lock")
}

// Release is a no-op: nothing was ever taken.
func (lock *Lock) Release() error { return nil }
