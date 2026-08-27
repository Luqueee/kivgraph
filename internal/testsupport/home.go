package testsupport

import (
	"runtime"
	"testing"
)

// SetHome points os.UserHomeDir at directory for the duration of a test.
//
// It exists because "the home directory" is not one environment variable.
// os.UserHomeDir reads HOME on Unix and USERPROFILE on Windows, so a test that
// sets only HOME redirects nothing there: it runs against the real profile of
// whoever is running it, and then fails by reporting that directory instead of
// the one it prepared. Twenty-odd tests in this repository set only HOME, and
// all of them read as bugs in the code under test when they fail.
//
// Both are set on both platforms. The one the running platform does not
// consult costs nothing, and setting it keeps a test that shells out to a tool
// with its own idea of home from disagreeing with the process that spawned it.
func SetHome(t testing.TB, directory string) {
	t.Helper()
	t.Setenv("HOME", directory)
	t.Setenv("USERPROFILE", directory)
	if runtime.GOOS == "windows" {
		// LocalAppData is where os.UserCacheDir looks, and a test that
		// redirects home without it leaves the cache pointing at the real
		// profile -- which is the same defect one directory over.
		t.Setenv("LocalAppData", directory)
		// APPDATA is where a client keeps its configuration, so a test that
		// redirects home and leaves this pointing at the real profile writes
		// into the machine it is running on.
		t.Setenv("APPDATA", directory)
	}
}
