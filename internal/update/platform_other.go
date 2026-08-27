//go:build !windows

package update

import "os"

// archiveExtension is the container the release publishes for this platform.
const archiveExtension = ".tar.gz"

// removeReplacedBundle deletes the bundle an update replaced.
//
// The old binary may still be running out of it, and the directory goes
// anyway: unlinking a file here detaches the name and the running image keeps
// the inode until it exits. So a failure means something is genuinely wrong
// and is reported.
func removeReplacedBundle(root string) error {
	return os.RemoveAll(root)
}
