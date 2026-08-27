//go:build unix

package main

import (
	"os"
	"syscall"
	"testing"
)

// Signal 0 asks whether a process can be signalled without sending anything.
//
// The question exists only where signals do. os.Process.Signal answers "not
// supported by windows" for everything but Kill, so there is no probe of this
// shape to write there -- and nothing asks for one: `stop` decides whether a
// pid is still the invocation it read by looking it up in the process table
// again, which is a stronger check anyway, because a pid can be reachable and
// belong to something else.
func TestSignalProcessReachesALiveProcess(t *testing.T) {
	if err := signalProcess(os.Getpid(), syscall.Signal(0)); err != nil {
		t.Fatalf("signalProcess(self, 0) error = %v, want the running test to be reachable", err)
	}
}
