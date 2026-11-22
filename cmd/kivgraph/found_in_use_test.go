package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/audit"
	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// `clean` read the directory the configuration names, which holds no
// generation since profiles exist: it reported nothing to remove beside every
// graph on the machine.
func TestCleanRemovesWhatTheProfilePublished(t *testing.T) {
	initialisedHome(t)
	profile, err := config.LoadProfile("", "")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(profile.Config.Storage.DatabasePath)
	store, err := generation.New(root, generation.DefaultConfig())
	if err != nil {
		testsupport.RequireSpaceOrSkip(t, err)
		t.Fatal(err)
	}
	publishCleanGeneration(t, store, "000001")
	publishCleanGeneration(t, store, "000002")

	var stdout, stderr bytes.Buffer
	if code := runClean(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("clean = %d, stderr %q", code, stderr.String())
	}
	if text := stdout.String(); !strings.Contains(text, "would remove generation(s) 000001, 000002") {
		t.Fatalf("clean without --yes said %q, want both generations named", text)
	}
	stdout.Reset()
	if code := runClean([]string{"--keep-active", "--yes"}, &stdout, &stderr); code != 0 {
		t.Fatalf("clean --keep-active --yes = %d, stderr %q", code, stderr.String())
	}
	left, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].ID != "000002" {
		t.Fatalf("generations left = %+v, want only the active 000002", left)
	}
}

// The help prints `profile remove NAME --yes`, and the flag package stops at
// the first argument that is not a flag.
func TestProfileRemoveTakesItsFlagOnEitherSide(t *testing.T) {
	for _, args := range [][]string{{"web", "--yes"}, {"--yes", "web"}} {
		initialisedHome(t)
		var stdout, stderr bytes.Buffer
		if code := runProfile("create", []string{"web"}, &stdout, &stderr); code != 0 {
			t.Fatalf("create = %d, %q", code, stderr.String())
		}
		if code := runProfile("remove", args, &stdout, &stderr); code != 0 {
			t.Errorf("profile remove %v = %d, stderr %q", args, code, stderr.String())
		}
	}
	initialisedHome(t)
	var stdout, stderr bytes.Buffer
	_ = runProfile("create", []string{"web"}, &stdout, &stderr)
	// Still refused: without the flag, with two names, and with a flag that
	// does not exist after the name.
	for _, args := range [][]string{{"web"}, {"web", "other", "--yes"}, {"web", "--nope"}, {}} {
		if code := runProfile("remove", args, &stdout, &stderr); code != 2 {
			t.Errorf("profile remove %v = %d, want 2", args, code)
		}
	}
}

// One entry that cannot be registered took the pass down for the others: a
// directory that is gone, one that is not a checkout, a path with a link in it.
func TestRepositoriesThatRegisterLeavesOutWhatCannotBe(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(root, "good")
	plain := filepath.Join(root, "plain")
	for _, directory := range []string{good, plain} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	initGitRepository(t, good)
	linked := filepath.Join(root, "linked")
	if err := os.Symlink(good, linked); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	entry := func(name, path string) config.Repository {
		return config.Repository{Name: name, Path: path, Languages: []string{"go"}}
	}
	source := config.RepositoriesFile{Version: 1, Repositories: []config.Repository{
		entry("gone", filepath.Join(root, "gone")),
		entry("good", good),
		entry("plain", plain),
		entry("linked", linked),
	}}
	kept, unregistered := repositoriesThatRegister(context.Background(), source)
	if !reflect.DeepEqual(kept, []config.Repository{entry("good", good)}) {
		t.Errorf("kept = %+v, want only the checkout", kept)
	}
	for index, want := range []string{"gone: ", "plain: ", "linked: "} {
		if index >= len(unregistered) || !strings.HasPrefix(unregistered[index], want) || len(unregistered[index]) == len(want) {
			t.Errorf("unregistered = %q, want a reason for %q in position %d", unregistered, want, index)
		}
	}
	if len(unregistered) != 3 {
		t.Errorf("%d entries left out, want 3", len(unregistered))
	}

	// The registry a pass and the watcher are given: the checkout alone, with
	// the lines. Where nothing registers, or every entry does, it is the
	// registry the profile always had.
	registry, lines, err := registryOfWhatRegisters(context.Background(), config.Loaded{Repositories: source})
	if err != nil || len(lines) != 3 || len(registry.List()) != 1 || registry.List()[0].Name != "good" {
		t.Errorf("registry = %v, lines = %q, err = %v, want the checkout and three lines", registry, lines, err)
	}
	none := config.RepositoriesFile{Version: 1, Repositories: []config.Repository{entry("gone", filepath.Join(root, "gone"))}}
	if _, _, err := registryOfWhatRegisters(context.Background(), config.Loaded{Repositories: none}); err == nil {
		t.Error("a registry where nothing registers was accepted")
	}
	all := config.RepositoriesFile{Version: 1, Repositories: []config.Repository{entry("good", good)}}
	if registry, lines, err := registryOfWhatRegisters(context.Background(), config.Loaded{Repositories: all}); err != nil || len(lines) != 0 || len(registry.List()) != 1 {
		t.Errorf("a registry with nothing wrong: lines = %q, err = %v", lines, err)
	}
}

