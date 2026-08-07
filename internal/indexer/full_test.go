package indexer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/workspace"
)

func TestFullBuildsGoFactsOutsideTheRepository(t *testing.T) {
	root := t.TempDir()
	writeFullFixture(t, filepath.Join(root, "go.mod"), "module example.com/fullfixture\n\ngo 1.24\n")
	writeFullFixture(t, filepath.Join(root, "fixture.go"), `package fixture

func Greeting() string { return "hello" }
`)
	workFile := filepath.Join(t.TempDir(), "go.work")

	set, report, err := Full(context.Background(), FullOptions{
		Repositories: []workspace.Repository{{
			Name:      "fixture",
			Path:      root,
			RealPath:  root,
			Languages: []string{"go"},
		}},
		SyntheticWorkFile: workFile,
	})
	if err != nil {
		t.Fatalf("Full() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("full facts validation error = %v", err)
	}
	if report.GoRepositories != 1 || report.GoModules != 1 || report.GoDefinitions == 0 {
		t.Fatalf("full report = %+v, want one repository/module and definitions", report)
	}
	if _, err := os.Stat(workFile); err != nil {
		t.Fatalf("synthetic work file %q: %v", workFile, err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.work")); !os.IsNotExist(err) {
		t.Fatalf("repository go.work error = %v, want absent", err)
	}
}

func TestFullRejectsGoIndexWithoutSyntheticWorkFile(t *testing.T) {
	root := t.TempDir()
	set, report, err := Full(context.Background(), FullOptions{
		Repositories: []workspace.Repository{{
			Name:      "fixture",
			Path:      root,
			RealPath:  root,
			Languages: []string{"go"},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "synthetic Go work file is required") {
		t.Fatalf("Full() error = %v, want missing synthetic work file", err)
	}
	if len(set.Repositories) != 0 || report.GoRepositories != 1 {
		t.Fatalf("partial full result = set=%+v report=%+v", set, report)
	}
}

func TestFullRejectsUnsupportedLanguageBeforeIndexing(t *testing.T) {
	_, report, err := Full(context.Background(), FullOptions{
		Repositories: []workspace.Repository{{
			Name:      "fixture",
			Path:      "/does/not/matter",
			RealPath:  "/does/not/matter",
			Languages: []string{"rust"},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported language "rust"`) {
		t.Fatalf("Full() error = %v, want unsupported language", err)
	}
	if report.GoRepositories != 0 || report.TypeScriptRepositories != 0 {
		t.Fatalf("report after unsupported language = %+v, want no work", report)
	}
}

func writeFullFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture %q: %v", path, err)
	}
}
