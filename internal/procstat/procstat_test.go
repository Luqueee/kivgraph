package procstat

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestResidentBytesRejectsInvalidPIDs(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if got := ResidentBytes(pid); got != 0 {
			t.Fatalf("ResidentBytes(%d) = %d, want 0", pid, got)
		}
	}
}

func TestResidentBytesReportsTheRunningProcess(t *testing.T) {
	if !supported() {
		t.Skipf("resident set size is not reported on %s", runtime.GOOS)
	}
	resident := ResidentBytes(os.Getpid())
	if resident <= 0 {
		t.Fatalf("ResidentBytes(self) = %d, want a positive size", resident)
	}
	if resident > 64<<30 {
		t.Fatalf("ResidentBytes(self) = %d, implausible for a test binary", resident)
	}
}

// TestResidentBytesReportsAnotherProcess covers the supervisor use: the worker
// is a child, not the caller, and macOS refuses several process introspection
// interfaces across process boundaries.
func TestResidentBytesReportsAnotherProcess(t *testing.T) {
	if !supported() {
		t.Skipf("resident set size is not reported on %s", runtime.GOOS)
	}
	command := exec.Command("/bin/sh", "-c", "read line")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe(): %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = command.Wait()
	})
	if resident := ResidentBytes(command.Process.Pid); resident <= 0 {
		t.Fatalf("ResidentBytes(child) = %d, want a positive size", resident)
	}
}

func TestResidentBytesReportsZeroForAnUnknownProcess(t *testing.T) {
	if got := ResidentBytes(1 << 30); got != 0 {
		t.Fatalf("ResidentBytes(unknown) = %d, want 0", got)
	}
}

// TestListSeesThisProcess is the floor the stop command stands on: a platform
// that cannot see the process running the test cannot be trusted to find a
// server either, and an empty list would read as "nothing is running".
func TestListSeesThisProcess(t *testing.T) {
	processes, err := List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	for _, process := range processes {
		if process.PID != os.Getpid() {
			continue
		}
		program, _ := process.Invocation()
		if program == "" {
			t.Fatalf("self has no invocation: %#v", process)
		}
		return
	}
	t.Fatalf("List() returned %d processes and none was this one (%d)", len(processes), os.Getpid())
}

// TestInvocationSplitsProgramAndCommand pins what the stop command matches on.
func TestInvocationSplitsProgramAndCommand(t *testing.T) {
	program, command := Process{Args: []string{"/opt/ladygraph/bin/ladygraph", "serve"}}.Invocation()
	if program != "ladygraph" || command != "serve" {
		t.Fatalf("invocation = %q/%q, want ladygraph/serve", program, command)
	}
	if program, command := (Process{}).Invocation(); program != "" || command != "" {
		t.Fatalf("empty invocation = %q/%q, want both empty", program, command)
	}
}
