package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadAppliesDefaultsAndExpandsPaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LADYGRAPH_CONFIG_ROOT", root)
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	writeConfigFixture(t, configPath, `version: 1
workspace:
  repositories_file: ${LADYGRAPH_CONFIG_ROOT}/repositories.yaml
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
	if !filepath.IsAbs(loaded.Config.Storage.SnapshotsPath) || !filepath.IsAbs(loaded.Config.Storage.BackupsPath) || !filepath.IsAbs(loaded.Config.Go.SyntheticWorkFile) {
		t.Fatalf("default paths were not expanded: snapshots=%q backups=%q work=%q", loaded.Config.Storage.SnapshotsPath, loaded.Config.Storage.BackupsPath, loaded.Config.Go.SyntheticWorkFile)
	}
	if loaded.Config.Web.Address != "0.0.0.0:7777" {
		t.Fatalf("web address default = %q, want 0.0.0.0:7777", loaded.Config.Web.Address)
	}
	if loaded.Config.Storage.RetainSnapshots != 3 || loaded.Config.Indexing.GeneratedFiles != "include" || loaded.Config.Indexing.UnresolvedReferences != "retain" {
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
	t.Setenv("LADYGRAPH_CONFIG_ROOT", root)
	configPath := filepath.Join(root, "config.yaml")
	writeConfigFixture(t, configPath, "version: 1\n")

	configuration, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
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
	for _, language := range []string{"go", "typescript", "javascript", "ts", "js", "rust", "rs", "  RUST  "} {
		if !SupportedLanguage(language) {
			t.Fatalf("SupportedLanguage(%q) = false", language)
		}
	}
	for _, language := range []string{"", "python", "rustlang"} {
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
			contents:  "storage:\n  retain_snapshots: 3\n",
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

func TestLoadConfigRejectsUnsetEnvironmentVariable(t *testing.T) {
	const variable = "LADYGRAPH_CONFIG_VARIABLE_THAT_IS_NOT_SET"
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
  database_path: ${LADYGRAPH_CONFIG_VARIABLE_THAT_IS_NOT_SET}/graph.lbdb
`)

	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), `environment variable "LADYGRAPH_CONFIG_VARIABLE_THAT_IS_NOT_SET" is not set`) {
		t.Fatalf("LoadConfig() error = %v, want unset-variable error", err)
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
	t.Setenv("HOME", home)
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
		loaded.Config.Storage.SnapshotsPath,
		loaded.Config.Storage.BackupsPath,
		filepath.Dir(loaded.Config.Go.SyntheticWorkFile),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("state path %q: %v", path, err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("state path %q = mode %04o dir=%v, want 0700 directory", path, info.Mode().Perm(), info.IsDir())
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
		"storage.snapshots_path":      loaded.Config.Storage.SnapshotsPath,
		"storage.backups_path":        loaded.Config.Storage.BackupsPath,
		"indexing.fact_cache_path":    loaded.Config.Indexing.FactCachePath,
		"go.synthetic_work_file":      loaded.Config.Go.SyntheticWorkFile,
		"workspace.repositories_file": loaded.Config.Workspace.RepositoriesFile,
	} {
		if !strings.HasPrefix(path, directory) {
			t.Fatalf("%s = %q, want it under the configuration's own directory %q", name, path, directory)
		}
		if strings.Contains(path, "state") && !strings.HasPrefix(path, state) {
			t.Fatalf("%s = %q, want it under %q", name, path, state)
		}
	}
}

// TestInitializeAtTheDefaultLocationKeepsTheDefaultState is the other half:
// isolating a custom location must not move the state of a normal install.
func TestInitializeAtTheDefaultLocationKeepsTheDefaultState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
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
	want := filepath.Join(home, ".local", "state", "ladygraph", "graph.lbdb")
	if loaded.Config.Storage.DatabasePath != want {
		t.Fatalf("database_path = %q, want the default %q", loaded.Config.Storage.DatabasePath, want)
	}
}
