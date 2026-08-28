package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/procstat"
)

// This file defends the defect LUQUE-2234 named. ADR 0069 sells "an `update`
// that restarts instead of killing eight" as one of the two things only a
// daemon allows, and `update` did neither: it listed the supervised daemon
// beside the `serve` processes and offered to stop it. Following that offer is
// worse than ignoring it -- `stop` asks politely first, the daemon exits zero,
// and both supervisors leave a clean exit alone on purpose -- so the better the
// daemon behaved, the more certainly it stayed down.

const testLabel = "com.kivgraph.daemon.abcd1234"

func TestUpdateRestartsTheSupervisedDaemonInsteadOfOfferingToStopIt(t *testing.T) {
	fixture := &stopFixture{processes: []procstat.Process{
		kivgraphProcess(51, "daemon"),
		kivgraphProcess(52, "serve"),
	}}
	restart := func([]procstat.Process) (daemonRestart, error) {
		return daemonRestart{Label: testLabel, PID: 51, Ownership: ownershipSupervised}, nil
	}

	var stdout, stderr bytes.Buffer
	if code := runUpdateWithRunner(nil, nil, &stdout, &stderr,
		installedRunner(), fixture.list, fixture.signal, restart, true); code != 0 {
		t.Fatalf("runUpdateWithRunner = %d, stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "update.daemon: "+testLabel+" restarted") {
		t.Fatalf("the restart was not reported:\n%s", output)
	}
	if strings.Contains(output, "update.stale: pid=51") {
		t.Fatalf("the restarted daemon was still offered up for stopping:\n%s", output)
	}
	// The caution is right for a process a client owns and wrong for the one
	// process that has an owner. Only the first is left in the question.
	if !strings.Contains(output, "update.stale: pid=52") {
		t.Fatalf("the serve stopped being reported:\n%s", output)
	}
	if !strings.Contains(output, "1 process(es) still run") {
		t.Fatalf("the count still included the daemon it dealt with:\n%s", output)
	}
	if got := strings.Join(fixture.signals, ","); got != "" {
		t.Fatalf("update signalled %q; a restart is not a kill", got)
	}
}

// A machine whose only stale process was the supervised daemon has nothing left
// for its operator to decide, so it is not asked -- and above all it is not
// told to run `kivgraph stop`, which is the advice that leaves it with none.
func TestUpdateAsksNothingWhenTheDaemonWasTheOnlyStaleProcess(t *testing.T) {
	fixture := &stopFixture{processes: []procstat.Process{kivgraphProcess(91, "daemon")}}
	restart := func([]procstat.Process) (daemonRestart, error) {
		return daemonRestart{Label: testLabel, PID: 91, Ownership: ownershipSupervised}, nil
	}

	var stdout, stderr bytes.Buffer
	if code := runUpdateWithRunner(nil, nil, &stdout, &stderr,
		installedRunner(), fixture.list, fixture.signal, restart, true); code != 0 {
		t.Fatalf("runUpdateWithRunner = %d, stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "update.daemon: "+testLabel+" restarted") {
		t.Fatalf("the restart was not reported:\n%s", output)
	}
	if strings.Contains(output, "still run the release") {
		t.Fatalf("a machine with nothing left to decide was asked to decide:\n%s", output)
	}
	if strings.Contains(output, "kivgraph stop") {
		t.Fatalf("the advice that would leave no daemon was printed anyway:\n%s", output)
	}
}

// `serve` and `ui` keep exactly the behaviour they had, and no supervisor is
// consulted about them: their comment is right, a client spawned them and will
// not start them again.
func TestUpdateDoesNotConsultTheSupervisorWithoutADaemon(t *testing.T) {
	fixture := &stopFixture{processes: []procstat.Process{
		kivgraphProcess(61, "serve"),
		kivgraphProcess(62, "ui"),
	}}
	consulted := false
	restart := func([]procstat.Process) (daemonRestart, error) {
		consulted = true
		return daemonRestart{}, nil
	}

	var stdout, stderr bytes.Buffer
	if code := runUpdateWithRunner(nil, nil, &stdout, &stderr,
		installedRunner(), fixture.list, fixture.signal, restart, true); code != 0 {
		t.Fatalf("runUpdateWithRunner = %d, stderr=%q", code, stderr.String())
	}
	if consulted {
		t.Fatal("update asked a supervisor about processes no supervisor owns")
	}
	output := stdout.String()
	for _, want := range []string{"update.stale: pid=61", "update.stale: pid=62", "nothing was stopped"} {
		if !strings.Contains(output, want) {
			t.Fatalf("update output lost %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "no supervisor owns") {
		t.Fatalf("a warning about a daemon appeared with no daemon in the list:\n%s", output)
	}
}

// A restart that failed leaves the daemon where it was: still stale, still
// listed, and the reason on stderr. Swallowing it would report a daemon back on
// the new release when it is answering from the old one.
func TestUpdateKeepsTheDaemonWhenItsRestartFails(t *testing.T) {
	fixture := &stopFixture{processes: []procstat.Process{kivgraphProcess(71, "daemon")}}
	restart := func([]procstat.Process) (daemonRestart, error) {
		return daemonRestart{Label: testLabel, PID: 71, Ownership: ownershipSupervised},
			errors.New("systemctl restart: exit status 1")
	}

	var stdout, stderr bytes.Buffer
	// The install succeeded, so the command succeeds: a restart that did not
	// happen is not an installation that did not happen.
	if code := runUpdateWithRunner(nil, nil, &stdout, &stderr,
		installedRunner(), fixture.list, fixture.signal, restart, true); code != 0 {
		t.Fatalf("runUpdateWithRunner = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "the supervised daemon was not restarted") {
		t.Fatalf("a failed restart was hidden:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "update.stale: pid=71") {
		t.Fatalf("a daemon that was not restarted stopped being reported:\n%s", stdout.String())
	}
	// A supervisor does own this one. Advising an install would say the single
	// thing that is not true about it.
	if strings.Contains(stdout.String(), "no supervisor owns") {
		t.Fatalf("a failed restart was reported as a daemon nobody supervises:\n%s", stdout.String())
	}
}

// A unit somebody edited by hand is reported, never restarted through -- that
// would start what the operator wrote instead of what the spec describes -- and
// it is still owned, so the advice about installing a supervisor stays away.
func TestUpdateReportsAHandEditedUnitWithoutAdvisingAnInstall(t *testing.T) {
	fixture := &stopFixture{processes: []procstat.Process{kivgraphProcess(101, "daemon")}}
	restart := func([]procstat.Process) (daemonRestart, error) {
		return daemonRestart{
			Label:     testLabel,
			Ownership: ownershipSupervised,
			Detail:    "the installed unit describes a different daemon: reinstall to replace it",
		}, nil
	}

	var stdout, stderr bytes.Buffer
	if code := runUpdateWithRunner(nil, nil, &stdout, &stderr,
		installedRunner(), fixture.list, fixture.signal, restart, true); code != 0 {
		t.Fatalf("runUpdateWithRunner = %d, stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		testLabel + " owns this daemon and could not be used",
		"reinstall to replace it",
		"update.stale: pid=101",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("update output lost %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "no supervisor owns") {
		t.Fatalf("a daemon with a unit was reported as one without:\n%s", output)
	}
}

// The advice printed under the list is wrong for a daemon nobody supervises,
// and that is the half of this defect that has no code fix: `stop` can end it
// and nothing can bring it back.
func TestUpdateWarnsThatStoppingAnUnownedDaemonLeavesNone(t *testing.T) {
	fixture := &stopFixture{processes: []procstat.Process{kivgraphProcess(81, "daemon")}}
	restart := func([]procstat.Process) (daemonRestart, error) {
		return daemonRestart{Ownership: ownershipNone}, nil
	}

	var stdout, stderr bytes.Buffer
	if code := runUpdateWithRunner(nil, nil, &stdout, &stderr,
		installedRunner(), fixture.list, fixture.signal, restart, true); code != 0 {
		t.Fatalf("runUpdateWithRunner = %d, stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"update.stale: pid=81",
		"daemon no supervisor owns",
		"kivgraph daemon install",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("update output lost %q:\n%s", want, output)
		}
	}
}

// A stale `kivgraph daemon` this configuration cannot identify -- no endpoint
// of its own, or one published by another state directory -- establishes
// nothing. It is still listed, and it gets no advice: the daemon in front of
// the operator may already be supervised somewhere else, and telling them to
// install a supervisor would be a guess dressed as a finding.
func TestUpdateAdvisesNothingAboutADaemonItCannotIdentify(t *testing.T) {
	fixture := &stopFixture{processes: []procstat.Process{kivgraphProcess(111, "daemon")}}
	restart := func([]procstat.Process) (daemonRestart, error) {
		return daemonRestart{Ownership: ownershipUnknown}, nil
	}

	var stdout, stderr bytes.Buffer
	if code := runUpdateWithRunner(nil, nil, &stdout, &stderr,
		installedRunner(), fixture.list, fixture.signal, restart, true); code != 0 {
		t.Fatalf("runUpdateWithRunner = %d, stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "update.stale: pid=111") {
		t.Fatalf("an unidentified daemon stopped being reported:\n%s", output)
	}
	if strings.Contains(output, "no supervisor owns") {
		t.Fatalf("update claimed nobody owns a daemon it could not identify:\n%s", output)
	}
	if strings.Contains(output, "owns this daemon") {
		t.Fatalf("update named an owner it never established:\n%s", output)
	}
}
