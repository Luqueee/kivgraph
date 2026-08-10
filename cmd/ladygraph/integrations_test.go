package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMCPInstallStatusRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"ladygraph", "mcp", "install", "--target", "oh-my-pi"}, &stdout, &stderr); code != 0 {
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
	if code := run([]string{"ladygraph", "mcp", "status", "--target", "oh-my-pi"}, &stdout, &stderr); code != 0 {
		t.Fatalf("status exit code = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "managed") {
		t.Fatalf("status output = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"ladygraph", "mcp", "remove", "--target", "oh-my-pi"}, &stdout, &stderr); code != 0 {
		t.Fatalf("remove exit code = %d, stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(path + ".ladygraph.bak"); err != nil {
		t.Fatalf("MCP backup missing: %v", err)
	}
}

func TestRunSkillInstallDryRunDoesNotWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"ladygraph", "skill", "install", "--target", "codex", "--dry-run"}, &stdout, &stderr); code != 0 {
		t.Fatalf("skill dry-run exit code = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "would-install") {
		t.Fatalf("skill dry-run output = %q", stdout.String())
	}
	path := filepath.Join(home, ".agents", "skills", "ladygraph", "SKILL.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("skill dry-run wrote %s: %v", path, err)
	}
}
