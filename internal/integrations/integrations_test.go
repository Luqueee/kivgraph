package integrations

import (
	"encoding/json"
	"errors"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testManager(t *testing.T) (Manager, string, string) {
	t.Helper()
	home := t.TempDir()
	project := t.TempDir()
	manager, err := New(Options{
		HomeDir:    home,
		ProjectDir: project,
		Executable: testsupport.InstalledExecutable(),
		GOOS:       "darwin",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return manager, home, project
}

func TestInstallJSONIsIdempotentAndBacksUpOnRemoval(t *testing.T) {
	manager, home, _ := testManager(t)
	path := filepath.Join(home, ".claude.json")

	plan, err := manager.InstallMCP(TargetClaudeCode, ScopeUser, false, false)
	if err != nil {
		t.Fatalf("InstallMCP() error = %v", err)
	}
	if plan.Status != "installed" || !plan.Changed {
		t.Fatalf("install plan = %#v", plan)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(first), `"command": "`+escapedPath(t, testsupport.InstalledExecutable())+`"`) {
		t.Fatalf("installed JSON does not contain executable: %s", first)
	}
	if mode := fileMode(t, path); mode.Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 600", mode.Perm())
	}

	plan, err = manager.InstallMCP(TargetClaudeCode, ScopeUser, false, false)
	if err != nil {
		t.Fatalf("idempotent InstallMCP() error = %v", err)
	}
	if plan.Status != "managed" || plan.Changed {
		t.Fatalf("idempotent plan = %#v", plan)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("second ReadFile() error = %v", err)
	}
	if string(second) != string(first) {
		t.Fatal("idempotent install rewrote the configuration")
	}

	plan, err = manager.RemoveMCP(TargetClaudeCode, ScopeUser, false, false)
	if err != nil {
		t.Fatalf("RemoveMCP() error = %v", err)
	}
	if plan.Status != "removed" {
		t.Fatalf("remove plan = %#v", plan)
	}
	if _, err := os.Stat(path + ".kivgraph.bak"); err != nil {
		t.Fatalf("backup missing after removal: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config missing after removal: %v", err)
	}
}

func TestIncompatibleJSONRequiresForceAndPreservesBackup(t *testing.T) {
	manager, home, _ := testManager(t)
	path := filepath.Join(home, ".claude.json")
	original := []byte(`{"mcpServers":{"kivgraph":{"command":"/other/kivgraph","args":["serve"]}},"custom":true}` + "\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.InstallMCP(TargetClaudeCode, ScopeUser, false, false); err == nil {
		t.Fatal("incompatible install succeeded without --force")
	}
	if _, err := manager.InstallMCP(TargetClaudeCode, ScopeUser, false, true); err != nil {
		t.Fatalf("forced install error = %v", err)
	}
	backup, err := os.ReadFile(path + ".kivgraph.bak")
	if err != nil {
		t.Fatalf("ReadFile(backup) error = %v", err)
	}
	if string(backup) != string(original) {
		t.Fatal("backup does not preserve the original incompatible configuration")
	}
}

func TestSymlinkDestinationIsRejected(t *testing.T) {
	manager, home, _ := testManager(t)
	path := filepath.Join(home, ".claude.json")
	target := filepath.Join(home, "outside.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	_, err := manager.InstallMCP(TargetClaudeCode, ScopeUser, false, false)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("InstallMCP() error = %v, want symlink rejection", err)
	}
}

func TestCodexTOMLPreservesUnmanagedContent(t *testing.T) {
	manager, home, _ := testManager(t)
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := "model = \"gpt\"\n\n[other]\nvalue = 7\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.InstallMCP(TargetCodex, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP(codex) error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `model = "gpt"`) || !strings.Contains(string(data), "[other]\nvalue = 7") || !strings.Contains(string(data), "[mcp_servers.kivgraph]") {
		t.Fatalf("Codex TOML lost content or entry: %s", data)
	}
	status, err := manager.StatusMCP(TargetCodex, ScopeUser)
	if err != nil || status.Status != "managed" {
		t.Fatalf("StatusMCP(codex) = %#v, %v", status, err)
	}
	if _, err := manager.RemoveMCP(TargetCodex, ScopeUser, false, false); err != nil {
		t.Fatalf("RemoveMCP(codex) error = %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Fatalf("Codex TOML after removal = %q, want %q", data, original)
	}
}

func TestSkillInstallConflictAndProjectPath(t *testing.T) {
	manager, _, project := testManager(t)
	path := filepath.Join(project, ".agents", "skills", "kivgraph", "SKILL.md")
	if _, err := manager.InstallSkill(TargetCodex, ScopeProject, true, false); err != nil {
		t.Fatalf("dry-run skill install error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run created skill: %v", err)
	}
	plan, err := manager.InstallSkill(TargetCodex, ScopeProject, false, false)
	if err != nil {
		t.Fatalf("InstallSkill() error = %v", err)
	}
	if plan.Status != "installed" {
		t.Fatalf("skill plan = %#v", plan)
	}
	status, err := manager.StatusSkill(TargetCodex, ScopeProject)
	if err != nil || status.Status != "managed" {
		t.Fatalf("StatusSkill() = %#v, %v", status, err)
	}
	if err := os.WriteFile(path, []byte("user content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RemoveSkill(TargetCodex, ScopeProject, false, false); err == nil {
		t.Fatal("incompatible skill removal succeeded without --force")
	}
	if _, err := manager.RemoveSkill(TargetCodex, ScopeProject, false, true); err != nil {
		t.Fatalf("forced skill removal error = %v", err)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}

// TestTheAdvertisedTargetListsMatchWhatEachKindAccepts keeps a name from being
// offered by help or completion and then refused by the command behind it. The
// lists are derived from the resolvers rather than restated, so a target that
// gains or loses support cannot leave one of them stale.
func TestTheAdvertisedTargetListsMatchWhatEachKindAccepts(t *testing.T) {
	manager, _, _ := testManager(t)
	for _, kind := range []struct {
		name       string
		advertised []Target
		resolve    func(Target) error
	}{
		{"skill", SkillTargets(), func(target Target) error {
			_, err := manager.skillPath(target, ScopeUser)
			return err
		}},
		{"hook", HookTargets(), func(target Target) error {
			_, err := manager.hookDocumentFor(target, ScopeUser)
			return err
		}},
	} {
		t.Run(kind.name, func(t *testing.T) {
			advertised := map[Target]bool{}
			for _, target := range kind.advertised {
				advertised[target] = true
				if err := kind.resolve(target); err != nil {
					t.Fatalf("%s advertises %q and refuses it: %v", kind.name, target, err)
				}
			}
			for _, target := range KnownTargets() {
				if advertised[target] {
					continue
				}
				if err := kind.resolve(target); err == nil {
					t.Fatalf("%s accepts %q and does not advertise it", kind.name, target)
				}
			}
		})
	}
}

// escapedPath is the path as it appears inside a JSON or TOML string.
//
// A Windows path is mostly backslashes and both formats escape a backslash the
// same way, so the encoder answers for both. Writing the expectation by hand
// would mean writing the escaping by hand, which is the thing under test one
// file over.
func escapedPath(t *testing.T, path string) string {
	t.Helper()
	encoded, err := json.Marshal(path)
	if err != nil {
		t.Fatalf("encode %q: %v", path, err)
	}
	return string(encoded[1 : len(encoded)-1])
}
