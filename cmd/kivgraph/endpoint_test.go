package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/integrations"
)

// TestStateDirectoryIsWhereTheDatabaseLives pins the derivation the daemon and
// `mcp install --daemon` share. If these two disagreed, the command would
// configure clients against a daemon this configuration never starts.
func TestStateDirectoryIsWhereTheDatabaseLives(t *testing.T) {
	loaded := config.Loaded{}
	loaded.Config.Storage.DatabasePath = "/home/u/.local/state/kivgraph/graph.lbdb"
	if got, want := stateDirectory(loaded), "/home/u/.local/state/kivgraph"; got != want {
		t.Fatalf("stateDirectory() = %q, want %q", got, want)
	}
	if got := daemon.EndpointPath(stateDirectory(loaded)); got != "/home/u/.local/state/kivgraph/daemon.json" {
		t.Fatalf("EndpointPath() = %q", got)
	}
}

// TestWithoutTheFlagNothingIsResolved is what keeps the installed entry from
// depending on whether a daemon happened to be running: the default reads no
// configuration at all.
func TestWithoutTheFlagNothingIsResolved(t *testing.T) {
	options, err := integrationManagerOptions(false)
	if err != nil {
		t.Fatalf("integrationManagerOptions(false) error = %v", err)
	}
	if options != (integrations.Options{}) {
		t.Fatalf("options = %#v, want the zero value", options)
	}
}

// TestTheFlagNamesWhatIsMissing covers the failure a user actually hits: they
// pass --daemon before starting one. Two failures reach here and they are not
// the same, so neither message may stand in for the other.
func TestTheFlagNamesWhatIsMissing(t *testing.T) {
	t.Run("no configuration", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))

		_, err := integrationManagerOptions(true)
		if err == nil {
			t.Fatal("integrationManagerOptions(true) succeeded with no configuration")
		}
		if !strings.Contains(err.Error(), "configuration") {
			t.Fatalf("error = %v, want it to name the configuration", err)
		}
	})

	t.Run("configured but no daemon", func(t *testing.T) {
		home := t.TempDir()
		state := filepath.Join(home, ".local", "state", "kivgraph")
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
		writeMinimalConfig(t, home, state)

		_, err := integrationManagerOptions(true)
		if err == nil {
			t.Fatal("integrationManagerOptions(true) succeeded with no daemon running")
		}
		// The directory, because a user with two configurations needs to know
		// which one was consulted, and the command, because knowing what is
		// missing is useless without knowing what starts it.
		if !strings.Contains(err.Error(), state) {
			t.Fatalf("error = %v, want it to name %s", err, state)
		}
		if !strings.Contains(err.Error(), "kivgraph daemon") {
			t.Fatalf("error = %v, want it to name the command that starts one", err)
		}
	})
}