// `init` wrote the path as typed. The registry refuses a link in it -- `/tmp`
// on macOS -- and reads a relative one against the configuration's directory.
func TestParseRepositorySpecWritesTheDirectoryThatIsThere(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	cases := map[string]string{
		"a=" + real: real,
		"a=real":    real,
		"a=./real/": real,
	}
	if err := os.Symlink(real, filepath.Join(root, "link")); err == nil {
		cases["a="+filepath.Join(root, "link")] = real
		cases["a=link"] = real
	}
	for specification, want := range cases {
		repository, err := parseRepositorySpec(specification, []string{"go"})
		if err != nil || repository.Path != want {
			t.Errorf("parseRepositorySpec(%q) path = %q, %v, want %q", specification, repository.Path, err, want)
		}
	}

	// Refused before anything is written, and by name.
	if err := os.WriteFile(filepath.Join(root, "file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for specification, reason := range map[string]string{
		"a=missing":        "no such file",
		"a=~/not-a-thing":  "no such file",
		"a=file":           "is not a directory",
		"rust:1.96.1=real": "is reserved",
		"=real":            "NAME=PATH",
		"a=":               "NAME=PATH",
		"real":             "NAME=PATH",
	} {
		if _, err := parseRepositorySpec(specification, []string{"go"}); err == nil || !strings.Contains(err.Error(), reason) {
			t.Errorf("parseRepositorySpec(%q) error = %v, want one that says %q", specification, err, reason)
		}
	}
}

// A refused repository left the configuration created, and the next `init`
// reported it as existing.
func TestInitWritesNothingWhenARepositoryIsRefused(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "state", "config.yaml")
	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--config", configPath, "--repository", "a=" + filepath.Join(directory, "missing")}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "missing") {
		t.Fatalf("init = %d, stderr %q, want a refusal that names the path", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Dir(configPath)); !os.IsNotExist(err) {
		t.Errorf("the refusal left %s behind (%v)", filepath.Dir(configPath), err)
	}
}

// `impact` run from a linked worktree said the checkout was not indexed: the
// path is another one, and the repository is the same.
func TestRegisteredCheckoutOfFindsTheRepositoryAWorktreeBelongsTo(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(root, "main")
	other := filepath.Join(root, "other")
	for _, directory := range []string{main, other} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		initGitRepository(t, directory)
	}
	worktree := filepath.Join(root, "feature")
	runGitTestCommand(t, "-C", main, "worktree", "add", "-q", worktree)

	registry := func(paths ...string) config.Loaded {
		var loaded config.Loaded
		for index, path := range paths {
			loaded.Repositories.Repositories = append(loaded.Repositories.Repositories,
				config.Repository{Name: string(rune('a' + index)), Path: path})
		}
		return loaded
	}
	if got, found := registeredCheckoutOf(registry(other, main), worktree); !found || got != main {
		t.Errorf("worktree of main: %q, %t, want %q", got, found, main)
	}
	for name, testCase := range map[string]struct {
		loaded config.Loaded
		root   string
	}{
		"the repository is not registered":    {registry(other), worktree},
		"two entries are the same repository": {registry(main, worktree), worktree},
		"not a checkout":                      {registry(main), root},
	} {
		if got, found := registeredCheckoutOf(testCase.loaded, testCase.root); found {
			t.Errorf("%s: found %q, want it declined", name, got)
		}
	}
}

