//go:build unix

package testsupport

import (
	"os"
	"syscall"
)

// ProcessAlive reports whether a process exists and this one may signal it.
//
// Signal 0 is the question without the request: the kernel performs every
// check it would for a real signal and delivers nothing.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
