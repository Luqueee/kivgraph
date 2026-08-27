//go:build !unix && !windows

package generation

import "errors"

// publishLock has no primitive to stand on here.
//
// Every other platform-specific file in this package answers with a slower
// implementation of the same contract; this one cannot, because the contract
// is exclusion between processes and there is nothing to exclude with. So it
// refuses instead of pretending: a publication that ran without the lock would
// leave the store's candidate directories indistinguishable from debris, and
// nothing downstream would report it.
type publishLock struct{}

func acquirePublishLock(string) (*publishLock, error) {
	return nil, errors.New("publishing a generation is unsupported on this " +
		"platform: it has no cross-process file lock")
}

func (lock *publishLock) release() error { return nil }
