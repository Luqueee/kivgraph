package workspace

import (
	"errors"
	"io/fs"
	"os"
)

// visitUnlessVanished calls visit and treats a file that no longer exists as
// one that was never listed, which is what the walks already do for a file that
// vanishes before they inspect it.
//
// The inspection cannot be relied on to see the removal: on Windows the
// DirEntry a listing returns carries the data the listing read, so Info answers
// without asking the filesystem again, and a file deleted after the listing
// reaches visit. The error is only believed when the path is gone: a manifest
// that is there and names something missing is still a failure.
func visitUnlessVanished(path string, visit func(string) error) error {
	err := visit(path)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if _, statErr := os.Lstat(path); errors.Is(statErr, fs.ErrNotExist) {
		return nil
	}
	return err
}
