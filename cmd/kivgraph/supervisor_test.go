package main

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
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
