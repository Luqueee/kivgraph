package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// CurrentSchemaVersion is the configuration schema understood by Luque.
	CurrentSchemaVersion = 1

	defaultConfigFile       = "~/.config/luque/config.yaml"
	defaultRepositoriesFile = "~/.config/luque/repositories.yaml"

	defaultDatabasePath  = "~/.local/state/luque/graph.lbdb"
	defaultSnapshotsPath = "~/.local/state/luque/snapshots"
	defaultBackupsPath   = "~/.local/state/luque/backups"
	defaultSyntheticWork = "~/.local/state/luque/go.work"

	maximumConfiguredDepth = 5
)

// Duration is a YAML duration such as 10m or 30s.
type Duration time.Duration

// UnmarshalYAML parses a Go duration from a YAML scalar.
func (duration *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
		return fmt.Errorf("duration must be a string such as 10m or 30s")
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value.Value))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value.Value, err)
	}
	*duration = Duration(parsed)
	return nil
}

// MarshalYAML emits durations in the same form accepted by UnmarshalYAML.
func (duration Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(duration).String(), nil
}

// String returns the duration in Go's standard representation.
func (duration Duration) String() string {
	return time.Duration(duration).String()
}

// Config is the complete Luque configuration document.
type Config struct {
	Version    int              `yaml:"version"`
	Workspace  WorkspaceConfig  `yaml:"workspace"`
	Storage    StorageConfig    `yaml:"storage"`
	MCP        MCPConfig        `yaml:"mcp"`
	Indexing   IndexingConfig   `yaml:"indexing"`
	Watcher    WatcherConfig    `yaml:"watcher"`
	TypeScript TypeScriptConfig `yaml:"typescript"`
	Go         GoConfig         `yaml:"go"`
	Telemetry  TelemetryConfig  `yaml:"telemetry"`
	Logging    LoggingConfig    `yaml:"logging"`
}

// WorkspaceConfig points to the repository registry.
type WorkspaceConfig struct {
	RepositoriesFile string `yaml:"repositories_file"`
}

// StorageConfig controls persistent graph and snapshot locations.
type StorageConfig struct {
	DatabasePath    string `yaml:"database_path"`
	SnapshotsPath   string `yaml:"snapshots_path"`
	BackupsPath     string `yaml:"backups_path"`
	RetainSnapshots int    `yaml:"retain_snapshots"`
}

// MCPConfig controls transport and query limits.
type MCPConfig struct {
	Transport           string `yaml:"transport"`
	DefaultLimit        int    `yaml:"default_limit"`
	MaximumLimit        int    `yaml:"maximum_limit"`
	MaximumDepth        int    `yaml:"maximum_depth"`
	MaximumVisitedNodes int    `yaml:"maximum_visited_nodes"`
}

// IndexingConfig controls which facts and references are retained.
type IndexingConfig struct {
	GeneratedFiles            string `yaml:"generated_files"`
	UnresolvedReferences      string `yaml:"unresolved_references"`
	SyntaxAcceleration        bool   `yaml:"syntax_acceleration"`
	FullRebuildOnSchemaChange bool   `yaml:"full_rebuild_on_schema_change"`
}

// WatcherConfig controls incremental file watching.
type WatcherConfig struct {
	Enabled                  bool     `yaml:"enabled"`
	DebounceMilliseconds     int      `yaml:"debounce_ms"`
	MaximumBatchMilliseconds int      `yaml:"maximum_batch_ms"`
	ReconciliationInterval   Duration `yaml:"reconciliation_interval"`
}

// TypeScriptConfig controls the persistent TypeScript worker.
type TypeScriptConfig struct {
	WorkerCommand      string   `yaml:"worker_command"`
	MaximumWorkers     int      `yaml:"maximum_workers"`
	ProjectIdleTimeout Duration `yaml:"project_idle_timeout"`
}

// GoConfig controls Go-specific synthetic workspace behavior.
type GoConfig struct {
	SyntheticWorkFile string `yaml:"synthetic_work_file"`
	IncludeTests      bool   `yaml:"include_tests"`
}

