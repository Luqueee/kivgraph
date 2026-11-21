package pythonloader

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/testpath"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func writeProject(t *testing.T, files map[string]string) (string, workspace.Repository) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project")
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root, workspace.Repository{Name: "project", Path: root, RealPath: root}
}

// uses lists, for one file, the declarations it references as
// `qualified name (kind of edge)`.
func uses(payload facts.SemanticPayload, file string) []string {
	var out []string
	for _, reference := range payload.References {
		if reference.File != file {
			continue
		}
		qualified := strings.Split(reference.TargetID, "\x00")[1]
		out = append(out, qualified+" ("+reference.Kind+")")
	}
	sort.Strings(out)
	return out
}

// The layout most packages ship: the package under `src/`, the tests beside it
// importing it by its own name, and an `__init__` that re-exports what the
// modules define. The syntactic worker named the module `src.pkg.core`, so
// `import pkg` resolved to nothing and no test reached the package -- reading
// tests bought it nothing on real code.
func TestFallbackResolvesAPackageByTheNameItIsImportedAs(t *testing.T) {
	root, repository := writeProject(t, map[string]string{
		"src/pkg/__init__.py":     "from .core import Thing as Thing\nfrom .core import make\nfrom .sub import deep\n",
		"src/pkg/core.py":         "class Thing:\n    pass\n\n\ndef make():\n    return Thing()\n",
		"src/pkg/sub/__init__.py": "from .leaf import deep\n",
		"src/pkg/sub/leaf.py":     "def deep():\n    return 1\n",
		"tests/test_pkg.py": "import pkg\nfrom pkg import Thing\nfrom pkg.core import make as build\n\n\n" +
			"def test_all():\n    assert pkg.make() and Thing() and build() and pkg.deep()\n    return pkg.missing\n",
	})
	payload, err := RunWithOptions(context.Background(), Options{
		IndexerCommand: "missing-scippython", PythonPath: "python3", IncludeTests: true,
	}, repository, root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"src.pkg.core.Thing (CALLS_DIRECT)",
		"src.pkg.core.make (CALLS_DIRECT)",
		"src.pkg.core.make (CALLS_DIRECT)",
		"src.pkg.sub.leaf.deep (CALLS_DIRECT)",
	}
	got := uses(payload, "tests/test_pkg.py")
	// The module itself is referenced where it is named before a member.
	var members []string
	for _, use := range got {
		if !strings.HasPrefix(use, "src.pkg (") {
			members = append(members, use)
		}
	}
	if !reflect.DeepEqual(members, want) {
		t.Errorf("the test file references %q, want %q", members, want)
	}
	// A member the package does not have is still an occurrence nobody resolved.
	missing := false
	for _, row := range payload.Unresolved {
		missing = missing || row.File == "tests/test_pkg.py" && row.RequestedSymbol == "missing"
	}
	if !missing {
		t.Error("`pkg.missing` left no unresolved row")
	}
	// The stable identity of a declaration is still its path from the
	// repository: only how an import finds it changed.
	for _, symbol := range payload.Symbols {
		if symbol.File == "src/pkg/core.py" && symbol.Name == "make" && symbol.QualifiedName != "src.pkg.core.make" {
			t.Errorf("make is now named %q", symbol.QualifiedName)
		}
	}
	for _, imported := range payload.Imports {
		if strings.HasPrefix(imported.File, "src/pkg/") && imported.TargetID == "" {
			t.Errorf("the relative import of %q in %s did not resolve", imported.RequestedSymbol, imported.File)
		}
	}
}

// Two packages of the same name under two roots: nothing says which one
// `import pkg` means, and this worker does not choose.
func TestFallbackDoesNotChooseBetweenTwoPackagesOfOneName(t *testing.T) {
	root, repository := writeProject(t, map[string]string{
		"a/pkg/__init__.py": "def make():\n    return 1\n",
		"b/pkg/__init__.py": "def make():\n    return 2\n",
		"use.py":            "import pkg\n\n\ndef run():\n    return pkg.make()\n",
	})
	payload, err := Run(context.Background(), "missing-scippython", "python3", repository, root)
	if err != nil {
		t.Fatal(err)
	}
	if got := uses(payload, "use.py"); len(got) != 0 {
		t.Errorf("use.py references %q: one of two candidates was chosen", got)
	}
	named := false
	for _, row := range payload.Unresolved {
		named = named || row.File == "use.py" && row.RequestedSymbol == "make"
	}
	if !named {
		t.Error("the unresolved member left no row")
	}
}

