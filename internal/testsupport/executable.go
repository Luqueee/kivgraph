package testsupport

import (
	"path/filepath"
	"runtime"
)

// InstalledExecutable is a plausible absolute path to an installed kivgraph,
// in the shape the running platform uses.
//
// The fixtures that need one were written as "/opt/kivgraph/bin/kivgraph",
// which is not an absolute path on Windows and is not what a program is called
// there. integrations.New absolutises what it is handed -- correctly -- so the
// manager held "C:\opt\kivgraph\bin\kivgraph" while the assertion beside it
// still held the Unix spelling, and the test failed for the fixture rather
// than for the code.
//
// A fixture demonstrates the real case or it demonstrates nothing.
func InstalledExecutable() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(`C:\`, "opt", "kivgraph", "bin", "kivgraph.exe")
	}
	return filepath.Join("/", "opt", "kivgraph", "bin", "kivgraph")
}

// MovedExecutable is a second plausible absolute path to an installed
// kivgraph, different from InstalledExecutable and in the same platform shape.
//
// The fixtures that need one are testing that a client configuration naming a
// kivgraph which has since moved is replaced rather than appended beside. That
// needs two paths, and the second was written as "/usr/local/bin/kivgraph" --
// which on Windows is not absolute, so it absolutised to
// "C:\usr\local\bin\kivgraph" while the assertion still held the Unix
// spelling. Same failure as above, one fixture further along.
func MovedExecutable() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(`C:\`, "Program Files", "kivgraph", "bin", "kivgraph.exe")
	}
	return filepath.Join("/", "usr", "local", "bin", "kivgraph")
}
