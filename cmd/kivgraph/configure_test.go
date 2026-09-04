package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
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

func TestConfigureRejectsFilesystemRoot(t *testing.T) {
	testsupport.SetHome(t, t.TempDir())
	t.Chdir("/")

	var stdout, stderr bytes.Buffer
	if code := runConfigure([]string{"--target", "codex", "--stdio"}, &stdout, &stderr); code != 1 {
		t.Fatalf("configure at filesystem root exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "filesystem root") {
		t.Fatalf("filesystem-root error = %q", stderr.String())
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
		filepath.Join(project, "AGENTS.md"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("configure dry-run wrote %s: %v", path, err)
		}
	}
	if !strings.Contains(stdout.String(), "would-install") {
		t.Fatalf("configure dry-run output = %q, want plans", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("configure dry-run stderr = %q, want empty", stderr.String())
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
		filepath.Join(project, "AGENTS.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("configure did not install %s: %v; stdout=%q stderr=%q", path, err, stdout.String(), stderr.String())
		}
	}
	for _, want := range []string{"mcp install", "skill install", "hook install", "instructions install"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("configure output = %q, want %q", stdout.String(), want)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("interactive configure stderr = %q, want empty", stderr.String())
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
		"skill skipped for Claude Desktop",
		"instructions skipped",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("configure output = %q, want %q", stdout.String(), want)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "Claude", "claude_desktop_config.json")); err != nil {
		t.Fatalf("Claude Desktop MCP configuration missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); err != nil {
		t.Fatalf("Claude Desktop hook configuration missing: %v", err)
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

func TestConfigureRejectsMalformedResolvedEndpoint(t *testing.T) {
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
	if _, err := os.Stat(filepath.Join(project, "AGENTS.md")); err != nil {
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
	if _, err := os.Stat(filepath.Join(project, "AGENTS.md")); err != nil {
		t.Fatalf("configure did not continue with instructions installation: %v", err)
	}
}

func TestConfigureReportsAnInstructionsDestinationFailure(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	testsupport.SetHome(t, home)
	t.Chdir(project)
	if err := os.WriteFile(filepath.Join(project, ".omp"), []byte("not a directory"), 0o600); err != nil {
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
	if err := os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("<!-- BEGIN KIVGRAPH INSTRUCTIONS -->\n"), 0o600); err != nil {
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
	for _, alias := range []string{"claude", "omp"} {
		targets, err := configureTargets(nil, io.Discard, integrationsManagerForConfigureTest(t), []string{alias})
		if err != nil {
			t.Fatalf("configureTargets(%q) error = %v", alias, err)
		}
		if len(targets) != 1 {
			t.Fatalf("configureTargets(%q) = %v, want one target", alias, targets)
		}
	}
}

func integrationsManagerForConfigureTest(t *testing.T) integrations.Manager {
	t.Helper()
	manager, err := integrations.New(integrations.Options{ProjectDir: t.TempDir()})
	if err != nil {
		t.Fatalf("integrations.New() error = %v", err)
	}
	return manager
}
