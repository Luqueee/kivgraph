package workspace

import (
	"path/filepath"
	"testing"
)

// ExclusionMatcher only exists so a loader does not have to carry an error
// through every file it looks at. It has to answer what MatchesExclusion
// answers, pattern by pattern and path by path.
func TestExclusionMatcherAnswersAsMatchesExclusion(t *testing.T) {
	root := t.TempDir()
	patterns := []string{"gen", "**/gen", "./gen", "lib/gen", "gen/**", "*.g.dart", "**/*.g.dart", "lib/*/gen",
		"lib/**/gen", filepath.Join(root, "gen"), "", ".", "vendor/"}
	paths := []string{"gen/a.dart", "lib/gen/a.dart", "lib/x/gen/a.dart", "lib/x/y/gen/a.dart", "lib/a.g.dart",
		"a.g.dart", "lib/gen", "gen", "generic/a.dart", "vendor/x/a.go", "src/vendor/a.go", "a.dart"}
	matched := 0
	for _, pattern := range patterns {
		matcher, err := ExclusionMatcher(root, []string{pattern})
		if err != nil {
			t.Fatalf("ExclusionMatcher(%q): %v", pattern, err)
		}
		for _, path := range paths {
			want, err := MatchesExclusion(root, filepath.Join(root, filepath.FromSlash(path)), []string{pattern})
			if err != nil {
				t.Fatalf("MatchesExclusion(%q, %q): %v", pattern, path, err)
			}
			if got := matcher(path); got != want {
				t.Errorf("pattern %q, path %q: matcher says %t, MatchesExclusion says %t", pattern, path, got, want)
			}
			if want {
				matched++
			}
		}
	}
	if matched == 0 || matched == len(patterns)*len(paths) {
		t.Fatalf("%d of %d cases matched: the corpus holds one answer only", matched, len(patterns)*len(paths))
	}
}

func TestExclusionMatcherRefusesAPatternOnceAndUpFront(t *testing.T) {
	root := t.TempDir()
	if _, err := ExclusionMatcher(root, []string{"vendor", "[unterminated"}); err == nil {
		t.Fatal("a pattern the matcher cannot read was accepted")
	}
	none, err := ExclusionMatcher(root, nil)
	if err != nil || none("vendor/a.go") {
		t.Fatalf("with no exclusions: %v, excluded=%t", err, none("vendor/a.go"))
	}
	matcher, err := ExclusionMatcher(root, []string{"vendor"})
	if err != nil {
		t.Fatal(err)
	}
	// The repository itself and a path that leaves it are not excluded from it.
	for _, path := range []string{".", "", "../outside/vendor/a.go"} {
		if matcher(path) {
			t.Errorf("%q was excluded", path)
		}
	}
}
