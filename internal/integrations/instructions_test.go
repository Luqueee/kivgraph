package integrations

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tailscale/hujson"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func testInstructionsManager(t *testing.T) (Manager, string) {
	t.Helper()
	t.Setenv("CODEX_HOME", "")
	t.Setenv("PI_CODING_AGENT_DIR", "")
	manager, home, _ := testManager(t)
	return manager, home
}

func instructionsTestPath(home, file string) string {
	switch file {
	case InstructionsFileAgents:
		return filepath.Join(home, ".codex", InstructionsFileAgents)
	case InstructionsFileClaude:
		return filepath.Join(home, ".claude", InstructionsFileClaude)
	case InstructionsFileOhMyPi:
		return filepath.Join(home, ".omp", "agent", InstructionsFileAgents)
	default:
		return filepath.Join(home, file)
	}
}

func writeInstructionsFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

func TestEmbeddedInstructionsRequireSemanticFirstResearch(t *testing.T) {
	for _, text := range []string{
		"Do not launch an Explore, research, or code-analysis subagent",
		"find_by_intent",
		"find_references",
		"get_blast_radius",
		"find_cross_repo_consumers",
		"get_source",
		"at most 20 symbols",
		"`index_project` changes Kivgraph state",
	} {
		if !bytes.Contains(embeddedInstructions, []byte(text)) {
			t.Fatalf("embedded instructions missing required workflow rule %q", text)
		}
	}
}

func TestInstructionsFileForTargetRejectsUnsupportedAgent(t *testing.T) {
	if _, err := InstructionsFileForTarget(Target("unknown-agent")); err == nil ||
		!strings.Contains(err.Error(), "claude-code") ||
		!strings.Contains(err.Error(), "oh-my-pi") {
		t.Fatalf("InstructionsFileForTarget() error = %v, want supported agents", err)
	}
}

func TestInstructionsFileForTargetMapsCodingAgents(t *testing.T) {
	tests := map[Target]string{
		Target("claude"):    InstructionsFileClaude,
		TargetClaudeCode:    InstructionsFileClaude,
		TargetClaudeDesktop: InstructionsFileClaude,
		TargetCodex:         InstructionsFileAgents,
		TargetOpenCode:      InstructionsFileAgents,
		Target("omp"):       InstructionsFileOhMyPi,
		TargetOhMyPi:        InstructionsFileOhMyPi,
	}
	for target, want := range tests {
		t.Run(string(target), func(t *testing.T) {
			got, err := InstructionsFileForTarget(target)
			if err != nil {
				t.Fatalf("InstructionsFileForTarget() error = %v", err)
			}
			if got != want {
				t.Fatalf("InstructionsFileForTarget() = %q, want %q", got, want)
			}
		})
	}
}

func TestInstructionsDestinationForTargetUsesGlobalClientPaths(t *testing.T) {
	manager, home := testInstructionsManager(t)
	tests := map[Target]string{
		TargetClaudeCode:    filepath.Join(home, ".claude", "CLAUDE.md"),
		TargetClaudeDesktop: filepath.Join(home, ".claude", "CLAUDE.md"),
		TargetCodex:         filepath.Join(home, ".codex", "AGENTS.md"),
		TargetOpenCode:      filepath.Join(home, ".config", "opencode", "opencode.json"),
		TargetOhMyPi:        filepath.Join(home, ".omp", "agent", "AGENTS.md"),
	}
	for target, wantPath := range tests {
		t.Run(string(target), func(t *testing.T) {
			_, gotPath, err := manager.InstructionsDestinationForTarget(target)
			if err != nil {
				t.Fatalf("InstructionsDestinationForTarget() error = %v", err)
			}
			if gotPath != wantPath {
				t.Fatalf("InstructionsDestinationForTarget() path = %q, want %q", gotPath, wantPath)
			}
		})
	}
}

