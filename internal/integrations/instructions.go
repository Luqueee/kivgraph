package integrations

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// InstructionsFileAgents is the portable root project instruction file read
	// by Codex and OpenCode.
	InstructionsFileAgents = "AGENTS.md"
	// InstructionsFileClaude is the project instruction file read by Claude Code.
	InstructionsFileClaude = "CLAUDE.md"
	// InstructionsFileOhMyPi is Oh My Pi's native project context file.
	InstructionsFileOhMyPi = ".omp/AGENTS.md"

	instructionsBeginMarker = "<!-- BEGIN KIVGRAPH INSTRUCTIONS -->"
	instructionsEndMarker   = "<!-- END KIVGRAPH INSTRUCTIONS -->"
)

// embeddedInstructions is the one source for the block installed into a
// project's agent context file.
//
//go:embed assets/kivgraph/INSTRUCTIONS.md
var embeddedInstructions []byte

// InstructionsPlan describes an agent-instructions file installation.
type InstructionsPlan struct {
	Action Action
	File   string
	Path   string
	Status string
	// Changed says whether applying the plan would change the file.
	Changed bool
	DryRun  bool
	Detail  string
}

var (
	errUnsupportedInstructionsFile  = errors.New("unsupported instructions file")
	errUnsupportedInstructionsAgent = errors.New("unsupported coding agent")
	errMalformedInstructionsBlock   = errors.New("malformed Kivgraph instructions block")
	errEditedInstructionsBlock      = errors.New("Kivgraph instructions block was edited")
)

// InstructionsTargets are the coding agents whose project context can carry
// the Kivgraph instructions block.
func InstructionsTargets() []Target {
	return []Target{
		TargetClaudeCode,
		TargetCodex,
		TargetOpenCode,
		TargetOhMyPi,
	}
}

// InstructionsAgentNames is the accepted --agent vocabulary. Short aliases
// keep the command convenient while the existing integration names remain
// valid for scripts that already use them.
func InstructionsAgentNames() []string {
	names := []string{"claude", "omp"}
	return append(names, targetNames(InstructionsTargets())...)
}

// InstructionsFileForTarget maps a coding agent to the project context file
// that it reads. Claude clients use CLAUDE.md, Oh My Pi uses its native
// .omp/AGENTS.md file, and the other supported clients use AGENTS.md.
func InstructionsFileForTarget(target Target) (string, error) {
	switch target {
	case Target("claude"), TargetClaudeCode:
		return InstructionsFileClaude, nil
	case TargetCodex, TargetOpenCode:
		return InstructionsFileAgents, nil
	case Target("omp"), TargetOhMyPi:
		return InstructionsFileOhMyPi, nil
	default:
		return "", fmt.Errorf("%w %q (want %s)", errUnsupportedInstructionsAgent, target,
			strings.Join(InstructionsAgentNames(), ", "))
	}
}

// DetectInstructionsTargets reports which project context files already exist.
// A shared file marks every client that reads it as detected, so the selector
// can offer the same defaults as the other integration installers.
func (manager Manager) DetectInstructionsTargets() ([]TargetDetection, error) {
	targets := InstructionsTargets()
	detections := make([]TargetDetection, 0, len(targets))
	for _, target := range targets {
		file, err := InstructionsFileForTarget(target)
		if err != nil {
			return nil, err
		}
		exists, err := pathExists(filepath.Join(manager.projectDir, file))
		if err != nil {
			return nil, fmt.Errorf("inspect instructions file %q: %w", file, err)
		}
		detections = append(detections, TargetDetection{Target: target, Detected: exists})
	}
	return detections, nil
}

// InstructionsDestination validates a project context filename and resolves
// the one conventional Claude link to the file that will actually be edited.
func (manager Manager) InstructionsDestination(file string) (string, error) {
	if err := validateInstructionsFile(file); err != nil {
		return "", err
	}
	return manager.instructionsPath(file)
}

