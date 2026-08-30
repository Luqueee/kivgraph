package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The guards are the part of this harness that has to be right even when the
// numbers are wrong. A benchmark that published into the live generation root,
// or that appended its probe to a registered repository, would leave the
// machine's own graph built from files it edited -- and nothing in the results
// would say so.

func TestWithinRejectsASiblingAndAcceptsAChild(t *testing.T) {
	cases := []struct {
		name      string
		parent    string
		candidate string
		want      bool
	}{
		{"child", "/home/user/repository", "/home/user/repository/internal", true},
		{"self", "/home/user/repository", "/home/user/repository", true},
		{"sibling", "/home/user/repository", "/home/user/other", false},
		{"prefix without separator", "/home/user/repo", "/home/user/repository", false},
		{"parent", "/home/user/repository", "/home/user", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := within(testCase.parent, testCase.candidate); got != testCase.want {
				t.Fatalf("within(%q, %q) = %v, want %v", testCase.parent, testCase.candidate, got, testCase.want)
			}
		})
	}
}

func TestSameDirectoryFollowsSymlinks(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "state")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatalf("create %q: %v", real, err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if !sameDirectory(real, link) {
		t.Fatal("a symlink to the live root must compare equal to it, or the guard is bypassed by one")
	}
	other := filepath.Join(root, "other")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatalf("create %q: %v", other, err)
	}
	if sameDirectory(real, other) {
		t.Fatal("two different directories must not compare equal")
	}
}

func TestEditTargetsSkipsTestsAndExclusions(t *testing.T) {
	root := t.TempDir()
	files := []string{
		"internal/one.go",
		"internal/one_test.go",
		"testdata/fixture.go",
		"excluded/two.go",
		"node_modules/package/three.go",
		"web/app.ts",
		"cmd/main.go",
	}
	for _, relative := range files {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create %q: %v", path, err)
		}
		if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("write %q: %v", path, err)
		}
	}

	targets, err := editTargets(root, []string{"excluded"})
	if err != nil {
		t.Fatalf("editTargets: %v", err)
	}

	want := []string{filepath.Join(root, "cmd/main.go"), filepath.Join(root, "internal/one.go")}
	if len(targets) != len(want) {
		t.Fatalf("editTargets returned %v, want %v", targets, want)
	}
	for index, target := range targets {
		if target != want[index] {
			t.Fatalf("editTargets[%d] = %q, want %q", index, target, want[index])
		}
	}
}

func TestApplyEditAddsOneDeclarationAndKeepsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unit.go")
	original := "package unit\n\nfunc Existing() {}\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := applyEdit(path, 3); err != nil {
		t.Fatalf("applyEdit: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(content)
	if len(text) <= len(original) || text[:len(original)] != original {
		t.Fatal("the edit must append: a rewrite would change what the pass had to analyse")
	}
	if want := "func editFrequencyProbe3() int { return 3 }"; !strings.Contains(text, want) {
		t.Fatalf("appended text %q does not contain %q", text[len(original):], want)
	}
}

func TestDistinctFilesKeepsFirstOccurrenceOrder(t *testing.T) {
	matches := []string{
		"/a/one.go:12:func Run() {}",
		"/a/one.go:44:Run()",
		"/b/two.go:3:Run()",
		"malformed line without a colon",
	}
	files := distinctFiles(matches)
	want := []string{"/a/one.go", "/b/two.go"}
	if len(files) != len(want) {
		t.Fatalf("distinctFiles = %v, want %v", files, want)
	}
	for index, file := range files {
		if file != want[index] {
			t.Fatalf("distinctFiles[%d] = %q, want %q", index, file, want[index])
		}
	}
}

// summarise decides what the report says, so its arithmetic is asserted rather
// than read off a run. The crossover in particular is the figure the issue asks
// for, and a summary that divided the wrong way would answer it backwards.
func TestSummariseReportsSharesAndTheCrossover(t *testing.T) {
	passes := []passCost{
		{TotalSeconds: 20, AnalysisSeconds: 4, PublicationSeconds: 16},
		{TotalSeconds: 10, AnalysisSeconds: 2, PublicationSeconds: 8},
		{TotalSeconds: 30, AnalysisSeconds: 6, PublicationSeconds: 24},
	}
	questions := []grepQuestion{
		{Seconds: 0.1},
		{Seconds: 0.2},
		{Seconds: 0.3},
		{Seconds: 99, Failed: true},
	}

	result := summarise(passes, questions)

	if result.Passes != 3 {
		t.Fatalf("passes = %d, want 3", result.Passes)
	}
	if result.MedianTotalSeconds != 20 {
		t.Fatalf("median total = %v, want 20", result.MedianTotalSeconds)
	}
	if result.FastestTotalSeconds != 10 || result.SlowestTotalSeconds != 30 {
		t.Fatalf("range = [%v, %v], want [10, 30]", result.FastestTotalSeconds, result.SlowestTotalSeconds)
	}
	if !near(result.AnalysisShare, 0.2) || !near(result.PublicationShare, 0.8) {
		t.Fatalf("shares = %v analysis / %v publication, want 0.2 / 0.8", result.AnalysisShare, result.PublicationShare)
	}
	if !near(result.GrepSecondsPerQuestion, 0.2) {
		t.Fatalf("median question = %v, want 0.2; a failed question must not enter the median", result.GrepSecondsPerQuestion)
	}
	if !near(result.RebuildsPerGrepQuestion, 100) {
		t.Fatalf("crossover = %v, want 100 questions per rebuild", result.RebuildsPerGrepQuestion)
	}
}

func TestSummariseSurvivesARunWithNothingToSummarise(t *testing.T) {
	result := summarise(nil, nil)
	if result.Passes != 0 || result.MedianTotalSeconds != 0 || result.RebuildsPerGrepQuestion != 0 {
		t.Fatalf("an empty run must summarise to zeroes, got %+v", result)
	}
}

func TestResolveSearchToolNamesWhatItWillRun(t *testing.T) {
	tool := resolveSearchTool()
	switch tool.name {
	case "ripgrep":
		if tool.binary() != "rg" {
			t.Fatalf("ripgrep must run rg, got %q", tool.binary())
		}
	case "grep":
		if tool.binary() != "grep" {
			t.Fatalf("grep must run grep, got %q", tool.binary())
		}
		arguments := tool.arguments("Run", []string{"/repo"})
		if !slices.Contains(arguments, "--exclude-dir=node_modules") {
			t.Fatal("the grep fallback must exclude installed dependencies, or it times a search no session runs")
		}
	default:
		t.Fatalf("unknown search tool %q", tool.name)
	}
	if arguments := tool.arguments("Run", []string{"/repo"}); !slices.Contains(arguments, "/repo") {
		t.Fatal("the search must be given the repository roots")
	}
}

func near(got, want float64) bool {
	difference := got - want
	if difference < 0 {
		difference = -difference
	}
	return difference < 1e-9
}
