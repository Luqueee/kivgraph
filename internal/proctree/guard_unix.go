//go:build unix

package proctree

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// Contain makes the command the leader of a process group of its own, so that
// what it starts can be found again, and makes cancelling its context kill the
// group rather than the one process. It is called before Start, on a command
// made with exec.CommandContext.
func Contain(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killTree(cmd) }
	cmd.WaitDelay = waitDelay
}

func contained(cmd *exec.Cmd) bool {
	return cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid
}

// killTree kills the command's group when it leads one and the command alone
// when it does not. A group that is already empty is not an error.
func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if !contained(cmd) {
		return cmd.Process.Kill()
	}
	// The leader's process id is the group's, and stays the group's for as
	// long as any member is alive, whether or not the leader still is.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return err
	}
	return nil
}

// Guard makes a started command die with this process, and returns what to
// call once the command has been waited for. A contained command takes its
// whole group with it.
//
// The Kill a caller runs when it is done only happens if the caller gets that
// far. A process ended by a timeout, by a harness or by the test runner never
// does, and what it started kept running: an analysis server that does not exit
// when its input closes, an indexer three processes deep building a solution
// nobody was waiting for any more.
//
// Nothing portable ties a child to its parent's death: Linux has a signal for
// it and macOS has not. A pipe does it everywhere. The watcher blocks reading
// one whose only writer is this process, and the kernel closes that end
// whichever way this process ends.
//
// It is a guard and not a dependency: without a shell to run it the command
// runs as it always did, and the leak this closes is back.
func Guard(cmd *exec.Cmd) (release func()) {
	if cmd.Process == nil {
		return func() {}
	}
	target := strconv.Itoa(cmd.Process.Pid)
	if contained(cmd) {
		target = "-" + target
	}
	read, write, err := os.Pipe()
	if err != nil {
		return func() {}
	}
	watcher := exec.Command("/bin/sh", "-c", `read _; kill -KILL -- "$0"`, target)
	watcher.Stdin = read
	err = watcher.Start()
	_ = read.Close()
	if err != nil {
		_ = write.Close()
		return func() {}
	}
	return func() {
		// Killed before the pipe closes: a watcher that saw the end of the
		// pipe first would signal an id that is no longer this command's.
		_ = watcher.Process.Kill()
		_ = watcher.Wait()
		_ = write.Close()
	}
}
