//go:build unix

package tsworker

import (
	"os/exec"
	"syscall"
)

// isolateProcess puts the worker in its own process group. The worker spawns
// the native TypeScript server, so signalling the group is the only way to
// avoid leaving that child behind.
func isolateProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// interruptTree asks the worker group to terminate.
func interruptTree(cmd *exec.Cmd) error {
	return signalTree(cmd, syscall.SIGTERM)
}

// killTree terminates the worker group without giving it a choice.
func killTree(cmd *exec.Cmd) error {
	return signalTree(cmd, syscall.SIGKILL)
}

func signalTree(cmd *exec.Cmd, signal syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// A group id of 0 or 1 would signal this process or init; fall back to the
	// single child instead.
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil && pgid > 1 {
		return syscall.Kill(-pgid, signal)
	}
	return cmd.Process.Signal(signal)
}
