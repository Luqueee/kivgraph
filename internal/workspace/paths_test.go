package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/config"
)

func TestValidatePathsAcceptsScopedMetadata(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	source := config.RepositoriesFile{
		Repositories: []config.Repository{
			{
				Name:       "service-a",
				Path:       first,
				Manifests:  []string{"package.json"},
				Roots:      []string{"src"},
				Exclusions: []string{"node_modules", "dist"},
			},
			{
				Name: "service-b",
				Path: second,
			},
		},
	}
	if err := ValidatePaths(context.Background(), source); err != nil {
		t.Fatalf("ValidatePaths() error = %v", err)
	}
}

func TestValidatePathsRejectsSecurityViolations(t *testing.T) {
	tests := []struct {
		name      string
		build     func(*testing.T) config.RepositoriesFile
		wantError string
	}{
		{
			name: "duplicate realpath",
			build: func(t *testing.T) config.RepositoriesFile {
				root := t.TempDir()
				return config.RepositoriesFile{Repositories: []config.Repository{
					{Name: "service-a", Path: root},
					{Name: "service-b", Path: root},
				}}
			},
			wantError: "duplicate realpath",
		},
		{
			name: "nested repositories",
			build: func(t *testing.T) config.RepositoriesFile {
				parent := t.TempDir()
				child := filepath.Join(parent, "child")
				if err := os.Mkdir(child, 0o700); err != nil {
					t.Fatalf("Mkdir() error = %v", err)
				}
				return config.RepositoriesFile{Repositories: []config.Repository{
					{Name: "parent", Path: parent},
					{Name: "child", Path: child},
				}}
			},
			wantError: "nested repositories",
		},
		{
			name: "symlink",
			build: func(t *testing.T) config.RepositoriesFile {
				target := t.TempDir()
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(target, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				return config.RepositoriesFile{Repositories: []config.Repository{{Name: "linked", Path: link}}}
			},
			wantError: "contains symlink component",
		},
		{
			name: "root escape",
			build: func(t *testing.T) config.RepositoriesFile {
				root := t.TempDir()
				return config.RepositoriesFile{Repositories: []config.Repository{{Name: "escape", Path: root, Roots: []string{"../outside"}}}}
			},
			wantError: "escapes repository realpath",
		},
		{
			name: "manifest escape",
			build: func(t *testing.T) config.RepositoriesFile {
				root := t.TempDir()
				return config.RepositoriesFile{Repositories: []config.Repository{{Name: "escape", Path: root, Manifests: []string{"/outside/manifest.json"}}}}
			},
			wantError: "escapes repository realpath",
		},
		{
			name: "exclusion escape",
			build: func(t *testing.T) config.RepositoriesFile {
				root := t.TempDir()
				return config.RepositoriesFile{Repositories: []config.Repository{{Name: "escape", Path: root, Exclusions: []string{"../outside"}}}}
			},
			wantError: "escapes repository realpath",
		},
		{
			name: "name collision",
			build: func(t *testing.T) config.RepositoriesFile {
				return config.RepositoriesFile{Repositories: []config.Repository{
					{Name: "Service", Path: t.TempDir()},
					{Name: "service", Path: t.TempDir()},
				}}
			},
			wantError: "name collision",
		},
		{
			name: "invalid name",
			build: func(t *testing.T) config.RepositoriesFile {
				return config.RepositoriesFile{Repositories: []config.Repository{{Name: "nested/name", Path: t.TempDir()}}}
			},
			wantError: "not a valid repository identifier",
		},
		{
			name: "missing path",
			build: func(t *testing.T) config.RepositoriesFile {
				return config.RepositoriesFile{Repositories: []config.Repository{{Name: "missing", Path: filepath.Join(t.TempDir(), "missing")}}}
			},
			wantError: "does not exist or is inaccessible",
		},
		{
			name: "file instead of directory",
			build: func(t *testing.T) config.RepositoriesFile {
				path := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(path, []byte("file"), 0o600); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
				return config.RepositoriesFile{Repositories: []config.Repository{{Name: "file", Path: path}}}
			},
			wantError: "is not a directory",
		},
		{
			name: "permissions",
			build: func(t *testing.T) config.RepositoriesFile {
				path := t.TempDir()
				if err := os.Chmod(path, 0o000); err != nil {
					t.Skipf("chmod unavailable: %v", err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0o700) })
				return config.RepositoriesFile{Repositories: []config.Repository{{Name: "private", Path: path}}}
			},
			wantError: "not readable and searchable",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := test.build(t)
			err := ValidatePaths(context.Background(), source)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("ValidatePaths() error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestValidatePathsHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := validatePaths(ctx, config.RepositoriesFile{Repositories: []config.Repository{{Name: "service", Path: t.TempDir()}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("validatePaths(canceled) error = %v, want context.Canceled", err)
	}
}
