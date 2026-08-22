package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// CurrentSchemaVersion is the configuration schema understood by Kivgraph.
	CurrentSchemaVersion = 1

	defaultConfigFile       = "~/.config/kivgraph/config.yaml"
	defaultRepositoriesFile = "~/.config/kivgraph/repositories.yaml"

	defaultDatabasePath  = "~/.local/state/kivgraph/graph.lbdb"
	defaultSnapshotsPath = "~/.local/state/kivgraph/snapshots"
	defaultBackupsPath   = "~/.local/state/kivgraph/backups"
	defaultFactCachePath = "~/.local/state/kivgraph/factcache"
	defaultSyntheticWork = "~/.local/state/kivgraph/go.work"
	defaultRustTargetDir = "~/.local/state/kivgraph/rust-target"
	defaultEventLogPath  = "~/.local/state/kivgraph/events.jsonl"

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

// Config is the complete Kivgraph configuration document.
type Config struct {
	Version    int              `yaml:"version"`
	Workspace  WorkspaceConfig  `yaml:"workspace"`
	Storage    StorageConfig    `yaml:"storage"`
	Web        WebConfig        `yaml:"web"`
	MCP        MCPConfig        `yaml:"mcp"`
	Indexing   IndexingConfig   `yaml:"indexing"`
	Watcher    WatcherConfig    `yaml:"watcher"`
	TypeScript TypeScriptConfig `yaml:"typescript"`
	Go         GoConfig         `yaml:"go"`
	Rust       RustConfig       `yaml:"rust"`
	Python     PythonConfig     `yaml:"python"`
	Dart       DartConfig       `yaml:"dart"`
	Telemetry  TelemetryConfig  `yaml:"telemetry"`
	Logging    LoggingConfig    `yaml:"logging"`
}

// WorkspaceConfig points to the repository registry.
type WorkspaceConfig struct {
	RepositoriesFile string `yaml:"repositories_file"`
}

// StorageConfig controls persistent graph locations.
//
// It used to carry snapshots_path and retain_snapshots. Neither did anything:
// nothing was ever written to the first, and nothing read the second. They are
// accepted and reported rather than rejected, because a configuration that was
// valid yesterday must not stop a server today. See ADR 0062.
type StorageConfig struct {
	DatabasePath string `yaml:"database_path"`
	BackupsPath  string `yaml:"backups_path"`
}

// retiredKey is a configuration key this build no longer implements.
//
// The decoder rejects unknown fields, which is what keeps a typo from being
// silently ignored, and that is exactly why a removed key needs naming: without
// this list, deleting a field from the struct would turn every configuration
// written by an older `kivgraph init` into a hard load failure over a key that
// never did anything. Punishing a user for our mistake is not a migration.
type retiredKey struct {
	Section string
	Field   string
	Reason  string
}

// retiredConfigKeys are accepted, ignored and reported. See ADR 0062.
var retiredConfigKeys = []retiredKey{
	{
		Section: "storage",
		Field:   "snapshots_path",
		Reason: "nothing was ever written there: a published snapshot lives inside " +
			"its own generation directory, which is what makes Prune delete it with " +
			"the generation and keeps it from being orphaned",
	},
	{
		Section: "storage",
		Field:   "retain_snapshots",
		Reason: "nothing read it: Prune keeps the current generation and its backup, " +
			"which is what a rollback needs",
	},
}

// Name is the dotted key a report and a doctor line name it by.
func (key retiredKey) Name() string { return key.Section + "." + key.Field }

// stripRetiredKeys removes the retired keys a document carries and reports
// which ones were there.
//
// It rewrites the document rather than relaxing the decoder, so a key that is
// neither known nor retired still fails: strictness is what this preserves.
// The result is only ever decoded, never written back, so losing comments and
// key order in the round trip costs nothing.
func stripRetiredKeys(data []byte) ([]byte, []string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		// A document this cannot parse is one the decoder below will report on
		// with a better message than anything invented here.
		return data, nil, nil
	}
	document := &root
	if document.Kind == yaml.DocumentNode {
		if len(document.Content) != 1 {
			return data, nil, nil
		}
		document = document.Content[0]
	}
	found := make([]string, 0, len(retiredConfigKeys))
	for _, key := range retiredConfigKeys {
		section := mappingValue(document, key.Section)
		if section == nil {
			continue
		}
		if removeMappingField(section, key.Field) {
			found = append(found, key.Name())
		}
	}
	if len(found) == 0 {
		return data, nil, nil
	}
	rewritten, err := yaml.Marshal(&root)
	if err != nil {
		return nil, nil, fmt.Errorf("rewrite configuration without retired keys: %w", err)
	}
	return rewritten, found, nil
}

