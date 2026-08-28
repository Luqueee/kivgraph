package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/procstat"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// TestALongRunningCommandDoesNotSwallowItsSubcommands is the guard that makes
// `daemon install` reachable at all. main takes `daemon` before the table's
// dispatch, and a guard on the first word alone would route the subcommand into
// the server loop, where `install` would be parsed as a daemon flag and refused.
func TestALongRunningCommandDoesNotSwallowItsSubcommands(t *testing.T) {
	for _, testCase := range []struct {
		name string
		args []string
		want bool
	}{
		{name: "daemon", args: []string{"daemon"}, want: true},
		{name: "daemon with flags", args: []string{"daemon", "--addr", "127.0.0.1:1"}, want: true},
		{name: "daemon install", args: []string{"daemon", "install"}, want: false},
		{name: "daemon remove", args: []string{"daemon", "remove"}, want: false},
		{name: "daemon status", args: []string{"daemon", "status"}, want: false},
		{name: "another command", args: []string{"doctor"}, want: false},
		{name: "no arguments", args: nil, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := interceptsLongRunning("daemon", testCase.args); got != testCase.want {
				t.Fatalf("interceptsLongRunning(%q) = %v, want %v", testCase.args, got, testCase.want)
			}
		})
	}
	// serve and ui are intercepted the same way and have no subcommands, so
	// the guard must not have changed what reaches them.
	if !interceptsLongRunning("serve", []string{"serve", "--config", "x"}) {
		t.Fatal("serve is no longer intercepted, so the MCP server would not start")
	}
	if !interceptsLongRunning("ui", []string{"ui"}) {
		t.Fatal("ui is no longer intercepted, so the viewer would not start")
	}
}

