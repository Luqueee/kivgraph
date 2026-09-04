package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/integrations"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestRunInstructionsInstallRejectsUnknownAgent(t *testing.T) {
	project := testsupport.TempDir(t)
	t.Chdir(project)
	testsupport.SetHome(t, testsupport.TempDir(t))

	var stdout, stderr bytes.Buffer
	if code := run([]string{"kivgraph", "instructions", "install", "--agent", "unknown-agent"}, &stdout, &stderr); code == 0 {
		t.Fatalf("unknown agent exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "claude-code") || !strings.Contains(stderr.String(), "oh-my-pi") {
		t.Fatalf("unknown agent error = %q, want supported agents", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(project, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("unknown agent created AGENTS.md: %v", err)
	}
}

func TestRunInstructionsInstallRejectsAgentAndFileTogether(t *testing.T) {
	project := testsupport.TempDir(t)
	t.Chdir(project)
	testsupport.SetHome(t, testsupport.TempDir(t))

	var stdout, stderr bytes.Buffer
	if code := run([]string{"kivgraph", "instructions", "install", "--agent", "codex", "--file", "CLAUDE.md"}, &stdout, &stderr); code == 0 {
		t.Fatalf("conflicting options exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "cannot be combined") {
		t.Fatalf("conflicting options error = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(project, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("conflicting options created AGENTS.md: %v", err)
	}
}

func TestRunInstructionsCommandRejectsUnknownOperation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"kivgraph", "instructions", "remove"}, &stdout, &stderr); code != 2 {
		t.Fatalf("unknown operation exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown operation") {
		t.Fatalf("unknown operation stderr = %q", stderr.String())
	}
}

func TestRunInstructionsInstallRejectsUnexpectedArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runInstructionsInstallWithInput([]string{"extra"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("unexpected argument exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unexpected arguments") {
		t.Fatalf("unexpected argument stderr = %q", stderr.String())
	}
}

func TestRunInstructionsInstallRejectsUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runInstructionsInstallWithInput([]string{"--unknown"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("unknown flag exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("unknown flag %v stderr = %q", []string{"--unknown"}, stderr.String())
	}
}

func TestRunInstructionsInstallUsesProjectRoot(t *testing.T) {
	home := testsupport.TempDir(t)
	project := testsupport.TempDir(t)
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(project, "src")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	testsupport.SetHome(t, home)

	var stdout, stderr bytes.Buffer
	if code := runInstructionsInstallWithInput(
		nil,
		strings.NewReader("n \x1b[B \r"),
		&stdout,
		&stderr,
	); code != 0 {
		t.Fatalf("install exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "installed") || stderr.Len() != 0 {
		t.Fatalf("install output = stdout %q stderr %q", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(project, "AGENTS.md")); err != nil {
		t.Fatalf("project-root instructions missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(nested, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("instructions were written below project root: %v", err)
	}
}

func TestRunInstructionsInstallWithoutAgentSelectsMultipleAgentsOncePerFile(t *testing.T) {
	project := testsupport.TempDir(t)
	t.Chdir(project)
	testsupport.SetHome(t, testsupport.TempDir(t))

	var stdout, stderr bytes.Buffer
	if code := runInstructionsInstallWithInput(
		nil,
		strings.NewReader("a\r"),
		&stdout,
		&stderr,
	); code != 0 {
		t.Fatalf("interactive install exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("interactive install stderr = %q", stderr.String())
	}
	for _, file := range []string{"AGENTS.md", "CLAUDE.md", filepath.Join(".omp", "AGENTS.md")} {
		data, err := os.ReadFile(filepath.Join(project, file))
		if err != nil {
			t.Fatalf("interactive install did not write %s: %v", file, err)
		}
		if got := strings.Count(string(data), "BEGIN KIVGRAPH INSTRUCTIONS"); got != 1 {
			t.Fatalf("%s contains %d managed blocks, want 1", file, got)
		}
	}
	if got := strings.Count(stdout.String(), "installed"); got != 3 {
		t.Fatalf("interactive install output has %d installed plans, want 3: %q", got, stdout.String())
	}
}

func TestRunInstructionsInstallWithoutAgentCanCancel(t *testing.T) {
	project := testsupport.TempDir(t)
	t.Chdir(project)
	testsupport.SetHome(t, testsupport.TempDir(t))

	var stdout, stderr bytes.Buffer
	if code := runInstructionsInstallWithInput(nil, strings.NewReader("q"), &stdout, &stderr); code != 2 {
		t.Fatalf("cancelled interactive install exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "selection cancelled") {
		t.Fatalf("cancelled interactive install stderr = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(project, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("cancelled interactive install wrote AGENTS.md: %v", err)
	}
}

func TestRunInstructionsInstallDeduplicatesTheAcceptedClaudeSymlink(t *testing.T) {
	skipWindowsSymlinkTest(t)
	project := testsupport.TempDir(t)
	t.Chdir(project)
	testsupport.SetHome(t, testsupport.TempDir(t))
	if err := os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("# project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("AGENTS.md", filepath.Join(project, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := runInstructionsInstallWithInput(nil, strings.NewReader("\r"), &stdout, &stderr); code != 0 {
		t.Fatalf("interactive symlink install exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if got := strings.Count(stdout.String(), "installed"); got != 1 {
		t.Fatalf("interactive symlink install output has %d installed plans, want 1: %q", got, stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(project, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "BEGIN KIVGRAPH INSTRUCTIONS"); got != 1 {
		t.Fatalf("AGENTS.md contains %d managed blocks, want 1", got)
	}
	linkTarget, err := os.Readlink(filepath.Join(project, "CLAUDE.md"))
	if err != nil || linkTarget != "AGENTS.md" {
		t.Fatalf("CLAUDE.md link target = %q, error = %v", linkTarget, err)
	}
}

func TestRunInstructionsInstallReportsSelectedFileErrors(t *testing.T) {
	project := testsupport.TempDir(t)
	t.Chdir(project)
	testsupport.SetHome(t, testsupport.TempDir(t))
	if err := os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("<!-- BEGIN KIVGRAPH INSTRUCTIONS -->\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := runInstructionsInstallWithInput(
		nil,
		strings.NewReader("n \x1b[B \r"),
		&stdout,
		&stderr,
	); code != 1 {
		t.Fatalf("invalid selected file exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "--agent codex") || !strings.Contains(stderr.String(), "malformed") {
		t.Fatalf("invalid selected file stderr = %q", stderr.String())
	}
}

func TestRunInstructionsInstallReportsExplicitDestinationSelectors(t *testing.T) {
	for _, test := range []struct {
		name     string
		args     []string
		selector string
	}{
		{name: "agent", args: []string{"--agent", "codex"}, selector: "--agent codex"},
		{name: "file", args: []string{"--file", "AGENTS.md"}, selector: "--file AGENTS.md"},
	} {
		t.Run(test.name, func(t *testing.T) {
			project := testsupport.TempDir(t)
			t.Chdir(project)
			testsupport.SetHome(t, testsupport.TempDir(t))
			if err := os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("<!-- BEGIN KIVGRAPH INSTRUCTIONS -->\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			var stdout, stderr bytes.Buffer
			args := append([]string{"kivgraph", "instructions", "install"}, test.args...)
			if code := run(args, &stdout, &stderr); code != 1 {
				t.Fatalf("%v exit code = %d, stdout=%q stderr=%q", test.args, code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), test.selector) || !strings.Contains(stderr.String(), "malformed") {
				t.Fatalf("%v stderr = %q, want selector %q and malformed error", test.args, stderr.String(), test.selector)
			}
		})
	}
}

func TestInstructionsDestinationHelpers(t *testing.T) {
	skipWindowsSymlinkTest(t)
	project := t.TempDir()
	manager, err := integrations.New(integrations.Options{
		HomeDir:    t.TempDir(),
		ProjectDir: project,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instructionsDestinations(manager, instructionsOptions{}, []integrations.Target{"unknown-agent"}); err == nil {
		t.Fatal("instructionsDestinations() accepted an unknown target")
	}
	if err := os.Symlink("outside.md", filepath.Join(project, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := instructionsDestinations(manager, instructionsOptions{Agent: "codex"}, nil); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("instructionsDestinations() error = %v, want symlink rejection", err)
	}
	if _, err := instructionsDestinations(manager, instructionsOptions{}, []integrations.Target{integrations.TargetCodex}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("instructionsDestinations() target error = %v, want symlink rejection", err)
	}
	var output bytes.Buffer
	writeInstructionsPlan(&output, integrations.InstructionsPlan{
		File: "AGENTS.md", Path: "AGENTS.md", Status: "managed", Detail: "already present",
	}, "")
	if !strings.Contains(output.String(), "--file AGENTS.md") {
		t.Fatalf("managed plan output = %q", output.String())
	}
}

func skipWindowsSymlinkTest(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symbolic-link tests require Windows link privileges")
	}
}

func TestRunInstructionsInstallSelectsAgentContextFile(t *testing.T) {
	tests := map[string]string{
		"claude":      "CLAUDE.md",
		"claude-code": "CLAUDE.md",
		"codex":       "AGENTS.md",
		"omp":         ".omp/AGENTS.md",
		"oh-my-pi":    ".omp/AGENTS.md",
		"opencode":    "AGENTS.md",
	}
	for agent, file := range tests {
		t.Run(agent, func(t *testing.T) {
			project := testsupport.TempDir(t)
			t.Chdir(project)
			testsupport.SetHome(t, testsupport.TempDir(t))

			var stdout, stderr bytes.Buffer
			args := []string{"kivgraph", "instructions", "install", "--agent", agent}
			if code := run(args, &stdout, &stderr); code != 0 {
				t.Fatalf("%s exit code = %d, stdout=%q stderr=%q", agent, code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), "--agent "+agent) || stderr.Len() != 0 {
				t.Fatalf("%s output = stdout %q stderr %q", agent, stdout.String(), stderr.String())
			}
			if _, err := os.Stat(filepath.Join(project, file)); err != nil {
				t.Fatalf("%s context file missing: %v", agent, err)
			}
			other := "AGENTS.md"
			if file == other {
				other = "CLAUDE.md"
			}
			if _, err := os.Stat(filepath.Join(project, other)); !os.IsNotExist(err) {
				t.Fatalf("%s also wrote %s: %v", agent, other, err)
			}
		})
	}
}

func TestRunInstructionsInstallSupportsClaudeFileAndDryRun(t *testing.T) {
	project := testsupport.TempDir(t)
	t.Chdir(project)
	testsupport.SetHome(t, testsupport.TempDir(t))

	var stdout, stderr bytes.Buffer
	if code := run([]string{"kivgraph", "instructions", "install", "--file", "CLAUDE.md", "--dry-run"}, &stdout, &stderr); code != 0 {
		t.Fatalf("dry-run exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "would-install") {
		t.Fatalf("dry-run output = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(project, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created CLAUDE.md: %v", err)
	}
}

func TestInstructionsCommandHelpIsAvailable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"kivgraph", "instructions", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "instructions install") || !strings.Contains(stdout.String(), "AGENTS.md") {
		t.Fatalf("instructions help = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("instructions help wrote stderr = %q", stderr.String())
	}
}
