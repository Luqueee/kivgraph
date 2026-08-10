// Package integrations manages the local configuration and skill files owned by
// Ladygraph for supported MCP clients. It deliberately keeps each client's
// schema behind this package so the CLI only deals in targets and scopes.
package integrations

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	TargetClaudeCode    = "claude-code"
	TargetClaudeDesktop = "claude-desktop"
	TargetCodex         = "codex"
	TargetOpenCode      = "opencode"
	TargetOhMyPi        = "oh-my-pi"

	ScopeUser    = "user"
	ScopeProject = "project"
)

type Scope string

type Target string

type Action string

const (
	ActionInstall Action = "install"
	ActionRemove  Action = "remove"
	ActionStatus  Action = "status"
)

// Options identifies the filesystem roots used by a Manager. HomeDir and
// ProjectDir are injectable to make safety checks testable without touching a
// real client configuration.
type Options struct {
	HomeDir    string
	ProjectDir string
	Executable string
	GOOS       string
}

// Manager applies integration plans for one local user and one project.
type Manager struct {
	homeDir    string
	projectDir string
	executable string
	goos       string
}

// Plan describes an inspected or applied integration operation.
type Plan struct {
	Action  Action
	Target  Target
	Scope   Scope
	Path    string
	Status  string
	Changed bool
	DryRun  bool
	Detail  string
}