// mappingValue returns the value node of field in a mapping, or nil.
func mappingValue(mapping *yaml.Node, field string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == field {
			return mapping.Content[index+1]
		}
	}
	return nil
}

// removeMappingField drops one key and its value from a mapping, reporting
// whether it was there. Keys come in pairs, so both halves go together.
func removeMappingField(mapping *yaml.Node, field string) bool {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return false
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value != field {
			continue
		}
		mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
		return true
	}
	return false
}

// WebConfig controls the explicit local HTTP viewer command.
type WebConfig struct {
	Address string `yaml:"address"`
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
	// FactCache decides whether an analysis unit may be served from the
	// facts a previous pass stored for it. `off` analyses everything,
	// `on` serves an entry whose recorded inputs all still match, and
	// `verify` analyses everything and fails the pass when a servable
	// entry disagrees with what the analysis produced.
	FactCache string `yaml:"fact_cache"`
	// FactCachePath holds one entry per analysis unit, outside every
	// indexed repository.
	FactCachePath string `yaml:"fact_cache_path"`
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
	// IncludeUnclaimedSources indexes the repository's TypeScript files that
	// no tsconfig claims. A file no project's "files"/"include" reaches
	// belongs to no program, so it is invisible by construction: nothing
	// type-checks it and nothing reports it absent. Enabling this loads
	// those files into TypeScript's inferred project, whose compiler
	// options are Kivgraph's choice and not the ones the project declaring
	// them would have applied -- there is no project declaring them. It is
	// off by default: what it adds is real, and what it adds it under an
	// authority weaker than a configured project's.
	IncludeUnclaimedSources bool `yaml:"include_unclaimed_sources"`
}

// GoConfig controls Go-specific synthetic workspace behavior.
type GoConfig struct {
	SyntheticWorkFile string `yaml:"synthetic_work_file"`
	IncludeTests      bool   `yaml:"include_tests"`
	// GOOS and GOARCH select the build platform. Empty values use the
	// platform of the configured Go toolchain.
	GOOS       string `yaml:"goos"`
	GOARCH     string `yaml:"goarch"`
	CGOEnabled *bool  `yaml:"cgo_enabled"`
	// BuildTags are the build constraints every Go load satisfies. A
	// package guarded by a tag that is absent here contributes no symbol to
	// the graph and is reported as unresolved.
	BuildTags []string `yaml:"build_tags"`
	// AllowNetwork lets the go command reach a module proxy while loading.
	// Indexing is hermetic by default: a module the local cache does not
	// hold is reported, not fetched. A multi-repository workspace resolves
	// one shared build list, so its selection can need a version no member
	// downloaded on its own.
	AllowNetwork bool `yaml:"allow_network"`
	// MaximumLoads bounds concurrent Go loads. Each load holds a complete
	// type universe, so this trades memory for speed. Zero uses the
	// processor count, capped.
	MaximumLoads int `yaml:"maximum_loads"`
}

