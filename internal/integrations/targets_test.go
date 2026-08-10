package integrations

import (
	"os"
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
func TestDetectTargetsUsesKnownMarkers(t *testing.T) {
	manager, home, project := testManager(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".opencode"), 0o700); err != nil {
		t.Fatal(err)
	}

	mcp, err := manager.DetectMCPTargets(ScopeUser)
	if err != nil {
		t.Fatalf("DetectMCPTargets() error = %v", err)
	}
	detected := make(map[Target]bool, len(mcp))
	for _, target := range mcp {
		detected[target.Target] = target.Detected
	}
	if !detected[TargetClaudeCode] || !detected[TargetCodex] || detected[TargetOpenCode] {
		t.Fatalf("MCP detections = %#v, want Claude Code and Codex only", mcp)
	}

	skills, err := manager.DetectSkillTargets(ScopeProject)
	if err != nil {
		t.Fatalf("DetectSkillTargets() error = %v", err)
	}
	for _, target := range skills {
		if target.Target == TargetClaudeDesktop {
			t.Fatalf("skill detections include unsupported Claude Desktop: %#v", skills)
		}
		if target.Target == TargetOpenCode && !target.Detected {
			t.Fatalf("skill detections = %#v, want OpenCode detected", skills)
		}
	}
}

func TestClaudeDesktopDoesNotOfferSkillTarget(t *testing.T) {
	manager, _, _ := testManager(t)
	_, err := manager.StatusSkill(TargetClaudeDesktop, ScopeUser)
	if err == nil || !strings.Contains(err.Error(), "local skill") {
		t.Fatalf("StatusSkill() error = %v, want local-skill rejection", err)
	}
}
