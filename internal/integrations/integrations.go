// Package integrations manages the local configuration and skill files owned by
// Kivgraph for supported MCP clients. It deliberately keeps each client's
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
	"sort"
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

// Endpoint is a running daemon's HTTP endpoint.
//
// When it is set, a plan points clients at that url instead of telling each one
// to spawn `serve`. That is the difference the daemon was built for, and this is
// the transport it is reachable over. At the load a real editor produces -- most
// often none at all: 48 of 51 servers in a real event log were asked nothing --
// N clients spawning `serve` cost `33 MB` of private pages each against
// `0,8`-`1,2 MB` per client on one daemon, `40` against `265 MB` at eight
// clients. Answering a question adds about `7 MB` to a server that already paid
// `33` to start, so what a daemon saves is the start. The peak is the widest gap:
// `994`-`1.000` against `134 MB`, because eight editors starting at once pay
// eight loads. Measured in `benchmarks/daemon-cost`.
//
// The two transports are indistinguishable: `33,4`-`33,7 MB` per client over HTTP
// against `33,1`-`34,4` over the socket. HTTP costs more only under sustained
// traffic, and no real session produces it.
type Endpoint struct {
	// URL is the streamable HTTP endpoint a client connects to.
	URL string
	// Token goes into the entry as an `Authorization: Bearer` header. It is
	// written literally, because no client reads a token out of a file for us,
	// and that is why an endpoint entry refuses project scope: a project file
	// is committed.
	Token string
}

// set reports whether an endpoint should be written into a client entry.
//
// A half-specified endpoint never reaches here: New refuses it. Silently
// treating a url with no token as "no endpoint" would install a stdio entry a
// caller asked not to install, and treating it as an endpoint would write a
// `Bearer ` header that every request fails on.
func (endpoint Endpoint) set() bool {
	return endpoint.URL != "" && endpoint.Token != ""
}

// ErrIncompleteEndpoint reports an endpoint missing one of its two halves.
var ErrIncompleteEndpoint = errors.New("an endpoint needs both a url and a token")

// validate refuses an endpoint that names one half of itself.
func (endpoint Endpoint) validate() error {
	if endpoint == (Endpoint{}) || endpoint.set() {
		return nil
	}
	missing := "token"
	if endpoint.URL == "" {
		missing = "url"
	}
	return fmt.Errorf("%w: the %s is empty", ErrIncompleteEndpoint, missing)
}

// Options identifies the filesystem roots used by a Manager. HomeDir and
// ProjectDir are injectable to make safety checks testable without touching a
// real client configuration.
type Options struct {
	HomeDir    string
	ProjectDir string
	Executable string
	GOOS       string
	// Endpoint, when set, makes plans point at a running daemon over HTTP
	// rather than at this executable over stdio.
	Endpoint Endpoint
}

// Manager applies integration plans for one local user and one project.
type Manager struct {
	homeDir    string
	projectDir string
	executable string
	goos       string
	endpoint   Endpoint
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

// TargetDetection reports whether a supported client appears to be installed
// in the requested scope.
type TargetDetection struct {
	Target   Target
	Detected bool
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
			return Manager{}, fmt.Errorf("resolve Kivgraph executable: %w", err)
		}
	}
	executable, err = absolutePath(executable, "Kivgraph executable")
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
	if err := options.Endpoint.validate(); err != nil {
		return Manager{}, err
	}
	return Manager{
		homeDir:    homeDir,
		projectDir: projectDir,
		executable: executable,
		goos:       goos,
		endpoint:   options.Endpoint,
	}, nil
}

// InstallMCP registers the local MCP server, or returns an idempotent plan if
// the exact Kivgraph-managed entry already exists.
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

// RemoveMCP removes only an exact Kivgraph-managed MCP entry. An incompatible
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

// DetectMCPTargets returns every MCP target supported by the requested scope,
// marking the targets whose local configuration or installation markers exist.
func (manager Manager) DetectMCPTargets(scope Scope) ([]TargetDetection, error) {
	return manager.detectTargets(scope, false)
}

// DetectSkillTargets returns every local skill target supported by the
// requested scope, marking the targets whose local installation markers exist.
func (manager Manager) DetectSkillTargets(scope Scope) ([]TargetDetection, error) {
	return manager.detectTargets(scope, true)
}

