package integrations

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tailscale/hujson"
)

const (
	// InstructionsFileAgents is the legacy selector for AGENTS.md-based clients.
	InstructionsFileAgents = "AGENTS.md"
	// InstructionsFileClaude is the legacy selector for Claude clients.
	InstructionsFileClaude = "CLAUDE.md"
	// InstructionsFileOhMyPi is the legacy selector for Oh My Pi.
	InstructionsFileOhMyPi = ".omp/AGENTS.md"
	// InstructionsCanonicalFile is the Kivgraph-owned source referenced by an
	// agent's user configuration.
	InstructionsCanonicalFile = "KIVGRAPH.md"

	instructionsBeginMarker = "<!-- BEGIN KIVGRAPH INSTRUCTIONS -->"
	instructionsEndMarker   = "<!-- END KIVGRAPH INSTRUCTIONS -->"
)

// embeddedInstructions is the one source for the Kivgraph-owned user prompt.
//
//go:embed assets/kivgraph/INSTRUCTIONS.md
var embeddedInstructions []byte

// legacyEmbeddedInstructions holds previous shipped prompts that can be
// upgraded without mistaking a managed file for a user customization.
//
//go:embed assets/kivgraph/INSTRUCTIONS-v0.9.9.md
var legacyEmbeddedInstructions []byte

// InstructionsPlan describes an agent-instructions installation. Path is the
// file read by the selected agent; SourcePath is the owned prompt it references.
type InstructionsPlan struct {
	Action     Action
	File       string
	Path       string
	SourcePath string
	Status     string
	// Changed says whether applying the plan would change a file.
	Changed bool
	DryRun  bool
	Detail  string
}

var (
	errUnsupportedInstructionsFile  = errors.New("unsupported instructions file")
	errUnsupportedInstructionsAgent = errors.New("unsupported coding agent")
	errMalformedInstructionsBlock   = errors.New("malformed Kivgraph instructions block")
	errEditedInstructionsBlock      = errors.New("kivgraph instructions block was edited")
	errEditedInstructionsSource     = errors.New("kivgraph instructions source was edited")
)

// InstructionsTargets are the coding agents whose user context can carry the
// Kivgraph instructions.
func InstructionsTargets() []Target {
	return []Target{TargetClaudeCode, TargetClaudeDesktop, TargetCodex, TargetOpenCode, TargetOhMyPi}
}

// InstructionsAgentNames is the accepted --agent vocabulary. Short aliases
// keep the command convenient while the existing integration names remain
// valid for scripts that already use them.
func InstructionsAgentNames() []string {
	names := []string{"claude", "omp"}
	return append(names, targetNames(InstructionsTargets())...)
}

