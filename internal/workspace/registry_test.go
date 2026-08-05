package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/luque/internal/config"
)

func TestNewRegistryRecordsMetadataAndCopiesResults(t *testing.T) {
	root := t.TempDir()
	source := config.RepositoriesFile{
		Version: 1,
		Repositories: []config.Repository{{
			Name:       "service-a",
			Path:       root,
			Languages:  []string{"go", "typescript"},
			Manifests:  []string{"package.json"},
			Roots:      []string{"src"},
			Exclusions: []string{"node_modules", "dist"},
		}},
	}
	git := fakeGit(map[string]string{
		"rev-parse HEAD":                              "deadbeef\n",
		"symbolic-ref --quiet --short HEAD":           "main\n",
		"status --porcelain=v1 --untracked-files=all": " M README.md\n",
	}, nil)

	registry, err := newRegistry(context.Background(), source, git)
	if err != nil {
		t.Fatalf("newRegistry() error = %v", err)
	}
	listed := registry.List()
	if len(listed) != 1 {
		t.Fatalf("List() length = %d, want 1", len(listed))
	}
	repository := listed[0]
	if repository.Name != "service-a" || repository.Path != root || repository.RealPath != root || repository.Commit != "deadbeef" || repository.Branch != "main" || !repository.Dirty {
		t.Fatalf("registered repository = %#v", repository)
	}
	if !equalStrings(repository.Languages, []string{"go", "typescript"}) || !equalStrings(repository.Manifests, []string{filepath.Join(root, "package.json")}) || !equalStrings(repository.Roots, []string{filepath.Join(root, "src")}) || !equalStrings(repository.Exclusions, []string{"node_modules", "dist"}) {
		t.Fatalf("registered paths = %#v", repository)
	}

	listed[0].Languages[0] = "changed"
	listed[0].Manifests[0] = "changed"
	got, ok := registry.Get(" service-a ")
	if !ok {
		t.Fatal("Get() did not find trimmed repository name")
	}
	if got.Languages[0] != "go" || got.Manifests[0] != filepath.Join(root, "package.json") {
		t.Fatalf("Get() returned aliased data = %#v", got)
	}
	if _, ok := registry.Get("missing"); ok {
		t.Fatal("Get(missing) = true, want false")
	}
}

func TestNewRegistryReadsRealGitMetadata(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required for repository registration: %v", err)
	}
	root := t.TempDir()
	gitTestCommand(t, "-C", root, "init", "-q")
	gitTestCommand(t, "-C", root, "symbolic-ref", "HEAD", "refs/heads/main")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("initial\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	gitTestCommand(t, "-C", root, "add", "README.md")
	gitTestCommand(t, "-C", root, "-c", "user.name=Luque Test", "-c", "user.email=luque-test@example.invalid", "commit", "-qm", "initial")

	source := config.RepositoriesFile{
		Version: 1,
		Repositories: []config.Repository{{
			Name:      "git-repository",
			Path:      root,
			Languages: []string{"go"},
		}},
	}
	registry, err := NewRegistry(context.Background(), source)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	repository, ok := registry.Get("git-repository")
	if !ok {
		t.Fatal("Get() did not find Git repository")
	}
	if repository.RealPath != root || repository.Branch != "main" || len(repository.Commit) != 40 || repository.Dirty {
		t.Fatalf("clean Git metadata = %#v", repository)
	}

	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	registry, err = NewRegistry(context.Background(), source)
	if err != nil {
		t.Fatalf("NewRegistry(dirty) error = %v", err)
	}
	repository, ok = registry.Get("git-repository")
	if !ok || !repository.Dirty {
		t.Fatalf("dirty Git metadata = %#v, want Dirty=true", repository)
	}
}

func TestNewRegistryRejectsInvalidEntriesAndContext(t *testing.T) {
	root := t.TempDir()
	git := fakeGit(map[string]string{
		"rev-parse HEAD":                              "commit",
		"symbolic-ref --quiet --short HEAD":           "main",
		"status --porcelain=v1 --untracked-files=all": "",
	}, nil)
	tests := []struct {
		name      string
		source    config.RepositoriesFile
		wantError string
	}{
		{
			name: "relative path",
			source: config.RepositoriesFile{Repositories: []config.Repository{{
				Name: "service", Path: "relative", Languages: []string{"go"},
			}}},
			wantError: "path must be absolute",
		},
		{
			name: "missing path",
			source: config.RepositoriesFile{Repositories: []config.Repository{{
				Name: "service", Path: filepath.Join(root, "missing"), Languages: []string{"go"},
			}}},
			wantError: "stat path",
		},
		{
			name: "file path",
			source: config.RepositoriesFile{Repositories: []config.Repository{{
				Name: "service", Path: filepath.Join(root, "file"), Languages: []string{"go"},
			}}},
			wantError: "is not a directory",
		},
		{
			name: "empty manifest",
			source: config.RepositoriesFile{Repositories: []config.Repository{{
				Name: "service", Path: root, Languages: []string{"go"}, Manifests: []string{""},
			}}},
			wantError: "manifests[0]: must not be empty",
		},
		{
			name: "empty languages",
			source: config.RepositoriesFile{Repositories: []config.Repository{{
				Name: "service", Path: root,
			}}},
			wantError: "languages: must contain at least one language",
		},
		{
			name: "duplicate name",
			source: config.RepositoriesFile{Repositories: []config.Repository{
				{Name: "service", Path: root, Languages: []string{"go"}},
				{Name: "service", Path: root, Languages: []string{"go"}},
			}},
			wantError: "duplicate name of repositories[0]",
		},
	}
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newRegistry(context.Background(), test.source, git)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("newRegistry() error = %v, want substring %q", err, test.wantError)
			}
		})
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newRegistry(canceled, config.RepositoriesFile{Repositories: []config.Repository{{Name: "service", Path: root, Languages: []string{"go"}}}}, git)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("newRegistry(canceled) error = %v, want context.Canceled", err)
	}
}

func fakeGit(outputs map[string]string, failures map[string]error) gitRunner {
	return func(ctx context.Context, _ string, arguments ...string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		key := strings.Join(arguments, " ")
		if err := failures[key]; err != nil {
			return "", err
		}
		output, ok := outputs[key]
		if !ok {
			return "", errors.New("unexpected fake git command: " + key)
		}
		return strings.TrimSpace(output), nil
	}
}

func gitTestCommand(t *testing.T, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, strings.TrimSpace(string(output)))
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
