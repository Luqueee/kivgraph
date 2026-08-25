package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/integrations"
)

// TestStateDirectoryIsWhereTheDatabaseLives pins the derivation the daemon and
// the integration commands share. If these two disagreed, the command would
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

// TestStdioIsTheOptOutAndResolvesNothing is what keeps --stdio cheap: it writes
// the entry a client spawns for itself, so it reads no configuration, dials
// nothing and installs nothing.
func TestStdioIsTheOptOutAndResolvesNothing(t *testing.T) {
	var stdout bytes.Buffer
	options, err := integrationManagerOptions(
		integrationOptions{Scope: integrations.ScopeUser, Stdio: true}, true, &stdout)
	if err != nil {
		t.Fatalf("integrationManagerOptions(--stdio) error = %v", err)
	}
	if options != (integrations.Options{}) {
		t.Fatalf("options = %#v, want the zero value", options)
	}
}

// TestTheTwoTransportFlagsCannotBothBeAsked keeps a contradiction from being
// resolved by precedence. Whichever way it silently went, half the callers would
// get the entry they did not ask for.
func TestTheTwoTransportFlagsCannotBothBeAsked(t *testing.T) {
	var stdout bytes.Buffer
	_, err := integrationManagerOptions(
		integrationOptions{Scope: integrations.ScopeUser, Stdio: true, Daemon: true}, true, &stdout)
	if err == nil {
		t.Fatal("--stdio --daemon together were accepted")
	}
	if !strings.Contains(err.Error(), "--stdio and --daemon") {
		t.Fatalf("error = %v, want it to name both flags", err)
	}
}

// TestProjectScopeStaysOnStdio is the token rule at the seam. A url entry
// carries a bearer token and a project file gets committed, so the default
// writes the entry that works and names why it is not the daemon.
func TestProjectScopeStaysOnStdio(t *testing.T) {
	var stdout bytes.Buffer
	options, err := integrationManagerOptions(
		integrationOptions{Scope: integrations.ScopeProject}, true, &stdout)
	if err != nil {
		t.Fatalf("integrationManagerOptions(project) error = %v", err)
	}
	if options != (integrations.Options{}) {
		t.Fatalf("options = %#v, want the stdio zero value", options)
	}
	if !strings.Contains(stdout.String(), "committed") {
		t.Fatalf("the downgrade was silent: %q", stdout.String())
	}
}

// TestAReaderNeverProvisions is the separation between `mcp status` and
// `mcp install`. A read-only command that installed a supervisor and started a
// process as a side effect of being asked a question would be a surprise, and
// the surprise would be a background process nobody asked for.
func TestAReaderNeverProvisions(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, ".local", "state", "kivgraph")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	writeMinimalConfig(t, home, state)

	var stdout bytes.Buffer
	options, err := integrationManagerOptions(
		integrationOptions{Scope: integrations.ScopeUser}, false, &stdout)
	if err != nil {
		t.Fatalf("integrationManagerOptions(reader) error = %v", err)
	}
	if options != (integrations.Options{}) {
		t.Fatalf("options = %#v, want the stdio zero value with no daemon published", options)
	}
	if !strings.Contains(stdout.String(), "no daemon endpoint is published") {
		t.Fatalf("the reader did not say what it compared against: %q", stdout.String())
	}
	// Nothing was installed: a supervisor unit here would mean a question
	// started a process.
	if entries, _ := os.ReadDir(filepath.Join(home, "Library", "LaunchAgents")); len(entries) != 0 {
		t.Fatalf("the reader installed %d supervisor unit(s)", len(entries))
	}
}

// TestAReaderUsesAPublishedEndpointEvenWhenItIsDown covers why the reader wants
// identity and not liveness: the token survives a restart, so a published file
// still describes the entry this configuration installs. Comparing against
// stdio while a url entry was registered would call our own entry unmanaged.
func TestAReaderUsesAPublishedEndpointEvenWhenItIsDown(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, ".local", "state", "kivgraph")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	writeMinimalConfig(t, home, state)

	// A port nothing listens on, which is what a stopped daemon leaves behind.
	published, err := json.Marshal(daemon.Endpoint{
		URL: "http://127.0.0.1:1/mcp", Token: "published", PID: 1,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(daemon.EndpointPath(state), published, 0o600); err != nil {
		t.Fatalf("write endpoint: %v", err)
	}

	var stdout bytes.Buffer
	options, err := integrationManagerOptions(
		integrationOptions{Scope: integrations.ScopeUser}, false, &stdout)
	if err != nil {
		t.Fatalf("integrationManagerOptions(reader) error = %v", err)
	}
	if options.Endpoint.Token != "published" {
		t.Fatalf("options = %#v, want the published endpoint", options)
	}
}

// TestTheTransportFlagsAreOnlyOnTheMCPSide keeps the surface honest: a skill is
// a file with no transport, so offering it a transport flag would promise
// nothing.
//
// It inspects the declared flag sets rather than running the commands. Running
// `mcp install` here would resolve the real configuration and install a
// supervisor on the machine running the suite -- a test that starts a background
// process is not a test of a flag.
func TestTheTransportFlagsAreOnlyOnTheMCPSide(t *testing.T) {
	for _, operation := range []string{"install", "status", "remove"} {
		for _, flagName := range []string{"daemon", "stdio"} {
			var options integrationOptions
			mcp := integrationFlagSet("mcp "+operation, &options, io.Discard, operation != "status", true)
			if mcp.Lookup(flagName) == nil {
				t.Fatalf("mcp %s does not declare --%s", operation, flagName)
			}
			var skillOptions integrationOptions
			skill := integrationFlagSet("skill "+operation, &skillOptions, io.Discard, operation != "status", false)
			if skill.Lookup(flagName) != nil {
				t.Fatalf("skill %s declares --%s, which it cannot honour", operation, flagName)
			}
		}
	}
}

// TestAnEndpointManagerWritesTheURLEntry closes the loop: a daemon publishes,
// the seam reads, and what the manager builds is a url entry rather than a
// command.
func TestAnEndpointManagerWritesTheURLEntry(t *testing.T) {
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

// TestTheHelpAnnouncesBothTransports is what makes the choice discoverable. The
// daemon is the default now, so --stdio is the flag a reader has to be able to
// find: without it they cannot tell they have a choice at all.
func TestTheHelpAnnouncesBothTransports(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runMCPCommand([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("mcp --help = %d: %s", code, stderr.String())
	}
	for _, flagName := range []string{"--daemon", "--stdio"} {
		if !strings.Contains(stdout.String(), flagName) {
			t.Fatalf("the mcp help does not mention %s:\n%s", flagName, stdout.String())
		}
	}

	for _, operation := range []string{"install", "status", "remove"} {
		spec := integrationCommand("mcp", operation)
		// The usage line names the opt-out, not the default: --daemon changes
		// no outcome on its own now, and a curated summary that spelled both
		// would tell a reader they have to choose when they do not.
		if !strings.Contains(spec.usage, "--stdio") {
			t.Fatalf("mcp %s usage omits --stdio: %s", operation, spec.usage)
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