// A pass says, in one line, that the audit has something to say; a repository
// that declares everything adds nothing to the report.
func TestWriteIndexAuditPointsAtWhatThePassCouldNotBeTold(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("holes/package.json", `{"name":"holes","version":"1.0.0"}`)
	write("holes/tsconfig.json", `{"include":["sources"]}`)
	write("holes/src/index.ts", "export const one = 1\n")
	write("whole/package.json", `{"name":"whole","version":"1.0.0"}`)
	write("whole/tsconfig.json", `{"include":["src"]}`)
	write("whole/src/index.ts", "export const one = 1\n")
	repository := func(name string) workspace.Repository {
		path := filepath.Join(root, name)
		return workspace.Repository{Name: name, Path: path, RealPath: path, Languages: []string{"typescript"}}
	}

	var stdout bytes.Buffer
	writeIndexAudit(context.Background(), &stdout, []workspace.Repository{repository("holes")}, audit.Options{})
	if text := stdout.String(); !strings.HasPrefix(text, "index.audit: ") || !strings.Contains(text, "blocking") ||
		!strings.Contains(text, "kivgraph doctor repositories") || strings.Count(text, "\n") != 1 {
		t.Errorf("audit line = %q, want one line that names the command", text)
	}
	stdout.Reset()
	writeIndexAudit(context.Background(), &stdout, []workspace.Repository{repository("whole")}, audit.Options{})
	if stdout.Len() != 0 {
		t.Errorf("a repository that declares everything printed %q", stdout.String())
	}
}

func TestDescribeUnreadSourcesOrdersByCount(t *testing.T) {
	got := describeUnreadSources(map[string]int{".kt": 3, ".swift": 70, ".astro": 3})
	if want := "70 .swift, 3 .astro, 3 .kt"; got != want {
		t.Errorf("describeUnreadSources = %q, want %q", got, want)
	}
}

// `kivgraph index` in a directory that is not a Git checkout failed in the
// pass, with `.kivgraph/` already created in it.
func TestIndexOutsideACheckoutWritesNothing(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	var stdout, stderr bytes.Buffer
	if code := runIndexProject(nil, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "nothing was written") {
		t.Fatalf("index = %d, stderr %q, want a refusal that says nothing was written", code, stderr.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Errorf("the directory holds %v (%v), want the one source file", entries, err)
	}
}

// `mcp`, `hook` and `profile` are only the first word of their commands, and
// `kivgraph mcp --help` said `unknown command "mcp"`.
func TestAFamilyOfCommandsAnswersForHelp(t *testing.T) {
	for _, word := range []string{"mcp", "hook", "profile"} {
		for _, args := range [][]string{{word}, {word, "--help"}, {word, "-h"}} {
			var stdout, stderr bytes.Buffer
			code := run(append([]string{"kivgraph"}, args...), &stdout, &stderr)
			if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "kivgraph "+word+" <command>") {
				t.Errorf("%v = %d, stdout %q, stderr %q, want the commands of %s", args, code, stdout.String(), stderr.String(), word)
			}
			for _, spec := range commandFamily(word) {
				if !strings.Contains(stdout.String(), spec.usage) {
					t.Errorf("%v does not list %q", args, spec.usage)
				}
			}
		}
		var stdout, stderr bytes.Buffer
		if code := run([]string{"kivgraph", word, "no-such-thing"}, &stdout, &stderr); code != 2 ||
			!strings.Contains(stderr.String(), "no-such-thing") || !strings.Contains(stderr.String(), word+" --help") {
			t.Errorf("%s no-such-thing = %d, stderr %q, want it to name the word and the help", word, code, stderr.String())
		}
	}
	if family := commandFamily("no-such-family"); len(family) != 0 {
		t.Errorf("commandFamily of nothing = %v", family)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"kivgraph", "no-such-family"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("an unknown word = %d, stderr %q", code, stderr.String())
	}
}