// RustConfig controls the external rust-analyzer batch indexer.
//
// Rust is analysed by a process Kivgraph does not ship, so the command, the
// build configuration it is given and the directory its build artifacts land
// in all belong to the configuration rather than to the code.
type RustConfig struct {
	AnalyzerCommand string `yaml:"analyzer_command"`
	// MaximumWorkspaces bounds concurrent rust-analyzer invocations. Each
	// one holds a whole Cargo workspace and its sysroot in memory, so this
	// trades memory for speed. Zero uses the processor count, capped.
	MaximumWorkspaces int `yaml:"maximum_workspaces"`
	// Features, AllFeatures and NoDefaultFeatures decide which code exists
	// at all: a symbol behind an inactive feature is absent from the graph
	// and reported as unresolved.
	Features          []string `yaml:"features"`
	AllFeatures       bool     `yaml:"all_features"`
	NoDefaultFeatures bool     `yaml:"no_default_features"`
	// Cfgs are the additional `--cfg` values the analysis assumes.
	Cfgs []string `yaml:"cfgs"`
	// BuildScripts and ProcMacros keep generated code and derive expansions
	// in the graph. Disabling them is faster and declares what it lost.
	BuildScripts bool `yaml:"build_scripts"`
	ProcMacros   bool `yaml:"proc_macros"`
	// IncludeTests sets `cfg(test)` for the crates of the workspace, which
	// is the analyzer's own default. Turning it off removes every test item
	// from the graph, and the grammar then reports each one as a
	// declaration the index does not carry.
	IncludeTests bool `yaml:"include_tests"`
	// AllowNetwork lets cargo reach a registry while the analyzer loads a
	// workspace. Indexing is hermetic by default: a crate the local cache
	// does not hold is reported, not fetched.
	AllowNetwork bool `yaml:"allow_network"`
	// TargetDirectory holds the build artifacts of the analysis. It lives
	// outside every indexed repository: rust-analyzer runs build scripts,
	// and a pass never writes inside the code it indexes.
	TargetDirectory string `yaml:"target_directory"`
	// Sysroot is `discover`, `none`, or a path. Without a sysroot the
	// standard library is absent and the proc-macro server cannot start.
	//
	// It says where the standard library is, never whether it enters the
	// graph: loading it is what lets the analyzer resolve `Vec` at all, and
	// that is needed even when nothing about `core` is published.
	Sysroot string `yaml:"sysroot"`
	// IndexSysroot publishes the standard library as a synthetic provider
	// repository named after the toolchain release. It is off by default
	// because it multiplies the symbols of a graph by an order of magnitude:
	// one toolchain is around 350.000 monikers and half a minute of indexing.
	//
	// With it off, a `#[derive]`, an overloaded operator, a `?` and every call
	// into the standard library leave no edge, and the pass says so. With it
	// on, they resolve to the crate that declares them.
	IndexSysroot bool `yaml:"index_sysroot"`
}

// PythonConfig controls the external Python semantic indexer.
//
// The command is deliberately configurable: Python projects may use different
// interpreters, environments and Pyright-derived indexers. The indexer must
// emit Kivgraph's versioned Python facts payload.
type PythonConfig struct {
	IndexerCommand string `yaml:"indexer_command"`
	// AnalyzerCommand is an optional type-aware producer. It must emit the
	// versioned semantic payload understood by Kivgraph. The bundled AST
	// worker remains the fallback when AnalyzerMode is `fallback`.
	AnalyzerCommand  string `yaml:"analyzer_command"`
	AnalyzerMode     string `yaml:"analyzer_mode"`
	MaximumWorkers   int    `yaml:"maximum_workers"`
	PythonPath       string `yaml:"python_path"`
	IncludeTests     bool   `yaml:"include_tests"`
	IncludeGenerated bool   `yaml:"include_generated"`
	IncludeExternal  bool   `yaml:"include_external_packages"`
}

// DartConfig controls the Dart analysis-server based indexer.
type DartConfig struct {
	AnalyzerCommand     string   `yaml:"analyzer_command"`
	MaximumWorkers      int      `yaml:"maximum_workers"`
	SDKPath             string   `yaml:"sdk_path"`
	IncludeTests        bool     `yaml:"include_tests"`
	IncludeGenerated    bool     `yaml:"include_generated"`
	IncludeExternal     bool     `yaml:"include_external_packages"`
	IncludeSDK          bool     `yaml:"include_sdk"`
	PackageConfig       string   `yaml:"package_config"`
	WaitForAnalysis     bool     `yaml:"wait_for_analysis"`
	MaximumAnalysisTime Duration `yaml:"maximum_analysis_time"`
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
	// EventLogPath is the append-only record of indexing passes, tool calls
	// and server lifecycle that `kivgraph logs` and `kivgraph tool-stats`
	// read. It lives in the state directory rather than beside the
	// configuration because it is derived data: losing it costs history, not
	// a working installation.
	EventLogPath string `yaml:"event_log_path"`
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
	// RetiredKeys are the dotted names of keys the file carries that this
	// build no longer implements. They are reported rather than rejected, so
	// something has to be able to say they were there. See ADR 0062.
	RetiredKeys []string
}