// KnownTargets is the vocabulary of clients this package can register, in the
// order detection reports them. It is a function rather than a slice so no
// caller can reorder or truncate the list for the next one.
func KnownTargets() []Target {
	return []Target{
		TargetClaudeCode,
		TargetClaudeDesktop,
		TargetCodex,
		TargetOpenCode,
		TargetOhMyPi,
	}
}

func (manager Manager) detectTargets(scope Scope, skill bool) ([]TargetDetection, error) {
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	targets := KnownTargets()
	detections := make([]TargetDetection, 0, len(targets))
	for _, target := range targets {
		if target == TargetClaudeDesktop && (skill || scope == ScopeProject) {
			continue
		}
		paths, err := manager.targetDetectionPaths(target, scope, skill)
		if err != nil {
			return nil, err
		}
		detected := false
		for _, path := range paths {
			exists, err := pathExists(path)
			if err != nil {
				return nil, err
			}
			if exists {
				detected = true
				break
			}
		}
		detections = append(detections, TargetDetection{Target: target, Detected: detected})
	}
	return detections, nil
}

func (manager Manager) targetDetectionPaths(target Target, scope Scope, skill bool) ([]string, error) {
	paths := make([]string, 0, 4)
	if skill {
		path, err := manager.skillPath(target, scope)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	} else {
		path, _, _, err := manager.mcpPath(target, scope)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}

	base := manager.homeDir
	if scope == ScopeProject {
		base = manager.projectDir
	}
	switch target {
	case TargetClaudeCode:
		paths = append(paths, filepath.Join(base, ".claude"))
	case TargetClaudeDesktop:
		if manager.goos == "darwin" {
			paths = append(paths,
				filepath.Join(manager.homeDir, "Applications", "Claude.app"),
				"/Applications/Claude.app",
			)
		} else {
			paths = append(paths,
				filepath.Join(manager.homeDir, ".local", "share", "applications", "claude.desktop"),
				"/usr/share/applications/claude.desktop",
			)
		}
	case TargetCodex:
		paths = append(paths, filepath.Join(base, ".codex"))
		if skill {
			paths = append(paths, filepath.Join(base, ".agents"))
		}
	case TargetOpenCode:
		if scope == ScopeUser {
			paths = append(paths, filepath.Join(base, ".config", "opencode"))
		}
		paths = append(paths, filepath.Join(base, ".opencode"))
	case TargetOhMyPi:
		paths = append(paths, filepath.Join(base, ".omp"))
	}
	return paths, nil
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("inspect detection path %q: %w", path, err)
}

// InstallSkill copies the canonical embedded skill to a client-native path.
func (manager Manager) InstallSkill(target Target, scope Scope, dryRun, force bool) (Plan, error) {
	path, err := manager.skillPath(target, scope)
	if err != nil {
		return Plan{}, err
	}
	return manager.installSkillFile(target, scope, path, dryRun, force)
}

