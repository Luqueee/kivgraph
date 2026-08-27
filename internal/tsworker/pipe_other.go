//go:build !windows

package tsworker

import "os"

// interruptibleOutputPipe returns the parent's read end of the worker's stdout
// and the end the child writes to.
//
// An ordinary pipe is already pollable here, so the read deadline that lets a
// blocked ReadFrame be cancelled works on it without anything further.
func interruptibleOutputPipe() (parent, child *os.File, err error) {
	return os.Pipe()
}
