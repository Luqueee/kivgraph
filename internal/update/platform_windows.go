//go:build windows

package update

import "os"

// archiveExtension is the container the release publishes for this platform.
// Windows gets a zip because that is what its own tools read: a reader who
// downloads the release opens it with Explorer or Expand-Archive, and neither
// of those reads a gzipped tarball. See ADR 0079.
const archiveExtension = ".zip"

// removeReplacedBundle deletes the bundle an update replaced, and tolerates
// not being able to.
//
// That directory holds the binary running this code. Windows keeps an
// executing image open and refuses to unlink it, so the removal fails on the
// one file the bundle is guaranteed to contain -- while the update itself has
// already succeeded, because the new bundle is in place under the real name
// and the next start runs it.
//
// Reporting that as a failed update would report the opposite of what
// happened, and would do it at the point where a reader is least able to check:
// after the replacement, with both trees on disk. The directory is left behind
// instead, and replaceBundle clears a stale backup before it starts rather
// than refusing on one -- by then the process that held it has exited.
func removeReplacedBundle(root string) error {
	// Best effort. A second attempt fails for the same reason as the first,
	// and the caller has nothing useful to do with the error.
	_ = os.RemoveAll(root)
	return nil
}
