//go:build !unix

package proctree

import "os/exec"

// Contain only bounds the wait here. Killing what a command started, and tying
// it to this process's life, are declared limitations on this platform: the
// unix implementation needs process groups and a shell, and the equivalent
// here is a job object.
func Contain(cmd *exec.Cmd) { cmd.WaitDelay = waitDelay }

func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// Guard does nothing here; see Contain.
func Guard(*exec.Cmd) (release func()) { return func() {} }