// InitOptions controls creation of the local configuration files.
type InitOptions struct {
	ConfigPath       string
	RepositoriesPath string
	Force            bool
}

// InitResult reports what Initialize created.
type InitResult struct {
	ConfigPath          string
	RepositoriesPath    string
	ConfigCreated       bool
	RepositoriesCreated bool
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
			DatabasePath: defaultDatabasePath,
			BackupsPath:  defaultBackupsPath,
		},
		Web: WebConfig{
			// The viewer listens on every interface. It is unauthenticated
			// and its responses carry source paths, symbol names and
			// signatures, so `kivgraph ui` warns on every bind that is not
			// loopback -- which, with this default, is every bind. Restrict
			// it with `web.address` or `--addr`.
			Address: "0.0.0.0:7777",
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
			FactCache:                 "on",
			FactCachePath:             defaultFactCachePath,
		},
		Watcher: WatcherConfig{
			Enabled:                  true,
			DebounceMilliseconds:     150,
			MaximumBatchMilliseconds: 500,
			ReconciliationInterval:   Duration(10 * time.Minute),
		},
		TypeScript: TypeScriptConfig{
			WorkerCommand:           "kivgraph-ts-worker",
			MaximumWorkers:          3,
			ProjectIdleTimeout:      Duration(30 * time.Minute),
			IncludeUnclaimedSources: false,
		},
		Go: GoConfig{
			SyntheticWorkFile: defaultSyntheticWork,
			IncludeTests:      false,
			GOOS:              "",
			GOARCH:            "",
			CGOEnabled:        nil,
		},
		Rust: RustConfig{
			AnalyzerCommand: "rust-analyzer",
			BuildScripts:    true,
			ProcMacros:      true,
			IncludeTests:    true,
			TargetDirectory: defaultRustTargetDir,
			Sysroot:         "discover",
		},
		Python: PythonConfig{
			IndexerCommand:   "kivgraph-python-worker",
			AnalyzerCommand:  "kivgraph-python-pyright",
			AnalyzerMode:     "fallback",
			MaximumWorkers:   3,
			PythonPath:       "python3",
			IncludeTests:     false,
			IncludeGenerated: false,
			IncludeExternal:  false,
		},
		Dart: DartConfig{
			AnalyzerCommand:     "dart",
			MaximumWorkers:      2,
			SDKPath:             "dart",
			IncludeTests:        false,
			IncludeGenerated:    false,
			IncludeExternal:     false,
			IncludeSDK:          false,
			PackageConfig:       "auto",
			WaitForAnalysis:     true,
			MaximumAnalysisTime: Duration(5 * time.Minute),
		},
		Telemetry: TelemetryConfig{
			Metrics: true,
			Traces:  false,
		},
		Logging: LoggingConfig{
			Format:       "json",
			Level:        "info",
			EventLogPath: defaultEventLogPath,
		},
	}
}

// stateBesideConfig answers a configuration whose state lives next to
// configPath, or nil when configPath is the default location and the default
// state is already the right answer.
//
// Only the paths move. Everything else a caller may have asked for -- the
// registry it named, the languages, the budgets -- is untouched.
func stateBesideConfig(configPath string, ownRegistry bool) (*Config, error) {
	defaultPath, err := DefaultConfigPath()
	if err != nil {
		return nil, err
	}
	if filepath.Clean(configPath) == filepath.Clean(defaultPath) {
		return nil, nil
	}
	directory := filepath.Dir(configPath)
	state := filepath.Join(directory, "state")
	configuration := DefaultConfig()
	configuration.Storage.DatabasePath = filepath.Join(state, "graph.lbdb")
	configuration.Storage.BackupsPath = filepath.Join(state, "backups")
	configuration.Indexing.FactCachePath = filepath.Join(state, "factcache")
	configuration.Go.SyntheticWorkFile = filepath.Join(state, "go.work")
	configuration.Rust.TargetDirectory = filepath.Join(state, "rust-target")
	configuration.Logging.EventLogPath = filepath.Join(state, "events.jsonl")
	if ownRegistry {
		configuration.Workspace.RepositoriesFile = filepath.Join(directory, "repositories.yaml")
	}
	return &configuration, nil
}

