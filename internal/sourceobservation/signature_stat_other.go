//go:build !darwin && !linux

package sourceobservation

import "io/fs"

// statChange has no inode change time to read on this platform, so a signature
// never vouches for a tree here and every verification reads it, as it always did.
func statChange(fs.FileInfo) (changeTime int64, inode uint64, mode uint32, ok bool) {
	return 0, 0, 0, false
}
