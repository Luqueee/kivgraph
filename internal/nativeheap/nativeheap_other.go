//go:build !(linux && cgo)

package nativeheap

// Return reports that this build has no native allocator to hand anything back
// to, which is not the same as having handed nothing back.
//
// The knob is glibc's. macOS allocates through its own zone allocator, whose
// free memory is returned by different means and which has no per-thread arenas
// to walk, and a build without cgo has no native engine allocating anything in
// the first place. Neither case is a failure and neither gets a fabricated
// success.
func Return() bool {
	return false
}