// `exclusions` is a fact about the repository, and Python ignored it: the
// producers walk the tree themselves.
func TestExclusionsReachThePythonPayload(t *testing.T) {
	root, repository := writeProject(t, map[string]string{
		"app/main.py":         "from examples.demo import helper\nfrom app.kept import keep\n\n\ndef run():\n    return helper() + keep()\n",
		"app/kept.py":         "def keep():\n    return 1\n",
		"examples/demo.py":    "def helper():\n    return 1\n",
		"docs/deep/conf.py":   "project = 'x'\n",
		"app/examples_not.py": "value = 1\n",
	})
	open, err := Run(context.Background(), "missing-scippython", "python3", repository, root)
	if err != nil {
		t.Fatal(err)
	}
	if got := uses(open, "app/main.py"); !reflect.DeepEqual(got, []string{"app.kept.keep (CALLS_DIRECT)", "examples.demo.helper (CALLS_DIRECT)"}) {
		t.Fatalf("without exclusions main.py references %q", got)
	}

	repository.Exclusions = []string{"examples", "**/deep"}
	closed, err := Run(context.Background(), "missing-scippython", "python3", repository, root)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, file := range closed.Files {
		files = append(files, file.Path)
	}
	sort.Strings(files)
	if want := []string{"app/examples_not.py", "app/kept.py", "app/main.py"}; !reflect.DeepEqual(files, want) {
		t.Errorf("files = %q, want %q", files, want)
	}
	for _, symbol := range closed.Symbols {
		if strings.HasPrefix(symbol.File, "examples/") || strings.HasPrefix(symbol.File, "docs/") {
			t.Errorf("symbol %s of an excluded file is still in the payload", symbol.QualifiedName)
		}
	}
	if got := uses(closed, "app/main.py"); !reflect.DeepEqual(got, []string{"app.kept.keep (CALLS_DIRECT)"}) {
		t.Errorf("with exclusions main.py references %q, want only the declaration that stayed", got)
	}
	// The use of `helper` is still in the source. It is not dropped with its
	// target: it becomes the occurrence that bounds an answer about the name.
	bounded := false
	for _, row := range closed.Unresolved {
		if strings.HasPrefix(row.File, "examples/") || strings.HasPrefix(row.File, "docs/") {
			t.Errorf("an unresolved row of excluded file %s is still in the payload", row.File)
		}
		bounded = bounded || row.File == "app/main.py" && row.RequestedSymbol == "helper" &&
			row.Reason == "TARGET_NOT_INDEXED" && row.Detail == "examples/demo.py"
	}
	if !bounded {
		t.Errorf("the call to the excluded helper left no TARGET_NOT_INDEXED row: %+v", closed.Unresolved)
	}
	for _, imported := range closed.Imports {
		if imported.RequestedSymbol == "helper" && imported.TargetID != "" {
			t.Errorf("the import of the excluded helper still names a target: %+v", imported)
		}
	}
	if _, err := withoutExcluded(open, root, []string{"[unterminated"}); err == nil {
		t.Error("a pattern the matcher refuses was applied as if it matched nothing")
	}
}

// The workers decide what to read before any graph exists, so the rule for a
// test is written in them a second time. This holds both copies to
// internal/testpath over paths built from every piece the rule looks at.
func TestTheWorkersAndTheToolsAgreeOnWhatATestIs(t *testing.T) {
	directories := []string{"", "src/", "tests/", "test/", "pkg/tests/unit/", "pkg/Tests/", "__tests__/", "spec/",
		"specs/x/", "integration_test/", "integration/", "src/it/", "module/src/it/java/", "it/src/", "locales/it/",
		"src/item/", "testdata/", "contest/", "latest/"}
	names := []string{"model.py", "test_model.py", "model_test.py", "conftest.py", "tests.py", "test.py", "testing.py",
		"contest.py", "TEST_model.py", "widget.test.ts", "widget.spec.tsx", "a_test.go", "a_test.dart",
		"ServiceTest.java", "ServiceTests.cs", "Latest.java", "Contest.cs", "Test.java", "service_test.pyi", "noextension"}
	var paths []string
	for _, directory := range directories {
		for _, name := range names {
			paths = append(paths, directory+name)
		}
	}
	want := map[string]bool{}
	for _, path := range paths {
		want[path] = testpath.Is(path)
	}
	encoded, err := json.Marshal(paths)
	if err != nil {
		t.Fatal(err)
	}
	const script = `import importlib.util, json, sys
spec = importlib.util.spec_from_file_location("worker", sys.argv[1])
worker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(worker)
print(json.dumps({path: worker.is_test_path(path) for path in json.load(sys.stdin)}))
`
	for _, worker := range []string{"index.py", "pyright_index.py"} {
		process := exec.Command("python3", "-B", "-c", script, filepath.Join("..", "..", "python-worker", worker))
		process.Stdin = strings.NewReader(string(encoded))
		output, err := process.Output()
		if err != nil {
			t.Fatalf("%s: %v", worker, err)
		}
		got := map[string]bool{}
		if err := json.Unmarshal(output, &got); err != nil {
			t.Fatalf("%s: %v: %s", worker, err, output)
		}
		if len(got) != len(want) {
			t.Fatalf("%s answered for %d paths, want %d", worker, len(got), len(want))
		}
		for _, path := range paths {
			if got[path] != want[path] {
				t.Errorf("%s: is_test_path(%q) = %t, internal/testpath says %t", worker, path, got[path], want[path])
			}
		}
	}
	// The corpus has to hold both answers, or agreeing on it proves nothing.
	tests := 0
	for _, is := range want {
		if is {
			tests++
		}
	}
	if tests < 60 || len(paths)-tests < 60 {
		t.Fatalf("the corpus has %d tests in %d paths: too one-sided to compare on", tests, len(paths))
	}
}