func TestInstructionsDestinationForTargetHonorsAgentConfigurationRoots(t *testing.T) {
	_, home := testInstructionsManager(t)
	codexRoot := filepath.Join(home, "custom-codex")
	ompRoot := filepath.Join(home, "custom-omp")
	manager, err := New(Options{HomeDir: home, CodexDir: codexRoot, OhMyPiDir: ompRoot})
	if err != nil {
		t.Fatal(err)
	}
	for target, want := range map[Target]string{
		TargetCodex:  filepath.Join(codexRoot, InstructionsFileAgents),
		TargetOhMyPi: filepath.Join(ompRoot, InstructionsFileAgents),
	} {
		_, got, err := manager.InstructionsDestinationForTarget(target)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s destination = %q, want %q", target, got, want)
		}
	}
	if path, _, _, err := manager.mcpPath(TargetCodex, ScopeUser); err != nil || path != filepath.Join(codexRoot, "config.toml") {
		t.Fatalf("Codex MCP path = %q, %v", path, err)
	}
	if path, err := manager.skillPath(TargetOhMyPi, ScopeUser); err != nil || path != filepath.Join(ompRoot, "skills", "kivgraph", "SKILL.md") {
		t.Fatalf("Oh My Pi skill path = %q, %v", path, err)
	}
	if document, err := manager.hookDocumentFor(TargetOhMyPi, ScopeUser); err != nil || document.path != filepath.Join(ompRoot, "extensions", "kivgraph.js") {
		t.Fatalf("Oh My Pi hook document = %#v, %v", document, err)
	}
}

