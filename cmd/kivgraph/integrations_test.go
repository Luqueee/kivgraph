package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/integrations"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestRunMCPInstallStatusRemove(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"kivgraph", "mcp", "install", "--target", "oh-my-pi"}, &stdout, &stderr); code != 0 {
		t.Fatalf("install exit code = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "installed") {
		t.Fatalf("install output = %q", stdout.String())
	}
	path := filepath.Join(home, ".omp", "agent", "mcp.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("installed MCP config missing: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"kivgraph", "mcp", "status", "--target", "oh-my-pi"}, &stdout, &stderr); code != 0 {
		t.Fatalf("status exit code = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "managed") {
		t.Fatalf("status output = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"kivgraph", "mcp", "remove", "--target", "oh-my-pi"}, &stdout, &stderr); code != 0 {
		t.Fatalf("remove exit code = %d, stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(path + ".kivgraph.bak"); err != nil {
		t.Fatalf("MCP backup missing: %v", err)
	}
}

func TestRunSkillInstallDryRunDoesNotWrite(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"kivgraph", "skill", "install", "--target", "codex", "--dry-run"}, &stdout, &stderr); code != 0 {
		t.Fatalf("skill dry-run exit code = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "would-install") {
		t.Fatalf("skill dry-run output = %q", stdout.String())
	}
	path := filepath.Join(home, ".agents", "skills", "kivgraph", "SKILL.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("skill dry-run wrote %s: %v", path, err)
	}
}
func TestRunMCPInstallInteractiveSelectsDetectedAgents(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	for _, directory := range []string{filepath.Join(home, ".claude"), filepath.Join(home, ".codex")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	if code := runMCPChangeWithInput(
		integrations.ActionInstall,
		nil,
		strings.NewReader("n \x1b[B\x1b[B \r"),
		&stdout,
		&stderr,
	); code != 0 {
		t.Fatalf("interactive MCP install exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, path := range []string{
		filepath.Join(home, ".claude.json"),
		filepath.Join(home, ".codex", "config.toml"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("interactive MCP install did not write %s: %v", path, err)
		}
	}
	if !strings.Contains(stdout.String(), "installed") {
		t.Fatalf("interactive MCP install output = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("interactive MCP install stderr = %q, want empty", stderr.String())
	}
}

func TestRunSkillInstallInteractiveFallsBackToSupportedAgents(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)

	var stdout, stderr bytes.Buffer
	if code := runSkillChangeWithInput(
		integrations.ActionInstall,
		nil,
		strings.NewReader("\x1b[B \r"),
		&stdout,
		&stderr,
	); code != 0 {
		t.Fatalf("interactive skill install exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	path := filepath.Join(home, ".agents", "skills", "kivgraph", "SKILL.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("interactive skill install did not write %s: %v", path, err)
	}
	if !strings.Contains(stdout.String(), "installed") {
		t.Fatalf("interactive skill install output = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("interactive skill install stderr = %q, want empty", stderr.String())
	}
}
func TestRunMCPInstallInteractiveCanCancel(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)

	var stdout, stderr bytes.Buffer
	if code := runMCPChangeWithInput(
		integrations.ActionInstall,
		nil,
		strings.NewReader("q"),
		&stdout,
		&stderr,
	); code != 2 {
		t.Fatalf("cancelled interactive install exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "selection cancelled") {
		t.Fatalf("cancelled interactive install stderr = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Fatalf("cancelled interactive install wrote configuration: %v", err)
	}
}

// TestSkillCompletesOnlyTargetsItAccepts is the sibling of the hook regression:
// pressing tab offered claude-desktop, which reads no local skill directory and
// which `skill install` then refuses. A completion is a promise about what the
// next word may be.
func TestSkillCompletesOnlyTargetsItAccepts(t *testing.T) {
	candidates := completionCandidates([]string{"skill", "install", "--target", ""})
	if len(candidates) != len(integrations.SkillTargets()) {
		t.Fatalf("completion offers %q, want the %d targets that take a skill",
			candidates, len(integrations.SkillTargets()))
	}
	for _, candidate := range candidates {
		if candidate == string(integrations.TargetClaudeDesktop) {
			t.Fatal("completion offers claude-desktop, which skill install refuses")
		}
	}
}
