package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunRecordsBothArmsWithoutUsingRealCaches(t *testing.T) {
	repository := newFixtureRepository(t)
	commands := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(commands, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(commands, "go"), "#!/bin/sh\necho 'go version go1.26.6 test/fixture'\n")
	writeExecutable(t, filepath.Join(commands, "bazel"), "#!/bin/sh\necho 'Build completed successfully'\n")
	t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repository); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	output := filepath.Join(t.TempDir(), "result")
	if err := run(context.Background(), config{Trials: 2, Output: output}); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(filepath.Join(output, "results.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got result
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got.Schema != schemaName || len(got.Trials) != 2 {
		t.Fatalf("result schema/trials = %q/%d", got.Schema, len(got.Trials))
	}
	if got.Trials[0].Arms[0].Name != armGo || got.Trials[1].Arms[0].Name != armBazel {
		t.Fatalf("arm order was not alternated: %#v", got.Trials)
	}
	bazelLog, err := os.ReadFile(filepath.Join(output, "trial-01-bazel.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bazelLog), "--disk_cache=") || !strings.Contains(string(bazelLog), "--ignore_all_rc_files") {
		t.Fatalf("Bazel commands did not isolate host configuration and caches:\n%s", bazelLog)
	}
	if _, err := os.Stat(filepath.Join(output, "report.md")); err != nil {
		t.Fatal(err)
	}
}

func TestCleanTrackedFilesRejectsDirtyRepository(t *testing.T) {
	repository := newFixtureRepository(t)
	if err := os.WriteFile(filepath.Join(repository, "untracked"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := cleanTrackedFiles(context.Background(), repository)
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("cleanTrackedFiles() error = %v, want dirty error", err)
	}
}

func TestValidateConfigRejectsInvalidTrials(t *testing.T) {
	for _, trials := range []int{-1, 0, maxTrials + 1} {
		err := validateConfig(config{Trials: trials, Output: t.TempDir()})
		if err == nil || !strings.Contains(err.Error(), "trials") {
			t.Fatalf("validateConfig(%d) error = %v, want trials error", trials, err)
		}
	}
}

func TestValidateConfigRejectsMissingOutput(t *testing.T) {
	err := validateConfig(config{Trials: 1})
	if err == nil || !strings.Contains(err.Error(), "output") {
		t.Fatalf("validateConfig() error = %v, want output error", err)
	}
}

func TestChangedFilesDistinguishesMissingAddedAndModified(t *testing.T) {
	before := map[string]string{"missing": "a", "modified": "b"}
	after := map[string]string{"added": "c", "modified": "d"}
	want := []string{"added", "missing", "modified"}
	if got := changedFiles(before, after); !reflect.DeepEqual(got, want) {
		t.Fatalf("changedFiles() = %v, want %v", got, want)
	}
}

func TestApplyEditChangesOnlyRequestedFile(t *testing.T) {
	root := t.TempDir()
	files := []string{"internal/version/version.go", "go.mod"}
	for _, relative := range files {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package version\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before, err := hashFiles(root, files)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyEdit(root, files[0], 7); err != nil {
		t.Fatal(err)
	}
	after, err := hashFiles(root, files)
	if err != nil {
		t.Fatal(err)
	}
	if got := changedFiles(before, after); !reflect.DeepEqual(got, files[:1]) {
		t.Fatalf("changed files = %v, want %v", got, files[:1])
	}
}

func TestApplyEditRejectsMissingFile(t *testing.T) {
	err := applyEdit(t.TempDir(), editFile, 1)
	if err == nil || !strings.Contains(err.Error(), editFile) {
		t.Fatalf("applyEdit() error = %v, want path in error", err)
	}
}

func TestCopyFilesPreservesTrackedSymlinks(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "AGENTS.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("AGENTS.md", filepath.Join(source, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	files := []string{"AGENTS.md", "CLAUDE.md"}
	if err := copyFiles(source, destination, files); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(destination, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "AGENTS.md" {
		t.Fatalf("copied link target = %q, want AGENTS.md", target)
	}
	if _, err := hashFiles(destination, files); err != nil {
		t.Fatal(err)
	}
}

func TestMedianRejectsEmptyInput(t *testing.T) {
	if _, err := median(nil); err == nil {
		t.Fatal("median(nil) succeeded, want error")
	}
}

func TestMedianDoesNotMutateInput(t *testing.T) {
	values := []float64{9, 1, 5, 3}
	wantValues := append([]float64(nil), values...)
	got, err := median(values)
	if err != nil {
		t.Fatal(err)
	}
	if got != 4 {
		t.Fatalf("median() = %v, want 4", got)
	}
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("median mutated input: got %v, want %v", values, wantValues)
	}
}

func TestArmOrderAlternates(t *testing.T) {
	if got := armOrder(1); !reflect.DeepEqual(got, []string{armGo, armBazel}) {
		t.Fatalf("armOrder(1) = %v", got)
	}
	if got := armOrder(2); !reflect.DeepEqual(got, []string{armBazel, armGo}) {
		t.Fatalf("armOrder(2) = %v", got)
	}
}

func TestSummarizeUsesMediansAndRatios(t *testing.T) {
	trials := []trialResult{
		{Arms: []armResult{{Name: armGo, Clean: phases{BuildSeconds: 4}, Warm: buildPhase{BuildSeconds: 2}, Edit: editPhase{BuildSeconds: 3}}, {Name: armBazel, Clean: phases{BuildSeconds: 2}, Warm: buildPhase{BuildSeconds: 1}, Edit: editPhase{BuildSeconds: 1.5}}}},
		{Arms: []armResult{{Name: armBazel, Clean: phases{BuildSeconds: 4}, Warm: buildPhase{BuildSeconds: 2}, Edit: editPhase{BuildSeconds: 2.5}}, {Name: armGo, Clean: phases{BuildSeconds: 6}, Warm: buildPhase{BuildSeconds: 4}, Edit: editPhase{BuildSeconds: 5}}}},
	}
	got, err := summarize(trials)
	if err != nil {
		t.Fatal(err)
	}
	if got.Go.CleanBuildSeconds != 5 || got.Bazel.CleanBuildSeconds != 3 {
		t.Fatalf("clean medians = go %v bazel %v", got.Go.CleanBuildSeconds, got.Bazel.CleanBuildSeconds)
	}
	if got.Ratios.GoOverBazelEdit != 2 {
		t.Fatalf("edit ratio = %v, want 2", got.Ratios.GoOverBazelEdit)
	}
}

func TestSummarizeRejectsIncompleteTrial(t *testing.T) {
	_, err := summarize([]trialResult{{Arms: []armResult{{Name: armGo}}}})
	if err == nil || !strings.Contains(err.Error(), armBazel) {
		t.Fatalf("summarize() error = %v, want missing Bazel arm", err)
	}
}

func TestSummarizeRejectsUnknownAndDuplicateArms(t *testing.T) {
	for _, arms := range [][]armResult{
		{{Name: "other"}, {Name: armGo}},
		{{Name: armGo}, {Name: armGo}, {Name: armBazel}},
	} {
		if _, err := summarize([]trialResult{{Index: 1, Arms: arms}}); err == nil {
			t.Fatalf("summarize(%v) succeeded, want error", arms)
		}
	}
}

func newFixtureRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		".bazelversion":               "9.2.0\n",
		"MODULE.bazel":                "module(name = \"fixture\")\n",
		"MODULE.bazel.lock":           "{}\n",
		"go.mod":                      "module example.test/fixture\n\ngo 1.26.6\n",
		"internal/version/version.go": "package version\n",
	}
	for relative, content := range files {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("internal/version/version.go", filepath.Join(root, "version-link.go")); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init", "--quiet", "--initial-branch=main")
	runGit(t, root, "add", "--all")
	runGit(t, root, "-c", "user.name=benchmark-test", "-c", "user.email=test@localhost", "commit", "--quiet", "-m", "fixture")
	return root
}

func runGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
