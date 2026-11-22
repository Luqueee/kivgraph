package javaloader

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func buildTree(t *testing.T, files ...string) string {
	t.Helper()
	root := testsupport.TempDir(t)
	for _, name := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The indexer ran at the root and nowhere else, so a repository of several
// builds had none where it looked. The outermost build of each branch is one.
func TestBuildRootsFindsTheBuildsBelowARootThatHasNone(t *testing.T) {
	none := func(string) bool { return false }
	for name, testCase := range map[string]struct {
		files    []string
		excluded func(string) bool
		want     []string
	}{
		"a build at the root is the only one": {
			files: []string{"pom.xml", "module/pom.xml", "other/build.gradle"},
			want:  []string{"."},
		},
		"builds side by side": {
			files: []string{"tools/build.gradle.kts", "backend/settings.gradle", "README.md"},
			want:  []string{"backend", "tools"},
		},
		"what is below a build is that build's": {
			files: []string{"backend/settings.gradle", "backend/api/build.gradle", "deep/er/service/pom.xml"},
			want:  []string{"backend", "deep/er/service"},
		},
		"not where no walk goes": {
			files:    []string{".cache/x/pom.xml", "node_modules/y/pom.xml", "vendor/z/pom.xml", "app/pom.xml"},
			excluded: func(relative string) bool { return relative == "vendor" },
			want:     []string{"app"},
		},
		// The indexer is asked at the root and says what it found there.
		"no build anywhere":                   {files: []string{"src/Main.java"}, want: []string{"."}},
		"a directory named like a build file": {files: []string{"pom.xml/keep", "app/build.sbt"}, want: []string{"app"}},
	} {
		excluded := testCase.excluded
		if excluded == nil {
			excluded = none
		}
		got, err := buildRoots(buildTree(t, testCase.files...), excluded)
		if err != nil {
			t.Errorf("%s: buildRoots: %v", name, err)
			continue
		}
		if !reflect.DeepEqual(got, testCase.want) {
			t.Errorf("%s: buildRoots = %v, want %v", name, got, testCase.want)
		}
	}
}

// A subtree the walk cannot read must fail the discovery: dropping it would
// leave its build out of the index while the pass reported success.
func TestBuildRootsFailsOnASubtreeItCannotRead(t *testing.T) {
	testsupport.SkipWithoutModeBits(t)
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode")
	}
	root := buildTree(t, "app/pom.xml", "locked/service/pom.xml")
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if got, err := buildRoots(root, func(string) bool { return false }); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("buildRoots = %v, err %v; want a permission error", got, err)
	}
}

func TestMergeBuildMakesPathsRelativeToTheRepository(t *testing.T) {
	first := scipwire.Index{ToolName: "scip-java", ToolVersion: "1", Documents: []scipwire.Document{{RelativePath: "src/A.java"}}}
	second := scipwire.Index{ToolName: "scip-java", ToolVersion: "2", Documents: []scipwire.Document{{RelativePath: "./src/B.java"}, {RelativePath: "src/C.java"}}}
	merged := mergeBuild(mergeBuild(scipwire.Index{}, first, "backend"), second, "tools/cli")
	var paths []string
	for _, document := range merged.Documents {
		paths = append(paths, document.RelativePath)
	}
	if want := []string{"backend/src/A.java", "tools/cli/src/B.java", "tools/cli/src/C.java"}; !reflect.DeepEqual(paths, want) || merged.ToolVersion != "1" {
		t.Errorf("merged paths = %v, tool version %q", paths, merged.ToolVersion)
	}
	if whole := mergeBuild(scipwire.Index{}, first, "."); whole.Documents[0].RelativePath != "src/A.java" {
		t.Errorf("a build at the root moved its paths: %v", whole.Documents)
	}
}

// End to end: two builds side by side in one repository, and nothing at its
// root. Both are in the payload, each file under its own directory.
func TestRunIndexesEveryBuildOfARepository(t *testing.T) {
	requireJavaToolchain(t)
	work := testsupport.TempDir(t)
	source := filepath.Join(work, "two")
	copyTree(t, fixture, filepath.Join(source, "first"))
	copyTree(t, fixture, filepath.Join(source, "nested", "second"))
	git(t, source, "init", "-q")
	git(t, source, "add", "-A")
	git(t, source, "-c", "user.email=a@b", "-c", "user.name=t", "commit", "-qm", "fixture")

	payload, err := Run(t.Context(), Options{
		Command:          "scip-java",
		TargetDirectory:  filepath.Join(work, "target"),
		Repository:       workspace.Repository{Name: "two", Path: source, RealPath: source},
		MaximumIndexTime: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	under := map[string]int{}
	for _, file := range payload.Files {
		switch {
		case strings.HasPrefix(file.Path, "first/"):
			under["first"]++
		case strings.HasPrefix(file.Path, "nested/second/"):
			under["nested/second"]++
		default:
			t.Errorf("a file outside both builds: %s", file.Path)
		}
	}
	if under["first"] == 0 || under["first"] != under["nested/second"] {
		t.Errorf("files per build = %v, want the same fixture twice", under)
	}
}
