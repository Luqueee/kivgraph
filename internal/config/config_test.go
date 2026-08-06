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
	if loaded.Config.MCP.Transport != "stdio" || loaded.Config.MCP.DefaultLimit != 50 || loaded.Config.MCP.MaximumLimit != 500 || loaded.Config.MCP.MaximumDepth != 5 || loaded.Config.MCP.MaximumVisitedNodes != 25_000 {
		t.Fatalf("MCP defaults = %#v", loaded.Config.MCP)
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