// TelemetryConfig controls metrics and tracing.
type TelemetryConfig struct {
	Metrics bool `yaml:"metrics"`
	Traces  bool `yaml:"traces"`
}

// LoggingConfig controls the process log output.
type LoggingConfig struct {
	Format string `yaml:"format"`
	Level  string `yaml:"level"`
}

// RepositoriesFile is the repository registry document referenced by Config.
type RepositoriesFile struct {
	Version      int          `yaml:"version"`
	Repositories []Repository `yaml:"repositories"`
}

// Repository identifies one source repository to index.
type Repository struct {
	Name       string   `yaml:"name"`
	Path       string   `yaml:"path"`
	Languages  []string `yaml:"languages"`
	Manifests  []string `yaml:"manifests,omitempty"`
	Roots      []string `yaml:"roots,omitempty"`
	Exclusions []string `yaml:"exclusions,omitempty"`
}

// Loaded combines the validated configuration and its repository registry.
type Loaded struct {
	Config           Config
	Repositories     RepositoriesFile
	ConfigPath       string
	RepositoriesPath string
}

// DefaultConfig returns the explicit defaults from the configuration contract.
// Paths use the documented home-directory notation until Load expands them.
func DefaultConfig() Config {
	return Config{
		Version: CurrentSchemaVersion,
		Workspace: WorkspaceConfig{
			RepositoriesFile: defaultRepositoriesFile,
		},
		Storage: StorageConfig{
			DatabasePath:    defaultDatabasePath,
			SnapshotsPath:   defaultSnapshotsPath,
			BackupsPath:     defaultBackupsPath,
			RetainSnapshots: 3,
		},
		MCP: MCPConfig{
			Transport:           "stdio",
			DefaultLimit:        50,
			MaximumLimit:        500,
			MaximumDepth:        maximumConfiguredDepth,
			MaximumVisitedNodes: 25_000,
		},
		Indexing: IndexingConfig{
			GeneratedFiles:            "include",
			UnresolvedReferences:      "retain",
			SyntaxAcceleration:        true,
			FullRebuildOnSchemaChange: true,
		},
		Watcher: WatcherConfig{
			Enabled:                  true,
			DebounceMilliseconds:     150,
			MaximumBatchMilliseconds: 500,
			ReconciliationInterval:   Duration(10 * time.Minute),
		},
		TypeScript: TypeScriptConfig{
			WorkerCommand:      "luque-ts-worker",
			MaximumWorkers:     3,
			ProjectIdleTimeout: Duration(30 * time.Minute),
		},
		Go: GoConfig{
			SyntheticWorkFile: defaultSyntheticWork,
			IncludeTests:      false,
		},
		Telemetry: TelemetryConfig{
			Metrics: true,
			Traces:  false,
		},
		Logging: LoggingConfig{
			Format: "json",
			Level:  "info",
		},
	}
}

// DefaultConfigPath returns the expanded default config path.
func DefaultConfigPath() (string, error) {
	return expandPath(defaultConfigFile, "")
}

// DefaultRepositoriesPath returns the expanded default repository registry path.
func DefaultRepositoriesPath() (string, error) {
	return expandPath(defaultRepositoriesFile, "")
}

// Load reads, validates, and combines config.yaml with its repositories.yaml.
// An empty path selects ~/.config/luque/config.yaml.
func Load(path string) (Loaded, error) {
	configPath, err := resolveConfigPath(path)
	if err != nil {
		return Loaded{}, fmt.Errorf("resolve config path: %w", err)
	}
	configuration, err := loadConfigFile(configPath)
	if err != nil {
		return Loaded{}, fmt.Errorf("load config %q: %w", configPath, err)
	}

	repositoriesPath, err := expandConfigPath(configuration.Workspace.RepositoriesFile, filepath.Dir(configPath), "workspace.repositories_file")
	if err != nil {
		return Loaded{}, err
	}
	repositories, err := loadRepositoriesFile(repositoriesPath)
	if err != nil {
		return Loaded{}, fmt.Errorf("load repositories %q: %w", repositoriesPath, err)
	}
	configuration.Workspace.RepositoriesFile = repositoriesPath
	return Loaded{
		Config:           configuration,
		Repositories:     repositories,
		ConfigPath:       configPath,
		RepositoriesPath: repositoriesPath,
	}, nil
}