// DefaultConfigPath returns the expanded default config path.
func DefaultConfigPath() (string, error) {
	return expandPath(defaultConfigFile, "")
}

// DefaultRepositoriesPath returns the expanded default repository registry path.
func DefaultRepositoriesPath() (string, error) {
	return expandPath(defaultRepositoriesFile, "")
}

// Initialize creates the default configuration and repository registry without
// replacing existing files unless Force is set. It also creates every local
// state directory named by the configuration.
func Initialize(options InitOptions) (InitResult, error) {
	configPath, err := resolveConfigPath(options.ConfigPath)
	if err != nil {
		return InitResult{}, fmt.Errorf("resolve config path: %w", err)
	}
	repositoriesPath, err := resolveRepositoriesPath(options.RepositoriesPath)
	if err != nil {
		return InitResult{}, fmt.Errorf("resolve repositories path: %w", err)
	}

	configuration := DefaultConfig()
	if strings.TrimSpace(options.ConfigPath) != "" {
		// A configuration written somewhere other than the default
		// location is an isolated one: a probe under /tmp used to be
		// created with storage still pointing at the state of the real
		// installation, so running it published generations over the
		// graph it was meant to leave alone.
		beside, err := stateBesideConfig(configPath, strings.TrimSpace(options.RepositoriesPath) == "")
		if err != nil {
			return InitResult{}, err
		}
		if beside != nil {
			configuration = *beside
			if strings.TrimSpace(options.RepositoriesPath) == "" {
				repositoriesPath = configuration.Workspace.RepositoriesFile
			}
		}
	}
	configuration.Workspace.RepositoriesFile = repositoriesPath
	if _, statErr := os.Stat(configPath); statErr == nil && !options.Force {
		configuration, _, err = loadConfigFile(configPath)
		if err != nil {
			return InitResult{}, fmt.Errorf("load existing config %q: %w", configPath, err)
		}
		repositoriesPath, err = expandConfigPath(
			configuration.Workspace.RepositoriesFile,
			filepath.Dir(configPath),
			"workspace.repositories_file",
		)
		if err != nil {
			return InitResult{}, err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return InitResult{}, fmt.Errorf("inspect config %q: %w", configPath, err)
	}

	configData, err := yaml.Marshal(configuration)
	if err != nil {
		return InitResult{}, fmt.Errorf("encode default config: %w", err)
	}
	repositoriesData, err := yaml.Marshal(RepositoriesFile{
		Version:      CurrentSchemaVersion,
		Repositories: []Repository{},
	})
	if err != nil {
		return InitResult{}, fmt.Errorf("encode default repositories: %w", err)
	}

	expandedConfiguration := configuration
	if err := expandConfigPaths(&expandedConfiguration, filepath.Dir(configPath)); err != nil {
		return InitResult{}, fmt.Errorf("expand default config paths: %w", err)
	}
	for _, directory := range []string{
		filepath.Dir(configPath),
		filepath.Dir(repositoriesPath),
		filepath.Dir(expandedConfiguration.Storage.DatabasePath),
		expandedConfiguration.Storage.BackupsPath,
		filepath.Dir(expandedConfiguration.Go.SyntheticWorkFile),
		filepath.Dir(expandedConfiguration.Logging.EventLogPath),
	} {
		if err := ensureDirectory(directory); err != nil {
			return InitResult{}, err
		}
	}

	configCreated, err := writeInitialFile(configPath, configData, options.Force)
	if err != nil {
		return InitResult{}, fmt.Errorf("write config %q: %w", configPath, err)
	}
	repositoriesCreated, err := writeInitialFile(repositoriesPath, repositoriesData, options.Force)
	if err != nil {
		return InitResult{}, fmt.Errorf("write repositories %q: %w", repositoriesPath, err)
	}
	return InitResult{
		ConfigPath:          configPath,
		RepositoriesPath:    repositoriesPath,
		ConfigCreated:       configCreated,
		RepositoriesCreated: repositoriesCreated,
	}, nil
}

func ensureDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create directory %q: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect directory %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path %q is not a directory", path)
	}
	return nil
}

