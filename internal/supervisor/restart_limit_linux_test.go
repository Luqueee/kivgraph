//go:build linux

package supervisor

import (
	"strconv"
	"strings"
	"testing"
)

// TestTheStartLimitCanActuallyTrip pins an arithmetic relation and not a pair of
// literals, because the defect it defends against was a pair of literals that
// happened to cancel out.
//
// systemd gives up on a unit that starts StartLimitBurst times inside
// StartLimitIntervalSec. Restarting every RestartSec seconds, `burst` starts
// span `RestartSec * (burst - 1)` seconds at the low end and `RestartSec *
// burst` at the high end, so a window equal to the latter sits exactly on its
// own boundary and never fires.
//
// Measured on 2026-08-28 with the shipped defaults -- burst 5, window 10s,
// RestartSec 2 -- against a unit whose ExecStart named a deleted binary:
// NRestarts reached 140 and kept going, one exec attempt every two seconds
// forever. With the window at 30s the same unit stopped at NRestarts=5 and
// entered `failed`.
//
// This matters beyond a typo: `serve` may install this unit for a `.mcpb`
// bundle, and that format has no uninstall hook. Deleting the extension leaves
// the unit naming a binary that is gone, with nothing of ours left to run.
func TestTheStartLimitCanActuallyTrip(t *testing.T) {
	rendered := unit(testSpec(t.TempDir()))
	burst := unitInteger(t, rendered, "StartLimitBurst")
	window := unitInteger(t, rendered, "StartLimitIntervalSec")
	restartSec := unitInteger(t, rendered, "RestartSec")

	if burst < 2 {
		t.Fatalf("StartLimitBurst = %d: a burst below two gives up on the first failure", burst)
	}
	if longest := restartSec * burst; window <= longest {
		t.Fatalf(
			"StartLimitIntervalSec = %d, but %d restarts %d seconds apart span %d seconds: "+
				"the limit sits on its own boundary and a unit that cannot exec restarts forever",
			window, burst, restartSec, longest)
	}
}

// TestTheUnitStillComesBackFromACrash keeps the bound above from turning into a
// unit that gives up on the thing it exists to survive: a daemon that died once
// has to come back, which is the promise ADR 0068 rests on.
func TestTheUnitStillComesBackFromACrash(t *testing.T) {
	rendered := unit(testSpec(t.TempDir()))
	if !strings.Contains(rendered, "Restart=on-failure") {
		t.Fatalf("the unit no longer restarts on failure:\n%s", rendered)
	}
	if strings.Contains(rendered, "Restart=always") {
		t.Fatalf("Restart=always would make `kivgraph stop` unable to stop anything:\n%s", rendered)
	}
}

// unitInteger reads a directive that must be there. A missing one is a failure
// rather than a zero: zero would satisfy some of the comparisons above.
func unitInteger(t *testing.T, rendered, directive string) int {
	t.Helper()
	for line := range strings.SplitSeq(rendered, "\n") {
		name, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found || name != directive {
			continue
		}
		parsed, err := strconv.Atoi(strings.TrimSuffix(value, "s"))
		if err != nil {
			t.Fatalf("%s=%q is not a number of seconds: %v", directive, value, err)
		}
		return parsed
	}
	t.Fatalf("the unit declares no %s:\n%s", directive, rendered)
	return 0
}