// LoadConfig reads and validates only the main configuration document.
func LoadConfig(path string) (Config, error) {
	configPath, err := resolveConfigPath(path)
	if err != nil {
		return Config{}, fmt.Errorf("resolve config path: %w", err)
	}
	configuration, err := loadConfigFile(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("load config %q: %w", configPath, err)
	}
	return configuration, nil
}

// LoadRepositories reads and validates a repository registry document.
// An empty path selects ~/.config/luque/repositories.yaml.
func LoadRepositories(path string) (RepositoriesFile, error) {
	repositoriesPath, err := resolveRepositoriesPath(path)
	if err != nil {
		return RepositoriesFile{}, fmt.Errorf("resolve repositories path: %w", err)
	}
	repositories, err := loadRepositoriesFile(repositoriesPath)
	if err != nil {
		return RepositoriesFile{}, fmt.Errorf("load repositories %q: %w", repositoriesPath, err)
	}
	return repositories, nil
}

func loadConfigFile(path string) (Config, error) {
	configuration := DefaultConfig()
	versionPresent, err := decodeYAML(path, &configuration)
	if err != nil {
		return Config{}, err
	}
	if !versionPresent {
		return Config{}, errors.New("config.version: field is required")
	}
	base := filepath.Dir(path)
	if err := expandConfigPaths(&configuration, base); err != nil {
		return Config{}, err
	}
	if err := validateConfig(configuration); err != nil {
		return Config{}, err
	}
	return configuration, nil
}

func loadRepositoriesFile(path string) (RepositoriesFile, error) {
	var repositories RepositoriesFile
	versionPresent, err := decodeYAML(path, &repositories)
	if err != nil {
		return RepositoriesFile{}, err
	}
	if !versionPresent {
		return RepositoriesFile{}, errors.New("repositories.version: field is required")
	}
	for index := range repositories.Repositories {
		expanded, err := expandPath(repositories.Repositories[index].Path, filepath.Dir(path))
		if err != nil {
			return RepositoriesFile{}, fmt.Errorf("repositories[%d].path: %w", index, err)
		}
		repositories.Repositories[index].Path = expanded
	}
	if err := validateRepositories(repositories); err != nil {
		return RepositoriesFile{}, err
	}
	return repositories, nil
}

func decodeYAML(path string, destination any) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read YAML: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(destination); err != nil {
		if errors.Is(err, io.EOF) {
			return false, errors.New("YAML document is empty")
		}
		return false, fmt.Errorf("decode YAML: %w", err)
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err == nil {
		return false, errors.New("multiple YAML documents are not supported")
	} else if !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("decode trailing YAML: %w", err)
	}

	var root yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&root); err != nil {
		return false, fmt.Errorf("inspect YAML: %w", err)
	}
	return hasMappingField(&root, "version"), nil
}

func hasMappingField(root *yaml.Node, field string) bool {
	if root == nil {
		return false
	}
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) != 1 {
			return false
		}
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return false
	}
	for index := 0; index+1 < len(root.Content); index += 2 {
		if root.Content[index].Value == field {
			return true
		}
	}
	return false
}

func expandConfigPaths(configuration *Config, base string) error {
	paths := []struct {
		name  string
		value *string
	}{
		{"workspace.repositories_file", &configuration.Workspace.RepositoriesFile},
		{"storage.database_path", &configuration.Storage.DatabasePath},
		{"storage.snapshots_path", &configuration.Storage.SnapshotsPath},
		{"storage.backups_path", &configuration.Storage.BackupsPath},
		{"go.synthetic_work_file", &configuration.Go.SyntheticWorkFile},
	}
	for _, path := range paths {
		expanded, err := expandPath(*path.value, base)
		if err != nil {
			return fmt.Errorf("config.%s: %w", path.name, err)
		}
		*path.value = expanded
	}
	return nil
}

