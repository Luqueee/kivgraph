package integrations

import (
	"encoding/json"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOurOwnStdioEntryIsMigratedWithoutForce is the case every existing
// installation hits the first time the default changed.
//
// The entry in the file is exactly what a previous `kivgraph mcp install` wrote:
// this executable, spawning `serve`. Demanding --force for it would mean the
// first run after the change failed on every machine that had ever registered a
// client, which is the same as saying the new default does not work.
func TestOurOwnStdioEntryIsMigratedWithoutForce(t *testing.T) {
	home := t.TempDir()
	executable := testsupport.InstalledExecutable()
	path := filepath.Join(home, ".claude.json")
	// Written by hand in the shape a previous install produced, so the fixture
	// demonstrates the real starting state rather than a guess at it.
	previous := []byte(`{"mcpServers":{"kivgraph":{"args":["serve"],"command":"` + executable + `"}},"custom":true}` + "\n")
	if err := os.WriteFile(path, previous, 0o600); err != nil {
		t.Fatalf("write the previous entry: %v", err)
	}

	manager, err := New(Options{
		HomeDir:    home,
		ProjectDir: t.TempDir(),
		Executable: executable,
		GOOS:       "darwin",
		Endpoint:   Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "a-token"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	status, err := manager.StatusMCP(TargetClaudeCode, ScopeUser)
	if err != nil {
		t.Fatalf("StatusMCP() error = %v", err)
	}
	if status.Status != statusSuperseded {
		t.Fatalf("StatusMCP() = %q, want %q", status.Status, statusSuperseded)
	}

	// force is false: this is the whole point.
	if _, err := manager.InstallMCP(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP() over our own previous entry error = %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var document struct {
		Servers map[string]map[string]any `json:"mcpServers"`
		Custom  bool                      `json:"custom"`
	}
	if err := json.Unmarshal(written, &document); err != nil {
		t.Fatalf("parse the written file: %v", err)
	}
	entry := document.Servers["kivgraph"]
	// Replaced, not merged: a command left beside a url is two registrations
	// under one key, and a client picks its transport from the shape.
	if _, present := entry["command"]; present {
		t.Fatalf("the stdio keys survived beside the url entry: %v", entry)
	}
	if _, present := entry["args"]; present {
		t.Fatalf("the stdio keys survived beside the url entry: %v", entry)
	}
	if entry["url"] != "http://127.0.0.1:7788/mcp" {
		t.Fatalf("entry = %v, want the daemon url", entry)
	}
	if !document.Custom {
		t.Fatal("the rest of the client's configuration was lost")
	}
}

// TestAnotherInstallationStillNeedsForce is the other half, and it is why the
// stdio comparison keeps the executable in it. An entry naming a different
// kivgraph binary belongs to another installation, and taking its clients over
// unasked would be worse than a refusal.
func TestAnotherInstallationStillNeedsForce(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude.json")
	foreign := []byte(`{"mcpServers":{"kivgraph":{"args":["serve"],"command":"/somewhere/else/kivgraph"}}}` + "\n")
	if err := os.WriteFile(path, foreign, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	manager, err := New(Options{
		HomeDir:    home,
		ProjectDir: t.TempDir(),
		Executable: testsupport.InstalledExecutable(),
		GOOS:       "darwin",
		Endpoint:   Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "a-token"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := manager.InstallMCP(TargetClaudeCode, ScopeUser, false, false); err == nil {
		t.Fatal("InstallMCP() replaced another installation's entry without --force")
	} else if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("error = %v, want it to name --force", err)
	}
}

// TestAURLEntryIsMigratedBackToStdio covers the reverse direction, which is what
// `--stdio` does on a machine already pointed at a daemon. The previous token and
// port are not knowable from here, so the recognition is structural.
func TestAURLEntryIsMigratedBackToStdio(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude.json")
	previous := []byte(`{"mcpServers":{"kivgraph":{"type":"http","url":"http://127.0.0.1:7788/mcp",` +
		`"headers":{"Authorization":"Bearer an-old-token"}}}}` + "\n")
	if err := os.WriteFile(path, previous, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	manager, err := New(Options{
		HomeDir:    home,
		ProjectDir: t.TempDir(),
		Executable: testsupport.InstalledExecutable(),
		GOOS:       "darwin",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := manager.InstallMCP(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP(--stdio) over our own url entry error = %v", err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(written), "Bearer an-old-token") {
		t.Fatalf("the stale token survived the migration:\n%s", written)
	}
	if !strings.Contains(string(written), `"command"`) {
		t.Fatalf("no command entry was written:\n%s", written)
	}
}

// TestCodexReplacesTheTableRatherThanAppending is the same rule in Codex's
// format, where the hazard is concrete: the file is TOML and a table appended
// beside the old one leaves `command` and `url` under one key.
func TestCodexReplacesTheTableRatherThanAppending(t *testing.T) {
	home := t.TempDir()
	executable := testsupport.InstalledExecutable()
	codex := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codex, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(codex, "config.toml")
	previous := "model = \"o3\"\n\n[mcp_servers.kivgraph]\ncommand = \"" + executable + "\"\nargs = [\"serve\"]\n"
	if err := os.WriteFile(path, []byte(previous), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	manager, err := New(Options{
		HomeDir:    home,
		ProjectDir: t.TempDir(),
		Executable: executable,
		GOOS:       "darwin",
		Endpoint:   Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "a-token"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := manager.InstallMCP(TargetCodex, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP(codex) over our own previous table error = %v", err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(written), "args") {
		t.Fatalf("the command table survived beside the url table:\n%s", written)
	}
	if strings.Count(string(written), "[mcp_servers.kivgraph]") != 1 {
		t.Fatalf("the table appears more than once:\n%s", written)
	}
	if !strings.Contains(string(written), "model = \"o3\"") {
		t.Fatal("the rest of the Codex configuration was lost")
	}
}
