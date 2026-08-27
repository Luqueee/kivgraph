//go:build !unix && !windows

package indexing

import "errors"

// writerLock has no primitive to stand on here.
//
// The two answers this function can give -- "you hold it" and "somebody else
// is rebuilding" -- are both claims about other processes, and without a
// cross-process lock neither can be made. Reporting the second one would be
// the worse lie: it reads as a healthy machine already doing the work, and the
// rebuild would never happen. So it refuses and names why.
type writerLock struct{}

func acquireWriterLock(string) (*writerLock, bool, error) {
	return nil, false, errors.New("rebuilding the graph is unsupported on this " +
		"platform: it has no cross-process file lock")
}

func (lock *writerLock) release() error { return nil }