func expandConfigPath(value, base, field string) (string, error) {
	expanded, err := expandPath(value, base)
	if err != nil {
		return "", fmt.Errorf("config.%s: %w", field, err)
	}
	return expanded, nil
}

func validateConfig(configuration Config) error {
	if configuration.Version != CurrentSchemaVersion {
		return fmt.Errorf("config.version: unsupported schema version %d, want %d", configuration.Version, CurrentSchemaVersion)
	}
	for field, value := range map[string]string{
		"workspace.repositories_file": configuration.Workspace.RepositoriesFile,
		"storage.database_path":       configuration.Storage.DatabasePath,
		"storage.snapshots_path":      configuration.Storage.SnapshotsPath,
		"storage.backups_path":        configuration.Storage.BackupsPath,
		"go.synthetic_work_file":      configuration.Go.SyntheticWorkFile,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("config.%s: path must be absolute after expansion, got %q", field, value)
		}
	}
	if configuration.Storage.RetainSnapshots < 1 {
		return fmt.Errorf("config.storage.retain_snapshots: must be positive, got %d", configuration.Storage.RetainSnapshots)
	}
	if configuration.MCP.Transport != "stdio" {
		return fmt.Errorf("config.mcp.transport: unsupported transport %q, want %q", configuration.MCP.Transport, "stdio")
	}
	if configuration.MCP.DefaultLimit < 1 {
		return fmt.Errorf("config.mcp.default_limit: must be positive, got %d", configuration.MCP.DefaultLimit)
	}
	if configuration.MCP.MaximumLimit < configuration.MCP.DefaultLimit {
		return fmt.Errorf("config.mcp.maximum_limit: must be at least default_limit (%d), got %d", configuration.MCP.DefaultLimit, configuration.MCP.MaximumLimit)
	}
	if configuration.MCP.MaximumDepth < 1 || configuration.MCP.MaximumDepth > maximumConfiguredDepth {
		return fmt.Errorf("config.mcp.maximum_depth: must be between 1 and %d, got %d", maximumConfiguredDepth, configuration.MCP.MaximumDepth)
	}
	if configuration.MCP.MaximumVisitedNodes < 1 {
		return fmt.Errorf("config.mcp.maximum_visited_nodes: must be positive, got %d", configuration.MCP.MaximumVisitedNodes)
	}
	if configuration.Indexing.GeneratedFiles == "" {
		return errors.New("config.indexing.generated_files: must not be empty")
	}
	if configuration.Indexing.UnresolvedReferences == "" {
		return errors.New("config.indexing.unresolved_references: must not be empty")
	}
	if configuration.Watcher.DebounceMilliseconds < 1 {
		return fmt.Errorf("config.watcher.debounce_ms: must be positive, got %d", configuration.Watcher.DebounceMilliseconds)
	}
	if configuration.Watcher.MaximumBatchMilliseconds < configuration.Watcher.DebounceMilliseconds {
		return fmt.Errorf("config.watcher.maximum_batch_ms: must be at least debounce_ms (%d), got %d", configuration.Watcher.DebounceMilliseconds, configuration.Watcher.MaximumBatchMilliseconds)
	}
	if configuration.Watcher.ReconciliationInterval <= 0 {
		return fmt.Errorf("config.watcher.reconciliation_interval: must be positive, got %s", configuration.Watcher.ReconciliationInterval)
	}
	if strings.TrimSpace(configuration.TypeScript.WorkerCommand) == "" {
		return errors.New("config.typescript.worker_command: must not be empty")
	}
	if configuration.TypeScript.MaximumWorkers < 1 {
		return fmt.Errorf("config.typescript.maximum_workers: must be positive, got %d", configuration.TypeScript.MaximumWorkers)
	}
	if configuration.TypeScript.ProjectIdleTimeout <= 0 {
		return fmt.Errorf("config.typescript.project_idle_timeout: must be positive, got %s", configuration.TypeScript.ProjectIdleTimeout)
	}
	if configuration.Logging.Format != "json" && configuration.Logging.Format != "text" {
		return fmt.Errorf("config.logging.format: unsupported format %q, want json or text", configuration.Logging.Format)
	}
	switch configuration.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config.logging.level: unsupported level %q, want debug, info, warn, or error", configuration.Logging.Level)
	}
	return nil
}

