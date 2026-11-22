package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// What Git ignores and holds source is left out of the repository, and named.
// What it ignores and holds none is not listed: the list is compared between
// passes, and a log directory that comes and goes is not a change of sources.
func TestRegistryExcludesTheSourcesGitIgnores(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required for repository registration: %v", err)
	}
	root := testsupport.TempDir(t)
	put := func(name, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	put(".gitignore", ".react-router/\nbuild/\nlogs/\nnode_modules/\n*.gen.ts\napp/[locale]/out/\nother/\n")
	put("app/root.ts", "export const root = 1\n")
	gitTestCommand(t, "-C", root, "init", "-q")
	gitTestCommand(t, "-C", root, "add", ".")
	gitTestCommand(t, "-C", root, "-c", "user.name=Kivgraph Test", "-c", "user.email=kivgraph-test@example.invalid", "commit", "-qm", "initial")
	put("web/.react-router/types/+types/root.ts", "export type Route = {}\n")
	put("build/server/index.js", "export const built = 1\n")
	put("app/routes.gen.ts", "export const routes = []\n")
	put("logs/run.log", "nothing\n")
	put("node_modules/dep/index.js", "export const dep = 1\n")
	// A name that is a pattern is not an exclusion this can write.
	put("app/[locale]/out/page.js", "export const page = 1\n")
	// Source of a language the repository does not register.
	put("other/tool.py", "x = 1\n")

	entry := config.Repository{Name: "app", Path: root, Languages: []string{"typescript"}}
	registered := func(entry config.Repository) Repository {
		t.Helper()
		registry, err := NewRegistry(context.Background(), config.RepositoriesFile{Version: 1, Repositories: []config.Repository{entry}})
		if err != nil {
			t.Fatalf("NewRegistry() error = %v", err)
		}
		repository, _ := registry.Get("app")
		return repository
	}
	repository := registered(entry)
	// `web` holds nothing but what is ignored, and Git names it too.
	want := []string{"app/routes.gen.ts", "build", "web", "web/.react-router"}
	if !reflect.DeepEqual(repository.Ignored, want) || !reflect.DeepEqual(repository.Exclusions, want) {
		t.Fatalf("ignored = %v, exclusions = %v, want %v", repository.Ignored, repository.Exclusions, want)
	}
	for path, excluded := range map[string]bool{
		"web/.react-router/types/+types/root.ts": true,
		"build/server/index.js":                  true,
		"app/routes.gen.ts":                      true,
		"app/root.ts":                            false,
		"logs/run.log":                           false,
	} {
		got, err := MatchesExclusion(root, filepath.Join(root, filepath.FromSlash(path)), repository.Exclusions)
		if err != nil || got != excluded {
			t.Errorf("MatchesExclusion(%s) = %t, %v, want %t", path, got, err, excluded)
		}
	}
	listed, err := IgnoredSources(context.Background(), repository)
	if err != nil || len(listed) != 4 || listed[1] != filepath.Join(root, "build") {
		t.Errorf("IgnoredSources() = %v, %v", listed, err)
	}

	// The repository's own exclusions stay, and it can ask for what a build
	// writes.
	entry.Exclusions = []string{"docs/**"}
	if got := registered(entry).Exclusions; !reflect.DeepEqual(got, append([]string{"docs/**"}, want...)) {
		t.Errorf("exclusions = %v", got)
	}
	entry.IncludeGitIgnored = true
	if repository := registered(entry); len(repository.Ignored) != 0 || !reflect.DeepEqual(repository.Exclusions, []string{"docs/**"}) {
		t.Errorf("with include_git_ignored: ignored = %v, exclusions = %v", repository.Ignored, repository.Exclusions)
	}

	// Git that cannot answer excludes nothing, as before.
	failing := func(context.Context, string, ...string) (string, error) { return "", errors.New("no git") }
	if got := ignoredSources(context.Background(), failing, root, []string{"typescript"}); got != nil {
		t.Errorf("ignoredSources with a failing git = %v", got)
	}
}

func TestHoldsSourceStopsAtItsBudget(t *testing.T) {
	root := t.TempDir()
	if holdsSource(root, []string{".ts"}) {
		t.Error("an empty directory holds source")
	}
	if holdsSource(filepath.Join(root, "missing"), []string{".ts"}) {
		t.Error("a path that is not there holds source")
	}
	if err := os.Mkdir(filepath.Join(root, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= ignoredWalkBudget; index++ {
		if err := os.WriteFile(filepath.Join(root, "data", strconv.Itoa(index)+".bin"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Too large to read whole: taken to hold some, which only excludes more.
	if !holdsSource(root, []string{".ts"}) {
		t.Error("a directory larger than the budget was read as holding none")
	}
}
