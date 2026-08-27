package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The sibling of TestRemoveLeavesNoEmptyScaffolding, for the MCP entry rather
// than the hook: an emptied `mcpServers` written back as `{}` is a trace of
// us, not an absence.
//
// The file has to exist first, and hold something of somebody else's. That is
// the whole reason this survived: a file this created is deleted whole on the
// way out, so the leftover is invisible unless the remove has a document it
// must leave behind. It took an install against a real Claude Desktop --
// whose configuration held a `preferences` key -- to produce one.
func TestRemoveMCPLeavesNoEmptyScaffolding(t *testing.T) {
	manager, home, _ := testManager(t)
	directory := filepath.Join(home, "Library", "Application Support", "Claude")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create configuration directory: %v", err)
	}
	path := filepath.Join(directory, "claude_desktop_config.json")
	if err := os.WriteFile(path, []byte(`{"preferences":{"theme":"dark"}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write existing configuration: %v", err)
	}

	if _, err := manager.InstallMCP(TargetClaudeDesktop, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP() error = %v", err)
	}
	if _, err := manager.RemoveMCP(TargetClaudeDesktop, ScopeUser, false, false); err != nil {
		t.Fatalf("RemoveMCP() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read configuration: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode configuration: %v", err)
	}
	if document["preferences"] == nil {
		t.Fatalf("remove took somebody else's key with it: %s", data)
	}
	if document["mcpServers"] != nil {
		t.Fatalf("remove left %#v behind, which is the shape of what was taken out", document["mcpServers"])
	}
}

// And the other direction: a section that still holds somebody else's server
// must survive, because deleting it would take their registration with ours.
func TestRemoveMCPKeepsASectionThatIsNotOnlyOurs(t *testing.T) {
	manager, home, _ := testManager(t)
	directory := filepath.Join(home, "Library", "Application Support", "Claude")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create configuration directory: %v", err)
	}
	path := filepath.Join(directory, "claude_desktop_config.json")
	existing := `{"mcpServers":{"somebody-else":{"command":"other"}}}` + "\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatalf("write existing configuration: %v", err)
	}

	if _, err := manager.InstallMCP(TargetClaudeDesktop, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP() error = %v", err)
	}
	if _, err := manager.RemoveMCP(TargetClaudeDesktop, ScopeUser, false, false); err != nil {
		t.Fatalf("RemoveMCP() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read configuration: %v", err)
	}
	var document map[string]map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode configuration: %v", err)
	}
	if document["mcpServers"]["somebody-else"] == nil {
		t.Fatalf("remove took another server with it: %s", data)
	}
	if document["mcpServers"]["kivgraph"] != nil {
		t.Fatalf("remove left our own entry behind: %s", data)
	}
}
