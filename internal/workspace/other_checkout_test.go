package workspace

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/checkout"
	"github.com/Luqueee/kivgraph/internal/config"
)

// otherCheckoutTree is a repository with a package at its root and, below it,
// the three shapes a `.git` takes that are not the root's own, each holding a
// second copy of the package and of a Go module.
//
//	worktree    `.git` is a file that points under `worktrees`: another checkout
//	clone       `.git` is a directory: another checkout
//	submodule   `.git` is a file that points under `modules`, and the root's
//	            .gitmodules lists it: part of this one
//	plain       no `.git`
func otherCheckoutTree(t *testing.T) (root string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	put := func(name, body string) {
		t.Helper()
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	put(".git/HEAD", "ref: refs/heads/main\n")
	put("package.json", `{"name": "app", "version": "1.0.0"}`)
	put("tsconfig.json", `{"include": ["src"]}`)
	put("src/index.ts", "export const one = 1\n")
	put("go.mod", "module example.test/app\n\ngo 1.22\n")
	for _, twin := range []string{".worktrees/feature", "vendor-clone", "plain"} {
		put(twin+"/package.json", `{"name": "app", "version": "1.0.0"}`)
		put(twin+"/tsconfig.json", `{"include": ["src"]}`)
		put(twin+"/src/index.ts", "export const one = 1\n")
		put(twin+"/go.mod", "module example.test/app\n\ngo 1.22\n")
	}
	put("sub/package.json", `{"name": "sub", "version": "1.0.0"}`)
	put("sub/tsconfig.json", `{"include": ["src"]}`)
	put("sub/src/index.ts", "export const two = 2\n")
	put(".worktrees/feature/.git", "gitdir: "+filepath.Join(root, ".git", "worktrees", "feature")+"\n")
	put("vendor-clone/.git/HEAD", "ref: refs/heads/main\n")
	put("sub/.git", "gitdir: ../.git/modules/sub\n")
	put(".gitmodules", "[submodule \"sub\"]\n\tpath = sub\n")
	return root
}

func TestIsOtherCheckoutTellsAWorktreeAndACloneFromASubmodule(t *testing.T) {
	root := otherCheckoutTree(t)
	if err := os.WriteFile(filepath.Join(root, "plain", "notes"), []byte("gitdir: nowhere"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A `.git` that is neither: a file that points nowhere, and a link.
	if err := os.MkdirAll(filepath.Join(root, "garbage"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "garbage", ".git"), []byte("not a pointer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		".worktrees/feature": true,
		"vendor-clone":       true,
		"sub":                false,
		"plain":              false,
		"garbage":            false,
		"src":                false,
		"missing":            false,
	}
	if err := os.MkdirAll(filepath.Join(root, "linked"), 0o700); err == nil {
		if os.Symlink(filepath.Join(root, ".git"), filepath.Join(root, "linked", ".git")) == nil {
			want["linked"] = false
		}
	}
	for directory, expected := range want {
		if got := checkout.IsOtherCheckout(filepath.Join(root, filepath.FromSlash(directory))); got != expected {
			t.Errorf("checkout.IsOtherCheckout(%s) = %t, want %t", directory, got, expected)
		}
	}
}

// Git once declared a submodule by an embedded `.git` directory, which has the
// shape of a clone. The root's .gitmodules is what tells them apart.
func TestIsOtherCheckoutFollowsTheRootsGitmodulesForAnEmbeddedGitDirectory(t *testing.T) {
	root := otherCheckoutTree(t)
	legacy := filepath.Join(root, "legacy")
	if err := os.MkdirAll(filepath.Join(legacy, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitmodules := filepath.Join(root, ".gitmodules")
	listed := "[submodule \"sub\"]\n\tpath = sub\n[submodule \"legacy\"]\n\tpath = legacy\n"
	if err := os.WriteFile(gitmodules, []byte(listed), 0o600); err != nil {
		t.Fatal(err)
	}
	if checkout.IsOtherCheckout(legacy) {
		t.Error("a submodule the root lists was skipped as another checkout")
	}
	unlisted := "[submodule \"sub\"]\n\tpath = sub\n"
	if err := os.WriteFile(gitmodules, []byte(unlisted), 0o600); err != nil {
		t.Fatal(err)
	}
	if !checkout.IsOtherCheckout(legacy) {
		t.Error("an embedded .git directory the root does not list was walked")
	}
}

// The gitdir of a linked worktree is an absolute path. An ancestor named modules
// in it declares nothing: only the enclosing repository's list does.
func TestIsOtherCheckoutDoesNotReadAModulesAncestorAsASubmodule(t *testing.T) {
	root := otherCheckoutTree(t)
	worktree := filepath.Join(root, "worktree")
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		t.Fatal(err)
	}
	pointer := "gitdir: /srv/modules/app/.git/worktrees/feature\n"
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte(pointer), 0o600); err != nil {
		t.Fatal(err)
	}
	if !checkout.IsOtherCheckout(worktree) {
		t.Error("a linked worktree with a modules ancestor in its gitdir was walked as a submodule")
	}
}

// A `.git` file that is not a gitdir pointer leaves its directory walked, so a
// broken file cannot hide a tree from every walk.
func TestIsOtherCheckoutWalksADirectoryWithAMalformedGitFile(t *testing.T) {
	root := otherCheckoutTree(t)
	for name, body := range map[string]string{
		"empty":   "",
		"nocolon": "gitdir\n",
		"text":    "not a pointer\n",
	} {
		directory := filepath.Join(root, name)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, ".git"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if checkout.IsOtherCheckout(directory) {
			t.Errorf("%s: a malformed .git was skipped as another checkout", name)
		}
	}
}

// The case found on real code: a worktree linked inside the repository repeats
// its package.json, the name had two providers, and neither was indexed.
func TestDiscoveryDoesNotEnterAnotherCheckout(t *testing.T) {
	root := otherCheckoutTree(t)
	repository := Repository{Name: "app", Path: root, RealPath: root, Languages: []string{"typescript", "go"}}

	registry, err := NewTypeScriptPackageRegistry(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	// `plain` is an ordinary directory and is still the duplicate it always
	// was: the rule is about checkouts, not about copies.
	conflicts := registry.Conflicts()
	if len(conflicts) != 1 || conflicts[0].Name != "app" || len(conflicts[0].Manifests) != 2 {
		t.Fatalf("conflicts = %+v, want the root and the plain copy and nothing from a checkout", conflicts)
	}
	if _, found := registry.Get("sub"); !found {
		t.Error("the submodule's package is gone: a submodule is part of the repository")
	}

	modules, err := DiscoverGo(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	var manifests []string
	for _, module := range modules.Modules {
		relative, _ := filepath.Rel(root, module.ManifestPath)
		manifests = append(manifests, filepath.ToSlash(relative))
	}
	if want := []string{"go.mod", "plain/go.mod"}; !reflect.DeepEqual(manifests, want) {
		t.Errorf("Go manifests = %v, want %v", manifests, want)
	}

	skipped, err := OtherCheckouts(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, ".worktrees", "feature"), filepath.Join(root, "vendor-clone")}
	if !reflect.DeepEqual(skipped, want) {
		t.Errorf("OtherCheckouts = %v, want %v", skipped, want)
	}
}

// A workspace whose root is a repository and that holds its applications as
// clones could be registered as the root or as the applications, never both.
// No walk of the root enters a clone, so the two hold no file in common.
func TestACheckoutBelowAnotherRegistersBesideIt(t *testing.T) {
	root := otherCheckoutTree(t)
	entry := func(name, relative string) config.Repository {
		return config.Repository{Name: name, Path: filepath.Join(root, filepath.FromSlash(relative))}
	}
	for name, testCase := range map[string]struct {
		inner     string
		wantError string
	}{
		"a clone":             {"vendor-clone", ""},
		"a linked worktree":   {".worktrees/feature", ""},
		"a plain directory":   {"plain", "nested repositories"},
		"a submodule":         {"sub", "nested repositories"},
		"a directory of ours": {"src", "nested repositories"},
	} {
		for _, order := range [][]config.Repository{
			{entry("outer", "."), entry("inner", testCase.inner)},
			{entry("inner", testCase.inner), entry("outer", ".")},
		} {
			_, err := validatePaths(context.Background(), config.RepositoriesFile{Repositories: order})
			switch {
			case testCase.wantError == "" && err != nil:
				t.Errorf("%s: %v, want the two registered", name, err)
			case testCase.wantError != "" && (err == nil || !strings.Contains(err.Error(), testCase.wantError)):
				t.Errorf("%s: error = %v, want %q", name, err, testCase.wantError)
			}
		}
	}
}

// TypeScript 5.5 reads `${configDir}` in a path as the directory of the project
// being loaded, so that a shared base can say "the project's files". Astro's
// base does. Read as a literal the project claimed nothing.
func TestClaimedSourcesResolveTheConfigDirectoryOfABase(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	put := func(name, body string) {
		t.Helper()
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	put("node_modules/base/tsconfig.json", `{
		"compilerOptions": {"allowJs": true},
		"include": ["${configDir}/.generated/types.d.ts", "${configDir}/**/*"],
		"exclude": ["${configDir}/dist"]
	}`)
	put("app/tsconfig.json", `{"extends": "base/tsconfig.json"}`)
	put("app/src/index.ts", "export const one = 1\n")
	put("app/tool.mjs", "export const two = 2\n")
	put("app/dist/out.js", "export const built = 3\n")
	// Beside the base, where the literal reading looked.
	put("node_modules/base/stray.ts", "export const stray = 4\n")

	claimed, err := ClaimedTypeScriptSources(filepath.Join(root, "app", "tsconfig.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	var relative []string
	for _, source := range claimed {
		path, _ := filepath.Rel(root, source)
		relative = append(relative, filepath.ToSlash(path))
	}
	if want := []string{"app/src/index.ts", "app/tool.mjs"}; !reflect.DeepEqual(relative, want) {
		t.Errorf("claimed = %v, want %v", relative, want)
	}

	// A pattern without the variable is the one it was.
	// Joined the way the platform joins: on Windows that is a backslash.
	if got := inConfigDirectory([]string{"/a/src/**/*", "/base/${configDir}/x"}, "/project"); !reflect.DeepEqual(got, []string{"/a/src/**/*", filepath.Join("/project", "x")}) {
		t.Errorf("inConfigDirectory = %v", got)
	}
	if inConfigDirectory(nil, "/project") != nil {
		t.Error("an absent list became an empty one: absent means the default, empty means nothing")
	}
}