// InstructionsFileForTarget maps a coding agent to its legacy instruction
// filename. OpenCode retains AGENTS.md as its legacy selector even though its
// native instruction list lives in opencode.json.
func InstructionsFileForTarget(target Target) (string, error) {
	switch target {
	case Target("claude"), TargetClaudeCode, TargetClaudeDesktop:
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

// DetectInstructionsTargets reports which user instruction integration files
// already exist. A shared Claude file marks each client that reads it.
func (manager Manager) DetectInstructionsTargets() ([]TargetDetection, error) {
	if _, err := manager.instructionsPathAt(manager.homeDir, ".kivgraph-instructions-probe"); err != nil {
		return nil, err
	}
	targets := InstructionsTargets()
	detections := make([]TargetDetection, 0, len(targets))
	for _, target := range targets {
		_, path, err := manager.InstructionsDestinationForTarget(target)
		if err != nil {
			detections = append(detections, TargetDetection{Target: target})
			continue
		}
		exists, err := pathExists(path)
		if err != nil {
			detections = append(detections, TargetDetection{Target: target})
			continue
		}
		detections = append(detections, TargetDetection{Target: target, Detected: exists})
	}
	return detections, nil
}

// InstructionsDestination resolves a deprecated --file selector to the
// matching primary user-configuration file. New callers should select a target
// with InstructionsDestinationForTarget.
func (manager Manager) InstructionsDestination(file string) (string, error) {
	if err := validateInstructionsFile(file); err != nil {
		return "", err
	}
	switch file {
	case InstructionsFileAgents:
		_, path, err := manager.InstructionsDestinationForTarget(TargetCodex)
		return path, err
	case InstructionsFileClaude:
		_, path, err := manager.InstructionsDestinationForTarget(TargetClaudeCode)
		return path, err
	case InstructionsFileOhMyPi:
		_, path, err := manager.InstructionsDestinationForTarget(TargetOhMyPi)
		return path, err
	default:
		return "", fmt.Errorf("%w %q", errUnsupportedInstructionsFile, file)
	}
}

// InstructionsDestinationForTarget resolves the user-level file whose native
// configuration makes the Kivgraph prompt available to one selected client.
func (manager Manager) InstructionsDestinationForTarget(target Target) (string, string, error) {
	file, err := InstructionsFileForTarget(target)
	if err != nil {
		return "", "", err
	}
	var relativePath string
	switch target {
	case Target("claude"), TargetClaudeCode, TargetClaudeDesktop:
		relativePath = filepath.Join(".claude", InstructionsFileClaude)
	case TargetCodex:
		path, err := manager.instructionsPathAt(manager.codexInstructionsDir(), InstructionsFileAgents)
		return file, path, err
	case TargetOpenCode:
		relativePath = filepath.Join(".config", "opencode", "opencode.json")
	case Target("omp"), TargetOhMyPi:
		path, err := manager.instructionsPathAt(manager.ohMyPiInstructionsDir(), InstructionsFileAgents)
		return file, path, err
	default:
		return "", "", fmt.Errorf("%w %q", errUnsupportedInstructionsAgent, target)
	}
	path, err := manager.instructionsPath(relativePath)
	if err != nil {
		return "", "", err
	}
	return file, path, nil
}

func (manager Manager) instructionsSourcePath(target Target) (string, error) {
	var relativePath string
	switch target {
	case Target("claude"), TargetClaudeCode, TargetClaudeDesktop:
		relativePath = filepath.Join(".claude", InstructionsCanonicalFile)
	case TargetCodex:
		return manager.instructionsPathAt(manager.codexInstructionsDir(), InstructionsCanonicalFile)
	case TargetOpenCode:
		relativePath = filepath.Join(".config", "opencode", InstructionsCanonicalFile)
	case Target("omp"), TargetOhMyPi:
		return manager.instructionsPathAt(manager.ohMyPiInstructionsDir(), InstructionsCanonicalFile)
	default:
		return "", fmt.Errorf("%w %q", errUnsupportedInstructionsAgent, target)
	}
	return manager.instructionsPath(relativePath)
}

// InstallInstructions adds Kivgraph instructions through a deprecated --file
// selector. Existing content remains in place, while the owned source is never
// overwritten unless it still matches the bundled prompt or --force is set.
func (manager Manager) InstallInstructions(file string, dryRun, force bool) (InstructionsPlan, error) {
	if err := validateInstructionsFile(file); err != nil {
		return InstructionsPlan{}, err
	}
	switch file {
	case InstructionsFileAgents:
		return manager.InstallInstructionsForTarget(TargetCodex, dryRun, force)
	case InstructionsFileClaude:
		return manager.InstallInstructionsForTarget(TargetClaudeCode, dryRun, force)
	case InstructionsFileOhMyPi:
		return manager.InstallInstructionsForTarget(TargetOhMyPi, dryRun, force)
	default:
		return InstructionsPlan{}, fmt.Errorf("%w %q", errUnsupportedInstructionsFile, file)
	}
}

// InstallInstructionsForTarget installs the owned source and registers it by
// the mechanism the selected coding agent actually reads.
func (manager Manager) InstallInstructionsForTarget(target Target, dryRun, force bool) (InstructionsPlan, error) {
	file, path, err := manager.InstructionsDestinationForTarget(target)
	if err != nil {
		return InstructionsPlan{}, err
	}
	sourcePath, err := manager.instructionsSourcePath(target)
	if err != nil {
		return InstructionsPlan{}, err
	}
	if target == TargetOpenCode {
		return manager.installOpenCodeInstructions(file, path, sourcePath, dryRun, force)
	}
	return manager.installReferencedInstructions(file, path, sourcePath, dryRun, force)
}

type instructionsSourceState struct {
	data    []byte
	exists  bool
	current bool
	managed bool
}

func readInstructionsSource(path string) (instructionsSourceState, error) {
	data, exists, err := readDestination(path)
	if err != nil {
		return instructionsSourceState{}, err
	}
	return instructionsSourceState{
		data:    data,
		exists:  exists,
		current: bytes.Equal(data, embeddedInstructions),
		managed: bytes.Equal(data, embeddedInstructions) || bytes.Equal(data, legacyEmbeddedInstructions),
	}, nil
}

func installInstructionsSource(path string, state instructionsSourceState, dryRun, force bool) (bool, error) {
	if state.exists && !state.managed && !force {
		return false, fmt.Errorf("%w in %q; use --force to replace it", errEditedInstructionsSource, path)
	}
	if state.exists && state.current {
		return false, nil
	}
	if dryRun {
		return true, nil
	}
	if err := writeDestination(path, embeddedInstructions, state.exists, state.data); err != nil {
		return false, err
	}
	return true, nil
}

func (manager Manager) installReferencedInstructions(file, path, sourcePath string, dryRun, force bool) (InstructionsPlan, error) {
	source, err := readInstructionsSource(sourcePath)
	if err != nil {
		return InstructionsPlan{}, err
	}
	data, exists, err := readDestination(path)
	if err != nil {
		return InstructionsPlan{}, err
	}
	updated, referenceChanged, managed, detail, err := referencedInstructionsUpdate(data, exists, sourcePath, force)
	if err != nil {
		return InstructionsPlan{}, fmt.Errorf("%w in %q", err, path)
	}
	sourceChanged := !source.exists || !source.current
	if source.exists && !source.managed && !force {
		return InstructionsPlan{}, fmt.Errorf("%w in %q; use --force to replace it", errEditedInstructionsSource, sourcePath)
	}
	if !sourceChanged && !referenceChanged && managed {
		return InstructionsPlan{Action: ActionInstall, File: file, Path: path, SourcePath: sourcePath,
			Status: "managed", Detail: "Kivgraph instructions are already referenced"}, nil
	}
	plan := InstructionsPlan{Action: ActionInstall, File: file, Path: path, SourcePath: sourcePath,
		Status: "installed", Changed: true, DryRun: dryRun, Detail: detail}
	if sourceChanged && !referenceChanged {
		plan.Detail = "write the Kivgraph canonical prompt"
	}
	if dryRun {
		plan.Status = "would-install"
		return plan, nil
	}
	if _, err := installInstructionsSource(sourcePath, source, false, force); err != nil {
		return InstructionsPlan{}, err
	}
	if referenceChanged {
		if err := writeDestination(path, updated, exists, data); err != nil {
			return InstructionsPlan{}, err
		}
	}
	return plan, nil
}

func referencedInstructionsUpdate(data []byte, exists bool, sourcePath string, force bool) ([]byte, bool, bool, string, error) {
	newline := instructionLineEnding(data)
	reference := managedInstructionsReference(newline, sourcePath)
	if !exists {
		return appendInstructionsBlock(nil, reference, newline), true, false,
			"write the Kivgraph prompt reference", nil
	}
	start, end, found, err := instructionsBlockBounds(data)
	if err != nil {
		return nil, false, false, "", fmt.Errorf("%w: repair the file before reinstalling", err)
	}
	if !found {
		return appendInstructionsBlock(data, reference, newline), true, false,
			"append the Kivgraph prompt reference and preserve existing instructions", nil
	}
	block := data[start:end]
	if bytes.Equal(block, reference) {
		return data, false, true, "Kivgraph instructions are already referenced", nil
	}
	blockNewline := instructionLineEnding(block)
	if bytes.Equal(block, legacyManagedInstructionsReference(blockNewline)) ||
		bytes.Equal(block, legacyManagedInstructionsBlock(blockNewline)) {
		return replaceInstructionsBlock(data, start, end, reference), true, false,
			"migrate the Kivgraph block to its canonical prompt reference", nil
	}
	if !force {
		return nil, false, false, "", fmt.Errorf("%w; use --force to replace it", errEditedInstructionsBlock)
	}
	return replaceInstructionsBlock(data, start, end, reference), true, false,
		"replace the edited Kivgraph reference and preserve surrounding instructions", nil
}

func (manager Manager) installOpenCodeInstructions(file, path, sourcePath string, dryRun, force bool) (InstructionsPlan, error) {
	source, err := readInstructionsSource(sourcePath)
	if err != nil {
		return InstructionsPlan{}, err
	}
	data, exists, err := readDestination(path)
	if err != nil {
		return InstructionsPlan{}, err
	}
	updated, configChanged, managed, err := openCodeInstructionsUpdate(data, exists, sourcePath)
	if err != nil {
		return InstructionsPlan{}, fmt.Errorf("configure OpenCode instructions in %q: %w", path, err)
	}
	sourceChanged := !source.exists || !source.current
	if source.exists && !source.managed && !force {
		return InstructionsPlan{}, fmt.Errorf("%w in %q; use --force to replace it", errEditedInstructionsSource, sourcePath)
	}
	if !sourceChanged && !configChanged && managed {
		return InstructionsPlan{Action: ActionInstall, File: file, Path: path, SourcePath: sourcePath,
			Status: "managed", Detail: "OpenCode already loads the Kivgraph instructions"}, nil
	}
	plan := InstructionsPlan{Action: ActionInstall, File: file, Path: path, SourcePath: sourcePath,
		Status: "installed", Changed: true, DryRun: dryRun,
		Detail: "register the Kivgraph prompt in OpenCode's instruction list"}
	if dryRun {
		plan.Status = "would-install"
		return plan, nil
	}
	if _, err := installInstructionsSource(sourcePath, source, false, force); err != nil {
		return InstructionsPlan{}, err
	}
	if configChanged {
		if err := writeDestination(path, updated, exists, data); err != nil {
			return InstructionsPlan{}, err
		}
	}
	return plan, nil
}

func openCodeInstructionsUpdate(data []byte, exists bool, sourcePath string) ([]byte, bool, bool, error) {
	root := map[string]json.RawMessage{}
	if !exists || len(bytes.TrimSpace(data)) == 0 {
		root["instructions"] = json.RawMessage(fmt.Sprintf("[%q]", sourcePath))
		updated, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			return nil, false, false, fmt.Errorf("encode JSON: %w", err)
		}
		return append(updated, '\n'), true, false, nil
	}
	value, err := hujson.Parse(data)
	if err != nil {
		return nil, false, false, fmt.Errorf("parse JSONC: %w", err)
	}
	if _, ok := value.Value.(*hujson.Object); !ok {
		return nil, false, false, errors.New("OpenCode configuration must be a JSON object")
	}
	standardized, err := hujson.Standardize(append([]byte(nil), data...))
	if err != nil {
		return nil, false, false, fmt.Errorf("standardize JSONC: %w", err)
	}
	if err := json.Unmarshal(standardized, &root); err != nil {
		return nil, false, false, fmt.Errorf("parse JSONC: %w", err)
	}
	var instructions []string
	if raw, found := root["instructions"]; found {
		if err := json.Unmarshal(raw, &instructions); err != nil {
			return nil, false, false, errors.New("instructions must be an array of strings")
		}
	}
	for _, instruction := range instructions {
		if instruction == sourcePath {
			return data, false, true, nil
		}
	}
	updated, err := appendOpenCodeJSONCInstruction(data, &value, sourcePath)
	if err != nil {
		return nil, false, false, err
	}
	return updated, true, false, nil
}

// appendOpenCodeJSONCInstruction changes only the syntax range that owns the
// instructions list. HuJSON gives that range byte offsets, so comments,
// indentation, key order, and trailing commas outside the inserted value stay
// exactly as the user wrote them.
func appendOpenCodeJSONCInstruction(data []byte, root *hujson.Value, sourcePath string) ([]byte, error) {
	encodedSource, err := json.Marshal(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("encode OpenCode instruction: %w", err)
	}
	if instruction := root.Find("/instructions"); instruction != nil {
		array, ok := instruction.Value.(*hujson.Array)
		if !ok {
			return nil, errors.New("instructions must be an array of strings")
		}
		insert := encodedSource
		if len(array.Elements) > 0 {
			if array.Elements[len(array.Elements)-1].AfterExtra == nil {
				insert = append([]byte{','}, insert...)
			} else {
				insert = append(insert, ',')
			}
		}
		return insertOpenCodeJSONCBytes(data, instruction.EndOffset-1, insert), nil
	}
	object, ok := root.Value.(*hujson.Object)
	if !ok {
		return nil, errors.New("OpenCode configuration must be a JSON object")
	}
	encodedName, err := json.Marshal("instructions")
	if err != nil {
		return nil, fmt.Errorf("encode OpenCode instruction name: %w", err)
	}
	insert := append(encodedName, ':', ' ')
	insert = append(insert, '[')
	insert = append(insert, encodedSource...)
	insert = append(insert, ']')
	if len(object.Members) > 0 {
		if object.Members[len(object.Members)-1].Value.AfterExtra == nil {
			insert = append([]byte{','}, insert...)
		} else {
			insert = append(insert, ',')
		}
	}
	return insertOpenCodeJSONCBytes(data, root.EndOffset-1, insert), nil
}

func insertOpenCodeJSONCBytes(data []byte, offset int, insert []byte) []byte {
	updated := append([]byte(nil), data[:offset]...)
	updated = append(updated, insert...)
	return append(updated, data[offset:]...)
}

// instructionsPath resolves a relative user-configuration path without
// following destination or parent-directory symlinks.
func (manager Manager) instructionsPath(relativePath string) (string, error) {
	return manager.instructionsPathAt(manager.homeDir, relativePath)
}

func (manager Manager) codexInstructionsDir() string {
	if manager.codexDir != "" {
		return manager.codexDir
	}
	return filepath.Join(manager.homeDir, ".codex")
}

func (manager Manager) ohMyPiInstructionsDir() string {
	if manager.ohMyPiDir != "" {
		return manager.ohMyPiDir
	}
	return filepath.Join(manager.homeDir, ".omp", "agent")
}

func (manager Manager) instructionsPathAt(root, relativePath string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("instructions root %q must be absolute", root)
	}
	if err := manager.validateInstructionsRoot(root); err != nil {
		return "", err
	}
	path := filepath.Join(root, relativePath)
	withinRoot, err := filepath.Rel(root, path)
	if err != nil || withinRoot == ".." || strings.HasPrefix(withinRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("instructions path %q escapes root %q", path, root)
	}
	if err := validateInstructionsParent(filepath.Dir(path), root); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect instructions path %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing symlink instructions path %q", path)
	}
	return path, nil
}

