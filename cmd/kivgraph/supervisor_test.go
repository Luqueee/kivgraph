package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// TestRestartSupervisedDaemonReportsNothingToDoRatherThanFailing drives the
// real restarter -- config, endpoint file and supervisor status against a
// temporary home -- through `update`, and asserts what the operator is told.
//
// Four inputs and no restart in any of them, because none of the four can
// execute a supervisor. What separates them is the advice underneath, which is
// the whole point of the ownership states: three establish nothing and must
// say nothing, and only the fourth establishes that this daemon has no owner.
//
// None of them may fail the command either. An error would print a failed
// restart on machines where `update` already worked, and the first of the four
// is the machine that never ran `init`.
func TestRestartSupervisedDaemonReportsNothingToDoRatherThanFailing(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// run drives `update` with the real restarter over the given stale
	// processes and returns what an operator would read.
	run := func(t *testing.T, stale ...procstat.Process) string {
		t.Helper()
		fixture := &stopFixture{processes: stale}
		var stdout, stderr bytes.Buffer
		if code := runUpdateWithRunner(nil, nil, &stdout, &stderr,
			installedRunner(), fixture.list, fixture.signal, restartSupervisedDaemon, true); code != 0 {
			t.Fatalf("runUpdateWithRunner = %d, stderr=%q", code, stderr.String())
		}
		if got := strings.Join(fixture.signals, ","); got != "" {
			t.Fatalf("update signalled %q while restarting nothing", got)
		}
		if strings.Contains(stdout.String(), "update.daemon: ") {
			t.Fatalf("update reported a restart it could not have performed:\n%s", stdout.String())
		}
		return stdout.String()
	}
	// The pid is named rather than merely present. Case three lists one daemon
	// and publishes another, so a loose check would pass over exactly the
	// confusion this test exists to catch.
	unadvised := func(t *testing.T, when string, pid int, output string) {
		t.Helper()
		if !strings.Contains(output, fmt.Sprintf("update.stale: pid=%d", pid)) {
			t.Fatalf("%s: pid=%d stopped being reported:\n%s", when, pid, output)
		}
		if strings.Contains(output, "no supervisor owns") {
			t.Fatalf("%s: update claimed nobody owns a daemon it could not identify:\n%s", when, output)
		}
		// Asserted apart from the line above, which today contains both: a
		// message split in two later must not take the guard with it.
		if strings.Contains(output, "kivgraph daemon install") {
			t.Fatalf("%s: update advised installing a supervisor it never ruled out:\n%s", when, output)
		}
		if strings.Contains(output, "owns this daemon") {
			t.Fatalf("%s: update named an owner it never established:\n%s", when, output)
		}
	}
	stale := kivgraphProcess(999, "daemon")

	// One: no configuration at all. A machine that never ran `init` has no
	// daemon of this state directory, whatever `kivgraph daemon` it is running
	// with a --config somewhere else.
	unadvised(t, "with no configuration", 999, run(t, stale))

	if _, err := config.Initialize(config.InitOptions{}); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}
	loaded, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load after init: %v", err)
	}
	directory := stateDirectory(loaded)

	// Two: no endpoint published. A daemon writes that file before it serves,
	// so its absence says this configuration has none running -- not that the
	// process in the list is unowned.
	unadvised(t, "with no endpoint", 999, run(t, stale))

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

	// Three: a published daemon that is not the stale one. The process in the
	// list belongs to another state directory and may already be supervised
	// there, so advising an install would be a guess dressed as a finding.
	unadvised(t, "with the daemon absent from the targets", 11, run(t, kivgraphProcess(11, "daemon")))

	// Four is the one case that establishes something: this configuration
	// published the daemon, its pid is one of the stale processes, and no unit
	// exists for it. Nobody owns it, and that is the only state in which the
	// advice below is true.
	output := run(t, stale)
	for _, want := range []string{
		"update.stale: pid=999",
		"daemon no supervisor owns",
		"kivgraph daemon install",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("with no unit installed, the output lost %q:\n%s", want, output)
		}
	}
}
