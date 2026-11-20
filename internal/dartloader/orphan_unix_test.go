//go:build unix

package dartloader

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const orphanHelperEnv = "KIVGRAPH_DART_ORPHAN_HELPER"

// TestOrphanHelperProcess is the process that gets killed. It launches a worker
// that, like the analysis server, outlives the end of its standard input, says
// which process that is, and never closes it.
func TestOrphanHelperProcess(t *testing.T) {
	if os.Getenv(orphanHelperEnv) == "" {
		t.Skip("only runs as the child of TestAnAnalyzerDiesWithTheProcessThatStartedIt")
	}
	worker, _, err := launchWorker(context.Background(), "/bin/sleep", []string{"300"}, os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("worker=%d\n", worker.cmd.Process.Pid)
	time.Sleep(5 * time.Minute)
}

func alive(pid int) bool {
	// A zombie still answers signal 0, and the worker here is reparented to
	// init, which reaps it; ESRCH is the process being gone.
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// A process killed outright runs no deferred close, and that is how analyzers
// were left behind for days. The worker has to go with it.
func TestAnAnalyzerDiesWithTheProcessThatStartedIt(t *testing.T) {
	helper := exec.Command(os.Args[0], "-test.run=^TestOrphanHelperProcess$", "-test.v")
	helper.Env = append(os.Environ(), orphanHelperEnv+"=1")
	stdout, err := helper.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = helper.Process.Kill(); _ = helper.Wait() })

	var worker int
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if value, found := strings.CutPrefix(scanner.Text(), "worker="); found {
			if worker, err = strconv.Atoi(value); err != nil {
				t.Fatalf("worker pid %q: %v", value, err)
			}
			break
		}
	}
	if worker == 0 {
		t.Fatalf("the helper never named its worker: %v", scanner.Err())
	}
	t.Cleanup(func() { _ = syscall.Kill(worker, syscall.SIGKILL) })
	if !alive(worker) {
		t.Fatal("the worker was not running before its parent was killed")
	}

	if err := helper.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for alive(worker) {
		if time.Now().After(deadline) {
			t.Fatalf("worker %d is still running 10s after the process that started it was killed", worker)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// And the other direction: a worker that is closed the ordinary way leaves no
// watcher behind, and the watcher signals nothing after the worker is reaped.
func TestClosingAWorkerLeavesNoWatcher(t *testing.T) {
	worker, _, err := launchWorker(t.Context(), "/bin/sleep", []string{"300"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pid := worker.cmd.Process.Pid
	watchers := func() string {
		out, _ := exec.Command("pgrep", "-f", fmt.Sprintf(`read _; kill -KILL -- "\$0" %d$`, pid)).Output()
		return strings.TrimSpace(string(out))
	}
	if watchers() == "" {
		t.Skip("the watcher cannot be found by its command line here")
	}
	worker.close()
	if left := watchers(); left != "" {
		t.Fatalf("watcher %s outlived the worker it guarded", left)
	}
}