// InstallInstructions adds the managed Kivgraph block to a project instruction
// file. Existing content remains in place, and only a block previously marked
// as Kivgraph-owned can be replaced with --force.
func (manager Manager) InstallInstructions(file string, dryRun, force bool) (InstructionsPlan, error) {
	path, err := manager.InstructionsDestination(file)
	if err != nil {
		return InstructionsPlan{}, err
	}
	data, exists, err := readDestination(path)
	if err != nil {
		return InstructionsPlan{}, err
	}

	if !exists {
		newline := instructionLineEnding(data)
		block := managedInstructionsBlock(newline)
		plan := InstructionsPlan{
			Action: ActionInstall, File: file, Path: path,
			Status: "installed", Changed: true, DryRun: dryRun,
			Detail: "add Kivgraph code-intelligence instructions",
		}
		if dryRun {
			plan.Status = "would-install"
			return plan, nil
		}
		if err := writeDestination(path, append(block, newline...), false, nil); err != nil {
			return InstructionsPlan{}, err
		}
		return plan, nil
	}

	start, end, found, err := instructionsBlockBounds(data)
	if err != nil {
		return InstructionsPlan{}, fmt.Errorf("%w in %q: repair the file before reinstalling", err, path)
	}
	if found {
		block := managedInstructionsBlock(instructionLineEnding(data[start:end]))
		if bytes.Equal(data[start:end], block) {
			return InstructionsPlan{
				Action: ActionInstall, File: file, Path: path,
				Status: "managed", Detail: "Kivgraph instructions are already present",
			}, nil
		}
		if !force {
			return InstructionsPlan{}, fmt.Errorf("%w in %q; use --force to replace it", errEditedInstructionsBlock, path)
		}
		updated := replaceInstructionsBlock(data, start, end, block)
		plan := InstructionsPlan{
			Action: ActionInstall, File: file, Path: path,
			Status: "installed", Changed: true, DryRun: dryRun,
			Detail: "replace the edited Kivgraph block and preserve surrounding instructions",
		}
		if dryRun {
			plan.Status = "would-install"
			return plan, nil
		}
		if err := writeDestination(path, updated, true, data); err != nil {
			return InstructionsPlan{}, err
		}
		return plan, nil
	}

	newline := instructionLineEnding(data)
	block := managedInstructionsBlock(newline)
	updated := appendInstructionsBlock(data, block, newline)
	plan := InstructionsPlan{
		Action: ActionInstall, File: file, Path: path,
		Status: "installed", Changed: true, DryRun: dryRun,
		Detail: "append Kivgraph code-intelligence instructions and preserve existing content",
	}
	if dryRun {
		plan.Status = "would-install"
		return plan, nil
	}
	if err := writeDestination(path, updated, true, data); err != nil {
		return InstructionsPlan{}, err
	}
	return plan, nil
}

// instructionsPath accepts the repository convention CLAUDE.md -> AGENTS.md
// without following arbitrary links. The target is compared after resolving a
// relative link against the project directory, and the destination is then
// read and written as a regular path.
func (manager Manager) instructionsPath(file string) (string, error) {
	path := filepath.Join(manager.projectDir, file)
	if err := validateInstructionsParent(filepath.Dir(path), manager.projectDir); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect instructions path %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	target, err := os.Readlink(path)
	if err != nil {
		return "", fmt.Errorf("read instructions link %q: %w", path, err)
	}
	canonical := filepath.Join(manager.projectDir, InstructionsFileAgents)
	resolvedTarget := target
	if !filepath.IsAbs(resolvedTarget) {
		resolvedTarget = filepath.Join(filepath.Dir(path), resolvedTarget)
	}
	if file != InstructionsFileClaude || filepath.Clean(resolvedTarget) != filepath.Clean(canonical) {
		return "", fmt.Errorf("refusing symlink instructions path %q", path)
	}
	return canonical, nil
}

func validateInstructionsParent(parent, project string) error {
	if filepath.Clean(parent) == filepath.Clean(project) {
		return nil
	}
	info, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect instructions parent %q: %w", parent, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink instructions parent %q", parent)
	}
	return nil
}

func validateInstructionsFile(file string) error {
	switch file {
	case InstructionsFileAgents, InstructionsFileClaude, InstructionsFileOhMyPi:
		return nil
	default:
		return fmt.Errorf("%w %q (want %q, %q or %q)", errUnsupportedInstructionsFile,
			file, InstructionsFileAgents, InstructionsFileClaude, InstructionsFileOhMyPi)
	}
}

func instructionsBlockBounds(data []byte) (start, end int, found bool, err error) {
	begin := []byte(instructionsBeginMarker)
	finish := []byte(instructionsEndMarker)
	beginAt := bytes.Index(data, begin)
	endAt := bytes.Index(data, finish)
	if beginAt < 0 && endAt < 0 {
		return 0, 0, false, nil
	}
	if beginAt < 0 || endAt < 0 || endAt < beginAt {
		return 0, 0, false, errMalformedInstructionsBlock
	}
	if bytes.Index(data[beginAt+len(begin):], begin) >= 0 ||
		bytes.Index(data[endAt+len(finish):], finish) >= 0 {
		return 0, 0, false, errMalformedInstructionsBlock
	}
	return beginAt, endAt + len(finish), true, nil
}

func instructionLineEnding(data []byte) string {
	if bytes.Contains(data, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

func managedInstructionsBlock(newline string) []byte {
	body := strings.ReplaceAll(string(embeddedInstructions), "\n", newline)
	return []byte(instructionsBeginMarker + newline + body + instructionsEndMarker)
}

func appendInstructionsBlock(data, block []byte, newline string) []byte {
	result := make([]byte, 0, len(data)+len(newline)*3+len(block))
	result = append(result, data...)
	if len(data) == 0 {
		result = append(result, block...)
		return append(result, newline...)
	}
	if !bytes.HasSuffix(data, []byte(newline)) {
		result = append(result, newline...)
	}
	result = append(result, newline...)
	result = append(result, block...)
	return append(result, newline...)
}

func replaceInstructionsBlock(data []byte, start, end int, block []byte) []byte {
	result := make([]byte, 0, len(data)-end+start+len(block))
	result = append(result, data[:start]...)
	result = append(result, block...)
	return append(result, data[end:]...)
}
