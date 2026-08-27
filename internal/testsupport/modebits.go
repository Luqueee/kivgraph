package testsupport

import (
	"runtime"
	"testing"
)

// SkipWithoutModeBits skips a test whose subject is a POSIX file permission.
//
// It is a skip and not a relaxed assertion because the two are not the same
// claim. Windows keeps an access control list, and Go reports every regular
// file there as 0666 and every directory as 0777 regardless of it -- so a test
// that widened what it accepts would pass on a machine where the file really
// is readable by everyone, and report that as the privacy it was written to
// check. Nothing here sets an ACL yet; when something does, these tests get a
// sibling that asserts it, not a looser version of themselves.
func SkipWithoutModeBits(t testing.TB) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("this platform keeps an ACL rather than POSIX mode bits; " +
			"see docs/development/windows.md")
	}
}

// ModeBitsHonoured reports whether this platform stores the permission bits Go
// reports, so a test that checks a mode as one claim among several can drop
// that one claim and keep the rest instead of skipping whole.
func ModeBitsHonoured() bool { return runtime.GOOS != "windows" }
