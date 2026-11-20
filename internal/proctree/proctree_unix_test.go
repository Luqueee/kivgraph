//go:build unix

package proctree

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

func alive(pid int) bool {
	// A zombie still answers signal 0; every process here ends up reparented
	// to init, which reaps it, so ESRCH is the process being gone.
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func waitGone(t *testing.T, pid int, what string) {
	t.Helper()
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	deadline := time.Now().Add(10 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("%s (process %d) is still running after 10s", what, pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func firstPID(t *testing.T, output []byte) int {
	t.Helper()
	line, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("the tool did not name its child: output %q", output)
	}
	return pid
}

// The shape of an indexer that hits its time limit: a launcher waiting on a
// child that holds the output pipe. Killing the launcher alone leaves the child
// running and leaves the caller reading a pipe the child keeps open.
func TestATimeoutKillsWhatTheToolStartedAndReturns(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	output, err := CombinedOutput(exec.CommandContext(ctx, "/bin/sh", "-c", "sleep 30 & echo $!; wait"))
	if err == nil {
		t.Fatal("a tool killed at its deadline reported success")
	}
	if waited := time.Since(started); waited > 5*time.Second {
		t.Fatalf("it returned %s after a 300ms deadline: it waited on the child's pipe", waited)
	}
	waitGone(t, firstPID(t, output), "the tool's child")
}

// A build daemon or a worker node: started by the tool, detached from its
// output, and meant to idle after the tool is done.
func TestWhatTheToolLeftRunningDoesNotOutliveTheCall(t *testing.T) {
	output, err := CombinedOutput(exec.CommandContext(t.Context(), "/bin/sh", "-c",
		"sleep 30 >/dev/null 2>&1 & echo $!"))
	if err != nil {
		t.Fatalf("CombinedOutput() = %v, output %q", err, output)
	}
	waitGone(t, firstPID(t, output), "the process the tool left behind")
}

func TestTheOutputAndTheExitStatusAreTheTools(t *testing.T) {
	output, err := CombinedOutput(exec.CommandContext(t.Context(), "/bin/sh", "-c", "echo out; echo err >&2; exit 3"))
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("CombinedOutput() error = %v, want exit status 3", err)
	}
	if string(output) != "out\nerr\n" {
		t.Fatalf("output = %q, want both streams in order", output)
	}
	if _, err := CombinedOutput(exec.CommandContext(t.Context(), "/nonexistent/tool")); err == nil {
		t.Fatal("a tool that cannot be started reported success")
	}
}

// The worker that is one process and writes its answer to standard output: a
// tool killed while it works took nothing with it before, and its diagnostics
// have to reach the caller the way exec.Cmd.Output hands them over.
func TestOutputKeepsTheStreamsApartAndKillsTheTreeToo(t *testing.T) {
	output, err := Output(exec.CommandContext(t.Context(), "/bin/sh", "-c", "echo answer; echo why >&2; exit 4"))
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 4 || string(exit.Stderr) != "why\n" {
		t.Fatalf("Output() error = %v, want exit status 4 carrying the tool's standard error", err)
	}
	if string(output) != "answer\n" {
		t.Fatalf("output = %q, want standard output alone", output)
	}

	output, err = Output(exec.CommandContext(t.Context(), "/bin/sh", "-c", "sleep 30 >/dev/null 2>&1 & echo $!"))
	if err != nil {
		t.Fatalf("Output() = %v", err)
	}
	waitGone(t, firstPID(t, output), "the process the tool left behind")

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	output, err = Output(exec.CommandContext(ctx, "/bin/sh", "-c", "sleep 30 & echo $!; wait"))
	if err == nil {
		t.Fatal("a tool killed at its deadline reported success")
	}
	waitGone(t, firstPID(t, output), "the tool's child")
}

const helperEnv = "KIVGRAPH_PROCTREE_HELPER"

// TestHelperProcess is the process that gets killed while a tool it started is
// two processes deep.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) == "" {
		t.Skip("only runs as the child of TestATreeDiesWithTheProcessThatStartedIt")
	}
	cmd := exec.CommandContext(context.Background(), "/bin/sh", "-c", "sleep 300 & echo child=$!; wait")
	cmd.Stdout = os.Stdout
	Contain(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	Guard(cmd)
	fmt.Printf("leader=%d\n", cmd.Process.Pid)
	time.Sleep(5 * time.Minute)
}

// A process killed outright cancels nothing and runs no deferred call, which is
// how whole indexer trees were left behind.
func TestATreeDiesWithTheProcessThatStartedIt(t *testing.T) {
	helper := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "-test.v")
	helper.Env = append(os.Environ(), helperEnv+"=1")
	stdout, err := helper.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = helper.Process.Kill(); _ = helper.Wait() })

	found := map[string]int{}
	scanner := bufio.NewScanner(stdout)
	for len(found) < 2 && scanner.Scan() {
		for _, name := range []string{"leader", "child"} {
			if value, ok := strings.CutPrefix(scanner.Text(), name+"="); ok {
				if found[name], err = strconv.Atoi(value); err != nil {
					t.Fatalf("%s pid %q: %v", name, value, err)
				}
			}
		}
	}
	if len(found) < 2 {
		t.Fatalf("the helper named %v, want its leader and its child: %v", found, scanner.Err())
	}
	if !alive(found["leader"]) || !alive(found["child"]) {
		t.Fatal("the tree was not running before its parent was killed")
	}
	if err := helper.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	waitGone(t, found["leader"], "the tool")
	waitGone(t, found["child"], "the tool's child")
}

// An uncontained command is guarded alone: the analysis server is one process
// and shares its group with the process that started it.
func TestKillTreeOfAnUncontainedCommandKillsOnlyIt(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := killTree(cmd); err != nil {
		t.Fatalf("killTree() before Start = %v, want nil", err)
	}
	if release := Guard(cmd); release == nil {
		t.Fatal("Guard() before Start returned no release")
	} else {
		release()
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := killTree(cmd); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("the command survived killTree")
	}
	if !alive(os.Getpid()) {
		t.Fatal("unreachable: killTree signalled this process's group")
	}
}