// TestTheFlagReadsAPublishedEndpoint closes the loop: a daemon publishes, the
// command reads, and what it builds is an endpoint-shaped manager.
func TestTheFlagReadsAPublishedEndpoint(t *testing.T) {
	directory := t.TempDir()
	served, err := daemon.ListenHTTP(
		daemon.Options{StateDirectory: directory},
		daemon.HTTPOptions{Address: "127.0.0.1:0"},
	)
	if err != nil {
		t.Fatalf("ListenHTTP() error = %v", err)
	}
	t.Cleanup(func() { _ = served.Close() })

	endpoint, err := daemon.ReadEndpoint(directory)
	if err != nil {
		t.Fatalf("ReadEndpoint() error = %v", err)
	}
	manager, err := integrations.New(integrations.Options{
		HomeDir:    t.TempDir(),
		ProjectDir: t.TempDir(),
		Executable: "/opt/kivgraph/bin/kivgraph",
		GOOS:       "darwin",
		Endpoint:   integrations.Endpoint{URL: endpoint.URL, Token: endpoint.Token},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := manager.InstallMCP(integrations.TargetClaudeCode, integrations.ScopeUser, true, false); err != nil {
		t.Fatalf("InstallMCP() error = %v", err)
	}
}

// TestTheDaemonFlagIsOnlyOnTheMCPSide keeps the surface honest: a skill is a
// file with no transport, so offering it a transport flag would promise nothing.
func TestTheDaemonFlagIsOnlyOnTheMCPSide(t *testing.T) {
	for _, operation := range []string{"install", "status", "remove"} {
		var stdout, stderr bytes.Buffer
		if code := runMCPCommand([]string{operation, "--daemon", "--target", "nope"}, &stdout, &stderr); code == 2 &&
			strings.Contains(stderr.String(), "flag provided but not defined") {
			t.Fatalf("mcp %s rejected --daemon: %s", operation, stderr.String())
		}

		stdout.Reset()
		stderr.Reset()
		code := runSkillCommand([]string{operation, "--daemon", "--target", "claude-code"}, &stdout, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "flag provided but not defined") {
			t.Fatalf("skill %s accepted --daemon: code=%d %s", operation, code, stderr.String())
		}
	}
}

// TestTheHelpAnnouncesTheFlag is what makes the transport discoverable. A flag
// nobody is told about is a flag nobody uses, and the whole point of this one is
// that it is the reachable path to the daemon's measured saving.
func TestTheHelpAnnouncesTheFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runMCPCommand([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("mcp --help = %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "--daemon") {
		t.Fatalf("the mcp help does not mention --daemon:\n%s", stdout.String())
	}

	for _, operation := range []string{"install", "status", "remove"} {
		spec := integrationCommand("mcp", operation)
		if !strings.Contains(spec.usage, "--daemon") {
			t.Fatalf("mcp %s usage omits --daemon: %s", operation, spec.usage)
		}
		if !strings.Contains(integrationCommand("skill", operation).usage, "--scope") {
			t.Fatalf("skill %s usage lost its scope", operation)
		}
		if strings.Contains(integrationCommand("skill", operation).usage, "--daemon") {
			t.Fatalf("skill %s usage offers --daemon", operation)
		}
	}
}

// TestAnEndpointInstallLeavesTheProjectAlone is the CLI half of the refusal: the
// token must not reach a file that gets committed, and the command has to fail
// rather than fall back to a stdio entry the user did not ask for.
func TestAnEndpointInstallLeavesTheProjectAlone(t *testing.T) {
	project := t.TempDir()
	manager, err := integrations.New(integrations.Options{
		HomeDir:    t.TempDir(),
		ProjectDir: project,
		Executable: "/opt/kivgraph/bin/kivgraph",
		GOOS:       "darwin",
		Endpoint:   integrations.Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "a-token"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := manager.InstallMCP(integrations.TargetClaudeCode, integrations.ScopeProject, false, false); err == nil {
		t.Fatal("InstallMCP(project) wrote a token into the repository")
	}
	entries, err := os.ReadDir(project)
	if err != nil {
		t.Fatalf("read the project: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the project is not empty: %d entries", len(entries))
	}
}

// writeMinimalConfig creates the default configuration in an isolated home, the
// same way `kivgraph init` does.
//
// Through Initialize rather than a hand-written YAML, because the decoder
// rejects unknown keys: a literal fixture would pass today and become a parse
// error the first time the contract gained a field, and the failure would point
// at this test rather than at the change.
func writeMinimalConfig(t *testing.T, home, wantState string) {
	t.Helper()
	if _, err := config.Initialize(config.InitOptions{}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	loaded, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// Both checks guard the same thing from two sides: a test that silently ran
	// against the developer's real configuration would prove nothing and could
	// touch a real daemon.
	directory := stateDirectory(loaded)
	if directory != wantState {
		t.Fatalf("state directory = %q, want %q", directory, wantState)
	}
	if !strings.HasPrefix(directory, home) {
		t.Fatalf("the configuration escaped the temporary home: %s", directory)
	}
}
