package indexer

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// One repository registered with a language it has no project for used to end
// the pass: a C# repository with no solution, a Java build that fails, a Python
// producer that exits. Every other repository lost its graph to it, and none of
// them had changed.
//
// The analyzer here is the `go` command, which exists wherever this suite runs
// and exits non-zero when it is given an indexer's arguments. That is the case
// that matters: the tool is installed, it ran, and it could not read the
// repository. (Not this test binary: given arguments it does not know, it runs
// the whole suite, this test included.)
func TestFullIndexesTheRestWhenOneRepositoryCannotBeRead(t *testing.T) {
	failing := failingAnalyzer(t)
	root := testsupport.TempDir(t)
	healthy := filepath.Join(root, "healthy")
	writeFullFixture(t, filepath.Join(healthy, "go.mod"), "module example.com/healthy\n\ngo 1.24\n")
	writeFullFixture(t, filepath.Join(healthy, "a.go"), "package healthy\n\nfunc Healthy() int { return 1 }\n")
	// Sources, and nothing to build them from.
	dotnet := filepath.Join(root, "dotnet")
	writeFullFixture(t, filepath.Join(dotnet, "Program.cs"), "class Program {}\n")
	java := filepath.Join(root, "java")
	writeFullFixture(t, filepath.Join(java, "pom.xml"), "<project/>\n")
	writeFullFixture(t, filepath.Join(java, "src", "main", "java", "App.java"), "class App {}\n")
	python := filepath.Join(root, "python")
	writeFullFixture(t, filepath.Join(python, "app.py"), "def run():\n    return 1\n")

	set, report, err := Full(context.Background(), FullOptions{
		Repositories: []workspace.Repository{
			{Name: "healthy", Path: healthy, RealPath: healthy, Languages: []string{"go"}},
			{Name: "dotnet", Path: dotnet, RealPath: dotnet, Languages: []string{"csharp"}},
			{Name: "java", Path: java, RealPath: java, Languages: []string{"java"}},
			{Name: "python", Path: python, RealPath: python, Languages: []string{"python"}},
		},
		CSharpIndexerCommand:  failing,
		CSharpTargetDirectory: filepath.Join(testsupport.TempDir(t), "csharp-target"),
		JavaIndexerCommand:    failing,
		JavaTargetDirectory:   filepath.Join(testsupport.TempDir(t), "java-target"),
		PythonIndexer:         failing,
		PythonPath:            "python3",
		WorkingDirectory:      root,
		SyntheticWorkFile:     filepath.Join(testsupport.TempDir(t), "go.work"),
	})
	if err != nil {
		t.Fatalf("Full() error = %v, want the Go repository indexed anyway", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("the published facts do not validate: %v", err)
	}
	healthySymbols := 0
	for _, symbol := range set.Symbols {
		if symbol.Name == "Healthy" {
			healthySymbols++
		}
	}
	if healthySymbols != 1 {
		t.Fatal("a repository that could not be read took an unrelated Go repository down with it")
	}
	if report.CSharpRepositoriesNotLoaded != 1 || report.JavaRepositoriesNotLoaded != 1 || report.PythonRepositoriesNotLoaded != 1 {
		t.Errorf("not loaded: csharp=%d java=%d python=%d, want one each",
			report.CSharpRepositoriesNotLoaded, report.JavaRepositoriesNotLoaded, report.PythonRepositoriesNotLoaded)
	}

	// Counted is not enough: each one is in the graph as a scope nobody could
	// read, which is what bounds an answer about that repository, and in the
	// report as a line somebody can act on.
	scopes := map[string]facts.UnresolvedReference{}
	for _, row := range set.Unresolved {
		if row.Reason == UnresolvedRepositoryNotLoaded {
			scopes[row.RequestedPackage] = row
		}
	}
	lines := strings.Join(report.SemanticNotRead, "\n")
	for repository, language := range map[string]facts.Language{
		"dotnet": facts.LanguageCSharp, "java": facts.LanguageJava, "python": facts.LanguagePython,
	} {
		row, found := scopes[repository]
		if !found {
			t.Errorf("%s: no %s row in the graph", repository, UnresolvedRepositoryNotLoaded)
			continue
		}
		if row.Language != language || row.RepositoryKey != facts.RepositoryKey(repository) {
			t.Errorf("%s: row = %+v, want its own repository and language", repository, row)
		}
		// Nothing was read, so nothing is claimed about a file.
		if row.FileKey != "" {
			t.Errorf("%s: a failure to read the repository names a file: %+v", repository, row)
		}
		if strings.TrimSpace(row.Detail) == "" || len(row.Detail) > maximumNotLoadedDetail+8 || strings.Contains(row.Detail, "\n") {
			t.Errorf("%s: detail = %q, want one bounded line saying why", repository, row.Detail)
		}
		// The reader has the repository, not the scratch copy an indexer built in.
		if repository == "dotnet" && !strings.Contains(row.Detail, dotnet) {
			t.Errorf("dotnet: detail = %q, want it to name the repository %s", row.Detail, dotnet)
		}
		if want := string(language) + ": " + repository + ": " + UnresolvedRepositoryNotLoaded + ": "; !strings.Contains(lines, want) {
			t.Errorf("the report has no line starting %q:\n%s", want, lines)
		}
	}
	if len(scopes) != 3 || len(report.SemanticNotRead) != 3 {
		t.Errorf("%d scopes and %d report lines, want three of each", len(scopes), len(report.SemanticNotRead))
	}
	// The repository that failed is still a repository of the graph.
	known := map[string]bool{}
	for _, repository := range set.Repositories {
		known[repository.Name] = true
	}
	for _, name := range []string{"healthy", "dotnet", "java", "python"} {
		if !known[name] {
			t.Errorf("repository %q is not in the published set", name)
		}
	}
}

// A pass that was cancelled did not learn anything about the repository it was
// reading, and must not publish that it could not be read.
func TestACancelledPassIsNotARepositoryThatCouldNotBeRead(t *testing.T) {
	root := testsupport.TempDir(t)
	writeFullFixture(t, filepath.Join(root, "app.py"), "def run():\n    return 1\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := indexSemantic(ctx, FullOptions{PythonIndexer: failingAnalyzer(t), PythonPath: "python3", WorkingDirectory: root},
		analysisUnit{kind: unitSemantic, language: facts.LanguagePython,
			repository: workspace.Repository{Name: "python", Path: root, RealPath: root}})
	if err == nil {
		t.Fatalf("indexSemantic() on a cancelled pass = %+v, want the cancellation", result)
	}
	if result.notLoaded || len(result.set.Unresolved) != 0 {
		t.Errorf("a cancelled pass published %+v", result)
	}
}

func TestRepositoryNotLoadedBoundsTheDetail(t *testing.T) {
	long := strings.Repeat("build log line\n", 200)
	result := repositoryNotLoadedFacts(
		analysisUnit{language: facts.LanguageJava, repository: workspace.Repository{Name: "java", RealPath: "/repo"}},
		errorString(long))
	detail := result.set.Unresolved[0].Detail
	if len(detail) != maximumNotLoadedDetail+len(" ...") || strings.Contains(detail, "\n") || !strings.HasSuffix(detail, " ...") {
		t.Errorf("detail is %d bytes ending %q, want %d on one line with the cut marked",
			len(detail), detail[len(detail)-12:], maximumNotLoadedDetail+4)
	}
	short := repositoryNotLoadedFacts(
		analysisUnit{language: facts.LanguageJava, repository: workspace.Repository{Name: "java"}}, errorString("no  index\nwritten"))
	if got := short.set.Unresolved[0].Detail; got != "no index written" {
		t.Errorf("detail = %q, want the message on one line and whole", got)
	}
}

func failingAnalyzer(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go command to stand in for an analyzer that fails: %v", err)
	}
	if strings.ContainsAny(path, " \t") {
		t.Skipf("the go command is at %q, which an indexer command cannot spell", path)
	}
	return path
}

type errorString string

func (e errorString) Error() string { return string(e) }
