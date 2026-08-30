package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
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
		t.Fatalf("sameDirectory(%q, %q) = false; a symlink to the live root must compare equal to it, or the guard is bypassed by one", real, link)
	}
	other := filepath.Join(root, "other")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatalf("create %q: %v", other, err)
	}
	if sameDirectory(real, other) {
		t.Fatalf("sameDirectory(%q, %q) = true; two different directories must not compare equal", real, other)
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
	// Exactly one, not at least one: a duplicate append would double the edit
	// this benchmark's whole workload is defined as.
	want := "func editFrequencyProbe3() int { return 3 }"
	if count := strings.Count(text, want); count != 1 {
		t.Fatalf("appended text %q declares editFrequencyProbe3 %d times, want exactly 1", text[len(original):], count)
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
		{TotalSeconds: 20, AnalysisSeconds: seconds(4), PublicationSeconds: seconds(16)},
		{TotalSeconds: 10, AnalysisSeconds: seconds(2), PublicationSeconds: seconds(8)},
		{TotalSeconds: 30, AnalysisSeconds: seconds(6), PublicationSeconds: seconds(24)},
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
	if result.PassesWithBoundary != 3 {
		t.Fatalf("passes with boundary = %d, want 3", result.PassesWithBoundary)
	}
	if result.AnalysisShare == nil || result.PublicationShare == nil {
		t.Fatal("three passes reported a boundary, so both shares must be present")
	}
	if !near(*result.AnalysisShare, 0.2) || !near(*result.PublicationShare, 0.8) {
		t.Fatalf("shares = %v analysis / %v publication, want 0.2 / 0.8", *result.AnalysisShare, *result.PublicationShare)
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
	if result.MedianAnalysisSeconds != nil || result.AnalysisShare != nil {
		t.Fatal("a run with nothing to summarise must publish no split at all")
	}
}

// A pass that never reported where the rebuild began has no boundary to split
// at, and a zero there would read as an analysis half that cost nothing. The
// repository's own rule for this is in benchmarks/AGENTS.md: what was not
// measured is not published as zero.
func TestSummariseOmitsTheSplitWhenNoPassReportedABoundary(t *testing.T) {
	passes := []passCost{{TotalSeconds: 20}, {TotalSeconds: 10}}

	result := summarise(passes, []grepQuestion{{Seconds: 0.5}})

	if result.Passes != 2 || result.MedianTotalSeconds != 15 {
		t.Fatalf("the totals were measured and must survive, got %+v", result)
	}
	if result.PassesWithBoundary != 0 {
		t.Fatalf("passes with boundary = %d, want 0", result.PassesWithBoundary)
	}
	if result.MedianAnalysisSeconds != nil || result.MedianPublicationSeconds != nil {
		t.Fatal("an unmeasured split must be absent, not zero")
	}
	if result.AnalysisShare != nil || result.PublicationShare != nil {
		t.Fatal("a share computed from an absent split would read as 0 %, which is a claim")
	}
	if !near(result.RebuildsPerGrepQuestion, 30) {
		t.Fatalf("the crossover only needs the totals, got %v", result.RebuildsPerGrepQuestion)
	}
}

// A pass that did report a boundary still counts when another did not: the
// split is theirs, and dropping every pass because one was blind would report
// nothing where something was measured.
func TestSummariseSplitsOnThePassesThatReportedABoundary(t *testing.T) {
	passes := []passCost{
		{TotalSeconds: 20},
		{TotalSeconds: 20, AnalysisSeconds: seconds(4), PublicationSeconds: seconds(16)},
	}

	result := summarise(passes, nil)

	if result.PassesWithBoundary != 1 {
		t.Fatalf("passes with boundary = %d, want 1", result.PassesWithBoundary)
	}
	if result.MedianAnalysisSeconds == nil || !near(*result.MedianAnalysisSeconds, 4) {
		t.Fatalf("the one pass with a boundary must supply the split, got %v", result.MedianAnalysisSeconds)
	}
}

func seconds(value float64) *float64 { return &value }

// checkDestinations is the guard that has to be right before anything exists:
// it runs ahead of the os.MkdirAll that would otherwise have created the
// directory it is refusing. All three configured paths are covered, because a
// fact cache dropped inside an indexed tree is the same violation as a scratch
// copy dropped there.
func TestCheckDestinationsRefusesEveryPathInsideARegisteredRepository(t *testing.T) {
	root := t.TempDir()
	repository := filepath.Join(root, "repository")
	outside := filepath.Join(root, "outside")
	loaded := config.Loaded{
		Config: config.Config{Storage: config.StorageConfig{DatabasePath: filepath.Join(root, "state", "graph.lbdb")}},
		Repositories: config.RepositoriesFile{
			Repositories: []config.Repository{{Name: "one", Path: repository}},
		},
	}
	source := loaded.Repositories.Repositories[0]

	cases := []struct {
		name    string
		options flags
		want    string
	}{
		{"scratch inside", flags{scratchRoot: filepath.Join(repository, "copy")}, "-scratch"},
		{"root inside", flags{root: filepath.Join(repository, "generations")}, "-root"},
		{"fact cache inside", flags{cache: filepath.Join(repository, "cache")}, "-fact-cache"},
		{"scratch encloses the repository", flags{scratchRoot: root}, "-scratch"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := checkDestinations(loaded, source, testCase.options)
			if err == nil {
				t.Fatalf("checkDestinations(%+v) returned no error, want a refusal", testCase.options)
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("refusal %q does not name %s, so a reader cannot tell which flag to change", err, testCase.want)
			}
		})
	}

	permitted := flags{
		scratchRoot: filepath.Join(outside, "copy"),
		root:        filepath.Join(outside, "generations"),
		cache:       filepath.Join(outside, "cache"),
	}
	if err := checkDestinations(loaded, source, permitted); err != nil {
		t.Fatalf("three paths outside every repository must be permitted, got %v", err)
	}
	if err := checkDestinations(loaded, source, flags{}); err != nil {
		t.Fatalf("unset flags pick temporary directories and must be permitted, got %v", err)
	}
}

func TestCheckDestinationsRefusesTheLiveGenerationRoot(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "state")
	if err := os.MkdirAll(live, 0o755); err != nil {
		t.Fatalf("create %q: %v", live, err)
	}
	loaded := config.Loaded{
		Config: config.Config{Storage: config.StorageConfig{DatabasePath: filepath.Join(live, "graph.lbdb")}},
	}

	err := checkDestinations(loaded, config.Repository{Path: filepath.Join(root, "repository")}, flags{root: live})
	if err == nil {
		t.Fatal("publishing into the live root would leave the user's graph built from files this harness edited")
	}
	if !strings.Contains(err.Error(), live) {
		t.Fatalf("refusal %q does not name the live root", err)
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
