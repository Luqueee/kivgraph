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

// TypeScript was the last unit that ended the pass: one package the worker
// could not read, of one repository, and no repository got a graph.
func TestFullIndexesTheRestWhenATypeScriptPackageCannotBeRead(t *testing.T) {
	failing := failingAnalyzer(t)
	root := testsupport.TempDir(t)
	healthy := filepath.Join(root, "healthy")
	writeFullFixture(t, filepath.Join(healthy, "go.mod"), "module example.com/healthy\n\ngo 1.24\n")
	writeFullFixture(t, filepath.Join(healthy, "a.go"), "package healthy\n\nfunc Healthy() int { return 1 }\n")
	web := filepath.Join(root, "web")
	for _, name := range []string{"one", "two"} {
		packageRoot := filepath.Join(web, "packages", name)
		writeFullFixture(t, filepath.Join(packageRoot, "package.json"), `{"name":"@fixture/`+name+`","version":"1.0.0"}`)
		writeFullFixture(t, filepath.Join(packageRoot, "tsconfig.json"), `{"compilerOptions":{"strict":true}}`)
		writeFullFixture(t, filepath.Join(packageRoot, "src", "index.ts"), "export const value = 1;\n")
	}
	repositories := []workspace.Repository{
		{Name: "healthy", Path: healthy, RealPath: healthy, Languages: []string{"go"}},
		{Name: "web", Path: web, RealPath: web, Languages: []string{"typescript"}},
	}

	set, report, err := Full(context.Background(), FullOptions{
		Repositories: repositories,
		// Installed, started, and exits non-zero on every package.
		TypeScriptWorker:  failing,
		WorkingDirectory:  root,
		SyntheticWorkFile: filepath.Join(testsupport.TempDir(t), "go.work"),
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
		t.Fatal("a TypeScript package that could not be read took an unrelated Go repository down with it")
	}
	if report.TypeScriptNotLoaded != 2 || len(report.TypeScriptNotRead) != 2 {
		t.Fatalf("not loaded = %d, lines = %q, want both packages", report.TypeScriptNotLoaded, report.TypeScriptNotRead)
	}
	scopes := map[string]facts.UnresolvedReference{}
	for _, row := range set.Unresolved {
		if row.Reason == UnresolvedPackageNotLoaded {
			scopes[row.RequestedPackage] = row
		}
	}
	lines := strings.Join(report.TypeScriptNotRead, "\n")
	for _, name := range []string{"@fixture/one", "@fixture/two"} {
		row, found := scopes[name]
		if !found {
			t.Errorf("%s: no %s row in the graph", name, UnresolvedPackageNotLoaded)
			continue
		}
		if row.Language != facts.LanguageTypeScript || row.RepositoryKey != facts.RepositoryKey("web") || row.FileKey != "" {
			t.Errorf("%s: row = %+v, want the web repository, TypeScript and no file", name, row)
		}
		if strings.TrimSpace(row.Detail) == "" || strings.Contains(row.Detail, "\n") {
			t.Errorf("%s: detail = %q, want one line saying why", name, row.Detail)
		}
		if want := "web: " + name + ": " + UnresolvedPackageNotLoaded + ": "; !strings.Contains(lines, want) {
			t.Errorf("the report has no line starting %q:\n%s", want, lines)
		}
	}

	// The two things that are not a package that could not be read: a worker
	// that is not there, and a command that cannot be prepared. The worker
	// ships with Kivgraph, so both are a broken installation, and counting every
	// package as unread and exiting 0 would hide it.
	for name, worker := range map[string]string{"absent": "kivgraph-no-such-typescript-worker", "empty": "   "} {
		if _, _, err := Full(context.Background(), FullOptions{
			Repositories:      repositories,
			TypeScriptWorker:  worker,
			WorkingDirectory:  root,
			SyntheticWorkFile: filepath.Join(testsupport.TempDir(t), "go.work"),
		}); err == nil {
			t.Errorf("%s worker: the pass succeeded with packages counted as unread", name)
		}
	}
}

func TestACancelledPassIsNotATypeScriptPackageThatCouldNotBeRead(t *testing.T) {
	root := testsupport.TempDir(t)
	writeFullFixture(t, filepath.Join(root, "package.json"), `{"name":"@fixture/one","version":"1.0.0"}`)
	writeFullFixture(t, filepath.Join(root, "tsconfig.json"), `{"compilerOptions":{"strict":true}}`)
	writeFullFixture(t, filepath.Join(root, "src", "index.ts"), "export const value = 1;\n")
	repository := workspace.Repository{Name: "web", Path: root, RealPath: root, Languages: []string{"typescript"}}
	discovered, err := discoverTypeScriptPackages(context.Background(), []workspace.Repository{repository}, false)
	if err != nil || len(discovered.packages) != 1 {
		t.Fatalf("discover: %d packages, %v", len(discovered.packages), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := indexTypeScriptPackage(ctx, FullOptions{TypeScriptWorker: failingAnalyzer(t), WorkingDirectory: root},
		analysisUnit{kind: unitTypeScript, repository: repository, pkg: discovered.packages[0]}, nil)
	if err == nil {
		t.Fatalf("indexTypeScriptPackage() on a cancelled pass = %+v, want the cancellation", result)
	}
	if result.notLoaded || len(result.set.Unresolved) != 0 {
		t.Errorf("a cancelled pass published %+v", result)
	}
}

// A tsconfig that does not parse is found before any worker runs, when the
// projects are discovered, and it ended the pass from there. So did a pass
// whose only TypeScript repository has no project at all -- which is what
// `init --languages go,typescript` leaves a Go repository as.
func TestFullIndexesTheRestWhenATypeScriptRepositoryCannotBeDiscovered(t *testing.T) {
	root := testsupport.TempDir(t)
	healthy := filepath.Join(root, "healthy")
	writeFullFixture(t, filepath.Join(healthy, "go.mod"), "module example.com/healthy\n\ngo 1.24\n")
	writeFullFixture(t, filepath.Join(healthy, "a.go"), "package healthy\n\nfunc Healthy() int { return 1 }\n")
	broken := filepath.Join(root, "broken")
	writeFullFixture(t, filepath.Join(broken, "package.json"), `{"name":"@fixture/broken","version":"1.0.0"}`)
	writeFullFixture(t, filepath.Join(broken, "tsconfig.json"), `{"compilerOptions": {"strict": true,, }`)
	writeFullFixture(t, filepath.Join(broken, "src", "index.ts"), "export const value = 1;\n")

	healthySymbols := func(set facts.Set) int {
		total := 0
		for _, symbol := range set.Symbols {
			if symbol.Name == "Healthy" {
				total++
			}
		}
		return total
	}

	set, report, err := Full(context.Background(), FullOptions{
		Repositories: []workspace.Repository{
			{Name: "healthy", Path: healthy, RealPath: healthy, Languages: []string{"go"}},
			{Name: "broken", Path: broken, RealPath: broken, Languages: []string{"typescript"}},
		},
		// Never started: there is no package left to run it on.
		TypeScriptWorker:  "kivgraph-no-such-typescript-worker",
		WorkingDirectory:  root,
		SyntheticWorkFile: filepath.Join(testsupport.TempDir(t), "go.work"),
	})
	if err != nil {
		t.Fatalf("Full() error = %v, want the Go repository indexed anyway", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("the published facts do not validate: %v", err)
	}
	if healthySymbols(set) != 1 {
		t.Fatal("a tsconfig that does not parse took an unrelated Go repository down with it")
	}
	if report.TypeScriptNotLoaded != 1 || len(report.TypeScriptNotRead) != 1 ||
		!strings.HasPrefix(report.TypeScriptNotRead[0], "broken: "+UnresolvedRepositoryNotLoaded+": ") ||
		!strings.Contains(report.TypeScriptNotRead[0], "tsconfig.json") {
		t.Errorf("not loaded = %d, lines = %q, want the broken repository named with its tsconfig",
			report.TypeScriptNotLoaded, report.TypeScriptNotRead)
	}
	declared := 0
	for _, row := range set.Unresolved {
		if row.Reason != UnresolvedRepositoryNotLoaded {
			continue
		}
		declared++
		if row.Language != facts.LanguageTypeScript || row.RepositoryKey != facts.RepositoryKey("broken") || row.FileKey != "" {
			t.Errorf("row = %+v, want the broken repository, TypeScript and no file", row)
		}
	}
	if declared != 1 {
		t.Errorf("%d %s rows, want the one repository", declared, UnresolvedRepositoryNotLoaded)
	}
	// A repository that could not be discovered is not one that has no project.
	if len(report.TypeScriptWithoutPackages) != 0 {
		t.Errorf("without packages = %q, want the broken repository reported once, as not read", report.TypeScriptWithoutPackages)
	}

	// The other shape: registered as TypeScript, and no project in it.
	set, report, err = Full(context.Background(), FullOptions{
		Repositories: []workspace.Repository{
			{Name: "healthy", Path: healthy, RealPath: healthy, Languages: []string{"go", "typescript"}},
		},
		TypeScriptWorker:  "kivgraph-no-such-typescript-worker",
		WorkingDirectory:  root,
		SyntheticWorkFile: filepath.Join(testsupport.TempDir(t), "go.work"),
	})
	if err != nil {
		t.Fatalf("Full() over a Go repository also registered as TypeScript = %v", err)
	}
	if healthySymbols(set) != 1 {
		t.Fatal("the Go half of a repository with no TypeScript project was not indexed")
	}
	if len(report.TypeScriptWithoutPackages) != 1 || report.TypeScriptWithoutPackages[0] != "healthy" || report.TypeScriptNotLoaded != 0 {
		t.Errorf("without packages = %q, not loaded = %d, want it named as contributing nothing",
			report.TypeScriptWithoutPackages, report.TypeScriptNotLoaded)
	}
}

// A scope row has to name something. A package with no name is named by the
// project it was going to be built from.
func TestAPackageWithNoNameIsDeclaredByItsProject(t *testing.T) {
	unit := analysisUnit{kind: unitTypeScript, repository: workspace.Repository{Name: "web", RealPath: "/web"}}
	unit.pkg.packageValue.ProjectPath = "/web/tsconfig.json"
	result := packageNotLoadedFacts(unit, errorString("worker exited"))
	row := result.set.Unresolved[0]
	if row.RequestedPackage != "/web/tsconfig.json" || row.Reason != UnresolvedPackageNotLoaded || row.Detail != "worker exited" {
		t.Errorf("row = %+v, want the project path as the scope", row)
	}
	if err := result.set.Validate(); err != nil {
		t.Errorf("the declared facts do not validate: %v", err)
	}
	if !result.notLoaded || result.unresolved != 1 || !strings.HasPrefix(result.notRead, "web: /web/tsconfig.json: ") {
		t.Errorf("result = %+v", result)
	}
}