// New validates and resolves the local roots used by an integration manager.
func New(options Options) (Manager, error) {
	var err error
	homeDir := options.HomeDir
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return Manager{}, fmt.Errorf("resolve user home: %w", err)
		}
	}
	homeDir, err = absolutePath(homeDir, "home directory")
	if err != nil {
		return Manager{}, err
	}
	if info, statErr := os.Stat(homeDir); statErr == nil {
		if !info.IsDir() {
			return Manager{}, fmt.Errorf("home path %q is not a directory", homeDir)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Manager{}, fmt.Errorf("inspect home directory %q: %w", homeDir, statErr)
	}

	projectDir := options.ProjectDir
	if projectDir == "" {
		var err error
		projectDir, err = os.Getwd()
		if err != nil {
			return Manager{}, fmt.Errorf("resolve project directory: %w", err)
		}
	}
	projectDir, err = absolutePath(projectDir, "project directory")
	if err != nil {
		return Manager{}, err
	}
	if info, statErr := os.Stat(projectDir); statErr == nil {
		if !info.IsDir() {
			return Manager{}, fmt.Errorf("project path %q is not a directory", projectDir)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Manager{}, fmt.Errorf("inspect project directory %q: %w", projectDir, statErr)
	}

	executable := options.Executable
	if executable == "" {
		var err error
		executable, err = os.Executable()
		if err != nil {
			return Manager{}, fmt.Errorf("resolve Ladygraph executable: %w", err)
		}
	}
	executable, err = absolutePath(executable, "Ladygraph executable")
	if err != nil {
		return Manager{}, err
	}
	goos := options.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos != "darwin" && goos != "linux" {
		return Manager{}, fmt.Errorf("client integrations are supported only on darwin and linux, got %s", goos)
	}
	return Manager{homeDir: homeDir, projectDir: projectDir, executable: executable, goos: goos}, nil
}

// InstallMCP registers the local MCP server, or returns an idempotent plan if
// the exact Ladygraph-managed entry already exists.
func (manager Manager) InstallMCP(target Target, scope Scope, dryRun, force bool) (Plan, error) {
	document, err := manager.mcpDocument(target, scope)
	if err != nil {
		return Plan{}, err
	}
	if document.format == formatTOML {
		return manager.installTOML(document, dryRun, force)
	}
	return manager.installJSON(document, dryRun, force)
}

// RemoveMCP removes only an exact Ladygraph-managed MCP entry. An incompatible
// entry requires force, even when the target name is the same.
func (manager Manager) RemoveMCP(target Target, scope Scope, dryRun, force bool) (Plan, error) {
	document, err := manager.mcpDocument(target, scope)
	if err != nil {
		return Plan{}, err
	}
	if document.format == formatTOML {
		return manager.removeTOML(document, dryRun, force)
	}
	return manager.removeJSON(document, dryRun, force)
}

// StatusMCP inspects one target without changing its file.
func (manager Manager) StatusMCP(target Target, scope Scope) (Plan, error) {
	document, err := manager.mcpDocument(target, scope)
	if err != nil {
		return Plan{}, err
	}
	if document.format == formatTOML {
		return manager.statusTOML(document)
	}
	return manager.statusJSON(document)
}

// InstallSkill copies the canonical embedded skill to a client-native path.
func (manager Manager) InstallSkill(target Target, scope Scope, dryRun, force bool) (Plan, error) {
	path, err := manager.skillPath(target, scope)
	if err != nil {
		return Plan{}, err
	}
	return manager.installSkillFile(target, scope, path, dryRun, force)
}

// RemoveSkill removes only the exact canonical skill installed by Ladygraph.
func (manager Manager) RemoveSkill(target Target, scope Scope, dryRun, force bool) (Plan, error) {
	path, err := manager.skillPath(target, scope)
	if err != nil {
		return Plan{}, err
	}
	return manager.removeSkillFile(target, scope, path, dryRun, force)
}

// StatusSkill inspects one client-native skill path.
func (manager Manager) StatusSkill(target Target, scope Scope) (Plan, error) {
	path, err := manager.skillPath(target, scope)
	if err != nil {
		return Plan{}, err
	}
	data, exists, err := readDestination(path)
	if err != nil {
		return Plan{}, err
	}
	status := "absent"
	detail := "skill is not installed"
	if exists {
		if bytes.Equal(data, embeddedSkill) {
			status = "managed"
			detail = "skill matches the embedded Ladygraph skill"
		} else {
			status = "incompatible"
			detail = "skill exists but does not match the embedded Ladygraph skill"
		}
	}
	return Plan{Action: ActionStatus, Target: target, Scope: scope, Path: path, Status: status, Detail: detail}, nil
}

type fileFormat uint8

const (
	formatJSON fileFormat = iota
	formatTOML
)

type mcpDocument struct {
	target  Target
	scope   Scope
	path    string
	format  fileFormat
	section string
}

func (manager Manager) mcpDocument(target Target, scope Scope) (mcpDocument, error) {
	if err := validateScope(scope); err != nil {
		return mcpDocument{}, err
	}
	path, format, section, err := manager.mcpPath(target, scope)
	if err != nil {
		return mcpDocument{}, err
	}
	return mcpDocument{target: target, scope: scope, path: path, format: format, section: section}, nil
}

func (manager Manager) mcpPath(target Target, scope Scope) (string, fileFormat, string, error) {
	switch target {
	case TargetClaudeCode:
		if scope == ScopeUser {
			return filepath.Join(manager.homeDir, ".claude.json"), formatJSON, "mcpServers", nil
		}
		return filepath.Join(manager.projectDir, ".mcp.json"), formatJSON, "mcpServers", nil
	case TargetClaudeDesktop:
		if scope != ScopeUser {
			return "", 0, "", fmt.Errorf("target %q does not support project scope", target)
		}
		switch manager.goos {
		case "darwin":
			return filepath.Join(manager.homeDir, "Library", "Application Support", "Claude", "claude_desktop_config.json"), formatJSON, "mcpServers", nil
		case "linux":
			return filepath.Join(manager.homeDir, ".config", "Claude", "claude_desktop_config.json"), formatJSON, "mcpServers", nil
		default:
			return "", 0, "", fmt.Errorf("target %q is unsupported on %s", target, manager.goos)
		}
	case TargetCodex:
		if scope == ScopeUser {
			return filepath.Join(manager.homeDir, ".codex", "config.toml"), formatTOML, "mcp_servers", nil
		}
		return filepath.Join(manager.projectDir, ".codex", "config.toml"), formatTOML, "mcp_servers", nil
	case TargetOpenCode:
		if scope == ScopeUser {
			return filepath.Join(manager.homeDir, ".config", "opencode", "opencode.json"), formatJSON, "mcp", nil
		}
		return filepath.Join(manager.projectDir, "opencode.json"), formatJSON, "mcp", nil
	case TargetOhMyPi:
		if scope == ScopeUser {
			return filepath.Join(manager.homeDir, ".omp", "agent", "mcp.json"), formatJSON, "mcpServers", nil
		}
		return filepath.Join(manager.projectDir, ".omp", "mcp.json"), formatJSON, "mcpServers", nil
	default:
		return "", 0, "", unsupportedTarget(target)
	}
}

func (manager Manager) skillPath(target Target, scope Scope) (string, error) {
	if err := validateScope(scope); err != nil {
		return "", err
	}
	base := manager.homeDir
	if scope == ScopeProject {
		base = manager.projectDir
	}
	switch target {
	case TargetClaudeCode:
		return filepath.Join(base, ".claude", "skills", "ladygraph", "SKILL.md"), nil
	case TargetCodex:
		return filepath.Join(base, ".agents", "skills", "ladygraph", "SKILL.md"), nil
	case TargetOpenCode:
		if scope == ScopeUser {
			return filepath.Join(base, ".config", "opencode", "skills", "ladygraph", "SKILL.md"), nil
		}
		return filepath.Join(base, ".opencode", "skills", "ladygraph", "SKILL.md"), nil
	case TargetOhMyPi:
		if scope == ScopeUser {
			return filepath.Join(base, ".omp", "agent", "skills", "ladygraph", "SKILL.md"), nil
		}
		return filepath.Join(base, ".omp", "skills", "ladygraph", "SKILL.md"), nil
	case TargetClaudeDesktop:
		return "", fmt.Errorf("target %q does not support local skill installation", target)
	default:
		return "", unsupportedTarget(target)
	}
}

func validateScope(scope Scope) error {
	if scope != ScopeUser && scope != ScopeProject {
		return fmt.Errorf("unsupported scope %q (want %q or %q)", scope, ScopeUser, ScopeProject)
	}
	return nil
}

func unsupportedTarget(target Target) error {
	return fmt.Errorf("unsupported integration target %q (want %s)", target, strings.Join([]string{
		TargetClaudeCode, TargetClaudeDesktop, TargetCodex, TargetOpenCode, TargetOhMyPi,
	}, ", "))
}

func (manager Manager) expectedJSONEntry(target Target) map[string]any {
	if target == TargetOpenCode {
		return map[string]any{
			"type":    "local",
			"command": []any{manager.executable, "serve"},
			"enabled": true,
		}
	}
	return map[string]any{"command": manager.executable, "args": []any{"serve"}}
}

func (manager Manager) expectedTOMLEntry() map[string]any {
	return map[string]any{"command": manager.executable, "args": []any{"serve"}}
}

type jsonState struct {
	root    map[string]json.RawMessage
	section map[string]json.RawMessage
	data    []byte
	exists  bool
	status  string
}

func (manager Manager) readJSON(document mcpDocument) (jsonState, error) {
	data, exists, err := readDestination(document.path)
	if err != nil {
		return jsonState{}, err
	}
	root := map[string]json.RawMessage{}
	if exists {
		if err := json.Unmarshal(data, &root); err != nil {
			return jsonState{}, fmt.Errorf("parse %s: %w", document.path, err)
		}
		if root == nil {
			return jsonState{}, fmt.Errorf("parse %s: top-level JSON value must be an object", document.path)
		}
	}
	section := map[string]json.RawMessage{}
	if raw, ok := root[document.section]; ok {
		if err := json.Unmarshal(raw, &section); err != nil || section == nil {
			return jsonState{}, fmt.Errorf("parse %s: %s must be an object", document.path, document.section)
		}
	}
	status := "absent"
	if raw, ok := section["ladygraph"]; ok {
		if rawJSONMatches(raw, manager.expectedJSONEntry(document.target)) {
			status = "managed"
		} else {
			status = "incompatible"
		}
	}
	return jsonState{root: root, section: section, data: data, exists: exists, status: status}, nil
}

func (manager Manager) statusJSON(document mcpDocument) (Plan, error) {
	state, err := manager.readJSON(document)
	if err != nil {
		return Plan{}, err
	}
	return Plan{Action: ActionStatus, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Detail: jsonStatusDetail(state.status)}, nil
}

func (manager Manager) installJSON(document mcpDocument, dryRun, force bool) (Plan, error) {
	state, err := manager.readJSON(document)
	if err != nil {
		return Plan{}, err
	}
	if state.status == "managed" {
		return Plan{Action: ActionInstall, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Detail: "MCP entry already matches Ladygraph"}, nil
	}
	if state.status == "incompatible" && !force {
		return Plan{}, incompatibleError(document.path)
	}
	state.section["ladygraph"], err = json.Marshal(manager.expectedJSONEntry(document.target))
	if err != nil {
		return Plan{}, fmt.Errorf("encode MCP entry: %w", err)
	}
	state.root[document.section], err = json.Marshal(state.section)
	if err != nil {
		return Plan{}, fmt.Errorf("encode %s: %w", document.section, err)
	}
	data, err := json.MarshalIndent(state.root, "", "  ")
	if err != nil {
		return Plan{}, fmt.Errorf("encode %s: %w", document.path, err)
	}
	data = append(data, '\n')
	plan := Plan{Action: ActionInstall, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Changed: true, DryRun: dryRun, Detail: "register Ladygraph MCP server"}
	if dryRun {
		plan.Status = "would-install"
		return plan, nil
	}
	if err := writeDestination(document.path, data, state.exists, state.data); err != nil {
		return Plan{}, err
	}
	plan.Status = "installed"
	return plan, nil
}

func (manager Manager) removeJSON(document mcpDocument, dryRun, force bool) (Plan, error) {
	state, err := manager.readJSON(document)
	if err != nil {
		return Plan{}, err
	}
	if state.status == "absent" {
		return Plan{Action: ActionRemove, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Detail: "MCP entry is not present"}, nil
	}
	if state.status == "incompatible" && !force {
		return Plan{}, incompatibleError(document.path)
	}
	delete(state.section, "ladygraph")
	state.root[document.section], err = json.Marshal(state.section)
	if err != nil {
		return Plan{}, fmt.Errorf("encode %s: %w", document.section, err)
	}
	data, err := json.MarshalIndent(state.root, "", "  ")
	if err != nil {
		return Plan{}, fmt.Errorf("encode %s: %w", document.path, err)
	}
	data = append(data, '\n')
	plan := Plan{Action: ActionRemove, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Changed: true, DryRun: dryRun, Detail: "remove Ladygraph MCP server"}
	if dryRun {
		plan.Status = "would-remove"
		return plan, nil
	}
	if err := writeDestination(document.path, data, state.exists, state.data); err != nil {
		return Plan{}, err
	}
	plan.Status = "removed"
	return plan, nil
}

func jsonStatusDetail(status string) string {
	switch status {
	case "managed":
		return "MCP entry matches Ladygraph"
	case "incompatible":
		return "MCP entry exists but does not match Ladygraph"
	default:
		return "MCP entry is not present"
	}
}

type tomlState struct {
	data   []byte
	exists bool
	status string
}

func (manager Manager) readTOML(document mcpDocument) (tomlState, error) {
	data, exists, err := readDestination(document.path)
	if err != nil {
		return tomlState{}, err
	}
	if !exists {
		return tomlState{data: data, exists: false, status: "absent"}, nil
	}
	var root map[string]interface{}
	if _, err := toml.Decode(string(data), &root); err != nil {
		return tomlState{}, fmt.Errorf("parse %s: %w", document.path, err)
	}
	status := "absent"
	if table, ok := tomlTable(root, "mcp_servers", "ladygraph"); ok {
		if valuesEqual(table, manager.expectedTOMLEntry()) {
			status = "managed"
		} else {
			status = "incompatible"
		}
	} else if _, ok := root["mcp_servers"]; ok {
		if _, tableOK := tomlMap(root["mcp_servers"]); !tableOK {
			return tomlState{}, fmt.Errorf("parse %s: mcp_servers must be a TOML table", document.path)
		}
	}
	return tomlState{data: data, exists: true, status: status}, nil
}

func (manager Manager) statusTOML(document mcpDocument) (Plan, error) {
	state, err := manager.readTOML(document)
	if err != nil {
		return Plan{}, err
	}
	return Plan{Action: ActionStatus, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Detail: tomlStatusDetail(state.status)}, nil
}

func (manager Manager) installTOML(document mcpDocument, dryRun, force bool) (Plan, error) {
	state, err := manager.readTOML(document)
	if err != nil {
		return Plan{}, err
	}
	if state.status == "managed" {
		return Plan{Action: ActionInstall, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Detail: "MCP entry already matches Ladygraph"}, nil
	}
	if state.status == "incompatible" && !force {
		return Plan{}, incompatibleError(document.path)
	}
	data := state.data
	if state.status == "incompatible" {
		data, err = removeTOMLSection(data, "mcp_servers.ladygraph")
		if err != nil {
			return Plan{}, fmt.Errorf("replace Ladygraph TOML table: %w", err)
		}
	}
	data = appendTOMLSection(data, manager.executable)
	plan := Plan{Action: ActionInstall, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Changed: true, DryRun: dryRun, Detail: "register Ladygraph MCP server"}
	if dryRun {
		plan.Status = "would-install"
		return plan, nil
	}
	if err := writeDestination(document.path, data, state.exists, state.data); err != nil {
		return Plan{}, err
	}
	plan.Status = "installed"
	return plan, nil
}

func (manager Manager) removeTOML(document mcpDocument, dryRun, force bool) (Plan, error) {
	state, err := manager.readTOML(document)
	if err != nil {
		return Plan{}, err
	}
	if state.status == "absent" {
		return Plan{Action: ActionRemove, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Detail: "MCP entry is not present"}, nil
	}
	if state.status == "incompatible" && !force {
		return Plan{}, incompatibleError(document.path)
	}
	data, err := removeTOMLSection(state.data, "mcp_servers.ladygraph")
	if err != nil {
		return Plan{}, fmt.Errorf("remove Ladygraph TOML table: %w", err)
	}
	plan := Plan{Action: ActionRemove, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Changed: true, DryRun: dryRun, Detail: "remove Ladygraph MCP server"}
	if dryRun {
		plan.Status = "would-remove"
		return plan, nil
	}
	if err := writeDestination(document.path, data, state.exists, state.data); err != nil {
		return Plan{}, err
	}
	plan.Status = "removed"
	return plan, nil
}

func tomlStatusDetail(status string) string {
	switch status {
	case "managed":
		return "MCP entry matches Ladygraph"
	case "incompatible":
		return "MCP entry exists but does not match Ladygraph"
	default:
		return "MCP entry is not present"
	}
}

func tomlTable(root map[string]interface{}, parent, child string) (map[string]interface{}, bool) {
	parentValue, ok := root[parent]
	if !ok {
		return nil, false
	}
	parentTable, ok := tomlMap(parentValue)
	if !ok {
		return nil, false
	}
	childValue, ok := parentTable[child]
	if !ok {
		return nil, false
	}
	childTable, ok := tomlMap(childValue)
	return childTable, ok
}

func tomlMap(value any) (map[string]interface{}, bool) {
	table, ok := value.(map[string]interface{})
	return table, ok
}

func valuesEqual(actual, expected map[string]interface{}) bool {
	if len(actual) != len(expected) {
		return false
	}
	for key, expectedValue := range expected {
		actualValue, ok := actual[key]
		if !ok || !reflect.DeepEqual(normalizeValue(actualValue), normalizeValue(expectedValue)) {
			return false
		}
	}
	return true
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]interface{}:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			result[key] = normalizeValue(child)
		}
		return result
	case []interface{}:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = normalizeValue(child)
		}
		return result
	default:
		return value
	}
}

