package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/topology"
)

func TestLoadProfileTopologyMissingKeepsLegacyProfile(t *testing.T) {
	configPath, _ := newTopologyProfile(t, "feature")

	value, present, err := LoadProfileTopology(configPath, "feature")
	if err != nil {
		t.Fatalf("LoadProfileTopology() error = %v", err)
	}
	if present {
		t.Fatal("LoadProfileTopology() present = true for a missing document")
	}
	if !reflect.DeepEqual(value, topology.Topology{}) {
		t.Fatalf("LoadProfileTopology() value = %#v, want the zero topology", value)
	}
}

func TestLoadProfileTopologyRejectsMalformedDocuments(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		want  string
		class error
	}{
		{
			name: "missing version",
			data: "repositories: []\n",
			want: "topology.version: field is required",
		},
		{
			name: "unsupported version",
			data: "version: 2\n",
			want: "unsupported schema version 2",
		},
		{
			name: "unknown field",
			data: "version: 1\nunknown: true\n",
			want: "field unknown not found",
		},
		{
			name:  "invalid topology",
			data:  "version: 1\nrepositories:\n  - id: backend\nworktrees:\n  - id: backend-main\n    repository: missing\n    path: /work/backend\nprofiles:\n  - id: feature\n    worktrees:\n      - repository: missing\n        worktree: backend-main\n",
			want:  "logical repository is not declared",
			class: topology.ErrInvalidTopology,
		},
		{
			name: "empty worktree path",
			data: "version: 1\nrepositories:\n  - id: backend\nworktrees:\n  - id: backend-main\n    repository: backend\n    path: \"\"\nprofiles:\n  - id: feature\n    worktrees:\n      - repository: backend\n        worktree: backend-main\n",
			want: "topology.worktrees[0].path",
		},
		{
			name:  "profile not selected",
			data:  "version: 1\nprofiles:\n  - id: another\n",
			want:  "profile \"feature\"",
			class: topology.ErrProfileNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configPath, loaded := newTopologyProfile(t, "feature")
			if err := os.WriteFile(loaded.TopologyPath, []byte(test.data), 0o600); err != nil {
				t.Fatalf("write malformed topology: %v", err)
			}
			_, present, err := LoadProfileTopology(configPath, "feature")
			if err == nil || present {
				t.Fatalf("LoadProfileTopology() = present %t, error %v, want a rejected document", present, err)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadProfileTopology() error = %q, want substring %q", err, test.want)
			}
			if test.class != nil && !errors.Is(err, test.class) {
				t.Fatalf("LoadProfileTopology() error = %v, want %v", err, test.class)
			}
		})
	}
}

func TestSaveAndLoadProfileTopologyExpandsRelativeWorktreePaths(t *testing.T) {
	configPath, loaded := newTopologyProfile(t, "feature")
	sourcePath := filepath.Join(filepath.Dir(loaded.ConfigPath), "frontend")
	relativePath, err := filepath.Rel(filepath.Dir(loaded.TopologyPath), sourcePath)
	if err != nil {
		t.Fatalf("relative topology path: %v", err)
	}
	want := topology.Topology{
		Version:      topology.CurrentSchemaVersion,
		Repositories: []topology.LogicalRepository{{ID: "frontend", Name: "Frontend"}},
		Worktrees:    []topology.Worktree{{ID: "frontend-main", Repository: "frontend", Path: relativePath}},
		Profiles: []topology.Profile{{
			ID:        "feature",
			Worktrees: []topology.WorktreeSelection{{Repository: "frontend", Worktree: "frontend-main"}},
		}},
	}
	if err := SaveProfileTopology(configPath, "feature", want); err != nil {
		t.Fatalf("SaveProfileTopology() error = %v", err)
	}

	got, present, err := LoadProfileTopology(configPath, "feature")
	if err != nil {
		t.Fatalf("LoadProfileTopology() error = %v", err)
	}
	if !present {
		t.Fatal("LoadProfileTopology() present = false after save")
	}
	want.Worktrees[0].Path = sourcePath
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded topology = %#v, want %#v", got, want)
	}
}

func TestSaveProfileTopologyRequiresCurrentVersionAndSelectedProfile(t *testing.T) {
	configPath, _ := newTopologyProfile(t, "feature")
	base := topology.Topology{
		Version:  topology.CurrentSchemaVersion,
		Profiles: []topology.Profile{{ID: "feature"}},
	}
	tests := []struct {
		name  string
		value topology.Topology
		want  string
	}{
		{name: "missing version", value: topology.Topology{}, want: "unsupported schema version 0"},
		{name: "missing selected profile", value: topology.Topology{Version: topology.CurrentSchemaVersion, Profiles: []topology.Profile{{ID: "other"}}}, want: "profile \"feature\""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := SaveProfileTopology(configPath, "feature", test.value); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("SaveProfileTopology() error = %v, want substring %q", err, test.want)
			}
		})
	}
	if err := SaveProfileTopology(configPath, "feature", base); err != nil {
		t.Fatalf("SaveProfileTopology(valid) error = %v", err)
	}
}

func TestSaveProfileTopologyReportsLoadAndWriteFailures(t *testing.T) {
	value := topology.Topology{
		Version:  topology.CurrentSchemaVersion,
		Profiles: []topology.Profile{{ID: "feature"}},
	}
	if err := SaveProfileTopology(filepath.Join(t.TempDir(), "missing.yaml"), "feature", value); err == nil {
		t.Fatal("SaveProfileTopology() succeeded with a missing config")
	}

	configPath, loaded := newTopologyProfile(t, "feature")
	if err := os.Mkdir(loaded.TopologyPath, 0o700); err != nil {
		t.Fatalf("Mkdir(topology path) error = %v", err)
	}
	if err := SaveProfileTopology(configPath, "feature", value); err == nil || !strings.Contains(err.Error(), "write profile topology") {
		t.Fatalf("SaveProfileTopology() error = %v, want a write error", err)
	}
}

func TestLoadProfileTopologyRejectsUnknownProfile(t *testing.T) {
	configPath, _ := newTopologyProfile(t, "feature")
	if _, _, err := LoadProfileTopology(configPath, "missing"); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("LoadProfileTopology(missing) error = %v, want ErrProfileNotFound", err)
	}
}

func newTopologyProfile(t *testing.T, name string) (string, Loaded) {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if _, err := Initialize(InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if name != "default" {
		if err := CreateProfile(configPath, name); err != nil {
			t.Fatalf("CreateProfile(%q) error = %v", name, err)
		}
	}
	loaded, err := LoadProfile(configPath, name)
	if err != nil {
		t.Fatalf("LoadProfile(%q) error = %v", name, err)
	}
	return configPath, loaded
}

// yaml.Marshal cannot fail for topology.Topology: every field is a scalar or a
// slice of structs with scalar fields. Reaching that defensive branch would
// require a test-only encoder seam, which production code must not carry.
