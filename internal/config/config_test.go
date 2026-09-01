package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestLoadAppliesDefaultsAndExpandsPaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KIVGRAPH_CONFIG_ROOT", root)
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	writeConfigFixture(t, configPath, `version: 1
workspace:
  repositories_file: ${KIVGRAPH_CONFIG_ROOT}/repositories.yaml
storage:
  database_path: state/graph.lbdb
`)
	writeConfigFixture(t, repositoriesPath, `version: 1
repositories:
  - name: service-a
    path: sources/service-a
    languages:
      - go
      - typescript
      - rust
    manifests:
      - package.json
    roots:
      - src
    exclusions:
      - node_modules
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.ConfigPath != configPath {
		t.Fatalf("ConfigPath = %q, want %q", loaded.ConfigPath, configPath)
	}
	if loaded.RepositoriesPath != repositoriesPath {
		t.Fatalf("RepositoriesPath = %q, want %q", loaded.RepositoriesPath, repositoriesPath)
	}
	if loaded.Config.Workspace.RepositoriesFile != repositoriesPath {
		t.Fatalf("repositories_file = %q, want %q", loaded.Config.Workspace.RepositoriesFile, repositoriesPath)
	}
	if got, want := loaded.Config.Storage.DatabasePath, filepath.Join(root, "state", "graph.lbdb"); got != want {
		t.Fatalf("database_path = %q, want %q", got, want)
	}
	if !filepath.IsAbs(loaded.Config.Storage.BackupsPath) || !filepath.IsAbs(loaded.Config.Go.SyntheticWorkFile) {
		t.Fatalf("default paths were not expanded: backups=%q work=%q", loaded.Config.Storage.BackupsPath, loaded.Config.Go.SyntheticWorkFile)
	}
	if loaded.Config.Web.Address != "0.0.0.0:7777" {
		t.Fatalf("web address default = %q, want 0.0.0.0:7777", loaded.Config.Web.Address)
	}
	if loaded.Config.Python.IndexerCommand != "kivgraph-python-worker" || loaded.Config.Python.AnalyzerCommand != "kivgraph-python-pyright" || loaded.Config.Python.AnalyzerMode != "fallback" || loaded.Config.Python.PythonPath != "python3" || loaded.Config.Python.MaximumWorkers != 3 || loaded.Config.Python.IncludeTests || loaded.Config.Python.IncludeGenerated || loaded.Config.Python.IncludeExternal {
		t.Fatalf("Python defaults = %#v", loaded.Config.Python)
	}
	if loaded.Config.Dart.AnalyzerCommand != "dart" || loaded.Config.Dart.SDKPath != "dart" || loaded.Config.Dart.MaximumWorkers != 2 || loaded.Config.Dart.IncludeTests || loaded.Config.Dart.IncludeGenerated || loaded.Config.Dart.IncludeExternal || loaded.Config.Dart.IncludeSDK || loaded.Config.Dart.PackageConfig != "auto" || !loaded.Config.Dart.WaitForAnalysis || loaded.Config.Dart.MaximumAnalysisTime != Duration(5*time.Minute) {
		t.Fatalf("Dart defaults = %#v", loaded.Config.Dart)
	}
	if loaded.Config.Indexing.GeneratedFiles != "include" || loaded.Config.Indexing.UnresolvedReferences != "retain" {
		t.Fatalf("core defaults = storage=%#v indexing=%#v", loaded.Config.Storage, loaded.Config.Indexing)
	}
	if loaded.Config.Watcher.ReconciliationInterval != Duration(10*time.Minute) || loaded.Config.TypeScript.ProjectIdleTimeout != Duration(30*time.Minute) {
		t.Fatalf("duration defaults = watcher=%s typescript=%s", loaded.Config.Watcher.ReconciliationInterval, loaded.Config.TypeScript.ProjectIdleTimeout)
	}
	if len(loaded.Repositories.Repositories) != 1 {
		t.Fatalf("repositories count = %d, want 1", len(loaded.Repositories.Repositories))
	}
	repository := loaded.Repositories.Repositories[0]
	if repository.Name != "service-a" || repository.Path != filepath.Join(root, "sources", "service-a") {
		t.Fatalf("repository = %#v", repository)
	}
	if !reflect.DeepEqual(repository.Manifests, []string{"package.json"}) || !reflect.DeepEqual(repository.Roots, []string{"src"}) || !reflect.DeepEqual(repository.Exclusions, []string{"node_modules"}) {
		t.Fatalf("repository configuration = %#v", repository)
	}
}

// TestRustDefaultsKeepBuildArtifactsOutsideEveryRepository is the reason the
// target directory is configuration rather than a constant: rust-analyzer runs
// build scripts, and cargo writes wherever CARGO_TARGET_DIR points.
func TestRustDefaultsKeepBuildArtifactsOutsideEveryRepository(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KIVGRAPH_CONFIG_ROOT", root)
	configPath := filepath.Join(root, "config.yaml")
	writeConfigFixture(t, configPath, "version: 1\n")

	configuration, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig(%q) error = %v", configPath, err)
	}
	if configuration.Rust.AnalyzerCommand != "rust-analyzer" {
		t.Fatalf("analyzer command = %q", configuration.Rust.AnalyzerCommand)
	}
	if !configuration.Rust.BuildScripts || !configuration.Rust.ProcMacros {
		t.Fatalf("expansion defaults = %#v", configuration.Rust)
	}
	if configuration.Rust.AllowNetwork {
		t.Fatal("Rust indexing must be hermetic by default")
	}
	if !filepath.IsAbs(configuration.Rust.TargetDirectory) {
		t.Fatalf("target directory = %q, want an absolute path", configuration.Rust.TargetDirectory)
	}
	if configuration.Rust.Sysroot != "discover" {
		t.Fatalf("sysroot = %q", configuration.Rust.Sysroot)
	}
}

// TestSupportedLanguagesCoversEveryAnalysedLanguage keeps the vocabulary and
// its aliases in one place: a second list is what let `init` accept a language
// the pass refuses.
func TestSupportedLanguagesCoversEveryAnalysedLanguage(t *testing.T) {
	for _, language := range []string{"go", "typescript", "javascript", "ts", "js", "rust", "rs", "python", "py", "dart", "  RUST  "} {
		if !SupportedLanguage(language) {
			t.Fatalf("SupportedLanguage(%q) = false", language)
		}
	}
	for _, language := range []string{"", "rustlang"} {
		if SupportedLanguage(language) {
			t.Fatalf("SupportedLanguage(%q) = true", language)
		}
	}
}

func TestLoadConfigRejectsInvalidDocuments(t *testing.T) {
	tests := []struct {
		name      string
		contents  string
		wantError string
	}{
		{
			name:      "missing schema version",
			contents:  "storage:\n  backups_path: /tmp/backups\n",
			wantError: "config.version: field is required",
		},
		{
			name: "unsupported schema version",
			contents: `version: 2
`,
			wantError: "unsupported schema version 2",
		},
		{
			name: "unknown field",
			contents: `version: 1
unknown: true
`,
			wantError: "unknown",
		},
		{
			// Retiring two keys must not turn the section into a place where
			// anything is accepted: a typo inside storage still has to fail, or
			// the compatibility path would have cost the strictness that makes
			// a misspelled key visible.
			name: "unknown field inside a section that has retired keys",
			contents: `version: 1
storage:
  snapshot_path: /tmp/typo
`,
			wantError: "snapshot_path",
		},
		{
			name: "invalid limit relationship",
			contents: `version: 1
mcp:
  maximum_limit: 10
`,
			wantError: "maximum_limit: must be at least default_limit",
		},
		{
			name: "unsupported transport",
			contents: `version: 1
mcp:
  transport: http
`,
			wantError: "unsupported transport",
		},
		{
			name: "invalid duration",
			contents: `version: 1
watcher:
  reconciliation_interval: soon
`,
			wantError: "invalid duration",
		},
		{
			name: "invalid web listen address",
			contents: `version: 1
web:
  address: localhost
`,
			wantError: "config.web.address: invalid listen address",
		},
		{
			name: "invalid web port",
			contents: `version: 1
web:
  address: 127.0.0.1:99999
`,
			wantError: "config.web.address: invalid port",
		},
		{
			name: "empty build tag",
			contents: `version: 1
go:
  build_tags:
    - " "
`,
			wantError: "config.go.build_tags[0]: must not be empty",
		},
		{
			name: "build tag list in one entry",
			contents: `version: 1
go:
  build_tags:
    - ladybug,cgo
`,
			wantError: "config.go.build_tags[0]: must not contain a comma or whitespace",
		},
		{
			name: "empty analyzer command",
			contents: `version: 1
rust:
  analyzer_command: "  "
`,
			wantError: "config.rust.analyzer_command: must not be empty",
		},
		{
			name: "features and all_features together",
			contents: `version: 1
rust:
  all_features: true
  features:
    - serde
`,
			wantError: "config.rust.all_features: must not be set together with rust.features",
		},
		{
			name: "negative rust workspace limit",
			contents: `version: 1
rust:
  maximum_workspaces: -1
`,
			wantError: "config.rust.maximum_workspaces: must not be negative",
		},
		{
			// A word nothing implements is a setting that lies: the pass
			// retains every unresolved reference whatever this says.
			name: "invented unresolved reference policy",
			contents: `version: 1
indexing:
  unresolved_references: drop
`,
			wantError: `config.indexing.unresolved_references: must be retain, got "drop"`,
		},
		{
			name: "invented generated file policy",
			contents: `version: 1
indexing:
  generated_files: skip
`,
			wantError: `config.indexing.generated_files: must be include, got "skip"`,
		},
		{
			name:      "multiple documents",
			contents:  "version: 1\n---\nversion: 1\n",
			wantError: "multiple YAML documents",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			writeConfigFixture(t, path, test.contents)
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatal("LoadConfig() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("LoadConfig() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

// TestARetiredKeyLoadsAndIsReported is the migration ADR 0062 promises.
//
// Every configuration written by an older `kivgraph init` carries both keys,
// and the decoder rejects unknown fields. Deleting the struct fields without
// this path would turn every one of those files into a hard load failure over a
// key that never did anything -- the same shape as the doctor that went red on
// an upgrade, and the same reason it is wrong.
func TestARetiredKeyLoadsAndIsReported(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.yaml")
	repositoriesPath := filepath.Join(directory, "repositories.yaml")
	writeConfigFixture(t, configPath, `version: 1
workspace:
  repositories_file: `+repositoriesPath+`
storage:
  snapshots_path: /tmp/nowhere
  retain_snapshots: 7
  backups_path: `+filepath.Join(directory, "backups")+`
`)
	writeConfigFixture(t, repositoriesPath, "version: 1\nrepositories: []\n")

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v, want a configuration with retired keys to load", err)
	}
	want := []string{"storage.snapshots_path", "storage.retain_snapshots"}
	if !slices.Equal(loaded.RetiredKeys, want) {
		t.Fatalf("RetiredKeys = %v, want %v", loaded.RetiredKeys, want)
	}
	// The value is ignored, not honoured: nothing may act on 7.
	if loaded.Config.Storage.BackupsPath != filepath.Join(directory, "backups") {
		t.Fatalf("backups_path = %q, want the value beside the retired keys to still apply",
			loaded.Config.Storage.BackupsPath)
	}

	// A file with none of them reports none, so the report cannot be a constant.
	clean := filepath.Join(directory, "clean.yaml")
	writeConfigFixture(t, clean, `version: 1
workspace:
  repositories_file: `+repositoriesPath+`
`)
	quiet, err := Load(clean)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(quiet.RetiredKeys) != 0 {
		t.Fatalf("RetiredKeys = %v, want none", quiet.RetiredKeys)
	}
}

func TestLoadConfigRejectsUnsetEnvironmentVariable(t *testing.T) {
	const variable = "KIVGRAPH_CONFIG_VARIABLE_THAT_IS_NOT_SET"
	oldValue, wasSet := os.LookupEnv(variable)
	if err := os.Unsetenv(variable); err != nil {
		t.Fatalf("Unsetenv() error = %v", err)
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv(variable, oldValue)
		} else {
			_ = os.Unsetenv(variable)
		}
	})
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigFixture(t, path, `version: 1
storage:
  database_path: ${KIVGRAPH_CONFIG_VARIABLE_THAT_IS_NOT_SET}/graph.lbdb
`)

	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), `environment variable "KIVGRAPH_CONFIG_VARIABLE_THAT_IS_NOT_SET" is not set`) {
		t.Fatalf("LoadConfig() error = %v, want unset-variable error", err)
	}
}

func TestLoadConfigRejectsInvalidDefaultProfile(t *testing.T) {
	for _, profile := range []string{"", "*", "../other", "nested/name", `nested\name`} {
		t.Run(profile, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			writeConfigFixture(t, path, "version: 1\nprofiles:\n  default: \""+profile+"\"\n")
			_, err := LoadConfig(path)
			if err == nil || !strings.Contains(err.Error(), "config.profiles.default") {
				t.Fatalf("LoadConfig() error = %v, want invalid default profile", err)
			}
		})
	}
}

func TestLoadConfigRejectsNonPositiveProfileCacheBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigFixture(t, path, "version: 1\nprofiles:\n  max_open: 0\n")
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "config.profiles.max_open") {
		t.Fatalf("LoadConfig() error = %v, want invalid profile cache bound", err)
	}
}

func TestDefaultConfigNamesTheDefaultProfile(t *testing.T) {
	configuration := DefaultConfig()
	if configuration.Profiles.Default != "default" {
		t.Fatalf("profiles.default = %q, want default", configuration.Profiles.Default)
	}
	if configuration.Profiles.MaxOpen != 3 {
		t.Fatalf("profiles.max_open = %d, want 3", configuration.Profiles.MaxOpen)
	}
}

func TestLoadRepositoriesAllowsExplicitEmptyList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repositories.yaml")
	writeConfigFixture(t, path, "version: 1\nrepositories: []\n")

	repositories, err := LoadRepositories(path)
	if err != nil {
		t.Fatalf("LoadRepositories() error = %v", err)
	}
	if repositories.Repositories == nil || len(repositories.Repositories) != 0 {
		t.Fatalf("repositories = %#v, want an explicit empty list", repositories.Repositories)
	}
}

func TestLoadRepositoriesValidatesEntries(t *testing.T) {
	tests := []struct {
		name      string
		contents  string
		wantError string
	}{
		{
			name: "missing repositories list",
			contents: `version: 1
`,
			wantError: "repositories: must contain at least one entry",
		},
		{
			name: "duplicate name",
			contents: `version: 1
repositories:
  - name: service
    path: /srv/service-a
    languages: [go]
  - name: service
    path: /srv/service-b
    languages: [go]
`,
			wantError: "duplicate of repositories[0].name",
		},
		{
			name: "duplicate path",
			contents: `version: 1
repositories:
  - name: service-a
    path: /srv/service
    languages: [go]
  - name: service-b
    path: /srv/service
    languages: [typescript]
`,
			wantError: "duplicate of repositories[0].path",
		},
		{
			name: "empty languages",
			contents: `version: 1
repositories:
  - name: service
    path: /srv/service
    languages: []
`,
			wantError: "languages: must contain at least one language",
		},
		{
			name: "missing schema version",
			contents: `repositories: []
`,
			wantError: "repositories.version: field is required",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "repositories.yaml")
			writeConfigFixture(t, path, test.contents)
			_, err := LoadRepositories(path)
			if err == nil {
				t.Fatal("LoadRepositories() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("LoadRepositories() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func writeConfigFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func TestInitializeCreatesSecureStateAndRegistersRepositories(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	testsupport.SetHome(t, home)
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")

	result, err := Initialize(InitOptions{
		ConfigPath:       configPath,
		RepositoriesPath: repositoriesPath,
	})
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !result.ConfigCreated || !result.RepositoriesCreated {
		t.Fatalf("Initialize() result = %#v, want both files created", result)
	}
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() after Initialize: %v", err)
	}
	for _, path := range []string{
		filepath.Dir(loaded.Config.Storage.DatabasePath),
		loaded.Config.Storage.BackupsPath,
		filepath.Dir(loaded.Config.Go.SyntheticWorkFile),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("state path %q: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("state path %q = mode %04o dir=%v, want a directory", path, info.Mode().Perm(), info.IsDir())
		}
		// That Initialize creates these is the claim on every platform; that
		// it creates them private is one only where a mode is what privacy is
		// made of.
		if testsupport.ModeBitsHonoured() && info.Mode().Perm() != 0o700 {
			t.Fatalf("state path %q = mode %04o, want 0700", path, info.Mode().Perm())
		}
	}

	if err := RegisterRepositories(result.RepositoriesPath, []Repository{{
		Name:      "service",
		Path:      "sources/service",
		Languages: []string{"go"},
	}}); err != nil {
		t.Fatalf("RegisterRepositories() error = %v", err)
	}
	loaded, err = Load(configPath)
	if err != nil {
		t.Fatalf("Load() after RegisterRepositories: %v", err)
	}
	if len(loaded.Repositories.Repositories) != 1 {
		t.Fatalf("repositories = %#v, want one entry", loaded.Repositories.Repositories)
	}
	wantPath := filepath.Join(root, "sources", "service")
	if loaded.Repositories.Repositories[0].Path != wantPath {
		t.Fatalf("repository path = %q, want %q", loaded.Repositories.Repositories[0].Path, wantPath)
	}
	if err := RegisterRepositories(result.RepositoriesPath, []Repository{{
		Name:      "service",
		Path:      "/other/service",
		Languages: []string{"go"},
	}}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate RegisterRepositories() error = %v, want duplicate error", err)
	}
}

// TestInitializeKeepsACustomLocationSelfContained stops a probe from
// publishing over the real graph. A configuration written outside the default
// location used to carry the default storage paths, so running it wrote
// generations into the state of the installation it was meant to leave alone.
func TestInitializeKeepsACustomLocationSelfContained(t *testing.T) {
	directory := t.TempDir()
	home := t.TempDir()
	testsupport.SetHome(t, home)
	configPath := filepath.Join(directory, "config.yaml")

	result, err := Initialize(InitOptions{ConfigPath: configPath})
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	loaded, err := Load(result.ConfigPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	state := filepath.Join(directory, "state")
	for name, path := range map[string]string{
		"storage.database_path":       loaded.Config.Storage.DatabasePath,
		"storage.backups_path":        loaded.Config.Storage.BackupsPath,
		"indexing.fact_cache_path":    loaded.Config.Indexing.FactCachePath,
		"workspace.repositories_file": loaded.Config.Workspace.RepositoriesFile,
	} {
		if !strings.HasPrefix(path, directory) {
			t.Fatalf("%s = %q, want it under the configuration's own directory %q", name, path, directory)
		}
		if strings.Contains(path, "state") && !strings.HasPrefix(path, state) {
			t.Fatalf("%s = %q, want it under %q", name, path, state)
		}
	}
	wantWorkParent := filepath.Join(home, ".local", "state", "kivgraph", "workspaces")
	if !strings.HasPrefix(loaded.Config.Go.SyntheticWorkFile, wantWorkParent+string(filepath.Separator)) {
		t.Fatalf("go.synthetic_work_file = %q, want it under %q", loaded.Config.Go.SyntheticWorkFile, wantWorkParent)
	}
	if strings.HasPrefix(loaded.Config.Go.SyntheticWorkFile, directory+string(filepath.Separator)) {
		t.Fatalf("go.synthetic_work_file = %q, want it outside the configuration directory %q", loaded.Config.Go.SyntheticWorkFile, directory)
	}
}

func TestInitializeGivesEachCustomConfigurationAStableSyntheticWorkspace(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	configPaths := []string{
		filepath.Join(t.TempDir(), ".kivgraph", "config.yaml"),
		filepath.Join(t.TempDir(), ".kivgraph", "config.yaml"),
	}
	workFiles := make([]string, 0, len(configPaths))
	for _, configPath := range configPaths {
		result, err := Initialize(InitOptions{ConfigPath: configPath})
		if err != nil {
			t.Fatalf("Initialize(%q) error = %v", configPath, err)
		}
		loaded, err := Load(result.ConfigPath)
		if err != nil {
			t.Fatalf("Load(%q) error = %v", result.ConfigPath, err)
		}
		workFiles = append(workFiles, loaded.Config.Go.SyntheticWorkFile)

		second, err := Initialize(InitOptions{ConfigPath: configPath})
		if err != nil {
			t.Fatalf("second Initialize(%q) error = %v", configPath, err)
		}
		loadedAgain, err := Load(second.ConfigPath)
		if err != nil {
			t.Fatalf("second Load(%q) error = %v", second.ConfigPath, err)
		}
		if loadedAgain.Config.Go.SyntheticWorkFile != loaded.Config.Go.SyntheticWorkFile {
			t.Fatalf("config %q synthetic workspace changed from %q to %q", configPath, loaded.Config.Go.SyntheticWorkFile, loadedAgain.Config.Go.SyntheticWorkFile)
		}
	}
	if workFiles[0] == workFiles[1] {
		t.Fatalf("configurations %q and %q share synthetic workspace %q", configPaths[0], configPaths[1], workFiles[0])
	}
}

func TestMigrateProjectSyntheticWorkFileLeavesCustomPathUntouched(t *testing.T) {
	directory := t.TempDir()
	home := t.TempDir()
	testsupport.SetHome(t, home)
	configPath := filepath.Join(directory, ".kivgraph", "config.yaml")
	custom := filepath.Join(t.TempDir(), "custom.go.work")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigFixture(t, configPath, "version: 1\ngo:\n  synthetic_work_file: "+custom+"\n")

	migrated, err := MigrateProjectSyntheticWorkFile(configPath)
	if err != nil {
		t.Fatalf("MigrateProjectSyntheticWorkFile() error = %v", err)
	}
	if migrated {
		t.Fatal("MigrateProjectSyntheticWorkFile() migrated = true, want false")
	}
	configuration, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig(%q) error = %v", configPath, err)
	}
	if configuration.Go.SyntheticWorkFile != custom {
		t.Fatalf("synthetic_work_file = %q, want custom path %q", configuration.Go.SyntheticWorkFile, custom)
	}
}

func TestMigrateProjectSyntheticWorkFileMovesLegacyPathAndKeepsConfigurationsIsolated(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	paths := []string{
		filepath.Join(t.TempDir(), ".kivgraph", "config.yaml"),
		filepath.Join(t.TempDir(), ".kivgraph", "config.yaml"),
	}
	workFiles := make([]string, 0, len(paths))
	for _, configPath := range paths {
		if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
			t.Fatal(err)
		}
		legacy := filepath.Join(filepath.Dir(configPath), "state", "go.work")
		writeConfigFixture(t, configPath, "version: 1\ngo:\n  synthetic_work_file: "+legacy+"\n  maximum_loads: 7\n")

		migrated, err := MigrateProjectSyntheticWorkFile(configPath)
		if err != nil {
			t.Fatalf("MigrateProjectSyntheticWorkFile(%q) error = %v", configPath, err)
		}
		if !migrated {
			t.Fatalf("MigrateProjectSyntheticWorkFile(%q) migrated = false, want true", configPath)
		}
		configuration, err := LoadConfig(configPath)
		if err != nil {
			t.Fatalf("LoadConfig(%q) error = %v", configPath, err)
		}
		if configuration.Go.MaximumLoads != 7 {
			t.Fatalf("maximum_loads = %d, want preserved value 7", configuration.Go.MaximumLoads)
		}
		if _, err := os.Stat(filepath.Dir(configuration.Go.SyntheticWorkFile)); err != nil {
			t.Fatalf("synthetic workspace directory: %v", err)
		}
		workFiles = append(workFiles, configuration.Go.SyntheticWorkFile)

		again, err := MigrateProjectSyntheticWorkFile(configPath)
		if err != nil || again {
			t.Fatalf("second migration for %q = %t, %v; want false, nil", configPath, again, err)
		}
	}
	if workFiles[0] == workFiles[1] {
		t.Fatalf("configurations %q and %q share synthetic workspace %q", paths[0], paths[1], workFiles[0])
	}
}

// TestInitializeAtTheDefaultLocationKeepsTheDefaultState is the other half:
// isolating a custom location must not move the state of a normal install.
func TestInitializeAtTheDefaultLocationKeepsTheDefaultState(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	defaultPath, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath() error = %v", err)
	}

	result, err := Initialize(InitOptions{ConfigPath: defaultPath})
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	loaded, err := Load(result.ConfigPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := filepath.Join(home, ".local", "state", "kivgraph", "graph.lbdb")
	if loaded.Config.Storage.DatabasePath != want {
		t.Fatalf("database_path = %q, want the default %q", loaded.Config.Storage.DatabasePath, want)
	}
}

func TestSetPythonAnalyzerChangesOnlyPythonSettings(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigFixture(t, configPath, `version: 1
go:
  # keep this comment with the user's setting
  maximum_loads: 7
python:
  analyzer_command: kivgraph-python-pyright
  analyzer_mode: fallback
`)

	wantCommand := `kivgraph-python-pyright --analyzer "/tmp/pyright-langserver"`
	wantMode := "exact"
	if err := SetPythonAnalyzer(configPath, wantCommand, wantMode); err != nil {
		t.Fatalf("SetPythonAnalyzer(%q, %q) error = %v", wantCommand, wantMode, err)
	}
	configuration, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig(%q) error = %v", configPath, err)
	}
	if configuration.Python.AnalyzerMode != wantMode || configuration.Python.AnalyzerCommand != wantCommand {
		t.Fatalf("Python analyzer for command %q and mode %q = %#v", wantCommand, wantMode, configuration.Python)
	}
	if configuration.Go.MaximumLoads != 7 {
		t.Fatalf("go.maximum_loads for %q = %d, want preserved value 7", configPath, configuration.Go.MaximumLoads)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config %q after update: %v", configPath, err)
	}
	if !strings.Contains(string(data), "# keep this comment") {
		t.Fatalf("config %q lost its comments after the analyzer update: %q", configPath, data)
	}
}

func TestSetPythonAnalyzerRejectsInvalidSettingsBeforeWriting(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	original := "version: 1\n"
	writeConfigFixture(t, configPath, original)
	for name, settings := range map[string][2]string{
		"empty command": {"", "exact"},
		"unknown mode":  {"pyright", "automatic"},
	} {
		t.Run(name, func(t *testing.T) {
			command, mode := settings[0], settings[1]
			if err := SetPythonAnalyzer(configPath, command, mode); err == nil {
				t.Fatalf("SetPythonAnalyzer(%q, %q) error = nil, want validation error", command, mode)
			}
			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("read config after rejected settings for command %q and mode %q: %v", command, mode, err)
			}
			if string(data) != original {
				t.Fatalf("configuration changed after rejected settings for command %q and mode %q: %q", command, mode, data)
			}
		})
	}
}

func TestSetPythonAnalyzerAcceptsAnEmptyPythonMapping(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigFixture(t, configPath, "version: 1\npython:\n")

	command := "kivgraph-python-pyright --analyzer '/state/pyright'"
	if err := SetPythonAnalyzer(configPath, command, "exact"); err != nil {
		t.Fatalf("SetPythonAnalyzer() with empty Python mapping error = %v", err)
	}
	configuration, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() after empty Python mapping update error = %v", err)
	}
	if configuration.Python.AnalyzerCommand != command || configuration.Python.AnalyzerMode != "exact" {
		t.Fatalf("Python analyzer after empty mapping update = %#v", configuration.Python)
	}
}

func TestSetPythonAnalyzerAddsMissingPythonMapping(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigFixture(t, configPath, "version: 1\n")

	command := "kivgraph-python-pyright --analyzer '/state/pyright'"
	if err := SetPythonAnalyzer(configPath, command, "exact"); err != nil {
		t.Fatalf("SetPythonAnalyzer() without Python mapping error = %v", err)
	}
	configuration, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() after missing Python mapping update error = %v", err)
	}
	if configuration.Python.AnalyzerCommand != command || configuration.Python.AnalyzerMode != "exact" {
		t.Fatalf("Python analyzer after missing mapping update = %#v", configuration.Python)
	}
}

func TestSetPythonAnalyzerIfCurrentMatchesAndPreservesConcurrentChange(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigFixture(t, configPath, `version: 1
python:
  analyzer_command: kivgraph-python-pyright
  analyzer_mode: fallback
`)

	wantCommand := "kivgraph-python-pyright --analyzer '/state/pyright'"
	changed, err := SetPythonAnalyzerIfCurrent(configPath, "kivgraph-python-pyright", wantCommand, "exact")
	if err != nil {
		t.Fatalf("SetPythonAnalyzerIfCurrent() matching error = %v", err)
	}
	if !changed {
		t.Fatal("SetPythonAnalyzerIfCurrent() matching changed = false, want true")
	}
	configuration, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() after matching update error = %v", err)
	}
	if configuration.Python.AnalyzerCommand != wantCommand || configuration.Python.AnalyzerMode != "exact" {
		t.Fatalf("Python analyzer after matching update = %#v", configuration.Python)
	}

	original, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config before non-matching update: %v", err)
	}
	changed, err = SetPythonAnalyzerIfCurrent(configPath, "another-analyzer", "ignored", "fallback")
	if err != nil {
		t.Fatalf("SetPythonAnalyzerIfCurrent() non-matching error = %v", err)
	}
	if changed {
		t.Fatal("SetPythonAnalyzerIfCurrent() non-matching changed = true, want false")
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after non-matching update: %v", err)
	}
	if string(after) != string(original) {
		t.Fatalf("configuration changed after non-matching update: before=%q after=%q", original, after)
	}
}
