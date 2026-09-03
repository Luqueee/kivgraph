package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
)

// TestIndexDispatchKeepsBothFormsDistinct protects the surface the operator
// sees: the short form is the current-project convenience command, while the
// longer form remains the explicit full pass over the registered workspace.
func TestIndexDispatchKeepsBothFormsDistinct(t *testing.T) {
	short, consumed, ok := findCommand([]string{"index"})
	if !ok || consumed != 1 || short.name() != "index" {
		t.Fatalf("findCommand(index) = %q, %d, %v", short.name(), consumed, ok)
	}
	full, consumed, ok := findCommand([]string{"index", "--full"})
	if !ok || consumed != 2 || full.name() != "index --full" {
		t.Fatalf("findCommand(index --full) = %q, %d, %v", full.name(), consumed, ok)
	}
}

// TestRunIndexRejectsAnEmptyProjectBeforeWritingState is the negative beside
// the happy setup: an empty directory is not proof that the project was read,
// so the command must not create a graph configuration for it.
func TestRunIndexRejectsAnEmptyProjectBeforeWritingState(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	var stdout, stderr bytes.Buffer
	if got := run([]string{"kivgraph", "index"}, &stdout, &stderr); got != 1 {
		t.Fatalf("run(index) = %d, want 1; stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "no supported source language detected") {
		t.Fatalf("stderr = %q, want the detection failure", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".kivgraph")); !os.IsNotExist(err) {
		t.Fatalf(".kivgraph stat error = %v, want no project state", err)
	}
}

// TestUpsertCurrentProjectIsIdempotentAndRefreshesLanguages covers the local
// registry contract without running the minute-scale index pass itself.
func TestUpsertCurrentProjectIsIdempotentAndRefreshesLanguages(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".kivgraph", "config.yaml")
	repositoriesPath := filepath.Join(root, ".kivgraph", "repositories.yaml")
	initialised, err := config.Initialize(config.InitOptions{
		ConfigPath:       configPath,
		RepositoriesPath: repositoriesPath,
	})
	if err != nil {
		t.Fatalf("config.Initialize() error = %v", err)
	}

	languages := []string{"go", "typescript"}
	name, changed, err := upsertCurrentProject(initialised.RepositoriesPath, root, languages)
	if err != nil {
		t.Fatalf("first upsert error = %v", err)
	}
	if name != "project" || !changed {
		t.Fatalf("first upsert = name %q changed %t, want project/true", name, changed)
	}
	name, changed, err = upsertCurrentProject(initialised.RepositoriesPath, root, languages)
	if err != nil {
		t.Fatalf("second upsert error = %v", err)
	}
	if name != "project" || changed {
		t.Fatalf("second upsert = name %q changed %t, want project/false", name, changed)
	}

	updated := []string{"go", "rust"}
	if _, changed, err = upsertCurrentProject(initialised.RepositoriesPath, root, updated); err != nil || !changed {
		t.Fatalf("language refresh = changed %t, error %v; want true/nil", changed, err)
	}
	registry, err := config.LoadRepositories(initialised.RepositoriesPath)
	if err != nil {
		t.Fatalf("config.LoadRepositories() error = %v", err)
	}
	if got := registry.Repositories[0].Languages; !reflect.DeepEqual(got, updated) {
		t.Fatalf("languages = %q, want %q", got, updated)
	}
}

// TestCurrentProjectRootUsesContainingGitRoot covers the invocation users make
// after changing into a nested package: discovery and registration must target
// the repository root rather than creating a second local project below it.
func TestCurrentProjectRootUsesContainingGitRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "internal", "feature")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", root, err)
	}

	projectRoot, err := currentProjectRoot()
	if err != nil {
		t.Fatalf("currentProjectRoot() from %q error = %v", nested, err)
	}
	if projectRoot != filepath.Clean(wantRoot) {
		t.Fatalf("currentProjectRoot() = %q, want %q", projectRoot, wantRoot)
	}
	languages, err := config.DetectLanguages(projectRoot)
	if err != nil {
		t.Fatalf("DetectLanguages(%q) error = %v", projectRoot, err)
	}
	initialised, err := config.Initialize(config.InitOptions{
		ConfigPath:       filepath.Join(projectRoot, ".kivgraph", "config.yaml"),
		RepositoriesPath: filepath.Join(projectRoot, ".kivgraph", "repositories.yaml"),
	})
	if err != nil {
		t.Fatalf("config.Initialize() error = %v", err)
	}
	if _, _, err := upsertCurrentProject(initialised.RepositoriesPath, projectRoot, languages); err != nil {
		t.Fatalf("upsertCurrentProject() error = %v", err)
	}
	registry, err := config.LoadRepositories(initialised.RepositoriesPath)
	if err != nil {
		t.Fatalf("config.LoadRepositories() error = %v", err)
	}
	if got := registry.Repositories[0].Path; got != projectRoot {
		t.Fatalf("registered project path = %q, want %q", got, projectRoot)
	}
}

// TestRunIndexRejectsAProjectStateSymlink keeps the local convenience command
// from writing through a project-controlled symlink into unrelated state.
func TestRunIndexRejectsAProjectStateSymlink(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, ".kivgraph")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if got := run([]string{"kivgraph", "index"}, &stdout, &stderr); got != 1 {
		t.Fatalf("run(index) = %d, want 1; stderr=%q", got, stderr.String())
	}
	if !strings.Contains(stderr.String(), "symbolic-link project state") {
		t.Fatalf("stderr = %q, want symlink refusal", stderr.String())
	}
}
