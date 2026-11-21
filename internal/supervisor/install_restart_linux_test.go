//go:build linux

package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// `systemctl enable --now` starts a unit that is not running and leaves one that
// is running alone, so installing over a live daemon wrote the new unit, reloaded
// systemd, and went on serving with the previous binary, arguments and PATH --
// while reporting the install done. The launchd side boots the old agent out
// before it bootstraps the new one; this is the same promise on Linux: after an
// install the process that runs is the one the unit describes.
//
// The fake systemctl records what it is asked, which is the whole observable of
// the contract without a systemd user manager in the test environment.
func TestInstallingOverARunningUnitRestartsIt(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	commandDirectory := filepath.Join(home, "bin")
	if err := os.MkdirAll(commandDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	commandLog := filepath.Join(home, "systemctl.log")
	t.Setenv("KIVGRAPH_TEST_SUPERVISOR_LOG", commandLog)
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$KIVGRAPH_TEST_SUPERVISOR_LOG\"\n"
	if err := os.WriteFile(filepath.Join(commandDirectory, "systemctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", commandDirectory)

	report, err := Install(testSpec(t.TempDir()))
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if report.State != StateInstalled {
		t.Fatalf("Install() state = %q, want %q", report.State, StateInstalled)
	}
	log, err := os.ReadFile(commandLog)
	if err != nil {
		t.Fatal(err)
	}
	// In this order: the unit has to be read before anything is started from it,
	// and the restart is what replaces a process that was already running.
	var seen []string
	for _, line := range strings.Split(strings.TrimSpace(string(log)), "\n") {
		seen = append(seen, strings.Fields(line)[1])
	}
	want := []string{"daemon-reload", "enable", "restart"}
	if strings.Join(seen, " ") != strings.Join(want, " ") {
		t.Fatalf("systemctl was asked %q, want %q:\n%s", seen, want, log)
	}
}