// RemoveSkill removes only the exact canonical skill installed by Kivgraph.
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
			detail = "skill matches the embedded Kivgraph skill"
		} else {
			status = "incompatible"
			detail = "skill exists but does not match the embedded Kivgraph skill"
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

// ErrEndpointNeedsUserScope reports an attempt to write a daemon's token into a
// file that lives in the repository.
var ErrEndpointNeedsUserScope = errors.New("an endpoint entry carries a token, so it is written only in user scope")

func (manager Manager) mcpDocument(target Target, scope Scope) (mcpDocument, error) {
	if err := validateScope(scope); err != nil {
		return mcpDocument{}, err
	}
	// Project scope writes inside the repository, and those files are committed
	// as a matter of course -- `.mcp.json` is meant to be. A stdio entry names
	// an executable and is safe to share; an endpoint entry names a secret.
	if manager.endpoint.set() && scope == ScopeProject {
		return mcpDocument{}, fmt.Errorf("%w: %s would be committed", ErrEndpointNeedsUserScope, scope)
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
		return filepath.Join(base, ".claude", "skills", "kivgraph", "SKILL.md"), nil
	case TargetCodex:
		return filepath.Join(base, ".agents", "skills", "kivgraph", "SKILL.md"), nil
	case TargetOpenCode:
		if scope == ScopeUser {
			return filepath.Join(base, ".config", "opencode", "skills", "kivgraph", "SKILL.md"), nil
		}
		return filepath.Join(base, ".opencode", "skills", "kivgraph", "SKILL.md"), nil
	case TargetOhMyPi:
		if scope == ScopeUser {
			return filepath.Join(base, ".omp", "agent", "skills", "kivgraph", "SKILL.md"), nil
		}
		return filepath.Join(base, ".omp", "skills", "kivgraph", "SKILL.md"), nil
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

// expectedJSONEntry is the entry Kivgraph owns in a JSON client file.
//
// The three shapes are the clients', not ours: `mcpServers` takes `type: http`
// with a `headers` map, and OpenCode's `mcp` section names the same thing
// `remote`. Writing one shape for all of them would leave two clients unable to
// parse their own configuration.
func (manager Manager) expectedJSONEntry(target Target) map[string]any {
	if manager.endpoint.set() {
		headers := map[string]any{"Authorization": bearer(manager.endpoint.Token)}
		if target == TargetOpenCode {
			return map[string]any{
				"type":    "remote",
				"url":     manager.endpoint.URL,
				"enabled": true,
				"headers": headers,
			}
		}
		return map[string]any{
			"type":    "http",
			"url":     manager.endpoint.URL,
			"headers": headers,
		}
	}
	if target == TargetOpenCode {
		return map[string]any{
			"type":    "local",
			"command": []any{manager.executable, "serve"},
			"enabled": true,
		}
	}
	return map[string]any{"command": manager.executable, "args": []any{"serve"}}
}

// expectedTOMLEntry is the entry Kivgraph owns in Codex's config.
//
// Codex picks its transport from the shape: a `url` instead of a `command` is
// what selects streamable HTTP. The token goes in `http_headers` rather than
// `bearer_token_env_var`, because the latter names an environment variable the
// user would have to export -- which is not something an integration can install.
func (manager Manager) expectedTOMLEntry() map[string]any {
	if manager.endpoint.set() {
		return map[string]any{
			"url":          manager.endpoint.URL,
			"http_headers": map[string]any{"Authorization": bearer(manager.endpoint.Token)},
		}
	}
	return map[string]any{"command": manager.executable, "args": []any{"serve"}}
}

// bearer formats the header value every one of these clients sends verbatim.
func bearer(token string) string { return "Bearer " + token }

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
	if raw, ok := section["kivgraph"]; ok {
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
		return Plan{Action: ActionInstall, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Detail: "MCP entry already matches Kivgraph"}, nil
	}
	if state.status == "incompatible" && !force {
		return Plan{}, incompatibleError(document.path)
	}
	state.section["kivgraph"], err = json.Marshal(manager.expectedJSONEntry(document.target))
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
	plan := Plan{Action: ActionInstall, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Changed: true, DryRun: dryRun, Detail: "register Kivgraph MCP server"}
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
	delete(state.section, "kivgraph")
	state.root[document.section], err = json.Marshal(state.section)
	if err != nil {
		return Plan{}, fmt.Errorf("encode %s: %w", document.section, err)
	}
	data, err := json.MarshalIndent(state.root, "", "  ")
	if err != nil {
		return Plan{}, fmt.Errorf("encode %s: %w", document.path, err)
	}
	data = append(data, '\n')
	plan := Plan{Action: ActionRemove, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Changed: true, DryRun: dryRun, Detail: "remove Kivgraph MCP server"}
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
		return "MCP entry matches Kivgraph"
	case "incompatible":
		return "MCP entry exists but does not match Kivgraph"
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
	if table, ok := tomlTable(root, "mcp_servers", "kivgraph"); ok {
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
		return Plan{Action: ActionInstall, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Detail: "MCP entry already matches Kivgraph"}, nil
	}
	if state.status == "incompatible" && !force {
		return Plan{}, incompatibleError(document.path)
	}
	data := state.data
	if state.status == "incompatible" {
		data, err = removeTOMLSection(data, "mcp_servers.kivgraph")
		if err != nil {
			return Plan{}, fmt.Errorf("replace Kivgraph TOML table: %w", err)
		}
	}
	data = appendTOMLSection(data, manager.expectedTOMLEntry())
	plan := Plan{Action: ActionInstall, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Changed: true, DryRun: dryRun, Detail: "register Kivgraph MCP server"}
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
	data, err := removeTOMLSection(state.data, "mcp_servers.kivgraph")
	if err != nil {
		return Plan{}, fmt.Errorf("remove Kivgraph TOML table: %w", err)
	}
	plan := Plan{Action: ActionRemove, Target: document.target, Scope: document.scope, Path: document.path, Status: state.status, Changed: true, DryRun: dryRun, Detail: "remove Kivgraph MCP server"}
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
		return "MCP entry matches Kivgraph"
	case "incompatible":
		return "MCP entry exists but does not match Kivgraph"
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

// appendTOMLSection renders the entry Kivgraph owns under `[mcp_servers]`.
//
// It renders whatever expectedTOMLEntry declares rather than a fixed pair of
// lines, because the two are compared against each other: a writer that emitted
// `command` while the comparison expected `url` would report a managed entry it
// had never written, and reinstall forever.
func appendTOMLSection(data []byte, entry map[string]any) []byte {
	var builder bytes.Buffer
	builder.Write(data)
	if builder.Len() > 0 && data[len(data)-1] != '\n' {
		builder.WriteByte('\n')
	}
	if builder.Len() > 0 {
		builder.WriteByte('\n')
	}
	builder.WriteString("[mcp_servers.kivgraph]\n")

	// Scalars and arrays first, then sub-tables: in TOML a key after a table
	// header belongs to that table, so a scalar written last would land inside
	// `http_headers`. Sorted, so two installs of the same entry produce the
	// same bytes.
	tables := make([]string, 0, 1)
	for _, key := range sortedKeys(entry) {
		if nested, ok := entry[key].(map[string]any); ok {
			_ = nested
			tables = append(tables, key)
			continue
		}
		builder.WriteString(key)
		builder.WriteString(" = ")
		builder.WriteString(tomlValue(entry[key]))
		builder.WriteByte('\n')
	}
	for _, key := range tables {
		nested := entry[key].(map[string]any)
		builder.WriteString("\n[mcp_servers.kivgraph.")
		builder.WriteString(key)
		builder.WriteString("]\n")
		for _, inner := range sortedKeys(nested) {
			builder.WriteString(inner)
			builder.WriteString(" = ")
			builder.WriteString(tomlValue(nested[inner]))
			builder.WriteByte('\n')
		}
	}
	return builder.Bytes()
}

// sortedKeys keeps the rendering deterministic, which is what makes an install
// idempotent.
func sortedKeys(entry map[string]any) []string {
	keys := make([]string, 0, len(entry))
	for key := range entry {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// tomlValue renders the two value kinds a Codex entry actually holds: the
// strings of a url, a path or a header, and the string array of an argv.
//
// There is no case for a boolean, because no TOML entry carries one -- OpenCode's
// `enabled` is JSON. A branch for a value that cannot arrive is a branch no test
// can falsify, so the default quotes whatever else appears and a wrong-looking
// quoted value in the file is the signal.
func tomlValue(value any) string {
	switch typed := value.(type) {
	case string:
		return tomlQuote(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, tomlValue(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return tomlQuote(fmt.Sprint(typed))
	}
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
			return Plan{Action: ActionInstall, Target: target, Scope: scope, Path: path, Status: "managed", Detail: "skill already matches Kivgraph"}, nil
		}
		status = "incompatible"
		if !force {
			return Plan{}, incompatibleError(path)
		}
	}
	plan := Plan{Action: ActionInstall, Target: target, Scope: scope, Path: path, Status: status, Changed: true, DryRun: dryRun, Detail: "install Kivgraph skill"}
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
	plan := Plan{Action: ActionRemove, Target: target, Scope: scope, Path: path, Status: "managed", Changed: true, DryRun: dryRun, Detail: "remove Kivgraph skill"}
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
	temporary, err := os.CreateTemp(parent, ".kivgraph-integration-*")
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
	backupPath := path + ".kivgraph.bak"
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
	tombstone, err := os.CreateTemp(parent, ".kivgraph-remove-*")
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
	return fmt.Errorf("integration path %q contains an incompatible Kivgraph entry; use --force to replace or remove it", path)
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