func (manager Manager) validateInstructionsRoot(root string) error {
	limit := filepath.Clean(root)
	if relative, err := filepath.Rel(manager.homeDir, root); err == nil &&
		relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		limit = filepath.Clean(manager.homeDir)
	}
	for current := filepath.Clean(root); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing symlink instructions parent %q", current)
			}
			if !info.IsDir() {
				return fmt.Errorf("instructions parent %q is not a directory", current)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect instructions parent %q: %w", current, err)
		}
		if current == limit {
			return nil
		}
	}
}

func validateInstructionsParent(parent, home string) error {
	for current := filepath.Clean(parent); ; current = filepath.Dir(current) {
		if filepath.Clean(current) == filepath.Clean(home) {
			return nil
		}
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing symlink instructions parent %q", current)
			}
			if !info.IsDir() {
				return fmt.Errorf("instructions parent %q is not a directory", current)
			}
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect instructions parent %q: %w", current, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("instructions parent %q escapes user home %q", current, home)
		}
	}
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
	if bytes.Contains(data[beginAt+len(begin):], begin) ||
		bytes.Contains(data[endAt+len(finish):], finish) {
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

func managedInstructionsReference(newline, sourcePath string) []byte {
	return []byte(instructionsBeginMarker + newline + "@" + sourcePath + newline + instructionsEndMarker)
}

func legacyManagedInstructionsReference(newline string) []byte {
	return []byte(instructionsBeginMarker + newline + "@" + InstructionsCanonicalFile + newline + instructionsEndMarker)
}

func legacyManagedInstructionsBlock(newline string) []byte {
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
