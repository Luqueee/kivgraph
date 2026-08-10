package integrations

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPUserTargetPaths(t *testing.T) {
	manager, home, _ := testManager(t)
	tests := []struct {
		target Target
		suffix string
	}{
		{TargetClaudeCode, filepath.Join(".claude.json")},
		{TargetClaudeDesktop, filepath.Join("Library", "Application Support", "Claude", "claude_desktop_config.json")},
		{TargetCodex, filepath.Join(".codex", "config.toml")},
		{TargetOpenCode, filepath.Join(".config", "opencode", "opencode.json")},
		{TargetOhMyPi, filepath.Join(".omp", "agent", "mcp.json")},
	}
	for _, test := range tests {
		t.Run(string(test.target), func(t *testing.T) {
			plan, err := manager.StatusMCP(test.target, ScopeUser)
			if err != nil {
				t.Fatalf("StatusMCP() error = %v", err)
			}
			if plan.Status != "absent" || plan.Path != filepath.Join(home, test.suffix) {
				t.Fatalf("status plan = %#v, want absent at %s", plan, filepath.Join(home, test.suffix))
			}
		})
	}
}

func TestClaudeDesktopDoesNotOfferSkillTarget(t *testing.T) {
	manager, _, _ := testManager(t)
	_, err := manager.StatusSkill(TargetClaudeDesktop, ScopeUser)
	if err == nil || !strings.Contains(err.Error(), "local skill") {
		t.Fatalf("StatusSkill() error = %v, want local-skill rejection", err)
	}
}