func appendTOMLSection(data []byte, executable string) []byte {
	var builder bytes.Buffer
	builder.Write(data)
	if builder.Len() > 0 && data[len(data)-1] != '\n' {
		builder.WriteByte('\n')
	}
	if builder.Len() > 0 {
		builder.WriteByte('\n')
	}
	builder.WriteString("[mcp_servers.ladygraph]\n")
	builder.WriteString("command = ")
	builder.WriteString(tomlQuote(executable))
	builder.WriteString("\nargs = [\"serve\"]\n")
	return builder.Bytes()
}

func tomlQuote(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\r", "\\r")
	value = strings.ReplaceAll(value, "\t", "\\t")
	return "\"" + value + "\""
}

func removeTOMLSection(data []byte, name string) ([]byte, error) {
	lines := strings.SplitAfter(string(data), "\n")
	start := -1
	end := -1
	for index, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if start < 0 {
			if tomlHeaderMatches(trimmed, name) {
				start = index
			}
			continue
		}
		if strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[[") {
			end = index
			break
		}
	}
	if start > 0 && strings.TrimSpace(strings.TrimSuffix(lines[start-1], "\n")) == "" {
		start--
	}
	if start < 0 {
		return nil, fmt.Errorf("table [%s] is not represented by a supported header", name)
	}
	if end < 0 {
		end = len(lines)
	}
	result := make([]string, 0, len(lines)-(end-start))
	result = append(result, lines[:start]...)
	result = append(result, lines[end:]...)
	return []byte(strings.Join(result, "")), nil
}

