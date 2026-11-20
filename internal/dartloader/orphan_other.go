//go:build !unix

package dartloader

// guardOrphan is a declared limitation here: an analyzer whose parent is
// killed before it closes the worker is left running. The unix watcher needs a
// shell and a signal, and this platform's equivalent is a job object.
func guardOrphan(int) (release func()) { return func() {} }
