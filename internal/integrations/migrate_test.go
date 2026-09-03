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
	previous := []byte(`{"mcpServers":{"kivgraph":{"args":["serve"],"command":"` + escapedPath(t, executable) + `"}},"custom":true}` + "\n")
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

// TestAURLEntryNeedsPersistedOwnershipForMigration covers the reverse direction,
// which is what `--stdio` does on a machine already pointed at a daemon. The
// endpoint shape alone is ambiguous, so an explicit previous endpoint is needed
// before a non-forced migration can replace it.
func TestAURLEntryNeedsPersistedOwnershipForMigration(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude.json")
	previousEndpoint := Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "an-old-token"}
	previous := []byte(`{"mcpServers":{"kivgraph":{"type":"http","url":"` + previousEndpoint.URL + `",` +
		`"headers":{"Authorization":"Bearer ` + previousEndpoint.Token + `"}}}}` + "\n")
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
	status, err := manager.StatusMCP(TargetClaudeCode, ScopeUser)
	if err != nil {
		t.Fatalf("StatusMCP() error = %v", err)
	}
	if status.Status != "incompatible" {
		t.Fatalf("ambiguous endpoint status = %q, want incompatible", status.Status)
	}
	if _, err := manager.InstallMCP(TargetClaudeCode, ScopeUser, false, false); err == nil {
		t.Fatal("InstallMCP(--stdio) replaced an ambiguous endpoint without --force")
	}
	unchanged, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read unchanged config: %v", err)
	}
	if string(unchanged) != string(previous) {
		t.Fatalf("ambiguous endpoint changed without force:\n%s", unchanged)
	}

	manager, err = New(Options{
		HomeDir: home, ProjectDir: t.TempDir(),
		Executable: testsupport.InstalledExecutable(), GOOS: "darwin",
		PreviousEndpoint: previousEndpoint,
	})
	if err != nil {
		t.Fatalf("New() with previous endpoint error = %v", err)
	}
	if status, err := manager.StatusMCP(TargetClaudeCode, ScopeUser); err != nil {
		t.Fatalf("StatusMCP() with previous endpoint error = %v", err)
	} else if status.Status != statusSuperseded {
		t.Fatalf("owned endpoint status = %q, want %q", status.Status, statusSuperseded)
	}
	if _, err := manager.InstallMCP(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP(--stdio) over the owned url entry error = %v", err)
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

func TestAnUnverifiedCodexEndpointNeedsForce(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	endpoint := Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "an-old-token"}
	previous := "[mcp_servers.kivgraph]\n" +
		"url = \"" + endpoint.URL + "\"\n\n" +
		"[mcp_servers.kivgraph.http_headers]\n" +
		"Authorization = \"Bearer " + endpoint.Token + "\"\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(previous), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	manager, err := New(Options{
		HomeDir: home, ProjectDir: t.TempDir(),
		Executable: testsupport.InstalledExecutable(), GOOS: "darwin",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	status, err := manager.StatusMCP(TargetCodex, ScopeUser)
	if err != nil {
		t.Fatalf("StatusMCP() error = %v", err)
	}
	if status.Status != "incompatible" {
		t.Fatalf("ambiguous endpoint status = %q, want incompatible", status.Status)
	}
	if _, err := manager.InstallMCP(TargetCodex, ScopeUser, false, false); err == nil {
		t.Fatalf("InstallMCP() replaced unverified endpoint %q without --force", endpoint.URL)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read unchanged config: %v", err)
	}
	if string(unchanged) != previous {
		t.Fatalf("unverified endpoint changed without force:\n%s", unchanged)
	}
}

