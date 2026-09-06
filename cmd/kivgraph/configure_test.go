package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/integrations"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestConfigureRejectsConflictingTransportBeforeWriting(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	testsupport.SetHome(t, home)
	t.Chdir(project)

	var stdout, stderr bytes.Buffer
	if code := runConfigure([]string{"--target", "codex", "--stdio", "--daemon"}, &stdout, &stderr); code != 2 {
		t.Fatalf("configure exit code = %d, want 2; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "--stdio and --daemon") {
		t.Fatalf("configure error = %q, want transport conflict", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "kivgraph", "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("conflicting configure created a config: %v", err)
	}
}

func TestInstallConfigureHooksSkipsUnsupportedTarget(t *testing.T) {
	var target integrations.Target = integrations.TargetClaudeDesktop
	var supportedTargets []integrations.Target
	report := newConfigureReport([]integrations.Target{target}, false)
	var stderr bytes.Buffer
	if failed := installConfigureHooks(
		integrations.Manager{},
		[]integrations.Target{target},
		supportedTargets,
		false,
		false,
		report,
		&stderr,
	); failed {
		t.Fatalf("installConfigureHooks(target=%s, supportedTargets=%v) reported failure: stderr=%q", target, supportedTargets, stderr.String())
	}
	cell := report.targets[0].cells[configureSurfaceHook]
	if cell.state != configureStateNotSupported || cell.text != "not supported" {
		t.Fatalf("installConfigureHooks(target=%s, supportedTargets=%v) cell = %#v, want not supported", target, supportedTargets, cell)
	}
	if stderr.Len() != 0 {
		t.Fatalf("installConfigureHooks(target=%s, supportedTargets=%v) stderr = %q, want empty", target, supportedTargets, stderr.String())
	}
}

func TestConfigureHandlesHelpAndUnexpectedArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runConfigure([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("configure --help exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "kivgraph configure") || stderr.Len() != 0 {
		t.Fatalf("configure --help output = %q stderr=%q", stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runConfigure([]string{"--stdio", "extra"}, &stdout, &stderr); code != 2 {
		t.Fatalf("configure with an extra argument exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unexpected arguments") {
		t.Fatalf("unexpected argument error = %q", stderr.String())
	}
}

func TestConfigureDoesNotRequireAProjectRoot(t *testing.T) {
	testsupport.SetHome(t, t.TempDir())
	t.Chdir("/")

	var stdout, stderr bytes.Buffer
	if code := runConfigure([]string{"--target", "codex", "--stdio", "--dry-run"}, &stdout, &stderr); code != 0 {
		t.Fatalf("configure at filesystem root exit code = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("configure at filesystem root stderr = %q", stderr.String())
	}
}

func TestConfigureRejectsAnInvalidHomeBeforeSelection(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home-file")
	if err := os.WriteFile(home, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	testsupport.SetHome(t, home)
	if err := os.MkdirAll(filepath.Join(root, "project"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(root, "project"))

	var stdout, stderr bytes.Buffer
	if code := runConfigure([]string{"--target", "codex", "--stdio"}, &stdout, &stderr); code != 1 {
		t.Fatalf("configure with an invalid home exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "detect coding agents") {
		t.Fatalf("invalid-home error = %q", stderr.String())
	}
}

func TestConfigureReportsConfigurationInitializationFailure(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	testsupport.SetHome(t, home)
	t.Chdir(project)
	if err := os.WriteFile(filepath.Join(home, ".config"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := runConfigure([]string{"--target", "codex", "--stdio"}, &stdout, &stderr); code != 1 {
		t.Fatalf("configure initialization failure exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "initialize Kivgraph") {
		t.Fatalf("initialization error = %q", stderr.String())
	}
}

func TestConfigureRejectsUnknownAndDuplicateTargets(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown", args: []string{"--target", "editor"}, want: "unsupported target"},
		{name: "duplicate", args: []string{"--target", "codex", "--target", "codex"}, want: "specified more than once"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			testsupport.SetHome(t, home)
			t.Chdir(t.TempDir())

			var stdout, stderr bytes.Buffer
			if code := runConfigure(append(test.args, "--stdio"), &stdout, &stderr); code != 2 {
				t.Fatalf("configure exit code = %d, want 2; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("configure error = %q, want %q", stderr.String(), test.want)
			}
			if _, err := os.Stat(filepath.Join(home, ".config", "kivgraph", "config.yaml")); !os.IsNotExist(err) {
				t.Fatalf("rejected configure created a config: %v", err)
			}
		})
	}
}

func TestConfigureDryRunDoesNotInitializeOrWrite(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	testsupport.SetHome(t, home)
	t.Chdir(project)

	var stdout, stderr bytes.Buffer
	if code := runConfigure([]string{"--target", "codex", "--stdio", "--dry-run"}, &stdout, &stderr); code != 0 {
		t.Fatalf("configure dry-run exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, path := range []string{
		filepath.Join(home, ".config", "kivgraph", "config.yaml"),
		filepath.Join(home, ".codex", "config.toml"),
		filepath.Join(home, ".agents", "skills", "kivgraph", "SKILL.md"),
		filepath.Join(home, ".codex", "hooks.json"),
		filepath.Join(home, ".codex", "AGENTS.md"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("configure dry-run wrote %s: %v", path, err)
		}
	}
	for _, want := range []string{
		"Kivgraph configuration plan",
		"Mode: dry-run; no files changed",
		"will install",
		"Changes: 4 planned.",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("configure dry-run output = %q, want %q", stdout.String(), want)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("configure dry-run stderr = %q, want empty", stderr.String())
	}
}

func TestConfigureNamesExistingComponentsAndCanReplaceThem(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	testsupport.SetHome(t, home)
	t.Chdir(project)

	var stdout, stderr bytes.Buffer
	args := []string{"--target", "codex", "--stdio"}
	if code := runConfigure(args, &stdout, &stderr); code != 0 {
		t.Fatalf("initial configure exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runConfigure(args, &stdout, &stderr); code != 0 {
		t.Fatalf("unchanged configure exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Codex  already configured") {
		t.Fatalf("unchanged configure output = %q, want an already configured status", stdout.String())
	}
	backupPath := filepath.Join(home, ".codex", "config.toml.kivgraph.bak")
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Fatalf("unchanged configure rewrote managed MCP configuration: %v", err)
	}

	question := ""
	stdout.Reset()
	stderr.Reset()
	if code := runConfigureWithResolverAndPrompt(
		args,
		strings.NewReader(""),
		&stdout,
		&stderr,
		integrationManagerOptionsWithInput,
		func(_ io.Reader, _ io.Writer, got string) bool {
			question = got
			return true
		},
	); code != 0 {
		t.Fatalf("replacement configure exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	const replacementQuestion = "Some selected Kivgraph components are already configured. Replace all selected components?"
	if question != replacementQuestion {
		t.Fatalf("replacement question for args %q = %q, want %q", args, question, replacementQuestion)
	}
	if !strings.Contains(stdout.String(), "Changes: 4 applied.") {
		t.Fatalf("replacement configure output = %q, want all Codex components applied", stdout.String())
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("replacement configure did not rewrite managed MCP configuration: %v", err)
	}
}

func TestConfigureInteractiveAppliesAllSupportedSurfaces(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	testsupport.SetHome(t, home)
	t.Chdir(project)

	var stdout, stderr bytes.Buffer
	if code := runConfigureWithInput(
		[]string{"--stdio"},
		strings.NewReader("a\r"),
		&stdout,
		&stderr,
	); code != 0 {
		t.Fatalf("interactive configure exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, path := range []string{
		filepath.Join(home, ".config", "kivgraph", "config.yaml"),
		filepath.Join(home, ".codex", "config.toml"),
		filepath.Join(home, ".agents", "skills", "kivgraph", "SKILL.md"),
		filepath.Join(home, ".codex", "hooks.json"),
		filepath.Join(home, ".codex", "AGENTS.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("configure did not install %s: %v; stdout=%q stderr=%q", path, err, stdout.String(), stderr.String())
		}
	}
	for _, want := range []string{
		"Kivgraph configured",
		"Search guard",
		"Claude Code     installed",
		"Claude Desktop  installed  not supported  already configured  shared",
		"Codex           installed",
		"Changes: 17 applied, 1 already configured, 1 not supported.",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("configure output = %q, want %q", stdout.String(), want)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("interactive configure stderr = %q, want empty", stderr.String())
	}

	question := ""
	stdout.Reset()
	stderr.Reset()
	if code := runConfigureWithResolverAndPrompt(
		[]string{
			"--target", "claude-code",
			"--target", "claude-desktop",
			"--target", "codex",
			"--target", "opencode",
			"--target", "oh-my-pi",
			"--stdio",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
		integrationManagerOptionsWithInput,
		func(_ io.Reader, _ io.Writer, got string) bool {
			question = got
			return true
		},
	); code != 0 {
		t.Fatalf("replacement configure exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	const replacementQuestion = "Some selected Kivgraph components are already configured. Replace all selected components?"
	if question != replacementQuestion {
		t.Fatalf("replacement question for all selected targets = %q, want %q", question, replacementQuestion)
	}
	if !strings.Contains(stdout.String(), "Changes: 18 applied, 1 not supported.") {
		t.Fatalf("replacement configure output = %q, want every supported component applied", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("replacement configure stderr = %q, want empty", stderr.String())
	}
}

func TestConfigureSummarizesSelectedAgentsWithoutImplementationPaths(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	testsupport.SetHome(t, home)
	t.Chdir(project)

	var stdout, stderr bytes.Buffer
	if code := runConfigure(
		[]string{"--target", "codex", "--target", "claude-desktop", "--stdio"},
		&stdout,
		&stderr,
	); code != 0 {
		t.Fatalf("configure exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"Kivgraph configured",
		"Agent           MCP",
		"Codex           installed",
		"Claude Desktop  installed",
		"not supported",
		"Changes:",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("configure output = %q, want %q", output, want)
		}
	}
	for _, unexpected := range []string{
		filepath.Join(home, ".codex", "config.toml"),
		"mcp install --target",
		"skill install --target",
		"hook install --target",
		"instructions install --agent",
	} {
		if strings.Contains(output, unexpected) {
			t.Fatalf("configure output exposes implementation detail %q: %q", unexpected, output)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("configure stderr = %q, want empty", stderr.String())
	}
}

func TestConfigureSkipsSurfacesUnsupportedByClaudeDesktop(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	testsupport.SetHome(t, home)
	t.Chdir(project)

	var stdout, stderr bytes.Buffer
	if code := runConfigure([]string{"--target", "claude-desktop", "--stdio"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Claude Desktop configure exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{
		"Claude Desktop  installed  not supported  installed     installed",
		"Changes: 3 applied, 1 not supported.",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("configure output = %q, want %q", stdout.String(), want)
		}
	}
	manager, err := integrations.New(integrations.Options{HomeDir: home, ProjectDir: project})
	if err != nil {
		t.Fatalf("create manager for configured paths: %v", err)
	}
	mcpPlan, err := manager.StatusMCP(integrations.TargetClaudeDesktop, integrations.ScopeUser)
	if err != nil {
		t.Fatalf("inspect Claude Desktop MCP configuration: %v", err)
	}
	if mcpPlan.Status != "managed" {
		t.Fatalf("StatusMCP(target=claude-desktop, scope=user) = %#v, want managed", mcpPlan)
	}
	wantMCPPath := configureClaudeDesktopMCPPath(home)
	if mcpPlan.Path != wantMCPPath {
		t.Fatalf("StatusMCP(target=claude-desktop, scope=user) path = %q, want %q", mcpPlan.Path, wantMCPPath)
	}
	mcpData, err := os.ReadFile(wantMCPPath)
	if err != nil {
		t.Fatalf("Claude Desktop MCP configuration missing at %s: %v", wantMCPPath, err)
	}
	if !bytes.Contains(mcpData, []byte(`"kivgraph"`)) {
		t.Fatalf("Claude Desktop MCP configuration at %s lacks the managed entry: %q", wantMCPPath, mcpData)
	}
	hookPlan, err := manager.StatusHook(integrations.TargetClaudeDesktop, integrations.ScopeUser)
	if err != nil {
		t.Fatalf("StatusHook(target=claude-desktop, scope=user) error: %v", err)
	}
	if hookPlan.Status != "managed" {
		t.Fatalf("StatusHook(target=claude-desktop, scope=user) = %#v, want managed", hookPlan)
	}
	wantHookPath := filepath.Join(home, ".claude", "settings.json")
	if hookPlan.Path != wantHookPath {
		t.Fatalf("StatusHook(target=claude-desktop, scope=user) path = %q, want %q", hookPlan.Path, wantHookPath)
	}
	hookData, err := os.ReadFile(wantHookPath)
	if err != nil {
		t.Fatalf("Claude Desktop hook configuration missing at %s: %v", wantHookPath, err)
	}
	if !bytes.Contains(hookData, []byte("hook run")) {
		t.Fatalf("Claude Desktop hook configuration at %s lacks the managed command: %q", wantHookPath, hookData)
	}
	for _, path := range []string{
		filepath.Join(home, ".agents", "skills", "kivgraph", "SKILL.md"),
		filepath.Join(project, "AGENTS.md"),
		filepath.Join(project, "CLAUDE.md"),
		filepath.Join(project, ".omp", "AGENTS.md"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unsupported Claude Desktop surface created %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "CLAUDE.md")); err != nil {
		t.Fatalf("Claude Desktop user instructions missing: %v", err)
	}
}

func TestConfigureContinuesAfterAnMCPInstallFailure(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	testsupport.SetHome(t, home)
	t.Chdir(project)
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("["), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := runConfigure([]string{"--target", "codex", "--stdio"}, &stdout, &stderr); code != 1 {
		t.Fatalf("configure MCP failure exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "configure mcp --target codex") {
		t.Fatalf("MCP failure = %q", stderr.String())
	}
	for _, want := range []string{
		"Kivgraph configuration incomplete",
		"Codex  failed  installed  installed     installed",
		"Changes: 3 applied, 1 failed; see errors above.",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("MCP failure summary = %q, want %q", stdout.String(), want)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "kivgraph", "SKILL.md")); err != nil {
		t.Fatalf("configure did not continue with skill installation: %v", err)
	}
}

func TestConfigureContinuesWhenMCPTransportSetupFails(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	testsupport.SetHome(t, home)
	t.Chdir(project)

	resolver := func(_ integrationOptions, _ bool, _ io.Reader, _ io.Writer) (integrations.Options, error) {
		return integrations.Options{}, errors.New("supervisor unavailable")
	}
	var stdout, stderr bytes.Buffer
	if code := runConfigureWithResolver(
		[]string{"--target", "codex", "--stdio"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		resolver,
	); code != 1 {
		t.Fatalf("configure transport failure exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "configure mcp: supervisor unavailable") {
		t.Fatalf("transport failure = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("configure wrote MCP configuration after transport failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "kivgraph", "SKILL.md")); err != nil {
		t.Fatalf("configure did not continue with skill installation: %v", err)
	}
}

func TestConfigureRejectsIncompleteResolvedEndpoint(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	testsupport.SetHome(t, home)
	t.Chdir(project)

	resolver := func(_ integrationOptions, _ bool, _ io.Reader, _ io.Writer) (integrations.Options, error) {
		return integrations.Options{Endpoint: integrations.Endpoint{URL: "http://127.0.0.1:7788"}}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runConfigureWithResolver(
		[]string{"--target", "codex", "--stdio"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		resolver,
	); code != 1 {
		t.Fatalf("configure malformed endpoint exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "create integration manager") {
		t.Fatalf("malformed endpoint error = %q", stderr.String())
	}
}

func TestConfigureContinuesAfterASkillInstallFailure(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	testsupport.SetHome(t, home)
	t.Chdir(project)
	path := filepath.Join(home, ".agents", "skills", "kivgraph", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("a foreign skill"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := runConfigure([]string{"--target", "codex", "--stdio"}, &stdout, &stderr); code != 1 {
		t.Fatalf("configure skill failure exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "configure skill --target codex") {
		t.Fatalf("skill failure = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "AGENTS.md")); err != nil {
		t.Fatalf("configure did not continue with instructions installation: %v", err)
	}
}

func TestConfigureContinuesAfterAHookInstallFailure(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	testsupport.SetHome(t, home)
	t.Chdir(project)
	path := filepath.Join(home, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := runConfigure([]string{"--target", "codex", "--stdio"}, &stdout, &stderr); code != 1 {
		t.Fatalf("configure hook failure exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "configure hook --target codex") {
		t.Fatalf("hook failure = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "AGENTS.md")); err != nil {
		t.Fatalf("configure did not continue with instructions installation: %v", err)
	}
}

func TestConfigureReportsAnInstructionsDestinationFailure(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	testsupport.SetHome(t, home)
	t.Chdir(project)
	if err := os.WriteFile(filepath.Join(home, ".omp"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := runConfigure([]string{"--target", "oh-my-pi", "--stdio"}, &stdout, &stderr); code != 1 {
		t.Fatalf("configure instructions destination failure exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "configure instructions") {
		t.Fatalf("instructions destination failure = %q", stderr.String())
	}
}

func TestConfigureReportsAnInstructionsInstallFailure(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	testsupport.SetHome(t, home)
	t.Chdir(project)
	path := filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("<!-- BEGIN KIVGRAPH INSTRUCTIONS -->\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := runConfigure([]string{"--target", "codex", "--stdio"}, &stdout, &stderr); code != 1 {
		t.Fatalf("configure instructions failure exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "configure instructions --agent codex") {
		t.Fatalf("instructions install failure = %q", stderr.String())
	}
}

func TestConfigureAcceptsAgentAliases(t *testing.T) {
	for alias, want := range map[string]integrations.Target{
		"claude": integrations.TargetClaudeCode,
		"omp":    integrations.TargetOhMyPi,
	} {
		targets, err := configureTargets(nil, io.Discard, integrationsManagerForConfigureTest(t), []string{alias})
		if err != nil {
			t.Fatalf("configureTargets(%q) error = %v", alias, err)
		}
		if len(targets) != 1 || targets[0] != want {
			t.Fatalf("configureTargets(%q) = %v, want [%s]", alias, targets, want)
		}
	}
}

func configureClaudeDesktopMCPPath(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "windows":
		roaming := os.Getenv("APPDATA")
		if strings.TrimSpace(roaming) == "" {
			roaming = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(roaming, "Claude", "claude_desktop_config.json")
	default:
		return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json")
	}
}

func integrationsManagerForConfigureTest(t *testing.T) integrations.Manager {
	t.Helper()
	manager, err := integrations.New(integrations.Options{
		HomeDir:    testsupport.TempDir(t),
		ProjectDir: testsupport.TempDir(t),
	})
	if err != nil {
		t.Fatalf("integrations.New() error = %v", err)
	}
	return manager
}
