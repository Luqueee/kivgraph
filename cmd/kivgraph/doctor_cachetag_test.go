package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func tagDirectory(t *testing.T, directory string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "CACHEDIR.TAG"), []byte(testsupport.CacheTagSignature+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

type cacheTagReport struct {
	name   string
	passed bool
	detail string
}

func cacheTagReports(t *testing.T, repositories ...workspace.Repository) []cacheTagReport {
	t.Helper()
	var reports []cacheTagReport
	reportDoctorCacheDirectories(t.Context(), func(name string, passed bool, detail string) {
		reports = append(reports, cacheTagReport{name, passed, detail})
	}, repositories)
	return reports
}

func repositoryAt(root, name string) workspace.Repository {
	return workspace.Repository{Name: name, Path: root, RealPath: root}
}

// A skip that decides what the graph holds must be named, with the repository it
// is in and a path that can be found.
func TestDoctorNamesTheDirectoriesSkippedForACacheTag(t *testing.T) {
	root := testsupport.TempDir(t)
	tagDirectory(t, filepath.Join(root, "target"))
	tagDirectory(t, filepath.Join(root, "crates", "inner", "target"))

	reports := cacheTagReports(t, repositoryAt(root, "rusty"))
	if len(reports) != 1 || reports[0].name != "walk.cache_tag" || !reports[0].passed {
		t.Fatalf("reports = %+v, want one passing walk.cache_tag", reports)
	}
	for _, want := range []string{"rusty: crates/inner/target, target", "never indexed"} {
		if !strings.Contains(reports[0].detail, want) {
			t.Errorf("detail = %q, want it to contain %q", reports[0].detail, want)
		}
	}
}

// Every repository with a tagged directory is named, and one without is not.
func TestDoctorNamesEachRepositoryThatHasATaggedDirectory(t *testing.T) {
	first, second, plain := testsupport.TempDir(t), testsupport.TempDir(t), testsupport.TempDir(t)
	tagDirectory(t, filepath.Join(first, "target"))
	tagDirectory(t, filepath.Join(second, "build-cache"))

	reports := cacheTagReports(t, repositoryAt(first, "first"), repositoryAt(plain, "plain"), repositoryAt(second, "second"))
	if len(reports) != 1 {
		t.Fatalf("reports = %+v, want one line", reports)
	}
	detail := reports[0].detail
	if !strings.Contains(detail, "first: target; second: build-cache") || strings.Contains(detail, "plain") {
		t.Fatalf("detail = %q", detail)
	}
}

// Finding nothing is said, so that silence is never the report.
func TestDoctorSaysWhenNoDirectoryCarriesACacheTag(t *testing.T) {
	for name, repositories := range map[string][]workspace.Repository{
		"a repository with none": {repositoryAt(testsupport.TempDir(t), "plain")},
		"no repository":          nil,
	} {
		reports := cacheTagReports(t, repositories...)
		if len(reports) != 1 || !reports[0].passed || reports[0].detail != "no directory carries a cache tag" {
			t.Errorf("%s: reports = %+v", name, reports)
		}
	}
}

// A monorepo with dozens of targets must not print a line nobody reads, and must
// not hide that there were more.
func TestDoctorBoundsTheListOfSkippedDirectories(t *testing.T) {
	root := testsupport.TempDir(t)
	total := doctorCacheDirectoriesShown + 2
	for index := range total {
		tagDirectory(t, filepath.Join(root, fmt.Sprintf("crate%d", index), "target"))
	}
	reports := cacheTagReports(t, repositoryAt(root, "wide"))
	detail := reports[0].detail
	if !strings.Contains(detail, "crate0/target") || !strings.Contains(detail, " and 2 more") {
		t.Fatalf("detail = %q, want the first directories and the count of the rest", detail)
	}
	if strings.Contains(detail, fmt.Sprintf("crate%d/target", total-1)) {
		t.Fatalf("detail = %q lists past the bound", detail)
	}
}

// A tree that cannot be walked is a failure and not "no tagged directory": that
// would be a claim about a tree nobody read. The other repositories are still
// reported.
func TestDoctorFailsForARepositoryItCannotWalkAndKeepsGoing(t *testing.T) {
	good := testsupport.TempDir(t)
	tagDirectory(t, filepath.Join(good, "target"))
	absent := filepath.Join(testsupport.TempDir(t), "absent")

	reports := cacheTagReports(t, repositoryAt(absent, "gone"), repositoryAt(good, "good"))
	if len(reports) != 2 {
		t.Fatalf("reports = %+v, want a failure and a summary", reports)
	}
	if reports[0].passed || !strings.HasPrefix(reports[0].detail, "gone: ") {
		t.Errorf("first report = %+v, want a failure naming the repository", reports[0])
	}
	if !reports[1].passed || !strings.Contains(reports[1].detail, "good: target") {
		t.Errorf("second report = %+v, want the readable repository listed", reports[1])
	}
}

// doctor runs the report. An empty registry has no toolchain to probe, so this
// exercises the wiring without spawning the processes a registered Go
// repository would.
func TestDoctorPrintsTheCacheTagLine(t *testing.T) {
	configPath := auditWorkspace(t)
	testsupport.SetHome(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := runDoctor([]string{"--config", configPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("doctor exit code = %d\n%s%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "walk.cache_tag: PASS (no directory carries a cache tag)") {
		t.Fatalf("doctor output = %q", stdout.String())
	}
}

// A repository registered but not yet resolved is listed relative to the path it
// was registered with.
func TestDoctorListsRelativeToTheRegisteredPathWhenNothingWasResolved(t *testing.T) {
	root := testsupport.TempDir(t)
	tagDirectory(t, filepath.Join(root, "target"))
	reports := cacheTagReports(t, workspace.Repository{Name: "bare", Path: root})
	if len(reports) != 1 || !strings.Contains(reports[0].detail, "bare: target") {
		t.Fatalf("reports = %+v", reports)
	}
}

// The other reason a walk skips a directory, on its own line.
func TestDoctorNamesTheCheckoutsEveryWalkSkips(t *testing.T) {
	root := testsupport.TempDir(t)
	for name, body := range map[string]string{
		".worktrees/feature/.git": "gitdir: /elsewhere/.git/worktrees/feature\n",
		"sub/.git":                "gitdir: ../.git/modules/sub\n",
	} {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var lines []string
	report := func(name string, passed bool, detail string) {
		lines = append(lines, fmt.Sprintf("%s %t %s", name, passed, detail))
	}
	repository := workspace.Repository{Name: "app", Path: root, RealPath: root}
	reportDoctorOtherCheckouts(context.Background(), report, []workspace.Repository{repository})
	reportDoctorOtherCheckouts(context.Background(), report, nil)
	want := []string{
		"walk.checkout true skipped by every walk and never indexed: app: .worktrees/feature (register a checkout as its own repository to index it)",
		"walk.checkout true no directory is another Git checkout",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %q, want %q", lines, want)
	}
}
