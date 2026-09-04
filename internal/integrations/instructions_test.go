package integrations

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestInstructionsFileForTargetRejectsUnsupportedAgent(t *testing.T) {
	if _, err := InstructionsFileForTarget(Target("unknown-agent")); err == nil ||
		!strings.Contains(err.Error(), "claude-code") ||
		!strings.Contains(err.Error(), "oh-my-pi") {
		t.Fatalf("InstructionsFileForTarget() error = %v, want supported agents", err)
	}
}

func TestInstructionsFileForTargetMapsCodingAgents(t *testing.T) {
	tests := map[Target]string{
		Target("claude"): InstructionsFileClaude,
		TargetClaudeCode: InstructionsFileClaude,
		TargetCodex:      InstructionsFileAgents,
		TargetOpenCode:   InstructionsFileAgents,
		Target("omp"):    InstructionsFileOhMyPi,
		TargetOhMyPi:     InstructionsFileOhMyPi,
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

func TestDetectInstructionsTargetsUsesExistingContextFiles(t *testing.T) {
	manager, _, project := testManager(t)
	if err := os.WriteFile(filepath.Join(project, InstructionsFileAgents), []byte("# project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".omp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, InstructionsFileOhMyPi), []byte("# project\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	detections, err := manager.DetectInstructionsTargets()
	if err != nil {
		t.Fatalf("DetectInstructionsTargets() error = %v", err)
	}
	want := map[Target]bool{
		TargetClaudeCode: false,
		TargetCodex:      true,
		TargetOpenCode:   true,
		TargetOhMyPi:     true,
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
	manager := Manager{projectDir: "bad\x00project"}
	if _, err := manager.DetectInstructionsTargets(); err == nil || !strings.Contains(err.Error(), "inspect instructions file") {
		t.Fatalf("DetectInstructionsTargets() error = %v, want inspection error", err)
	}
}

func TestInstallInstructionsRejectsUnsupportedFile(t *testing.T) {
	manager, _, project := testManager(t)

	_, err := manager.InstallInstructions("PROJECT.md", false, false)
	if err == nil || !strings.Contains(err.Error(), "AGENTS.md") || !strings.Contains(err.Error(), "CLAUDE.md") {
		t.Fatalf("InstallInstructions() error = %v, want the supported file names", err)
	}
	entries, readErr := os.ReadDir(project)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected file name created project entries: %v", entries)
	}
}

func TestInstallInstructionsRejectsSymlinkDestination(t *testing.T) {
	manager, _, project := testManager(t)
	outside := filepath.Join(project, "outside.md")
	path := filepath.Join(project, InstructionsFileAgents)
	if err := os.WriteFile(outside, []byte("keep me\n"), 0o600); err != nil {
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
	manager, _, project := testManager(t)
	outside := testsupport.TempDir(t)
	if err := os.Symlink(outside, filepath.Join(project, ".omp")); err != nil {
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

func TestValidateInstructionsParentReportsInspectionErrors(t *testing.T) {
	err := validateInstructionsParent("bad\x00parent", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "inspect instructions parent") {
		t.Fatalf("validateInstructionsParent() error = %v, want inspection error", err)
	}
}

func TestInstructionsDestinationReportsInspectionErrors(t *testing.T) {
	manager := Manager{projectDir: "bad\x00project"}
	if _, err := manager.InstructionsDestination(InstructionsFileAgents); err == nil || !strings.Contains(err.Error(), "inspect instructions path") {
		t.Fatalf("InstructionsDestination() error = %v, want inspection error", err)
	}
}

func TestInstallInstructionsFollowsOnlyTheClaudeToAgentsLink(t *testing.T) {
	// The os.Readlink error branch is a filesystem race between Lstat and
	// Readlink. Reproducing it deterministically would require a production
	// filesystem seam that exists only for this test; the conventional link
	// and every rejection path remain covered here and below.
	manager, _, project := testManager(t)
	claudePath := filepath.Join(project, InstructionsFileClaude)
	agentsPath := filepath.Join(project, InstructionsFileAgents)
	if err := os.Symlink(InstructionsFileAgents, claudePath); err != nil {
		t.Fatal(err)
	}

	plan, err := manager.InstallInstructions(InstructionsFileClaude, false, false)
	if err != nil {
		t.Fatalf("InstallInstructions() error = %v", err)
	}
	if plan.Path != agentsPath || plan.Status != "installed" {
		t.Fatalf("link installation plan = %#v, want destination %s", plan, agentsPath)
	}
	data, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, embeddedInstructions) {
		t.Fatalf("AGENTS.md does not contain Kivgraph instructions: %q", data)
	}
	linkTarget, err := os.Readlink(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if linkTarget != InstructionsFileAgents {
		t.Fatalf("CLAUDE.md link changed to %q", linkTarget)
	}
}

func TestInstallInstructionsRejectsMalformedBlock(t *testing.T) {
	manager, _, project := testManager(t)
	for name, malformed := range map[string]string{
		"missing end":      "# Project\n\n" + instructionsBeginMarker + "\n",
		"missing begin":    "# Project\n\n" + instructionsEndMarker + "\n",
		"end before begin": instructionsEndMarker + "\n" + instructionsBeginMarker + "\n",
		"duplicate begin":  instructionsBeginMarker + "\n" + instructionsBeginMarker + "\n" + instructionsEndMarker + "\n",
		"duplicate end":    instructionsBeginMarker + "\n" + instructionsEndMarker + "\n" + instructionsEndMarker + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(project, InstructionsFileAgents)
			if err := os.WriteFile(path, []byte(malformed), 0o600); err != nil {
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
	manager, _, project := testManager(t)
	path := filepath.Join(project, InstructionsFileAgents)
	if _, err := manager.InstallInstructions(InstructionsFileAgents, false, false); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := bytes.Replace(original, embeddedInstructions, []byte("## Local Kivgraph instructions\n"), 1)
	if bytes.Equal(edited, original) {
		t.Fatal("test fixture did not edit the managed block")
	}
	if err := os.WriteFile(path, edited, 0o600); err != nil {
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
	manager, _, project := testManager(t)

	plan, err := manager.InstallInstructions(InstructionsFileAgents, true, false)
	if err != nil {
		t.Fatalf("InstallInstructions() error = %v", err)
	}
	if plan.Status != "would-install" || !plan.DryRun || !plan.Changed {
		t.Fatalf("dry-run plan = %#v", plan)
	}
	if _, err := os.Stat(filepath.Join(project, InstructionsFileAgents)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run wrote instructions: %v", err)
	}
}

func TestInstallInstructionsDryRunDoesNotReplaceEditedBlock(t *testing.T) {
	manager, _, project := testManager(t)
	path := filepath.Join(project, InstructionsFileAgents)
	if _, err := manager.InstallInstructions(InstructionsFileAgents, false, false); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := bytes.Replace(original, embeddedInstructions, []byte("## Edited Kivgraph block\n"), 1)
	if err := os.WriteFile(path, edited, 0o600); err != nil {
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
	manager, _, project := testManager(t)
	path := filepath.Join(project, InstructionsFileAgents)
	if err := os.WriteFile(path, []byte("\r\n"), 0o600); err != nil {
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
	manager, _, project := testManager(t)
	path := filepath.Join(project, InstructionsFileAgents)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
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
	manager, _, project := testManager(t)
	path := filepath.Join(project, InstructionsFileAgents)
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
		"new file": func(t *testing.T, manager Manager, project string) {
			makeInstructionsDirectoryReadOnly(t, project)
			_, err := manager.InstallInstructions(InstructionsFileAgents, false, false)
			if err == nil || !strings.Contains(err.Error(), "create integration temporary file") {
				t.Fatalf("InstallInstructions() error = %v, want atomic-write failure", err)
			}
		},
		"existing file": func(t *testing.T, manager Manager, project string) {
			path := filepath.Join(project, InstructionsFileAgents)
			if err := os.WriteFile(path, []byte("# Project\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path+".kivgraph.bak", []byte("existing backup\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			makeInstructionsDirectoryReadOnly(t, project)
			_, err := manager.InstallInstructions(InstructionsFileAgents, false, false)
			if err == nil || !strings.Contains(err.Error(), "create integration temporary file") {
				t.Fatalf("InstallInstructions() error = %v, want atomic-write failure", err)
			}
		},
		"forced replacement": func(t *testing.T, manager Manager, project string) {
			path := filepath.Join(project, InstructionsFileAgents)
			if _, err := manager.InstallInstructions(InstructionsFileAgents, false, false); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = bytes.Replace(data, embeddedInstructions, []byte("## Edited\n"), 1)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path+".kivgraph.bak", []byte("existing backup\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			makeInstructionsDirectoryReadOnly(t, project)
			_, err = manager.InstallInstructions(InstructionsFileAgents, false, true)
			if err == nil || !strings.Contains(err.Error(), "create integration temporary file") {
				t.Fatalf("InstallInstructions() error = %v, want atomic-write failure", err)
			}
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			manager, _, project := testManager(t)
			test(t, manager, project)
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
	manager, _, project := testManager(t)
	path := filepath.Join(project, InstructionsFileAgents)
	original := []byte("# Project instructions\n\nUse pnpm for JavaScript commands.\n\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
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
	if !bytes.HasPrefix(data, original) || !bytes.Contains(data, embeddedInstructions) {
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
	manager, _, project := testManager(t)
	path := filepath.Join(project, InstructionsFileAgents)
	data := []byte("# Project instructions\r\n\n")
	data = append(data, managedInstructionsBlock("\n")...)
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
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
		t.Fatal("mixed-line-ending install rewrote the instructions file")
	}
}

func TestInstallInstructionsSeparatesContentWithoutFinalNewline(t *testing.T) {
	manager, _, project := testManager(t)
	path := filepath.Join(project, InstructionsFileAgents)
	original := []byte("# Project instructions")
	if err := os.WriteFile(path, original, 0o600); err != nil {
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
	manager, _, project := testManager(t)
	path := filepath.Join(project, InstructionsFileAgents)
	original := []byte("# Project instructions\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
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
	manager, _, project := testManager(t)

	plan, err := manager.InstallInstructions(InstructionsFileClaude, false, false)
	if err != nil {
		t.Fatalf("InstallInstructions(CLAUDE.md) error = %v", err)
	}
	wantPath := filepath.Join(project, InstructionsFileClaude)
	if plan.Path != wantPath || plan.Status != "installed" {
		t.Fatalf("Claude plan = %#v, want path %s", plan, wantPath)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, embeddedInstructions) {
		t.Fatalf("CLAUDE.md does not contain Kivgraph instructions: %q", data)
	}
}

func TestInstallInstructionsSupportsOhMyPiPath(t *testing.T) {
	manager, _, project := testManager(t)
	path := filepath.Join(project, InstructionsFileOhMyPi)

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
	manager, _, project := testManager(t)
	path := filepath.Join(project, InstructionsFileAgents)
	original := []byte("# Project instructions\n\nKeep the API backward compatible.\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.InstallInstructions(InstructionsFileAgents, false, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := bytes.Replace(data, embeddedInstructions, []byte("## Edited Kivgraph block\n"), 1)
	if err := os.WriteFile(path, edited, 0o600); err != nil {
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
	if !bytes.Contains(restored, original) || !bytes.Contains(restored, embeddedInstructions) || bytes.Contains(restored, []byte("Edited Kivgraph")) {
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