func tomlHeaderMatches(line, name string) bool {
	header := "[" + name + "]"
	if line == header {
		return true
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, header))
	return rest != line && strings.HasPrefix(rest, "#")
}

func (manager Manager) installSkillFile(target Target, scope Scope, path string, dryRun, force bool) (Plan, error) {
	data, exists, err := readDestination(path)
	if err != nil {
		return Plan{}, err
	}
	status := "absent"
	if exists {
		if bytes.Equal(data, embeddedSkill) {
			return Plan{Action: ActionInstall, Target: target, Scope: scope, Path: path, Status: "managed", Detail: "skill already matches Ladygraph"}, nil
		}
		status = "incompatible"
		if !force {
			return Plan{}, incompatibleError(path)
		}
	}
	plan := Plan{Action: ActionInstall, Target: target, Scope: scope, Path: path, Status: status, Changed: true, DryRun: dryRun, Detail: "install Ladygraph skill"}
	if dryRun {
		plan.Status = "would-install"
		return plan, nil
	}
	if err := writeDestination(path, embeddedSkill, exists, data); err != nil {
		return Plan{}, err
	}
	plan.Status = "installed"
	return plan, nil
}

func (manager Manager) removeSkillFile(target Target, scope Scope, path string, dryRun, force bool) (Plan, error) {
	data, exists, err := readDestination(path)
	if err != nil {
		return Plan{}, err
	}
	if !exists {
		return Plan{Action: ActionRemove, Target: target, Scope: scope, Path: path, Status: "absent", Detail: "skill is not installed"}, nil
	}
	managed := bytes.Equal(data, embeddedSkill)
	if !managed && !force {
		return Plan{}, incompatibleError(path)
	}
	plan := Plan{Action: ActionRemove, Target: target, Scope: scope, Path: path, Status: "managed", Changed: true, DryRun: dryRun, Detail: "remove Ladygraph skill"}
	if !managed {
		plan.Status = "incompatible"
	}
	if dryRun {
		plan.Status = "would-remove"
		return plan, nil
	}
	if err := removeDestination(path, data); err != nil {
		return Plan{}, err
	}
	plan.Status = "removed"
	return plan, nil
}

