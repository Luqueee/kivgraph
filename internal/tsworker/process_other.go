//go:build !unix

package tsworker

import (
	"os"
	"os/exec"
)

// isolateProcess is a no-op where process groups are not available. The worker
// is still terminated, but a native server it spawned may outlive it.
func isolateProcess(*exec.Cmd) {}

// interruptTree asks the worker to terminate.
func interruptTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(os.Interrupt)
}

// killTree terminates the worker without giving it a choice.
func killTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
