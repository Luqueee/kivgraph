package workspace

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

var unclaimedFixtureRoot = filepath.Join("..", "..", "testdata", "typescript", "unclaimed-sources")

func unclaimedFixture(t *testing.T) (Repository, TypeScriptDiscovery) {
	t.Helper()
	root, err := filepath.Abs(unclaimedFixtureRoot)
	if err != nil {
		t.Fatalf("resolve fixture root: %v", err)
	}
	repository := Repository{Name: "unclaimed", Path: root, RealPath: root}
	discovery, err := DiscoverTypeScript(context.Background(), repository)
	if err != nil {
		t.Fatalf("DiscoverTypeScript() error = %v", err)
	}
	return repository, discovery
}

// TestUnclaimedTypeScriptSourcesNamesEveryFileNoProjectOwns is the contract of
// the whole feature: a repository whose tsconfig includes only `src/**` has a
// `tests/` tree that belongs to no program, and the set has to be exactly
// those files -- not the sources a project already owns, not what a project
// excludes, not build output, not an installed dependency, and not a
// declaration file.
func TestUnclaimedTypeScriptSourcesNamesEveryFileNoProjectOwns(t *testing.T) {
	repository, discovery := unclaimedFixture(t)

	unclaimed, err := UnclaimedTypeScriptSources(context.Background(), repository, discovery)
	if err != nil {
		t.Fatalf("UnclaimedTypeScriptSources() error = %v", err)
	}

	root := repository.RealPath
	want := []string{
		filepath.Join(root, "scripts", "release.ts"),
		filepath.Join(root, "tests", "case.test.ts"),
		filepath.Join(root, "tests", "helpers", "fixture.ts"),
		filepath.Join(root, "tests", "widget.tsx"),
	}
	if !slices.Equal(unclaimed, want) {
		t.Fatalf("UnclaimedTypeScriptSources() = %#v, want %#v", unclaimed, want)
	}
}

// TestUnclaimedTypeScriptSourcesHonorsRepositoryExclusions keeps a configured
// exclusion effective: a repository that declares it does not want a tree
// indexed must not get it back through the unclaimed path.
func TestUnclaimedTypeScriptSourcesHonorsRepositoryExclusions(t *testing.T) {
	repository, discovery := unclaimedFixture(t)
	repository.Exclusions = []string{"tests/**"}

	unclaimed, err := UnclaimedTypeScriptSources(context.Background(), repository, discovery)
	if err != nil {
		t.Fatalf("UnclaimedTypeScriptSources() error = %v", err)
	}
	want := []string{filepath.Join(repository.RealPath, "scripts", "release.ts")}
	if !slices.Equal(unclaimed, want) {
		t.Fatalf("UnclaimedTypeScriptSources() = %#v, want %#v", unclaimed, want)
	}
}

// TestUnclaimedTypeScriptSourcesIsEmptyWhenEveryFileIsClaimed defends the
// negative case: a project that owns the whole tree leaves nothing unclaimed,
// so turning the option on adds no file and cannot change the payload.
func TestUnclaimedTypeScriptSourcesIsEmptyWhenEveryFileIsClaimed(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name":"whole","version":"1.0.0"}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{"compilerOptions":{"strict":true}}`)
	writeDiscoveryFile(t, filepath.Join(root, "src", "index.ts"), "export const index = 1\n")
	writeDiscoveryFile(t, filepath.Join(root, "tests", "index.test.ts"), "export const test = 1\n")

	repository := Repository{Name: "whole", Path: root, RealPath: root}
	discovery, err := DiscoverTypeScript(context.Background(), repository)
	if err != nil {
		t.Fatalf("DiscoverTypeScript() error = %v", err)
	}
	unclaimed, err := UnclaimedTypeScriptSources(context.Background(), repository, discovery)
	if err != nil {
		t.Fatalf("UnclaimedTypeScriptSources() error = %v", err)
	}
	if len(unclaimed) != 0 {
		t.Fatalf("UnclaimedTypeScriptSources() = %#v, want no file", unclaimed)
	}
}

// TestUnclaimedTypeScriptSourcesCoversMtsAndCtsButNotJavaScript draws the
// boundary of the walk. A ".mts" no project claims is TypeScript by its own
// extension and can be offered to one; whether a ".mjs" beside it is a source
// at all is the project's "allowJs" to answer, and this walk knows no
// project, so it must not offer it.
func TestUnclaimedTypeScriptSourcesCoversMtsAndCtsButNotJavaScript(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name":"edges","version":"1.0.0"}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"),
		`{"compilerOptions":{"strict":true},"include":["src/**/*.ts"]}`)
	writeDiscoveryFile(t, filepath.Join(root, "src", "index.ts"), "export const index = 1\n")
	writeDiscoveryFile(t, filepath.Join(root, "scripts", "build.mts"), "export const build = 1\n")
	writeDiscoveryFile(t, filepath.Join(root, "scripts", "load.cts"), "export const load = 1\n")
	writeDiscoveryFile(t, filepath.Join(root, "scripts", "tool.mjs"), "export const tool = 1\n")
	writeDiscoveryFile(t, filepath.Join(root, "scripts", "tool.cjs"), "module.exports = {}\n")

	repository := Repository{Name: "edges", Path: root, RealPath: root}
	discovery, err := DiscoverTypeScript(context.Background(), repository)
	if err != nil {
		t.Fatalf("DiscoverTypeScript() error = %v", err)
	}
	unclaimed, err := UnclaimedTypeScriptSources(context.Background(), repository, discovery)
	if err != nil {
		t.Fatalf("UnclaimedTypeScriptSources() error = %v", err)
	}
	want := []string{
		filepath.Join(root, "scripts", "build.mts"),
		filepath.Join(root, "scripts", "load.cts"),
	}
	if !slices.Equal(unclaimed, want) {
		t.Fatalf("UnclaimedTypeScriptSources() = %#v, want %#v", unclaimed, want)
	}
}