func TestInstallInstructionsForTargetLeavesProjectInstructionsUntouched(t *testing.T) {
	manager, home, project := testManager(t)
	projectInstructions := filepath.Join(project, "AGENTS.md")
	if err := os.WriteFile(projectInstructions, []byte("# Project instructions\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := manager.InstallInstructionsForTarget(TargetCodex, false, false)
	if err != nil {
		t.Fatalf("InstallInstructionsForTarget() error = %v", err)
	}
	wantPath := filepath.Join(home, ".codex", "AGENTS.md")
	if plan.Path != wantPath || plan.Status != "installed" {
		t.Fatalf("installation plan = %#v, want global path %q", plan, wantPath)
	}
	projectData, err := os.ReadFile(projectInstructions)
	if err != nil {
		t.Fatal(err)
	}
	if string(projectData) != "# Project instructions\n" {
		t.Fatalf("project instructions changed to %q", projectData)
	}
}

func TestDetectInstructionsTargetsUsesExistingContextFiles(t *testing.T) {
	manager, home := testInstructionsManager(t)
	codexPath := instructionsTestPath(home, InstructionsFileAgents)
	if err := os.MkdirAll(filepath.Dir(codexPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte("# user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ohmypiPath := instructionsTestPath(home, InstructionsFileOhMyPi)
	if err := os.MkdirAll(filepath.Dir(ohmypiPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ohmypiPath, []byte("# user\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	detections, err := manager.DetectInstructionsTargets()
	if err != nil {
		t.Fatalf("DetectInstructionsTargets() error = %v", err)
	}
	want := map[Target]bool{
		TargetClaudeCode:    false,
		TargetClaudeDesktop: false,
		TargetCodex:         true,
		TargetOpenCode:      false,
		TargetOhMyPi:        true,
	}
	if len(detections) != len(want) {
		t.Fatalf("DetectInstructionsTargets() returned %d targets, want %d: %#v", len(detections), len(want), detections)
	}
	for _, detection := range detections {
		if detection.Detected != want[detection.Target] {
			t.Fatalf("detection for %s = %v, want %v", detection.Target, detection.Detected, want[detection.Target])
		}
	}
}

func TestDetectInstructionsTargetsReportsInspectionErrors(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	manager := Manager{homeDir: "bad\x00home"}
	if _, err := manager.DetectInstructionsTargets(); err == nil || !strings.Contains(err.Error(), "instructions root") {
		t.Fatalf("DetectInstructionsTargets() error = %v, want inspection error", err)
	}
}

func TestDetectInstructionsTargetsContinuesAfterOneAgentPathFails(t *testing.T) {
	manager, _ := testInstructionsManager(t)
	manager.codexDir = "bad\x00codex"
	detections, err := manager.DetectInstructionsTargets()
	if err != nil {
		t.Fatal(err)
	}
	if len(detections) != len(InstructionsTargets()) {
		t.Fatalf("detections = %#v, want one entry per target", detections)
	}
	targetCounts := make(map[Target]int, len(InstructionsTargets()))
	for _, target := range InstructionsTargets() {
		targetCounts[target]++
	}
	foundCodex := false
	for _, detection := range detections {
		targetCounts[detection.Target]--
		if detection.Target != TargetCodex {
			continue
		}
		foundCodex = true
		if detection.Detected {
			t.Fatalf("Codex detection for root %q = %#v, want an undetected invalid root", manager.codexDir, detection)
		}
	}
	if !foundCodex {
		t.Fatalf("detections = %#v, want a Codex entry", detections)
	}
	for target, count := range targetCounts {
		if count != 0 {
			t.Fatalf("detections = %#v, target %q count = %d, want one", detections, target, 1-count)
		}
	}
}

func TestInstallInstructionsRejectsUnsupportedFile(t *testing.T) {
	manager, home := testInstructionsManager(t)

	_, err := manager.InstallInstructions("PROJECT.md", false, false)
	if err == nil || !strings.Contains(err.Error(), "AGENTS.md") || !strings.Contains(err.Error(), "CLAUDE.md") {
		t.Fatalf("InstallInstructions() error = %v, want the supported file names", err)
	}
	entries, readErr := os.ReadDir(home)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected file name created project entries: %v", entries)
	}
}

func TestInstallInstructionsRejectsSymlinkDestination(t *testing.T) {
	skipWindowsSymlinkTest(t)
	manager, home := testInstructionsManager(t)
	outside := filepath.Join(home, "outside.md")
	path := instructionsTestPath(home, InstructionsFileAgents)
	if err := os.WriteFile(outside, []byte("keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}

	_, err := manager.InstallInstructions(InstructionsFileAgents, false, false)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("InstallInstructions() error = %v, want symlink rejection", err)
	}
	data, readErr := os.ReadFile(outside)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "keep me\n" {
		t.Fatalf("symlink target changed to %q", data)
	}
}

func TestInstallInstructionsRejectsSymlinkParent(t *testing.T) {
	skipWindowsSymlinkTest(t)
	manager, home := testInstructionsManager(t)
	outside := testsupport.TempDir(t)
	if err := os.Symlink(outside, filepath.Join(home, ".omp")); err != nil {
		t.Fatal(err)
	}

	_, err := manager.InstallInstructions(InstructionsFileOhMyPi, false, false)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("InstallInstructions() error = %v, want parent symlink rejection", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "AGENTS.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink parent target changed: %v", err)
	}
}

func TestInstallInstructionsRejectsNonDirectoryParent(t *testing.T) {
	manager, home := testInstructionsManager(t)
	parent := filepath.Join(home, ".omp")
	if err := os.WriteFile(parent, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := manager.InstallInstructions(InstructionsFileOhMyPi, false, false)
	if err == nil || !strings.Contains(err.Error(), "instructions parent") {
		t.Fatalf("InstallInstructions() error = %v, want non-directory rejection", err)
	}
}

func TestValidateInstructionsParentReportsInspectionErrors(t *testing.T) {
	err := validateInstructionsParent("bad\x00parent", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "inspect instructions parent") {
		t.Fatalf("validateInstructionsParent() error = %v, want inspection error", err)
	}
}

func TestInstructionsDestinationReportsInspectionErrors(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	manager := Manager{homeDir: "bad\x00home"}
	if _, err := manager.InstructionsDestination(InstructionsFileAgents); err == nil || !strings.Contains(err.Error(), "instructions root") {
		t.Fatalf("InstructionsDestination() error = %v, want inspection error", err)
	}
}

func TestInstallInstructionsRejectsClaudeSymlinkDestination(t *testing.T) {
	skipWindowsSymlinkTest(t)
	manager, home := testInstructionsManager(t)
	claudePath := instructionsTestPath(home, InstructionsFileClaude)
	if err := os.MkdirAll(filepath.Dir(claudePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("outside.md", claudePath); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.InstallInstructions(InstructionsFileClaude, false, false); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("InstallInstructions() error = %v, want symlink rejection", err)
	}
}

func skipWindowsSymlinkTest(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symbolic-link tests require Windows link privileges")
	}
}

func TestInstallInstructionsRejectsMalformedBlock(t *testing.T) {
	manager, home := testInstructionsManager(t)
	for name, malformed := range map[string]string{
		"missing end":      "# Project\n\n" + instructionsBeginMarker + "\n",
		"missing begin":    "# Project\n\n" + instructionsEndMarker + "\n",
		"end before begin": instructionsEndMarker + "\n" + instructionsBeginMarker + "\n",
		"duplicate begin":  instructionsBeginMarker + "\n" + instructionsBeginMarker + "\n" + instructionsEndMarker + "\n",
		"duplicate end":    instructionsBeginMarker + "\n" + instructionsEndMarker + "\n" + instructionsEndMarker + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := instructionsTestPath(home, InstructionsFileAgents)
			if err := writeInstructionsFile(path, []byte(malformed), 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := manager.InstallInstructions(InstructionsFileAgents, false, true)
			if err == nil || !strings.Contains(err.Error(), "malformed") {
				t.Fatalf("InstallInstructions() error = %v, want malformed-block rejection", err)
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(data) != malformed {
				t.Fatalf("malformed instructions changed to %q", data)
			}
		})
	}
}

func TestInstallInstructionsRejectsEditedBlockWithoutForce(t *testing.T) {
	manager, home := testInstructionsManager(t)
	path := instructionsTestPath(home, InstructionsFileAgents)
	if _, err := manager.InstallInstructions(InstructionsFileAgents, false, false); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := bytes.Replace(original, []byte("@"+filepath.Join(home, ".codex", InstructionsCanonicalFile)), []byte("@LOCAL.md"), 1)
	if bytes.Equal(edited, original) {
		t.Fatal("test fixture did not edit the managed block")
	}
	if err := writeInstructionsFile(path, edited, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.InstallInstructions(InstructionsFileAgents, false, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("InstallInstructions() error = %v, want force guidance", err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, edited) {
		t.Fatalf("refused install changed edited file to %q", current)
	}
	if _, err := os.Stat(path + ".kivgraph.bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused install created a backup: %v", err)
	}
}

func TestInstallInstructionsDryRunDoesNotWrite(t *testing.T) {
	manager, home := testInstructionsManager(t)

	plan, err := manager.InstallInstructions(InstructionsFileAgents, true, false)
	if err != nil {
		t.Fatalf("InstallInstructions() error = %v", err)
	}
	if plan.Status != "would-install" || !plan.DryRun || !plan.Changed {
		t.Fatalf("dry-run plan = %#v", plan)
	}
	if _, err := os.Stat(instructionsTestPath(home, InstructionsFileAgents)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run wrote instructions: %v", err)
	}
}

func TestInstallInstructionsDryRunDoesNotReplaceEditedBlock(t *testing.T) {
	manager, home := testInstructionsManager(t)
	path := instructionsTestPath(home, InstructionsFileAgents)
	if _, err := manager.InstallInstructions(InstructionsFileAgents, false, false); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := bytes.Replace(original, []byte("@"+filepath.Join(home, ".codex", InstructionsCanonicalFile)), []byte("@LOCAL.md"), 1)
	if err := writeInstructionsFile(path, edited, 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := manager.InstallInstructions(InstructionsFileAgents, true, true)
	if err != nil {
		t.Fatalf("dry-run replacement error = %v", err)
	}
	if plan.Status != "would-install" || !plan.DryRun {
		t.Fatalf("dry-run replacement plan = %#v", plan)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, edited) {
		t.Fatalf("dry-run replacement changed file to %q", current)
	}
}

func TestInstallInstructionsSupportsEmptyAndCRLFFile(t *testing.T) {
	manager, home := testInstructionsManager(t)
	path := instructionsTestPath(home, InstructionsFileAgents)
	if err := writeInstructionsFile(path, []byte("\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := manager.InstallInstructions(InstructionsFileAgents, false, false)
	if err != nil {
		t.Fatalf("InstallInstructions() error = %v", err)
	}
	if plan.Status != "installed" {
		t.Fatalf("plan = %#v", plan)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bareLF := bytes.ReplaceAll(data, []byte("\r\n"), nil)
	if !bytes.Contains(data, []byte("\r\n")) || bytes.Contains(bareLF, []byte("\n")) {
		t.Fatalf("CRLF file was not preserved: %q", data)
	}
	if !bytes.Contains(data, []byte(instructionsBeginMarker+"\r\n")) {
		t.Fatalf("CRLF instructions block missing: %q", data)
	}
}

func TestInstallInstructionsSupportsZeroByteFile(t *testing.T) {
	manager, home := testInstructionsManager(t)
	path := instructionsTestPath(home, InstructionsFileAgents)
	if err := writeInstructionsFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := manager.InstallInstructions(InstructionsFileAgents, false, false)
	if err != nil {
		t.Fatalf("zero-byte InstallInstructions() error = %v", err)
	}
	if plan.Status != "installed" {
		t.Fatalf("zero-byte plan = %#v", plan)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte(instructionsBeginMarker+"\n")) {
		t.Fatalf("zero-byte instructions block missing: %q", data)
	}
}

func TestInstallInstructionsRejectsDirectoryDestination(t *testing.T) {
	manager, home := testInstructionsManager(t)
	path := instructionsTestPath(home, InstructionsFileAgents)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := manager.InstallInstructions(InstructionsFileAgents, false, false)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("InstallInstructions() error = %v, want directory rejection", err)
	}
}

func TestInstallInstructionsReportsAtomicWriteFailures(t *testing.T) {
	testsupport.SkipWithoutModeBits(t)
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not the write boundary on Windows")
	}

	tests := map[string]func(*testing.T, Manager, string){
		"new file": func(t *testing.T, manager Manager, home string) {
			directory := filepath.Dir(instructionsTestPath(home, InstructionsFileAgents))
			if err := os.MkdirAll(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			makeInstructionsDirectoryReadOnly(t, directory)
			_, err := manager.InstallInstructions(InstructionsFileAgents, false, false)
			if err == nil || !strings.Contains(err.Error(), "create integration temporary file") {
				t.Fatalf("InstallInstructions() error = %v, want atomic-write failure", err)
			}
		},
		"existing file": func(t *testing.T, manager Manager, home string) {
			path := instructionsTestPath(home, InstructionsFileAgents)
			if err := writeInstructionsFile(path, []byte("# Project\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path+".kivgraph.bak", []byte("existing backup\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			makeInstructionsDirectoryReadOnly(t, filepath.Dir(path))
			_, err := manager.InstallInstructions(InstructionsFileAgents, false, false)
			if err == nil || !strings.Contains(err.Error(), "create integration temporary file") {
				t.Fatalf("InstallInstructions() error = %v, want atomic-write failure", err)
			}
		},
		"forced replacement": func(t *testing.T, manager Manager, home string) {
			path := instructionsTestPath(home, InstructionsFileAgents)
			if _, err := manager.InstallInstructions(InstructionsFileAgents, false, false); err != nil {
				t.Fatal(err)
			}
			sourceBackup := filepath.Join(home, ".codex", InstructionsCanonicalFile) + ".kivgraph.bak"
			if err := os.WriteFile(sourceBackup, []byte("existing source backup\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = bytes.Replace(data, []byte("@"+filepath.Join(home, ".codex", InstructionsCanonicalFile)), []byte("@LOCAL.md"), 1)
			if err := writeInstructionsFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path+".kivgraph.bak", []byte("existing backup\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			makeInstructionsDirectoryReadOnly(t, filepath.Dir(path))
			_, err = manager.InstallInstructions(InstructionsFileAgents, false, true)
			if err == nil || !strings.Contains(err.Error(), "create integration temporary file") {
				t.Fatalf("InstallInstructions() error = %v, want atomic-write failure", err)
			}
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			manager, home := testInstructionsManager(t)
			test(t, manager, home)
		})
	}
}

func makeInstructionsDirectoryReadOnly(t *testing.T, directory string) {
	t.Helper()
	if err := os.Chmod(directory, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(directory, 0o700); err != nil {
			t.Errorf("restore instructions directory permissions: %v", err)
		}
	})
}

func TestInstallInstructionsPreservesExistingContentAndIsIdempotent(t *testing.T) {
	manager, home := testInstructionsManager(t)
	path := instructionsTestPath(home, InstructionsFileAgents)
	original := []byte("# Project instructions\n\nUse pnpm for JavaScript commands.\n\n")
	if err := writeInstructionsFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := manager.InstallInstructions(InstructionsFileAgents, false, false)
	if err != nil {
		t.Fatalf("first InstallInstructions() error = %v", err)
	}
	if first.Status != "installed" || !first.Changed {
		t.Fatalf("first plan = %#v", first)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, original) || !bytes.Contains(data, []byte("@"+first.SourcePath)) {
		t.Fatalf("install did not preserve project instructions or add Kivgraph block: %q", data)
	}
	backup, err := os.ReadFile(path + ".kivgraph.bak")
	if err != nil {
		t.Fatalf("read install backup: %v", err)
	}
	if !bytes.Equal(backup, original) {
		t.Fatalf("install backup = %q, want %q", backup, original)
	}

	second, err := manager.InstallInstructions(InstructionsFileAgents, false, false)
	if err != nil {
		t.Fatalf("idempotent InstallInstructions() error = %v", err)
	}
	if second.Status != "managed" || second.Changed {
		t.Fatalf("idempotent plan = %#v", second)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unchanged, data) {
		t.Fatal("idempotent install rewrote the instructions file")
	}
}

func TestInstallInstructionsUsesTheManagedBlockLineEnding(t *testing.T) {
	manager, home := testInstructionsManager(t)
	path := instructionsTestPath(home, InstructionsFileAgents)
	if _, err := manager.InstallInstructions(InstructionsFileAgents, false, false); err != nil {
		t.Fatal(err)
	}
	data := []byte("# Project instructions\r\n\n")
	data = append(data, managedInstructionsReference("\r\n", filepath.Join(home, ".codex", InstructionsCanonicalFile))...)
	data = append(data, '\r', '\n')
	if err := writeInstructionsFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := manager.InstallInstructions(InstructionsFileAgents, false, false)
	if err != nil {
		t.Fatalf("mixed-line-ending InstallInstructions() error = %v", err)
	}
	if plan.Status != "managed" || plan.Changed {
		t.Fatalf("mixed-line-ending plan = %#v", plan)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unchanged, data) {
		t.Fatal("mixed-line-ending install rewrote the reference file")
	}
}

func TestInstallInstructionsSeparatesContentWithoutFinalNewline(t *testing.T) {
	manager, home := testInstructionsManager(t)
	path := instructionsTestPath(home, InstructionsFileAgents)
	original := []byte("# Project instructions")
	if err := writeInstructionsFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.InstallInstructions(InstructionsFileAgents, false, false); err != nil {
		t.Fatalf("no-final-newline InstallInstructions() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := append(append(append([]byte{}, original...), '\n'), '\n')
	if !bytes.HasPrefix(data, wantPrefix) {
		t.Fatalf("instructions did not separate from content without final newline: %q", data)
	}
}

func TestInstallInstructionsDryRunPreservesExistingContent(t *testing.T) {
	manager, home := testInstructionsManager(t)
	path := instructionsTestPath(home, InstructionsFileAgents)
	original := []byte("# Project instructions\n")
	if err := writeInstructionsFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := manager.InstallInstructions(InstructionsFileAgents, true, false)
	if err != nil {
		t.Fatalf("dry-run InstallInstructions() error = %v", err)
	}
	if plan.Status != "would-install" || !plan.DryRun || !plan.Changed {
		t.Fatalf("dry-run existing-file plan = %#v", plan)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, original) {
		t.Fatalf("dry-run changed existing file to %q", data)
	}
}

func TestInstallInstructionsSupportsClaudeFile(t *testing.T) {
	manager, home := testInstructionsManager(t)

	plan, err := manager.InstallInstructions(InstructionsFileClaude, false, false)
	if err != nil {
		t.Fatalf("InstallInstructions(CLAUDE.md) error = %v", err)
	}
	wantPath := instructionsTestPath(home, InstructionsFileClaude)
	if plan.Path != wantPath || plan.Status != "installed" {
		t.Fatalf("Claude plan = %#v, want path %s", plan, wantPath)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("@"+plan.SourcePath)) {
		t.Fatalf("CLAUDE.md does not reference Kivgraph instructions: %q", data)
	}
}

func TestInstallInstructionsSupportsOhMyPiPath(t *testing.T) {
	manager, home := testInstructionsManager(t)
	path := instructionsTestPath(home, InstructionsFileOhMyPi)

	if _, err := manager.InstallInstructions(InstructionsFileOhMyPi, false, false); err != nil {
		t.Fatalf("Oh My Pi InstallInstructions() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Oh My Pi instructions missing: %v", err)
	}
	plan, err := manager.InstallInstructions(InstructionsFileOhMyPi, false, false)
	if err != nil {
		t.Fatalf("second Oh My Pi InstallInstructions() error = %v", err)
	}
	if plan.Status != "managed" || plan.Changed {
		t.Fatalf("second Oh My Pi plan = %#v", plan)
	}
}

func TestInstallInstructionsForceReplacesOnlyManagedBlock(t *testing.T) {
	manager, home := testInstructionsManager(t)
	path := instructionsTestPath(home, InstructionsFileAgents)
	original := []byte("# Project instructions\n\nKeep the API backward compatible.\n")
	if err := writeInstructionsFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.InstallInstructions(InstructionsFileAgents, false, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := bytes.Replace(data, []byte("@"+filepath.Join(home, ".codex", InstructionsCanonicalFile)), []byte("@LOCAL.md"), 1)
	if err := writeInstructionsFile(path, edited, 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := manager.InstallInstructions(InstructionsFileAgents, false, true)
	if err != nil {
		t.Fatalf("forced InstallInstructions() error = %v", err)
	}
	if plan.Status != "installed" || !plan.Changed {
		t.Fatalf("forced plan = %#v", plan)
	}
	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(restored, original) || !bytes.Contains(restored, []byte("@"+filepath.Join(home, ".codex", InstructionsCanonicalFile))) || bytes.Contains(restored, []byte("@LOCAL.md")) {
		t.Fatalf("forced install did not replace only the managed block: %q", restored)
	}
	backup, err := os.ReadFile(path + ".kivgraph.bak")
	if err != nil {
		t.Fatalf("read forced-install backup: %v", err)
	}
	if !bytes.Equal(backup, original) {
		t.Fatalf("forced-install backup = %q, want the pre-install file %q", backup, original)
	}
}

func TestInstallInstructionsWritesCanonicalPromptAndSmallReference(t *testing.T) {
	manager, _ := testInstructionsManager(t)
	for _, target := range []Target{TargetCodex, TargetClaudeCode, TargetOhMyPi} {
		t.Run(string(target), func(t *testing.T) {
			plan, err := manager.InstallInstructionsForTarget(target, false, false)
			if err != nil {
				t.Fatal(err)
			}
			reference, err := os.ReadFile(plan.Path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(reference, []byte("@"+plan.SourcePath)) || bytes.Contains(reference, embeddedInstructions) {
				t.Fatalf("reference = %q, want only @%s", reference, plan.SourcePath)
			}
			source, err := os.ReadFile(plan.SourcePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(source, embeddedInstructions) {
				t.Fatalf("canonical prompt = %q, want bundled instructions", source)
			}
		})
	}
}

func TestInstallInstructionsMigratesExactLegacyBlock(t *testing.T) {
	manager, home := testInstructionsManager(t)
	path := filepath.Join(home, ".codex", InstructionsFileAgents)
	legacy := append(legacyManagedInstructionsBlock("\n"), '\n')
	if err := writeInstructionsFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.InstallInstructionsForTarget(TargetCodex, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "installed" || !plan.Changed {
		t.Fatalf("migration plan = %#v", plan)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(updated, embeddedInstructions) || !bytes.Contains(updated, []byte("@"+plan.SourcePath)) {
		t.Fatalf("migrated file = %q", updated)
	}
}

func TestInstallInstructionsMigratesRelativeReference(t *testing.T) {
	manager, home := testInstructionsManager(t)
	path := filepath.Join(home, ".codex", InstructionsFileAgents)
	legacy := append(legacyManagedInstructionsReference("\n"), '\n')
	if err := writeInstructionsFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.InstallInstructionsForTarget(TargetCodex, false, false)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "installed" || !plan.Changed || !bytes.Contains(updated, []byte("@"+plan.SourcePath)) {
		t.Fatalf("relative-reference migration = %#v, file %q", plan, updated)
	}
}

func TestInstallInstructionsUpgradesKnownCanonicalPrompt(t *testing.T) {
	manager, home := testInstructionsManager(t)
	sourcePath := filepath.Join(home, ".codex", InstructionsCanonicalFile)
	if err := writeInstructionsFile(sourcePath, legacyEmbeddedInstructions, 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.InstallInstructionsForTarget(TargetCodex, false, false)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "installed" || !plan.Changed || !bytes.Equal(updated, embeddedInstructions) {
		t.Fatalf("canonical-prompt upgrade = %#v, file %q", plan, updated)
	}
}

func TestInstallInstructionsProtectsEditedCanonicalPrompt(t *testing.T) {
	manager, home := testInstructionsManager(t)
	if _, err := manager.InstallInstructionsForTarget(TargetCodex, false, false); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(home, ".codex", InstructionsCanonicalFile)
	if err := os.WriteFile(sourcePath, []byte("# Local Kivgraph policy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.InstallInstructionsForTarget(TargetCodex, false, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("install error = %v, want force guidance", err)
	}
	plan, err := manager.InstallInstructionsForTarget(TargetCodex, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "installed" || !plan.Changed {
		t.Fatalf("forced plan = %#v", plan)
	}
	restored, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, embeddedInstructions) {
		t.Fatalf("forced source = %q, want bundled prompt", restored)
	}
}

func TestInstallInstructionsRegistersOpenCodeInstructionWithoutTouchingAgents(t *testing.T) {
	manager, home := testInstructionsManager(t)
	configPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	original := []byte("{\n  \"model\": \"openai/gpt-5\",\n  \"instructions\": [\"/tmp/local.md\"]\n}\n")
	if err := writeInstructionsFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.InstallInstructionsForTarget(TargetOpenCode, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Path != configPath || plan.SourcePath != filepath.Join(home, ".config", "opencode", InstructionsCanonicalFile) {
		t.Fatalf("OpenCode plan = %#v", plan)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Model        string   `json:"model"`
		Instructions []string `json:"instructions"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Model != "openai/gpt-5" || len(config.Instructions) != 2 || config.Instructions[1] != plan.SourcePath {
		t.Fatalf("OpenCode config = %#v", config)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "opencode", "AGENTS.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenCode AGENTS.md unexpectedly written: %v", err)
	}
	second, err := manager.InstallInstructionsForTarget(TargetOpenCode, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != "managed" || second.Changed {
		t.Fatalf("second OpenCode plan = %#v", second)
	}
}

func TestInstallInstructionsPreservesOpenCodeJSONCCommentsAndTrailingCommas(t *testing.T) {
	manager, home := testInstructionsManager(t)
	configPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	original := []byte("{\n  // Keep this comment.\n  \"instructions\": [\n    \"/tmp/local.md\",\n  ],\n  \"model\": \"openai/gpt-5\",\n}\n")
	if err := writeInstructionsFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.InstallInstructionsForTarget(TargetOpenCode, false, false)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(updated, []byte("// Keep this comment.")) ||
		!bytes.Contains(updated, []byte("\"model\": \"openai/gpt-5\",")) ||
		!bytes.Contains(updated, []byte(plan.SourcePath)) {
		t.Fatalf("JSONC update did not preserve unrelated content: %q", updated)
	}
	if _, err := hujson.Standardize(updated); err != nil {
		t.Fatalf("updated JSONC is invalid: %v", err)
	}
}
