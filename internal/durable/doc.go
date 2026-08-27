// Package durable flushes a write that has already returned.
//
// Three packages needed the same two operations -- flush a file the process is
// no longer holding open, and flush the directory a rename or a create just
// changed -- and each had written its own copy of them. The copies agreed,
// which is what made them dangerous: both operations rest on POSIX semantics
// that Windows does not share, so the same defect had to be found and fixed
// three times, and the reason for the difference had nowhere to live.
//
// It lives here. What a caller gets is the strongest flush the platform can
// give for that path, and a comment saying where that is weaker than a
// POSIX fsync.
package durable