// TestSupervisorStatusNamesTheUnit covers the command an operator runs to find
// out whether the daemon has an owner. A status that answered nothing would
// leave a `url` registration unverifiable.
func TestSupervisorStatusNamesTheUnit(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("this platform has no supervisor: %s", runtime.GOOS)
	}
	home := t.TempDir()
	testsupport.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	configPath := writeSupervisorConfig(t, home)
	var stdout, stderr bytes.Buffer
	if code := runSupervisorCommand("status", []string{"--config", configPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("daemon status exit = %d, stderr = %q", code, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{"daemon.supervisor:", "state=absent", "label=com.kivgraph.daemon."} {
		if !strings.Contains(output, want) {
			t.Fatalf("daemon status omitted %q:\n%s", want, output)
		}
	}
	if !strings.Contains(output, "kivgraph daemon install") {
		t.Fatalf("daemon status does not say how to install one:\n%s", output)
	}
}

// TestRemoteWithoutAnAddressIsRefused keeps an install from recording a
// permission that changes nothing. --allow-remote on a loopback bind reads as a
// remote daemon and is not one, and the recorded unit would outlive the mistake.
func TestRemoteWithoutAnAddressIsRefused(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	configPath := writeSupervisorConfig(t, home)

	var stdout, stderr bytes.Buffer
	code := runSupervisorCommand("install", []string{"--config", configPath, "--allow-remote"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("daemon install accepted --allow-remote without --addr, stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--allow-remote needs --addr") {
		t.Fatalf("the refusal does not name the missing flag: %q", stderr.String())
	}
}

// TestAStrayArgumentIsRefused keeps a mistyped invocation from installing the
// default unit. `daemon install --addr 127.0.0.1:1 extra` should not silently
// ignore the word it cannot use.
func TestAStrayArgumentIsRefused(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	configPath := writeSupervisorConfig(t, home)

	var stdout, stderr bytes.Buffer
	code := runSupervisorCommand("status", []string{"--config", configPath, "extra"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("daemon status accepted a stray argument, stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), `unexpected argument "extra"`) {
		t.Fatalf("the refusal does not name the argument: %q", stderr.String())
	}
}

// writeSupervisorConfig produces the configuration these commands read.
//
// It calls the real initializer rather than hand-writing YAML: the state
// directory is derived from the database path that `kivgraph init` chooses, and
// a fixture that guessed the shape would pass while the command failed against
// a real installation.
func writeSupervisorConfig(t *testing.T, home string) string {
	t.Helper()
	configPath := filepath.Join(home, "config.yaml")
	if _, err := config.Initialize(config.InitOptions{
		ConfigPath:       configPath,
		RepositoriesPath: filepath.Join(home, "repositories.yaml"),
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	return configPath
}

// TestRestartSupervisedDaemonReportsNothingToDoRatherThanFailing covers the
// four ways `update` finds nothing to restart, and the line between them.
//
// All four answer with a zero pid and no error -- an error would print a failed
// restart on machines where this command already worked -- but they do not
// answer the same thing about ownership, and that is what decides whether the
// caller may advise installing a supervisor. Three establish nothing. Only the
// fourth establishes that nobody has it. None of them executes a supervisor,
// which is why they can be tested at all: the fifth branch, the one that
// restarts, needs a real systemd or launchd and is covered by the smoke test of
// the binary.
func TestRestartSupervisedDaemonReportsNothingToDoRatherThanFailing(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	stale := []procstat.Process{kivgraphProcess(999, "daemon")}

	// One: no configuration at all. A machine that never ran `init` has no
	// daemon of this state directory, whatever `kivgraph daemon` it is running
	// with a --config somewhere else.
	if outcome, err := restartSupervisedDaemon(stale); err != nil || outcome.PID != 0 || outcome.Ownership != ownershipUnknown {
		t.Fatalf("with no configuration: pid=%d ownership=%d err=%v, want 0, unknown and nil", outcome.PID, outcome.Ownership, err)
	}

	if _, err := config.Initialize(config.InitOptions{}); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}
	loaded, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load after init: %v", err)
	}
	directory := stateDirectory(loaded)

	// Two: no endpoint published. The daemon writes the file before it serves
	// and removes it when it stops, so its absence is the answer rather than a
	// failure to get one.
	if outcome, err := restartSupervisedDaemon(stale); err != nil || outcome.PID != 0 || outcome.Ownership != ownershipUnknown {
		t.Fatalf("with no endpoint: pid=%d ownership=%d err=%v, want 0, unknown and nil", outcome.PID, outcome.Ownership, err)
	}

	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create state directory: %v", err)
	}
	encoded, err := json.Marshal(daemon.Endpoint{URL: "http://127.0.0.1:9/mcp", Token: "t", PID: 999})
	if err != nil {
		t.Fatalf("encode endpoint: %v", err)
	}
	if err := os.WriteFile(daemon.EndpointPath(directory), encoded, 0o600); err != nil {
		t.Fatalf("write endpoint: %v", err)
	}

	// Three: a published daemon that is not one of the stale processes. It is
	// already answering from the release this update installed, or it belongs
	// to another state directory; restarting it would take down a graph this
	// update never touched.
	if outcome, err := restartSupervisedDaemon([]procstat.Process{kivgraphProcess(11, "daemon")}); err != nil || outcome.PID != 0 || outcome.Ownership != ownershipUnknown {
		t.Fatalf("with the daemon absent from the targets: pid=%d ownership=%d err=%v, want 0, unknown and nil", outcome.PID, outcome.Ownership, err)
	}

	// Four is the one case that establishes something: this configuration
	// published the daemon, its pid is one of the stale processes, and no unit
	// exists for it. Nobody owns it, and that is the only state in which the
	// caller may advise installing a supervisor.
	outcome, err := restartSupervisedDaemon(stale)
	if err != nil || outcome.PID != 0 {
		t.Fatalf("with no unit installed: pid=%d err=%v, want 0 and nil", outcome.PID, err)
	}
	if outcome.Ownership != ownershipNone {
		t.Fatalf("with no unit installed: ownership=%d, want %d (none)", outcome.Ownership, ownershipNone)
	}
}
