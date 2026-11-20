package workspace

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestIsCacheDirectory(t *testing.T) {
	root := testsupport.TempDir(t)
	skipped := testsupport.WriteCacheTagTree(t, root)

	trailing := filepath.Join(root, "trailing")
	if err := os.MkdirAll(trailing, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trailing, "CACHEDIR.TAG"), []byte(testsupport.CacheTagSignature+" and more on the same line"), 0o600); err != nil {
		t.Fatal(err)
	}
	short := filepath.Join(root, "short")
	if err := os.MkdirAll(short, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(short, "CACHEDIR.TAG"), []byte(testsupport.CacheTagSignature[:len(testsupport.CacheTagSignature)-1]), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, test := range map[string]struct {
		directory string
		want      bool
	}{
		"valid tag":                    {skipped[0], true},
		"valid tag below a kept tree":  {skipped[1], true},
		"valid tag, text after":        {trailing, true},
		"no tag":                       {filepath.Join(root, "plain"), false},
		"the tree root has no tag":     {root, false},
		"signature one byte off":       {filepath.Join(root, "wrongsig"), false},
		"signature one byte short":     {short, false},
		"empty tag":                    {filepath.Join(root, "emptytag"), false},
		"a directory named like a tag": {filepath.Join(root, "dirtag"), false},
		"a directory that is absent":   {filepath.Join(root, "absent"), false},
		"a file, not a directory":      {filepath.Join(root, "go.mod"), false},
	} {
		if got := IsCacheDirectory(test.directory); got != test.want {
			t.Errorf("%s: IsCacheDirectory(%q) = %v, want %v", name, test.directory, got, test.want)
		}
	}

	if link := filepath.Join(root, "linktag"); isSymlink(filepath.Join(link, "CACHEDIR.TAG")) && IsCacheDirectory(link) {
		t.Error("a tag that is a symlink hid a directory: a link to a valid tag is not a tag")
	}
}

func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func TestIsCacheDirectoryIgnoresATagItCannotRead(t *testing.T) {
	testsupport.SkipWithoutModeBits(t)
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode")
	}
	directory := testsupport.TempDir(t)
	tag := filepath.Join(directory, "CACHEDIR.TAG")
	if err := os.WriteFile(tag, []byte(testsupport.CacheTagSignature), 0o000); err != nil {
		t.Fatal(err)
	}
	if IsCacheDirectory(directory) {
		t.Fatal("an unreadable tag hid a directory")
	}
}

type discovered struct {
	Go         GoDiscovery
	TypeScript TypeScriptDiscovery
	Unclaimed  []string
}

func discoverEverything(t *testing.T, root string) discovered {
	t.Helper()
	repository := Repository{Name: "tagged", Path: root, RealPath: root}
	goDiscovery, err := DiscoverGo(context.Background(), repository)
	if err != nil {
		t.Fatalf("DiscoverGo() error = %v", err)
	}
	typeScript, err := DiscoverTypeScript(context.Background(), repository)
	if err != nil {
		t.Fatalf("DiscoverTypeScript() error = %v", err)
	}
	unclaimed, err := UnclaimedTypeScriptSources(context.Background(), repository, typeScript)
	if err != nil {
		t.Fatalf("UnclaimedTypeScriptSources() error = %v", err)
	}
	return discovered{goDiscovery, typeScript, unclaimed}
}

func goDirectories(found discovered) []string {
	var directories []string
	for _, pkg := range found.Go.Packages {
		directories = append(directories, pkg.Directory)
	}
	return directories
}

// The discovery walks equal a walk that applies the rule by hand: what they
// report over a tree with tagged directories is what they report once those
// directories are deleted.
func TestDiscoveryWalksAgreeWithDeletingTheTaggedDirectories(t *testing.T) {
	root := testsupport.TempDir(t)
	skipped := testsupport.WriteCacheTagTree(t, root)
	got := discoverEverything(t, root)

	for _, directory := range skipped {
		if err := os.RemoveAll(directory); err != nil {
			t.Fatal(err)
		}
	}
	want := discoverEverything(t, root)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a tagged directory changed what discovery reports:\n got  %#v\n want %#v", got, want)
	}

	// The rule must not be over-broad: every directory that is not validly
	// tagged is still walked, by all three walks.
	for _, name := range []string{"src", "plain", "wrongsig", "emptytag", "dirtag", "linktag"} {
		if !slices.Contains(goDirectories(got), filepath.Join(root, name)) {
			t.Errorf("Go discovery lost %q, which carries no valid tag; found %v", name, goDirectories(got))
		}
	}
	for _, name := range []string{"plain", "wrongsig", "emptytag", "dirtag", "linktag"} {
		if !slices.Contains(got.Unclaimed, filepath.Join(root, name, "gen.ts")) {
			t.Errorf("the unclaimed walk lost %q/gen.ts; found %v", name, got.Unclaimed)
		}
	}
}

