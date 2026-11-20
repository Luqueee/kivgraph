//go:build !linux

package indexing

// cgroupMemoryLimit is a limit only Linux containers have. Elsewhere a pass is
// not told one, which is how it ran before there was a limit to read.
func cgroupMemoryLimit() (int64, bool) {
	return 0, false
}
