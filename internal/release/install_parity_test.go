package release_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The two installers implement one set of pre-extraction checks, and ADR 0079
// names the cost of that: two implementations of the same rule drift. This is
// the gate that makes the drift a failure rather than a discovery.
//
// It compares declarations, not behaviour -- a marker says a check is there
// and cannot say it is right. What it does catch is the failure that actually
// happens: somebody hardens one installer against something a release taught
// them, and the other keeps letting it through for a year because nobody
// installs from the platform they do not use.
//
// A check that genuinely cannot exist on one platform is not silently dropped:
// removing its marker fails here, which is the point at which somebody has to
// write down why.
var checkMarker = regexp.MustCompile(`(?m)^\s*(?:#|rem)\s*check:\s*([a-z0-9-]+)\s*$`)

func markers(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	found := make([]string, 0, 8)
	seen := make(map[string]string, 8)
	for _, match := range checkMarker.FindAllStringSubmatch(string(data), -1) {
		name := match[1]
		if previous, duplicate := seen[name]; duplicate {
			t.Errorf("%s declares check %q twice (%s): one check, one place",
				filepath.Base(path), name, previous)
		}
		seen[name] = name
		found = append(found, name)
	}
	sort.Strings(found)
	return found
}

func TestBothInstallersDeclareTheSameChecks(t *testing.T) {
	shell := markers(t, filepath.Join("..", "..", "scripts", "install.sh"))
	powershell := markers(t, filepath.Join("..", "..", "scripts", "install.ps1"))

	if len(shell) == 0 {
		t.Fatal("scripts/install.sh declares no checks, so this gate is asserting nothing")
	}
	if strings.Join(shell, ",") != strings.Join(powershell, ",") {
		t.Fatalf("the installers do not declare the same checks\n  install.sh:  %v\n  install.ps1: %v\n"+
			"only in install.sh:  %v\n  only in install.ps1: %v",
			shell, powershell, missing(shell, powershell), missing(powershell, shell))
	}
}

// TestTheDeclaredChecksAreTheOnesThatMatter pins the set itself, so that
// deleting a check from both scripts is also a failure. Agreement between two
// files is not the property being defended; the checks are.
func TestTheDeclaredChecksAreTheOnesThatMatter(t *testing.T) {
	want := []string{
		"archive-digest",     // the asset is what the release says it is
		"bundle-checksums",   // every file inside it is what the bundle says
		"entry-paths",        // nothing escapes the bundle directory
		"entry-types",        // nothing but files and directories
		"launcher-ownership", // a launcher this did not write is not replaced
		"no-symlinks",        // the installed tree does not point elsewhere
		"required-programs",  // the bundle carries what it promises
		"version-match",      // it is the release that was asked for
	}
	got := markers(t, filepath.Join("..", "..", "scripts", "install.sh"))
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("declared checks = %v, want %v: adding one here is deliberate, losing one is not", got, want)
	}
}

func missing(from, in []string) []string {
	present := make(map[string]struct{}, len(in))
	for _, name := range in {
		present[name] = struct{}{}
	}
	absent := make([]string, 0)
	for _, name := range from {
		if _, ok := present[name]; !ok {
			absent = append(absent, name)
		}
	}
	return absent
}
