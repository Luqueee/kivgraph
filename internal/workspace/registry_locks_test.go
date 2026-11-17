package workspace

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// Observing a repository must not write to it. `git status` refreshes the
// index when a file's stat data moved, and to do that it takes
// `.git/index.lock`: a server that observes every registered repository on
// each change would then make the user's own `git add` or `git commit` fail
// with "Unable to create .git/index.lock" whenever the two overlapped.
func TestRegistryObservationLeavesTheGitIndexAlone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := testsupport.TempDir(t)
	gitTestCommand(t, "-C", root, "init", "-q")
	readme := filepath.Join(root, "README.md")
	if err := os.WriteFile(readme, []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, "-C", root, "add", "README.md")
	gitTestCommand(t, "-C", root, "-c", "user.name=Kivgraph Test", "-c", "user.email=kivgraph-test@example.invalid", "commit", "-qm", "initial")

	// Same bytes, different stat: exactly what makes git want to refresh.
	moved := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(readme, moved, moved); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(root, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}

	registry, err := NewRegistry(context.Background(), config.RepositoriesFile{
		Version:      1,
		Repositories: []config.Repository{{Name: "observed", Path: root, Languages: []string{"go"}}},
	})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if repository, _ := registry.Get("observed"); repository.Dirty {
		t.Fatalf("repository = %#v, want clean: only the stat data moved", repository)
	}
	after, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("observing the repository rewrote .git/index, so it took the index lock")
	}
}
