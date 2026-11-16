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