// A payload comes from a producer, and an external one is under no obligation to
// list in `files` every file its other sections name. Each section is matched
// on its own, so a pattern the matcher refuses is refused wherever it is first
// needed, and nothing is filtered when there is nothing to filter by.
func TestWithoutExcludedMatchesEverySectionOnItsOwn(t *testing.T) {
	root := t.TempDir()
	sections := map[string]facts.SemanticPayload{
		"files":      {Files: []facts.SemanticFile{{Path: "a.py"}}},
		"symbols":    {Symbols: []facts.SemanticSymbol{{ID: "s", File: "a.py"}}},
		"unresolved": {Unresolved: []facts.SemanticUnresolved{{File: "a.py"}}},
		"references": {References: []facts.SemanticReference{{File: "a.py"}}},
		"imports":    {Imports: []facts.SemanticImport{{File: "a.py"}}},
	}
	for name, payload := range sections {
		if _, err := withoutExcluded(payload, root, []string{"[unterminated"}); err == nil {
			t.Errorf("%s: a pattern the matcher refuses was accepted", name)
		}
		kept, err := withoutExcluded(payload, root, nil)
		if err != nil || !reflect.DeepEqual(kept, payload) {
			t.Errorf("%s: with no exclusions the payload changed: %v", name, err)
		}
		emptied, err := withoutExcluded(payload, root, []string{"a.py"})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if left := len(emptied.Files) + len(emptied.Symbols) + len(emptied.Unresolved) + len(emptied.References) + len(emptied.Imports); left != 0 {
			t.Errorf("%s: %d rows of the excluded file are left", name, left)
		}
	}
}

// unresolvedNames lists, for one file, the names it used and nobody resolved.
func unresolvedNames(payload facts.SemanticPayload, file string) []string {
	var out []string
	for _, row := range payload.Unresolved {
		if row.File == file {
			out = append(out, row.RequestedSymbol)
		}
	}
	sort.Strings(out)
	return out
}

// A name one module binds to two declarations -- an import under a platform
// test and a `def` in the other branch. The worker attributed every use to the
// import, so the `def` was answered as used by nobody, and the answer said
// COMPLETE. Which of the two a use means is decided when the program runs.
func TestFallbackDoesNotChooseBetweenTwoDeclarationsOfOneName(t *testing.T) {
	root, repository := writeProject(t, map[string]string{
		"pkg/__init__.py": "from .compat import stream\nfrom .compat import plain\n",
		"pkg/win.py":      "def stream():\n    return 1\n\n\ndef other():\n    return 2\n",
		"pkg/posix.py":    "def other():\n    return 3\n",
		"pkg/compat.py": "import sys\n\nif sys.platform == 'win32':\n    from .win import stream\nelse:\n    def stream():\n        return 0\n\n" +
			"try:\n    from .win import other\nexcept ImportError:\n    from .posix import other\n\n" +
			// Not contested: the other binding is outside the repository, or
			// is the same declaration a second time.
			"try:\n    from os import getcwd\nexcept ImportError:\n    def getcwd():\n        return ''\n\n" +
			"from .win import stream as again\nfrom .win import stream as again\n\n\n" +
			"def plain():\n    return 4\n\n\n" +
			"def run(value: 'stream'):\n    return stream(), other(), getcwd(), again(), plain()\n",
		// The same doubt reaches whoever imports the name, directly or through
		// the package that re-exports it.
		"pkg/direct.py": "from .compat import stream, plain\n\n\ndef run():\n    return stream(), plain()\n",
		"use.py":        "from pkg import stream, plain\n\n\ndef run():\n    return stream(), plain()\n",
	})
	payload, err := Run(context.Background(), "missing-scippython", "python3", repository, root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct{ uses, unresolved []string }{
		"pkg/compat.py": {
			[]string{"pkg.compat.getcwd (CALLS_DIRECT)", "pkg.compat.plain (CALLS_DIRECT)", "pkg.win.stream (CALLS_DIRECT)"},
			[]string{"ImportError", "ImportError", "other", "platform", "stream", "stream", "sys"},
		},
		"pkg/direct.py": {[]string{"pkg.compat.plain (CALLS_DIRECT)"}, []string{"stream"}},
		"use.py":        {[]string{"pkg.compat.plain (CALLS_DIRECT)"}, []string{"stream"}},
	}
	for file, expected := range want {
		if got := uses(payload, file); !reflect.DeepEqual(got, expected.uses) {
			t.Errorf("%s references %q, want %q", file, got, expected.uses)
		}
		if got := unresolvedNames(payload, file); !reflect.DeepEqual(got, expected.unresolved) {
			t.Errorf("%s left %q unresolved, want %q", file, got, expected.unresolved)
		}
	}
}