func TestAnOldDaemonEndpointIsRefreshedWithoutForce(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	path := filepath.Join(home, ".claude.json")
	oldEndpoint := Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "old-token"}
	currentEndpoint := Endpoint{URL: "http://127.0.0.1:7799/mcp", Token: "current-token"}
	base := Options{
		HomeDir: home, ProjectDir: project,
		Executable: testsupport.InstalledExecutable(), GOOS: "darwin",
	}
	oldManager, err := New(Options{HomeDir: base.HomeDir, ProjectDir: base.ProjectDir,
		Executable: base.Executable, GOOS: base.GOOS, Endpoint: oldEndpoint})
	if err != nil {
		t.Fatalf("New(old) error = %v", err)
	}
	if _, err := oldManager.InstallMCP(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP(old) error = %v", err)
	}
	currentManager, err := New(Options{HomeDir: base.HomeDir, ProjectDir: base.ProjectDir,
		Executable: base.Executable, GOOS: base.GOOS, Endpoint: currentEndpoint,
		PreviousEndpoint: oldEndpoint})
	if err != nil {
		t.Fatalf("New(current) error = %v", err)
	}
	status, err := currentManager.StatusMCP(TargetClaudeCode, ScopeUser)
	if err != nil {
		t.Fatalf("StatusMCP() error = %v", err)
	}
	if status.Status != statusSuperseded {
		t.Fatalf("old endpoint status = %q, want %q", status.Status, statusSuperseded)
	}
	if _, err := currentManager.InstallMCP(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP(current) error = %v", err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	content := string(updated)
	if strings.Contains(content, oldEndpoint.URL) || !strings.Contains(content, currentEndpoint.URL) ||
		strings.Contains(content, oldEndpoint.Token) || !strings.Contains(content, currentEndpoint.Token) {
		t.Fatalf("endpoint was not refreshed from old=%q to current=%q: %s", oldEndpoint.URL, currentEndpoint.URL, updated)
	}
}

func TestAnOldCodexDaemonEndpointIsRefreshedWithoutForce(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	oldEndpoint := Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "old-token"}
	currentEndpoint := Endpoint{URL: "http://127.0.0.1:7799/mcp", Token: "current-token"}
	base := Options{
		HomeDir: home, ProjectDir: project,
		Executable: testsupport.InstalledExecutable(), GOOS: "darwin",
	}
	oldManager, err := New(Options{HomeDir: base.HomeDir, ProjectDir: base.ProjectDir,
		Executable: base.Executable, GOOS: base.GOOS, Endpoint: oldEndpoint})
	if err != nil {
		t.Fatalf("New(old) error = %v", err)
	}
	if _, err := oldManager.InstallMCP(TargetCodex, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP(old) error = %v", err)
	}
	currentManager, err := New(Options{HomeDir: base.HomeDir, ProjectDir: base.ProjectDir,
		Executable: base.Executable, GOOS: base.GOOS, Endpoint: currentEndpoint,
		PreviousEndpoint: oldEndpoint})
	if err != nil {
		t.Fatalf("New(current) error = %v", err)
	}
	status, err := currentManager.StatusMCP(TargetCodex, ScopeUser)
	if err != nil {
		t.Fatalf("StatusMCP() error = %v", err)
	}
	if status.Status != statusSuperseded {
		t.Fatalf("old endpoint status = %q, want %q", status.Status, statusSuperseded)
	}
	if _, err := currentManager.InstallMCP(TargetCodex, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP(current) error = %v", err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	content := string(updated)
	if strings.Contains(content, oldEndpoint.URL) || !strings.Contains(content, currentEndpoint.URL) ||
		strings.Contains(content, oldEndpoint.Token) || !strings.Contains(content, currentEndpoint.Token) {
		t.Fatalf("endpoint was not refreshed from old=%q to current=%q: %s", oldEndpoint.URL, currentEndpoint.URL, updated)
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
	previous := "model = \"o3\"\n\n[mcp_servers.kivgraph]\ncommand = \"" + escapedPath(t, executable) + "\"\nargs = [\"serve\"]\n\n[[plugins]]\nname = \"formatter\"\n"
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
	if !strings.Contains(string(written), "name = \"formatter\"") {
		t.Fatal("the unrelated array table was lost")
	}
}