// Control for the test above: with the tags removed the same directories are
// walked and reported, so it is the tag that hides them and not the fixture.
func TestDiscoveryWalksReachATaggedDirectoryOnceItsTagIsGone(t *testing.T) {
	root := testsupport.TempDir(t)
	skipped := testsupport.WriteCacheTagTree(t, root)
	with := discoverEverything(t, root)
	for _, directory := range skipped {
		if err := os.Remove(filepath.Join(directory, "CACHEDIR.TAG")); err != nil {
			t.Fatal(err)
		}
	}
	without := discoverEverything(t, root)

	for _, directory := range skipped {
		if slices.Contains(goDirectories(with), directory) {
			t.Errorf("Go discovery entered the tagged %q", directory)
		}
		if !slices.Contains(goDirectories(without), directory) {
			t.Errorf("Go discovery did not reach %q without its tag; found %v", directory, goDirectories(without))
		}
	}
	tagged := filepath.Join(root, "tagged", "gen.ts")
	if slices.Contains(with.Unclaimed, tagged) {
		t.Errorf("the unclaimed walk entered the tagged directory: %v", with.Unclaimed)
	}
	if !slices.Contains(without.Unclaimed, tagged) {
		t.Errorf("the unclaimed walk did not reach %q without its tag: %v", tagged, without.Unclaimed)
	}
}

func writeTag(t *testing.T, directory string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "CACHEDIR.TAG"), []byte(testsupport.CacheTagSignature+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// What doctor lists is what the walks skip, no more and no fewer: the fixture's
// own list of skipped directories is the oracle.
func TestCacheDirectoriesListsExactlyWhatTheWalksSkip(t *testing.T) {
	root := testsupport.TempDir(t)
	skipped := testsupport.WriteCacheTagTree(t, root)
	// Tagged, but never reached by a walk: a dependency tree, a repository
	// internal, an excluded directory, and a tagged directory inside another one,
	// which is skipped with everything below it and so is not a second entry.
	writeTag(t, filepath.Join(root, "node_modules", "pkg"))
	writeTag(t, filepath.Join(root, ".git", "cache"))
	writeTag(t, filepath.Join(root, "excluded", "cache"))
	writeTag(t, filepath.Join(skipped[0], "inner"))

	got, err := CacheDirectories(context.Background(), Repository{Name: "tagged", Path: root, RealPath: root, Exclusions: []string{"excluded"}})
	if err != nil {
		t.Fatalf("CacheDirectories() error = %v", err)
	}
	want := slices.Clone(skipped)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("CacheDirectories() = %v, want %v", got, want)
	}
}

func TestCacheDirectoriesNamesNothingWithoutATag(t *testing.T) {
	root := testsupport.TempDir(t)
	testsupport.WriteCacheTagTree(t, root)
	for _, name := range []string{"tagged", filepath.Join("src", "nested", "target")} {
		if err := os.Remove(filepath.Join(root, name, "CACHEDIR.TAG")); err != nil {
			t.Fatal(err)
		}
	}
	got, err := CacheDirectories(context.Background(), Repository{Name: "tagged", Path: root, RealPath: root})
	if err != nil || len(got) != 0 {
		t.Fatalf("CacheDirectories() = %v, %v, want no directory and no error", got, err)
	}
}

// The root is the repository the user asked for, even when it is itself tagged.
func TestCacheDirectoriesNeverNamesTheRoot(t *testing.T) {
	root := testsupport.TempDir(t)
	writeTag(t, root)
	got, err := CacheDirectories(context.Background(), Repository{Name: "tagged", Path: root, RealPath: root})
	if err != nil || len(got) != 0 {
		t.Fatalf("CacheDirectories() = %v, %v, want the root left out", got, err)
	}
}