func readDestination(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect integration path %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, false, fmt.Errorf("refusing symlink integration path %q", path)
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("integration path %q is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("read integration path %q: %w", path, err)
	}
	return data, true, nil
}

func writeDestination(path string, data []byte, exists bool, previous []byte) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create integration directory %q: %w", parent, err)
	}
	if exists {
		if err := preserveBackup(path, previous); err != nil {
			return err
		}
	}
	temporary, err := os.CreateTemp(parent, ".ladygraph-integration-*")
	if err != nil {
		return fmt.Errorf("create integration temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set integration permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write integration file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync integration file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close integration file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace integration file %q: %w", path, err)
	}
	if err := syncDirectory(parent); err != nil {
		return fmt.Errorf("sync integration directory %q: %w", parent, err)
	}
	return nil
}

func preserveBackup(path string, data []byte) error {
	backupPath := path + ".ladygraph.bak"
	info, err := os.Lstat(backupPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing unsafe integration backup %q", backupPath)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect integration backup %q: %w", backupPath, err)
	}
	file, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create integration backup %q: %w", backupPath, err)
	}
	if _, writeErr := file.Write(data); writeErr != nil {
		_ = file.Close()
		_ = os.Remove(backupPath)
		return fmt.Errorf("write integration backup %q: %w", backupPath, writeErr)
	}
	if syncErr := file.Sync(); syncErr != nil {
		_ = file.Close()
		_ = os.Remove(backupPath)
		return fmt.Errorf("sync integration backup %q: %w", backupPath, syncErr)
	}
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("close integration backup %q: %w", backupPath, closeErr)
	}
	return nil
}

