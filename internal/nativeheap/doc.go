// Package nativeheap returns the memory the native engine borrowed.
//
// It is not the Go heap. LadybugDB is C++ and allocates through libc, so
// nothing in runtime/debug can see that memory or hand it back, and a snapshot
// build borrows most of its bytes there.
//
// The documentation lives in a file of its own because the package has one
// exported function and two implementations of it, split on whether the build
// has the allocator whose knob it turns. Kept beside either one, the package's
// documentation disappears wherever that build constraint excludes it -- which
// is what a windows/amd64 build found, and what a reader on any platform other
// than Linux would have found first.
package nativeheap