func writeInitialFile(path string, data []byte, force bool) (bool, error) {
	if !force {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			return false, err
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return false, err
		}
		if err := file.Close(); err != nil {
			return false, err
		}
		return true, nil
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".kivgraph-init-*")
	if err != nil {
		return false, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return false, err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return false, err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return false, err
	}
	if err := temporary.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return false, err
	}
	return true, nil
}

// RegisterRepositories appends repositories to the validated registry. Paths
// are resolved relative to the registry file and duplicate names or paths are
// rejected before the file is replaced.
func RegisterRepositories(path string, additions []Repository) error {
	repositoriesPath, err := resolveRepositoriesPath(path)
	if err != nil {
		return fmt.Errorf("resolve repositories path: %w", err)
	}
	current, err := loadRepositoriesFile(repositoriesPath)
	if err != nil {
		return fmt.Errorf("load repositories %q: %w", repositoriesPath, err)
	}
	for index := range additions {
		addition := additions[index]
		expanded, err := expandPath(addition.Path, filepath.Dir(repositoriesPath))
		if err != nil {
			return fmt.Errorf("repositories addition %d path: %w", index, err)
		}
		addition.Path = expanded
		current.Repositories = append(current.Repositories, addition)
	}
	return SaveRepositories(repositoriesPath, current)
}

// SaveRepositories validates and atomically replaces a repository registry.
// Callers use this when a candidate registry has already been validated in
// memory and must become durable without exposing a partially written YAML
// document.
func SaveRepositories(path string, repositories RepositoriesFile) error {
	repositoriesPath, err := resolveRepositoriesPath(path)
	if err != nil {
		return fmt.Errorf("resolve repositories path: %w", err)
	}
	if err := validateRepositories(repositories); err != nil {
		return err
	}
	data, err := yaml.Marshal(repositories)
	if err != nil {
		return fmt.Errorf("encode repositories: %w", err)
	}
	if _, err := writeInitialFile(repositoriesPath, data, true); err != nil {
		return fmt.Errorf("write repositories %q: %w", repositoriesPath, err)
	}
	return nil
}

// Load reads, validates, and combines config.yaml with its repositories.yaml.
// An empty path selects ~/.config/kivgraph/config.yaml.
func Load(path string) (Loaded, error) {
	configPath, err := resolveConfigPath(path)
	if err != nil {
		return Loaded{}, fmt.Errorf("resolve config path: %w", err)
	}
	configuration, retired, err := loadConfigFile(configPath)
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
		RetiredKeys:      retired,
	}, nil
}

// LoadConfig reads and validates only the main configuration document.
func LoadConfig(path string) (Config, error) {
	configPath, err := resolveConfigPath(path)
	if err != nil {
		return Config{}, fmt.Errorf("resolve config path: %w", err)
	}
	configuration, _, err := loadConfigFile(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("load config %q: %w", configPath, err)
	}
	return configuration, nil
}

// LoadRepositories reads and validates a repository registry document.
// An empty path selects ~/.config/kivgraph/repositories.yaml.
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

func loadConfigFile(path string) (Config, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, nil, fmt.Errorf("read YAML: %w", err)
	}
	stripped, retired, err := stripRetiredKeys(data)
	if err != nil {
		return Config{}, nil, err
	}
	configuration := DefaultConfig()
	versionPresent, err := decodeYAMLBytes(stripped, &configuration)
	if err != nil {
		return Config{}, nil, err
	}
	if !versionPresent {
		return Config{}, nil, errors.New("config.version: field is required")
	}
	base := filepath.Dir(path)
	if err := expandConfigPaths(&configuration, base); err != nil {
		return Config{}, nil, err
	}
	if err := validateConfig(configuration); err != nil {
		return Config{}, nil, err
	}
	return configuration, retired, nil
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
	return decodeYAMLBytes(data, destination)
}

