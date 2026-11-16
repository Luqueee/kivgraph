//go:build darwin

package sourceobservation

import (
	"io/fs"
	"syscall"
)

// statChange reads the inode change time, the inode and the mode from the stat
// an entry was read with.
func statChange(info fs.FileInfo) (changeTime int64, inode uint64, mode uint32, ok bool) {
	raw, isStat := info.Sys().(*syscall.Stat_t)
	if !isStat {
		return 0, 0, 0, false
	}
	return raw.Ctimespec.Nano(), raw.Ino, uint32(raw.Mode), true
}
