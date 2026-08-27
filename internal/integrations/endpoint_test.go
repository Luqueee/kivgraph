package integrations

import (
	"encoding/json"
	"errors"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// endpointManager builds a manager pointed at a daemon rather than at an
// executable.
func endpointManager(t *testing.T) (Manager, string, string) {
	t.Helper()
	home := t.TempDir()
	project := t.TempDir()
	manager, err := New(Options{
		HomeDir:    home,
		ProjectDir: project,
		Executable: testsupport.InstalledExecutable(),
		GOOS:       "darwin",
		Endpoint:   Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "a-token"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return manager, home, project
}

// TestAnEndpointEntryUsesEachClientsOwnShape is the whole point of writing these
// entries from here: the three shapes are the clients', and one shape for all of
// them would leave two unable to parse their own configuration.
func TestAnEndpointEntryUsesEachClientsOwnShape(t *testing.T) {
	manager, _, _ := endpointManager(t)
	header := map[string]any{"Authorization": "Bearer a-token"}

	tests := map[Target]map[string]any{
		TargetClaudeCode: {
			"type": "http", "url": "http://127.0.0.1:7788/mcp", "headers": header,
		},
		TargetClaudeDesktop: {
			"type": "http", "url": "http://127.0.0.1:7788/mcp", "headers": header,
		},
		TargetOhMyPi: {
			"type": "http", "url": "http://127.0.0.1:7788/mcp", "headers": header,
		},
		TargetOpenCode: {
			"type": "remote", "url": "http://127.0.0.1:7788/mcp",
			"enabled": true, "headers": header,
		},
	}
	for target, want := range tests {
		if got := manager.expectedJSONEntry(target); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s entry = %#v, want %#v", target, got, want)
		}
	}

	wantTOML := map[string]any{
		"url":          "http://127.0.0.1:7788/mcp",
		"http_headers": map[string]any{"Authorization": "Bearer a-token"},
	}
	if got := manager.expectedTOMLEntry(); !reflect.DeepEqual(got, wantTOML) {
		t.Fatalf("codex entry = %#v, want %#v", got, wantTOML)
	}
}

// TestAnEndpointEntryNamesNoCommand keeps the two transports from being written
// at once. Codex picks its transport from the shape, so a `command` left beside
// a `url` would have it spawn a process and ignore the daemon.
func TestAnEndpointEntryNamesNoCommand(t *testing.T) {
	manager, _, _ := endpointManager(t)
	for _, target := range KnownTargets() {
		entry := manager.expectedJSONEntry(target)
		if _, found := entry["command"]; found {
			t.Fatalf("%s entry still names a command: %#v", target, entry)
		}
	}
	if _, found := manager.expectedTOMLEntry()["command"]; found {
		t.Fatal("the codex entry still names a command")
	}
}

// TestWithoutAnEndpointNothingChanges is the regression that matters most: the
// stdio path is what every existing installation uses.
func TestWithoutAnEndpointNothingChanges(t *testing.T) {
	manager, _, _ := testManager(t)
	entry := manager.expectedJSONEntry(TargetClaudeCode)
	if entry["command"] != testsupport.InstalledExecutable() {
		t.Fatalf("entry = %#v, want the executable", entry)
	}
	if _, found := entry["url"]; found {
		t.Fatalf("entry names a url with no endpoint configured: %#v", entry)
	}
}

// TestAnEndpointRefusesProjectScope is what keeps a bearer token out of version
// control. `.mcp.json` is meant to be committed, and a stdio entry is safe to
// share precisely because it names an executable and not a secret.
func TestAnEndpointRefusesProjectScope(t *testing.T) {
	manager, _, project := endpointManager(t)
	for _, target := range []Target{TargetClaudeCode, TargetCodex, TargetOpenCode, TargetOhMyPi} {
		_, err := manager.InstallMCP(target, ScopeProject, false, false)
		if !errors.Is(err, ErrEndpointNeedsUserScope) {
			t.Fatalf("%s: InstallMCP(project) error = %v, want ErrEndpointNeedsUserScope", target, err)
		}
	}

	// And nothing was written on the way to that refusal.
	entries, err := os.ReadDir(project)
	if err != nil {
		t.Fatalf("read the project: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("the refusal left files behind: %v", names)
	}

	// User scope is still allowed: it is the user's own home, not a checkout.
	if _, err := manager.InstallMCP(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP(user) error = %v", err)
	}
}

// TestAnInstalledEndpointIsWhatTheClientReads closes the loop through the real
// writer: a hand-built map proves the shape, not that it survives the file.
func TestAnInstalledEndpointIsWhatTheClientReads(t *testing.T) {
	// The mode is the claim here, and only a platform that keeps one can
	// answer it. Where it does not, the file is narrowed with an ACL and
	// asserting 0600 would assert what Go reports about every file there.
	testsupport.SkipWithoutModeBits(t)
	manager, home, _ := endpointManager(t)

	if _, err := manager.InstallMCP(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("read the config: %v", err)
	}
	var document struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Command string            `json:"command"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("parse the config: %v", err)
	}
	entry, found := document.MCPServers["kivgraph"]
	if !found {
		t.Fatalf("no kivgraph entry in %s", raw)
	}
	if entry.Type != "http" || entry.URL != "http://127.0.0.1:7788/mcp" {
		t.Fatalf("entry = %#v", entry)
	}
	if entry.Headers["Authorization"] != "Bearer a-token" {
		t.Fatalf("headers = %#v", entry.Headers)
	}
	if entry.Command != "" {
		t.Fatalf("entry still names a command: %q", entry.Command)
	}

	// A token in a world-readable file is the same as no token.
	if mode := fileMode(t, filepath.Join(home, ".claude.json")); mode.Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 600", mode.Perm())
	}
}

// TestAnInstalledCodexEndpointParsesAsTOML pins the one file that is not JSON.
// A nested header table is where a hand-written encoder goes wrong.
func TestAnInstalledCodexEndpointParsesAsTOML(t *testing.T) {
	manager, home, _ := endpointManager(t)

	if _, err := manager.InstallMCP(TargetCodex, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP() error = %v", err)
	}
	path := filepath.Join(home, ".codex", "config.toml")
	var document struct {
		MCPServers map[string]struct {
			URL         string            `toml:"url"`
			Command     string            `toml:"command"`
			HTTPHeaders map[string]string `toml:"http_headers"`
		} `toml:"mcp_servers"`
	}
	if _, err := toml.DecodeFile(path, &document); err != nil {
		raw, _ := os.ReadFile(path)
		t.Fatalf("parse %s: %v\n%s", path, err, raw)
	}
	entry, found := document.MCPServers["kivgraph"]
	if !found {
		raw, _ := os.ReadFile(path)
		t.Fatalf("no kivgraph entry:\n%s", raw)
	}
	if entry.URL != "http://127.0.0.1:7788/mcp" {
		t.Fatalf("url = %q", entry.URL)
	}
	if entry.HTTPHeaders["Authorization"] != "Bearer a-token" {
		t.Fatalf("http_headers = %#v", entry.HTTPHeaders)
	}
	if entry.Command != "" {
		t.Fatalf("entry still names a command: %q", entry.Command)
	}
}

// TestReinstallingOverAStdioEntryReplacesIt is the upgrade path a real user
// takes: they already have `serve` installed and now start a daemon. Leaving
// both would have the client spawn a process and never reach the daemon.
func TestReinstallingOverAStdioEntryReplacesIt(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	base := Options{
		HomeDir:    home,
		ProjectDir: project,
		Executable: testsupport.InstalledExecutable(),
		GOOS:       "darwin",
	}
	stdio, err := New(base)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := stdio.InstallMCP(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("stdio InstallMCP() error = %v", err)
	}

	withEndpoint := base
	withEndpoint.Endpoint = Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "a-token"}
	daemon, err := New(withEndpoint)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	plan, err := daemon.InstallMCP(TargetClaudeCode, ScopeUser, false, true)
	if err != nil {
		t.Fatalf("endpoint InstallMCP() error = %v", err)
	}
	if !plan.Changed {
		t.Fatalf("plan = %#v, want the stdio entry replaced", plan)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), `"command"`) {
		t.Fatalf("the stdio command survived the endpoint install:\n%s", raw)
	}
	if !strings.Contains(string(raw), "http://127.0.0.1:7788/mcp") {
		t.Fatalf("the endpoint was not written:\n%s", raw)
	}
}

// TestAnEndpointInstallIsIdempotent is the property the renderer exists for. The
// written entry and the expected one are compared against each other, so a
// writer that emitted a shape the comparison did not recognise would report
// "installed" on every run and rewrite the file forever.
func TestAnEndpointInstallIsIdempotent(t *testing.T) {
	for _, target := range []Target{TargetClaudeCode, TargetCodex, TargetOpenCode, TargetOhMyPi} {
		t.Run(string(target), func(t *testing.T) {
			manager, _, _ := endpointManager(t)
			document, err := manager.mcpDocument(target, ScopeUser)
			if err != nil {
				t.Fatalf("mcpDocument() error = %v", err)
			}

			if _, err := manager.InstallMCP(target, ScopeUser, false, false); err != nil {
				t.Fatalf("first InstallMCP() error = %v", err)
			}
			first, err := os.ReadFile(document.path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			plan, err := manager.InstallMCP(target, ScopeUser, false, false)
			if err != nil {
				t.Fatalf("second InstallMCP() error = %v", err)
			}
			if plan.Status != "managed" || plan.Changed {
				t.Fatalf("plan = %#v, want an unchanged managed entry\n%s", plan, first)
			}
			second, err := os.ReadFile(document.path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if string(second) != string(first) {
				t.Fatalf("the second install rewrote the file:\n--- first ---\n%s--- second ---\n%s", first, second)
			}
		})
	}
}

// TestTheRenderedEntryIsExactlyTheseBytes fixes the key order, which no
// comparison of two runs can do reliably: Go randomises map iteration, so a
// renderer that dropped its sort would agree with itself often enough to pass a
// sampling test and rewrite a user's file on a later run.
//
// The stdio entry is the one that can vary -- it has two scalars where the
// endpoint entry has one -- so it is the one pinned here.
func TestTheRenderedEntryIsExactlyTheseBytes(t *testing.T) {
	manager, _, _ := testManager(t)

	want := "[mcp_servers.kivgraph]\n" +
		"args = [\"serve\"]\n" +
		"command = \"" + escapedPath(t, testsupport.InstalledExecutable()) + "\"\n"
	if got := string(appendTOMLSection(nil, manager.expectedTOMLEntry())); got != want {
		t.Fatalf("rendered:\n%s\nwant:\n%s", got, want)
	}

	endpoint, _, _ := endpointManager(t)
	wantEndpoint := "[mcp_servers.kivgraph]\n" +
		"url = \"http://127.0.0.1:7788/mcp\"\n" +
		"\n[mcp_servers.kivgraph.http_headers]\n" +
		"Authorization = \"Bearer a-token\"\n"
	if got := string(appendTOMLSection(nil, endpoint.expectedTOMLEntry())); got != wantEndpoint {
		t.Fatalf("rendered:\n%s\nwant:\n%s", got, wantEndpoint)
	}
}

// TestNoScalarLandsInsideATable pins the TOML rule a hand-written renderer gets
// wrong. After `[mcp_servers.kivgraph.http_headers]` every following key belongs
// to that table, so a scalar emitted late becomes a header named `url`.
func TestNoScalarLandsInsideATable(t *testing.T) {
	rendered := string(appendTOMLSection(nil, map[string]any{
		"url":          "http://127.0.0.1:7788/mcp",
		"args":         []any{"serve"},
		"http_headers": map[string]any{"Authorization": "Bearer a-token"},
	}))

	table := strings.Index(rendered, "[mcp_servers.kivgraph.http_headers]")
	if table < 0 {
		t.Fatalf("no header table was rendered:\n%s", rendered)
	}
	for _, scalar := range []string{"url = ", "args = "} {
		at := strings.Index(rendered, scalar)
		if at < 0 {
			t.Fatalf("%q was not rendered:\n%s", scalar, rendered)
		}
		if at > table {
			t.Fatalf("%q was written after the table header, so it belongs to it:\n%s", scalar, rendered)
		}
	}

	// And it has to parse as what it looks like, which is the only real judge.
	var document struct {
		MCPServers map[string]struct {
			URL         string            `toml:"url"`
			Args        []string          `toml:"args"`
			HTTPHeaders map[string]string `toml:"http_headers"`
		} `toml:"mcp_servers"`
	}
	if _, err := toml.Decode(rendered, &document); err != nil {
		t.Fatalf("parse:\n%s\n%v", rendered, err)
	}
	entry := document.MCPServers["kivgraph"]
	if entry.URL != "http://127.0.0.1:7788/mcp" || len(entry.Args) != 1 ||
		entry.HTTPHeaders["Authorization"] != "Bearer a-token" {
		t.Fatalf("entry = %#v\n%s", entry, rendered)
	}
}

// TestNewRefusesAHalfSpecifiedEndpoint keeps a caller from configuring clients
// against a url with no token: every request would fail on a `Bearer ` header,
// and falling back to stdio would install what the caller asked not to.
func TestNewRefusesAHalfSpecifiedEndpoint(t *testing.T) {
	for name, endpoint := range map[string]Endpoint{
		"no token": {URL: "http://127.0.0.1:7788/mcp"},
		"no url":   {Token: "a-token"},
	} {
		_, err := New(Options{
			HomeDir:    t.TempDir(),
			ProjectDir: t.TempDir(),
			Executable: testsupport.InstalledExecutable(),
			GOOS:       "darwin",
			Endpoint:   endpoint,
		})
		if !errors.Is(err, ErrIncompleteEndpoint) {
			t.Fatalf("%s: New() error = %v, want ErrIncompleteEndpoint", name, err)
		}
	}
}