// decodeYAMLBytes is the strict decode both documents share. Unknown fields
// are an error here, which is what makes a retired key something that has to be
// removed from the bytes before this runs rather than tolerated inside it.
func decodeYAMLBytes(data []byte, destination any) (bool, error) {
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

// An empty event_log_path is refused rather than defaulted: the default lives
// in the shared state directory, so silently substituting it would make an
// isolated configuration write its history into the real installation.
func expandConfigPaths(configuration *Config, base string) error {
	paths := []struct {
		name  string
		value *string
	}{
		{"workspace.repositories_file", &configuration.Workspace.RepositoriesFile},
		{"storage.database_path", &configuration.Storage.DatabasePath},
		{"storage.backups_path", &configuration.Storage.BackupsPath},
		{"go.synthetic_work_file", &configuration.Go.SyntheticWorkFile},
		{"rust.target_directory", &configuration.Rust.TargetDirectory},
		{"indexing.fact_cache_path", &configuration.Indexing.FactCachePath},
		{"logging.event_log_path", &configuration.Logging.EventLogPath},
	}
	if strings.TrimSpace(configuration.Logging.EventLogPath) == "" {
		return errors.New("config.logging.event_log_path: must not be empty")
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
		"storage.backups_path":        configuration.Storage.BackupsPath,
		"go.synthetic_work_file":      configuration.Go.SyntheticWorkFile,
		"rust.target_directory":       configuration.Rust.TargetDirectory,
		"logging.event_log_path":      configuration.Logging.EventLogPath,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("config.%s: path must be absolute after expansion, got %q", field, value)
		}
	}
	if configuration.Web.Address == "" {
		return errors.New("config.web.address: must not be empty")
	}
	if _, port, err := net.SplitHostPort(configuration.Web.Address); err != nil {
		return fmt.Errorf("config.web.address: invalid listen address %q: %w", configuration.Web.Address, err)
	} else if number, err := strconv.Atoi(port); err != nil || number < 0 || number > 65535 {
		return fmt.Errorf("config.web.address: invalid port %q", port)
	}
	if configuration.MCP.Transport != "stdio" {
		return fmt.Errorf("config.mcp.transport: unsupported transport %q", configuration.MCP.Transport)
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
	// These two name what the pass does, and the pass does exactly one thing
	// with each: it indexes generated files and it retains every unresolved
	// reference, because a declared hole is a fact the graph owes its
	// readers. Accepting any other word would promise behaviour no code
	// implements, which is how a typo becomes a silent misconfiguration.
	switch configuration.Indexing.GeneratedFiles {
	case "include":
	default:
		return fmt.Errorf("config.indexing.generated_files: must be include, got %q", configuration.Indexing.GeneratedFiles)
	}
	switch configuration.Indexing.UnresolvedReferences {
	case "retain":
	default:
		return fmt.Errorf("config.indexing.unresolved_references: must be retain, got %q", configuration.Indexing.UnresolvedReferences)
	}
	switch configuration.Indexing.FactCache {
	case "off", "on", "verify":
	default:
		return fmt.Errorf("config.indexing.fact_cache: must be off, on or verify, got %q", configuration.Indexing.FactCache)
	}
	if configuration.Indexing.FactCache != "off" && strings.TrimSpace(configuration.Indexing.FactCachePath) == "" {
		return errors.New("config.indexing.fact_cache_path: must not be empty when the fact cache is enabled")
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
	if configuration.Go.MaximumLoads < 0 {
		return fmt.Errorf("config.go.maximum_loads: must not be negative, got %d", configuration.Go.MaximumLoads)
	}
	if configuration.TypeScript.MaximumWorkers < 1 {
		return fmt.Errorf("config.typescript.maximum_workers: must be positive, got %d", configuration.TypeScript.MaximumWorkers)
	}
	if configuration.TypeScript.ProjectIdleTimeout <= 0 {
		return fmt.Errorf("config.typescript.project_idle_timeout: must be positive, got %s", configuration.TypeScript.ProjectIdleTimeout)
	}
	for index, tag := range configuration.Go.BuildTags {
		if strings.TrimSpace(tag) == "" {
			return fmt.Errorf("config.go.build_tags[%d]: must not be empty", index)
		}
		if strings.ContainsAny(tag, ", \t") {
			return fmt.Errorf("config.go.build_tags[%d]: must not contain a comma or whitespace, got %q", index, tag)
		}
	}
	if strings.TrimSpace(configuration.Rust.AnalyzerCommand) == "" {
		return errors.New("config.rust.analyzer_command: must not be empty")
	}
	if configuration.Rust.MaximumWorkspaces < 0 {
		return fmt.Errorf("config.rust.maximum_workspaces: must not be negative, got %d", configuration.Rust.MaximumWorkspaces)
	}
	if configuration.Rust.AllFeatures && len(configuration.Rust.Features) != 0 {
		return errors.New("config.rust.all_features: must not be set together with rust.features")
	}
	for index, feature := range configuration.Rust.Features {
		if strings.TrimSpace(feature) == "" {
			return fmt.Errorf("config.rust.features[%d]: must not be empty", index)
		}
		if strings.ContainsAny(feature, ", \t") {
			return fmt.Errorf("config.rust.features[%d]: must not contain a comma or whitespace, got %q", index, feature)
		}
	}
	for index, cfg := range configuration.Rust.Cfgs {
		if strings.TrimSpace(cfg) == "" {
			return fmt.Errorf("config.rust.cfgs[%d]: must not be empty", index)
		}
		if strings.ContainsAny(cfg, ", \t") {
			return fmt.Errorf("config.rust.cfgs[%d]: must not contain a comma or whitespace, got %q", index, cfg)
		}
	}
	if strings.TrimSpace(configuration.Rust.Sysroot) == "" {
		return errors.New("config.rust.sysroot: must not be empty, want discover, none, or a path")
	}
	if strings.TrimSpace(configuration.Python.IndexerCommand) == "" {
		return errors.New("config.python.indexer_command: must not be empty")
	}
	if strings.TrimSpace(configuration.Python.AnalyzerCommand) == "" {
		return errors.New("config.python.analyzer_command: must not be empty")
	}
	switch strings.ToLower(strings.TrimSpace(configuration.Python.AnalyzerMode)) {
	case "fallback", "exact":
	default:
		return fmt.Errorf("config.python.analyzer_mode: unsupported mode %q, want fallback or exact", configuration.Python.AnalyzerMode)
	}
	if configuration.Python.MaximumWorkers < 1 {
		return fmt.Errorf("config.python.maximum_workers: must be positive, got %d", configuration.Python.MaximumWorkers)
	}
	if strings.TrimSpace(configuration.Python.PythonPath) == "" {
		return errors.New("config.python.python_path: must not be empty")
	}
	if strings.TrimSpace(configuration.Dart.AnalyzerCommand) == "" {
		return errors.New("config.dart.analyzer_command: must not be empty")
	}
	if configuration.Dart.MaximumWorkers < 1 {
		return fmt.Errorf("config.dart.maximum_workers: must be positive, got %d", configuration.Dart.MaximumWorkers)
	}
	if strings.TrimSpace(configuration.Dart.SDKPath) == "" {
		return errors.New("config.dart.sdk_path: must not be empty")
	}
	if strings.TrimSpace(configuration.Dart.PackageConfig) == "" {
		return errors.New("config.dart.package_config: must not be empty")
	}
	if configuration.Dart.MaximumAnalysisTime <= 0 {
		return errors.New("config.dart.maximum_analysis_time: must be positive")
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
			if !SupportedLanguage(language) {
				return fmt.Errorf("%s.languages[%d]: unsupported language %q, want one of %s",
					field, languageIndex, language, strings.Join(SupportedLanguages(), ", "))
			}
			seenLanguages[language] = struct{}{}
		}
	}
	return nil
}

// SupportedLanguages is the vocabulary an indexed repository may declare, in
// the order an error lists them.
//
// It lives here, next to the registry that stores the value, because the
// registry is where a language is written: a name the indexer cannot analyse
// used to be accepted by `init` and only rejected by the rebuild, hours later
// and in another process.
func SupportedLanguages() []string {
	return []string{"go", "typescript", "javascript", "ts", "js", "rust", "rs", "python", "py", "dart"}
}

// SupportedLanguage reports whether the indexer can analyse a repository that
// declares this language. The comparison ignores case and surrounding space,
// which is what every consumer of the field already does.
func SupportedLanguage(language string) bool {
	normalised := strings.ToLower(strings.TrimSpace(language))
	for _, supported := range SupportedLanguages() {
		if normalised == supported {
			return true
		}
	}
	return false
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