func validateRepositories(repositories RepositoriesFile) error {
	if repositories.Version != CurrentSchemaVersion {
		return fmt.Errorf("repositories.version: unsupported schema version %d, want %d", repositories.Version, CurrentSchemaVersion)
	}
	if repositories.Repositories == nil {
		return errors.New("repositories: must contain at least one entry or an explicit empty list")
	}
	seenNames := make(map[string]int, len(repositories.Repositories))
	seenPaths := make(map[string]int, len(repositories.Repositories))
	for index, repository := range repositories.Repositories {
		field := fmt.Sprintf("repositories[%d]", index)
		if strings.TrimSpace(repository.Name) == "" {
			return fmt.Errorf("%s.name: must not be empty", field)
		}
		if previous, exists := seenNames[repository.Name]; exists {
			return fmt.Errorf("%s.name: duplicate of repositories[%d].name", field, previous)
		}
		seenNames[repository.Name] = index
		if !filepath.IsAbs(repository.Path) {
			return fmt.Errorf("%s.path: path must be absolute after expansion, got %q", field, repository.Path)
		}
		if previous, exists := seenPaths[repository.Path]; exists {
			return fmt.Errorf("%s.path: duplicate of repositories[%d].path", field, previous)
		}
		seenPaths[repository.Path] = index
		if len(repository.Languages) == 0 {
			return fmt.Errorf("%s.languages: must contain at least one language", field)
		}
		seenLanguages := make(map[string]struct{}, len(repository.Languages))
		for languageIndex, language := range repository.Languages {
			if strings.TrimSpace(language) == "" {
				return fmt.Errorf("%s.languages[%d]: must not be empty", field, languageIndex)
			}
			if _, exists := seenLanguages[language]; exists {
				return fmt.Errorf("%s.languages[%d]: duplicate language %q", field, languageIndex, language)
			}
			seenLanguages[language] = struct{}{}
		}
	}
	return nil
}

func resolveConfigPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = defaultConfigFile
	}
	return expandPath(path, "")
}

func resolveRepositoriesPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = defaultRepositoriesFile
	}
	return expandPath(path, "")
}

func expandPath(value, base string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("path must not be empty")
	}
	expanded, err := expandEnvironment(value)
	if err != nil {
		return "", err
	}
	if expanded == "~" || strings.HasPrefix(expanded, "~/") || strings.HasPrefix(expanded, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		expanded = filepath.Join(home, strings.TrimLeft(expanded[1:], `/\`))
	} else if strings.HasPrefix(expanded, "~") {
		return "", fmt.Errorf("unsupported user-home expansion in path %q", expanded)
	}
	if !filepath.IsAbs(expanded) {
		if base == "" {
			var err error
			base, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("resolve working directory: %w", err)
			}
		}
		expanded = filepath.Join(base, expanded)
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("make path absolute: %w", err)
	}
	return filepath.Clean(absolute), nil
}

func expandEnvironment(value string) (string, error) {
	var missing string
	expanded := os.Expand(value, func(key string) string {
		if key == "" {
			missing = "<empty>"
			return ""
		}
		value, exists := os.LookupEnv(key)
		if !exists {
			missing = key
			return ""
		}
		return value
	})
	if missing != "" {
		return "", fmt.Errorf("environment variable %q is not set", missing)
	}
	return expanded, nil
}
