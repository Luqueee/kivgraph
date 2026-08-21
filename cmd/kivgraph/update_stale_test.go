package main

import (
	"bytes"
	"context"
	"strings"
	"syscall"
	"testing"

	"github.com/Luqueee/kivgraph/internal/procstat"
	"github.com/Luqueee/kivgraph/internal/update"
	"github.com/Luqueee/kivgraph/internal/version"
)

func installedRunner() updateRunner {
	return func(context.Context, update.Options) (update.Result, error) {
		return update.Result{
			CurrentVersion:  version.Value,
			LatestVersion:   "9.9.9",
			UpdateAvailable: true,
			Updated:         true,
		}, nil
	}
}

// After the bundle is replaced, a serve that was already running answers from
// the image that was swapped out. The command has to say so even when it cannot
// ask whether to end it.
func TestUpdateReportsTheProcessesLeftOnTheOldRelease(t *testing.T) {
	fixture := &stopFixture{processes: []procstat.Process{
		kivgraphProcess(21, "serve"),
		kivgraphProcess(22, "ui"),
	}}
	var stdout, stderr bytes.Buffer
	if code := runUpdateWithRunner(nil, nil, &stdout, &stderr,
		installedRunner(), fixture.list, fixture.signal); code != 0 {
		t.Fatalf("runUpdateWithRunner() = %d, stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"kivgraph updated: ",
		"2 process(es) still run the release this update replaced",
		"update.stale: pid=21 /opt/kivgraph/bin/kivgraph serve",
		"update.stale: pid=22 /opt/kivgraph/bin/kivgraph ui",
		// Nothing may be ended without an answer: a client owns these
		// processes and a silent kill reads to it as a crash.
		"nothing was stopped",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("update output lost %q:\n%s", want, output)
		}
	}
	if got := strings.Join(fixture.signals, ","); got != "" {
		t.Fatalf("update signalled %q without being told to", got)
	}
}

// --stop is the scriptable answer: the same escalation `stop` uses, with no
// question asked.
func TestUpdateStopsTheOldProcessesWhenAsked(t *testing.T) {
	shortenStopGrace(t)
	fixture := &stopFixture{
		processes: []procstat.Process{kivgraphProcess(31, "serve")},
		diesOn:    map[int]syscall.Signal{31: syscall.SIGTERM},
	}
	var stdout, stderr bytes.Buffer
	if code := runUpdateWithRunner([]string{"--stop"}, nil, &stdout, &stderr,
		installedRunner(), fixture.list, fixture.signal); code != 0 {
		t.Fatalf("runUpdateWithRunner() = %d, stderr=%q", code, stderr.String())
	}
	if got := strings.Join(fixture.signals, ","); got != "31:terminated" {
		t.Fatalf("signals = %q, want a single SIGTERM", got)
	}
	if !strings.Contains(stdout.String(), "update.stop: 1 process(es) stopped, 0 killed") {
		t.Fatalf("update did not report what it stopped:\n%s", stdout.String())
	}
}

// A machine with nothing of ours running gets no extra output at all: the
// update line is the whole answer.
func TestUpdateSaysNothingWhenNoProcessIsStale(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runUpdateWithRunner(nil, nil, &stdout, &stderr,
		installedRunner(), noProcesses, nil); code != 0 {
		t.Fatalf("runUpdateWithRunner() = %d, stderr=%q", code, stderr.String())
	}
	if got, want := stdout.String(), "kivgraph updated: "+version.Value+" -> 9.9.9\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

// An install that succeeded must not be reported as a failure because the
// process list could not be read.
func TestUpdateSucceedsWhenTheProcessListIsUnavailable(t *testing.T) {
	list := func() ([]procstat.Process, error) { return nil, procstat.ErrProcessListUnsupported }
	var stdout, stderr bytes.Buffer
	if code := runUpdateWithRunner(nil, nil, &stdout, &stderr,
		installedRunner(), list, nil); code != 0 {
		t.Fatalf("runUpdateWithRunner() = %d, want 0 (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "kivgraph updated: ") {
		t.Fatalf("the successful install was not reported:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "could not list the processes") {
		t.Fatalf("the limitation was hidden:\n%s", stderr.String())
	}
}

// --check must never look at processes: nothing was replaced.
func TestUpdateCheckDoesNotTouchProcesses(t *testing.T) {
	fixture := &stopFixture{processes: []procstat.Process{kivgraphProcess(41, "serve")}}
	var stdout, stderr bytes.Buffer
	if code := runUpdateWithRunner([]string{"--check"}, nil, &stdout, &stderr,
		func(_ context.Context, options update.Options) (update.Result, error) {
			if !options.CheckOnly {
				t.Fatal("--check did not ask for a check")
			}
			return update.Result{
				CurrentVersion:  version.Value,
				LatestVersion:   "9.9.9",
				UpdateAvailable: true,
			}, nil
		}, fixture.list, fixture.signal); code != 0 {
		t.Fatalf("runUpdateWithRunner() = %d, stderr=%q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "update.stale") {
		t.Fatalf("--check reported stale processes:\n%s", stdout.String())
	}
	if got := strings.Join(fixture.signals, ","); got != "" {
		t.Fatalf("--check signalled %q", got)
	}
}

// A prompt written into a pipe would block on an answer that never comes.
func TestPromptYesRefusesWithoutATerminal(t *testing.T) {
	var stdout bytes.Buffer
	if promptYes(strings.NewReader("y\n"), &stdout, "stop them?") {
		t.Fatal("promptYes() answered yes on a writer that is not a terminal")
	}
	if stdout.Len() != 0 {
		t.Fatalf("promptYes() wrote a question nobody could answer: %q", stdout.String())
	}
	if promptYes(nil, &stdout, "stop them?") {
		t.Fatal("promptYes() answered yes with no input at all")
	}
}