func removeDestination(path string, previous []byte) error {
	if err := preserveBackup(path, previous); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	tombstone, err := os.CreateTemp(parent, ".ladygraph-remove-*")
	if err != nil {
		return fmt.Errorf("create integration removal marker: %w", err)
	}
	tombstonePath := tombstone.Name()
	if err := tombstone.Close(); err != nil {
		_ = os.Remove(tombstonePath)
		return fmt.Errorf("close integration removal marker: %w", err)
	}
	if err := os.Remove(tombstonePath); err != nil {
		return fmt.Errorf("remove integration removal marker: %w", err)
	}
	if err := os.Rename(path, tombstonePath); err != nil {
		return fmt.Errorf("move integration path %q for removal: %w", path, err)
	}
	if err := syncDirectory(parent); err != nil {
		return fmt.Errorf("sync integration directory %q: %w", parent, err)
	}
	if err := os.Remove(tombstonePath); err != nil {
		return fmt.Errorf("delete removed integration path: %w", err)
	}
	if err := syncDirectory(parent); err != nil {
		return fmt.Errorf("sync integration directory %q: %w", parent, err)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func incompatibleError(path string) error {
	return fmt.Errorf("integration path %q contains an incompatible Ladygraph entry; use --force to replace or remove it", path)
}

func absolutePath(value, label string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must not be empty", label)
	}
	resolved, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve %s %q: %w", label, value, err)
	}
	return filepath.Clean(resolved), nil
}

func rawJSONMatches(raw json.RawMessage, expected map[string]any) bool {
	var actual map[string]any
	if err := json.Unmarshal(raw, &actual); err != nil || actual == nil {
		return false
	}
	return reflect.DeepEqual(actual, expected)
}