func unclaimedOf(t *testing.T, files map[string]string) []string {
	t.Helper()
	root := testsupport.TempDir(t)
	for name, content := range files {
		writeDiscoveryFile(t, filepath.Join(root, filepath.FromSlash(name)), content)
	}
	repository := Repository{Name: "base", Path: root, RealPath: root}
	discovery, err := DiscoverTypeScript(context.Background(), repository)
	if err != nil {
		t.Fatalf("DiscoverTypeScript() error = %v", err)
	}
	unclaimed, err := UnclaimedTypeScriptSources(context.Background(), repository, discovery)
	if err != nil {
		t.Fatalf("UnclaimedTypeScriptSources() error = %v", err)
	}
	relative := make([]string, 0, len(unclaimed))
	for _, file := range unclaimed {
		path, err := filepath.Rel(root, file)
		if err != nil {
			t.Fatal(err)
		}
		relative = append(relative, filepath.ToSlash(path))
	}
	return relative
}

// A monorepo's base config lists no source, so by itself it would claim every file
// below it -- including the ones its packages leave out of their own `include`.
// Those files belong to no program the packages build, and it is the base config,
// which nothing loads on its own, that used to hide them: with the option on the
// repository indexed no more than with it off.
func TestAConfigThatListsNoSourceDoesNotHideWhatTheProjectsBelowLeaveOut(t *testing.T) {
	base := `{"compilerOptions":{"strict":true}}`
	for name, files := range map[string]map[string]string{
		"base config beside the root manifest": {
			"package.json":                    `{"name":"root","version":"1.0.0"}`,
			"tsconfig.base.json":              base,
			"packages/a/package.json":         `{"name":"a","version":"1.0.0"}`,
			"packages/a/tsconfig.json":        `{"extends":"../../tsconfig.base.json","include":["src"]}`,
			"packages/a/src/index.ts":         "export const a = 1\n",
			"packages/a/test/outside.test.ts": "export const x = 1\n",
		},
		"root tsconfig without sources": {
			"package.json":                    `{"name":"root","version":"1.0.0"}`,
			"tsconfig.json":                   base,
			"packages/a/package.json":         `{"name":"a","version":"1.0.0"}`,
			"packages/a/tsconfig.json":        `{"include":["src"]}`,
			"packages/a/src/index.ts":         "export const a = 1\n",
			"packages/a/test/outside.test.ts": "export const x = 1\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := unclaimedOf(t, files)
			if want := []string{"packages/a/test/outside.test.ts"}; !slices.Equal(got, want) {
				t.Fatalf("unclaimed = %v, want %v", got, want)
			}
		})
	}
}

// The narrow side of the same rule: a config without sources that has no project
// below it is the whole repository's project and owns every file, and one that
// does list a source owns exactly that.
func TestAConfigThatListsNoSourceStillOwnsTheTreeWhenNoProjectIsBelowIt(t *testing.T) {
	got := unclaimedOf(t, map[string]string{
		"package.json":        `{"name":"whole","version":"1.0.0"}`,
		"tsconfig.base.json":  `{"compilerOptions":{"strict":true}}`,
		"src/index.ts":        "export const index = 1\n",
		"scripts/release.ts":  "export const release = 1\n",
		"tests/index.test.ts": "export const test = 1\n",
	})
	if len(got) != 0 {
		t.Fatalf("unclaimed = %v, want none: nothing below the config has a project of its own", got)
	}

	got = unclaimedOf(t, map[string]string{
		"package.json":             `{"name":"root","version":"1.0.0"}`,
		"tsconfig.base.json":       `{"compilerOptions":{"strict":true}}`,
		"packages/a/package.json":  `{"name":"a","version":"1.0.0"}`,
		"packages/a/tsconfig.json": `{"include":["src"]}`,
		"packages/a/src/index.ts":  "export const a = 1\n",
		"packages/a/src/more.ts":   "export const more = 1\n",
	})
	if len(got) != 0 {
		t.Fatalf("unclaimed = %v, want none: the package's own include claims both files", got)
	}
}

func TestUnclaimedTypeScriptSourcesHonorsCancellation(t *testing.T) {
	repository, discovery := unclaimedFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := UnclaimedTypeScriptSources(ctx, repository, discovery); !errors.Is(err, context.Canceled) {
		t.Fatalf("UnclaimedTypeScriptSources(canceled) error = %v, want context.Canceled", err)
	}
}
