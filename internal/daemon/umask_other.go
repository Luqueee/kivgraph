//go:build !unix

package daemon

// withPrivateUmask runs body without narrowing what it creates.
//
// This is a declared limitation, not a silence: the distribution targets
// linux/amd64 and darwin/arm64, both unix, and a platform outside them gets a
// socket with whatever mode its filesystem gives. It is not reported as an error
// because the file this guards is created by the body either way -- returning one
// would refuse to start a daemon over a permission bit rather than say so.
func withPrivateUmask(body func() error) error { return body() }