func TestCacheDirectoriesFailsOnWhatItCannotLookAt(t *testing.T) {
	root := testsupport.TempDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, test := range map[string]struct {
		ctx        context.Context
		repository Repository
	}{
		"a cancelled context":       {ctx, Repository{Path: root, RealPath: root}},
		"a root that is absent":     {context.Background(), Repository{Path: filepath.Join(root, "absent"), RealPath: filepath.Join(root, "absent")}},
		"an exclusion it rejects":   {context.Background(), Repository{Path: root, RealPath: root, Exclusions: []string{"../outside"}}},
		"a repository with no path": {context.Background(), Repository{}},
	} {
		if got, err := CacheDirectories(test.ctx, test.repository); err == nil {
			t.Errorf("%s: CacheDirectories() = %v, want an error: a walk that could not look is not an empty answer", name, got)
		}
	}
}

// afterCalls runs act the moment its Err method is called for the at-th time:
// the walk asks its context once per entry, so this changes the tree after a
// directory was listed and before it was read, without a seam in the walk.
type afterCalls struct {
	context.Context
	at, calls int
	act       func()
}

func (c *afterCalls) Err() error {
	c.calls++
	if c.calls == c.at {
		c.act()
	}
	return c.Context.Err()
}

// A directory that vanished between being listed and being read held no tag, so
// the walk goes on and still names the one that is there.
func TestCacheDirectoriesSkipsADirectoryThatVanishesMidWalk(t *testing.T) {
	root := testsupport.TempDir(t)
	writeTag(t, filepath.Join(root, "b-tagged"))
	if err := os.MkdirAll(filepath.Join(root, "a-gone"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "c-gone"), 0o700); err != nil {
		t.Fatal(err)
	}
	// One call before the walk, one at the root and one per directory after it:
	// the third is the first child, so c-gone is removed after it was listed.
	ctx := &afterCalls{Context: context.Background(), at: 3, act: func() { _ = os.RemoveAll(filepath.Join(root, "c-gone")) }}
	got, err := CacheDirectories(ctx, Repository{Path: root, RealPath: root})
	if err != nil {
		t.Fatalf("CacheDirectories() error = %v, want a vanished directory skipped", err)
	}
	if want := []string{filepath.Join(root, "b-tagged")}; !slices.Equal(got, want) {
		t.Fatalf("CacheDirectories() = %v, want %v", got, want)
	}
}

// Cancelling while the walk runs stops it with the cancellation, not with a
// shorter list.
func TestCacheDirectoriesStopsWhenCancelledMidWalk(t *testing.T) {
	root := testsupport.TempDir(t)
	writeTag(t, filepath.Join(root, "a"))
	writeTag(t, filepath.Join(root, "b"))
	inner, cancel := context.WithCancel(context.Background())
	ctx := &afterCalls{Context: inner, at: 2, act: cancel}
	if got, err := CacheDirectories(ctx, Repository{Path: root, RealPath: root}); err == nil {
		t.Fatalf("CacheDirectories() = %v, want the cancellation", got)
	}
}

// A directory that exists and cannot be read is not an absent tag: listing it as
// "no tagged directory" would be a claim about something nobody looked at.
func TestCacheDirectoriesFailsOnADirectoryItCannotRead(t *testing.T) {
	testsupport.SkipWithoutModeBits(t)
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode")
	}
	root := testsupport.TempDir(t)
	locked := filepath.Join(root, "locked")
	if err := os.MkdirAll(filepath.Join(locked, "inner"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if got, err := CacheDirectories(context.Background(), Repository{Path: root, RealPath: root}); err == nil {
		t.Fatalf("CacheDirectories() = %v, want an error for an unreadable directory", got)
	}
}

// A repository with no resolved path is read through its registered one.
func TestCacheDirectoriesFallsBackToTheRegisteredPath(t *testing.T) {
	root := testsupport.TempDir(t)
	writeTag(t, filepath.Join(root, "target"))
	got, err := CacheDirectories(context.Background(), Repository{Path: root})
	if err != nil || !slices.Equal(got, []string{filepath.Join(root, "target")}) {
		t.Fatalf("CacheDirectories() = %v, %v", got, err)
	}
}
