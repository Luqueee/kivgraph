//go:build unix

package dartloader

import (
	"os"
	"os/exec"
	"strconv"
)

// guardOrphan makes the analyzer die with this process, and returns what to
// call once the analyzer has been reaped.
//
// The Dart analysis server does not exit when its standard input closes, so the
// only thing that ever stopped one was the Kill in workerProcess.close. A
// process that never gets there -- killed by a timeout, by a harness, by the
// test runner -- left its analyzer running for good, at a quarter of a gigabyte
// each once it had analysed anything.
//
// Nothing portable ties a child to its parent's death: Linux has a signal for
// it and macOS has not. A pipe does it everywhere. The watcher blocks reading
// one whose only writer is this process, and the kernel closes that end
// whichever way this process ends.
//
// It is a guard and not a dependency: without a shell to run it the analyzer
// starts as it always did, and the leak this closes is back.
func guardOrphan(pid int) (release func()) {
	read, write, err := os.Pipe()
	if err != nil {
		return func() {}
	}
	watcher := exec.Command("/bin/sh", "-c", `read _; kill -9 "$0"`, strconv.Itoa(pid))
	watcher.Stdin = read
	err = watcher.Start()
	_ = read.Close()
	if err != nil {
		_ = write.Close()
		return func() {}
	}
	return func() {
		// Killed before the pipe closes: a watcher that saw the end of the
		// pipe first would signal a process id that is no longer the analyzer's.
		_ = watcher.Process.Kill()
		_ = watcher.Wait()
		_ = write.Close()
	}
}
