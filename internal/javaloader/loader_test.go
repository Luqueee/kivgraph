package javaloader

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

const fixture = "../../testdata/java/basic"

func TestRunReportsAMissingIndexerAsNotFound(t *testing.T) {
	// The pass reads exec.ErrNotFound to isolate the repository instead of
	// failing every other one. A wrapped error that loses it would turn a
	// laptop without a JDK into a failed index of five repositories, four of
	// which have nothing to do with Java.
	_, err := Run(t.Context(), Options{
		Command:         "kivgraph-java-indexer-that-does-not-exist",
		TargetDirectory: testsupport.TempDir(t),
		Repository:      repository(t),
	})
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("err = %v, want it to wrap exec.ErrNotFound", err)
	}
}

func TestRunRefusesToWriteInsideTheRepository(t *testing.T) {
	// An indexer whose output lands in the repository makes the pass modify
	// what it came to read, and the next pass indexes its own output.
	root := repository(t)
	_, err := Run(t.Context(), Options{
		Command:         "scip-java",
		TargetDirectory: filepath.Join(root.RealPath, "target"),
		Repository:      root,
	})
	if err == nil || !strings.Contains(err.Error(), "inside the indexed repository") {
		t.Fatalf("err = %v, want a refusal to write inside the repository", err)
	}
}

func TestRunRequiresATargetDirectory(t *testing.T) {
	_, err := Run(t.Context(), Options{Command: "scip-java", Repository: repository(t)})
	if err == nil || !strings.Contains(err.Error(), "target directory") {
		t.Fatalf("err = %v, want a refusal without a target directory", err)
	}
}

func TestIncludeFileExcludesTestsAndGeneratedSources(t *testing.T) {
	closed := Options{}
	open := Options{IncludeTests: true, IncludeGenerated: true}
	for _, testCase := range []struct {
		path            string
		defaultIncluded bool
	}{
		{"src/main/java/com/example/Service.java", true},
		{"src/test/java/com/example/ServiceTest.java", false},
		{"src/it/java/com/example/ServiceIT.java", false},
		{"target/generated-sources/annotations/com/example/Gen.java", false},
		{"build/generated/source/com/example/Gen.java", false},
		{"src/main/resources/application.yaml", false},
	} {
		if got := includeFile(testCase.path, closed); got != testCase.defaultIncluded {
			t.Errorf("includeFile(%q) = %t, want %t", testCase.path, got, testCase.defaultIncluded)
		}
	}
	// Opening the two switches must reach the Java files and nothing else: a
	// resource is not a source at any setting.
	if !includeFile("src/test/java/com/example/ServiceTest.java", open) {
		t.Error("include_tests does not reach a test source")
	}
	if includeFile("src/main/resources/application.yaml", open) {
		t.Error("a resource was included as a Java source")
	}
}

// TestRunAgainstTheFixture is the end-to-end: it drives the real scip-java over
// a copy of the fixture and checks the payload it produces.
//
// It copies rather than indexing in place, and that is not tidiness: scip-java
// runs the project's own build, and Maven writes `target/` into the directory
// it builds. Indexing the fixture where it lives would leave the repository
// dirty, which the project forbids.
func TestRunAgainstTheFixture(t *testing.T) {
	if _, err := exec.LookPath("scip-java"); err != nil {
		t.Skip("scip-java is not installed")
	}
	if _, err := exec.LookPath("mvn"); err != nil {
		t.Skip("maven is not installed")
	}

	work := testsupport.TempDir(t)
	source := filepath.Join(work, "basic")
	copyTree(t, fixture, source)

	payload, err := Run(t.Context(), Options{
		Command:          "scip-java",
		TargetDirectory:  filepath.Join(work, "target"),
		Repository:       workspace.Repository{Name: "basic", Path: source, RealPath: source},
		MaximumIndexTime: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if payload.Language != facts.LanguageJava {
		t.Fatalf("language = %q", payload.Language)
	}
	if !payload.Authoritative {
		t.Fatal("a scip-java payload is not authoritative, so every edge would be a candidate")
	}
	if len(payload.Symbols) == 0 || len(payload.References) == 0 {
		t.Fatalf("payload is empty: symbols=%d references=%d",
			len(payload.Symbols), len(payload.References))
	}

	set, err := facts.NormalizeSemantic(t.Context(),
		workspace.Repository{Name: "basic", Path: source, RealPath: source}, payload)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	// The fixture must survive its own indexing. scip-java writes `target/`
	// through Maven, which is why the loader points --targetroot outside; this
	// is the assertion that the original tree is untouched either way.
	if entries, err := os.ReadDir(fixture); err == nil {
		for _, entry := range entries {
			switch entry.Name() {
			case "target", "build", ".gradle", "index.scip":
				t.Errorf("indexing left %q in the fixture", entry.Name())
			}
		}
	}
}

// TestRecordedIndexMatchesTheToolchain keeps the checked-in index honest. The
// hermetic tests of internal/scip read a recorded file; if the fixture changes
// and nobody re-records it, they would keep asserting about code that is gone.
func TestRecordedIndexMatchesTheToolchain(t *testing.T) {
	if _, err := exec.LookPath("scip-java"); err != nil {
		t.Skip("scip-java is not installed")
	}
	if _, err := exec.LookPath("mvn"); err != nil {
		t.Skip("maven is not installed")
	}
	work := testsupport.TempDir(t)
	source := filepath.Join(work, "basic")
	copyTree(t, fixture, source)

	payload, err := Run(t.Context(), Options{
		Command:          "scip-java",
		TargetDirectory:  filepath.Join(work, "target"),
		Repository:       workspace.Repository{Name: "basic", Path: source, RealPath: source},
		MaximumIndexTime: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	recorded := recordedPayload(t)
	if len(payload.Symbols) != len(recorded.Symbols) {
		t.Errorf("the toolchain produced %d symbols and the recorded index has %d: re-record testdata/java/index/basic.scip",
			len(payload.Symbols), len(recorded.Symbols))
	}
	live := map[string]string{}
	for _, symbol := range payload.Symbols {
		live[symbol.QualifiedName] = symbol.Kind
	}
	for _, symbol := range recorded.Symbols {
		kind, present := live[symbol.QualifiedName]
		if !present {
			t.Errorf("the recorded index has %q and the toolchain does not", symbol.QualifiedName)
			continue
		}
		if kind != symbol.Kind {
			t.Errorf("%q is %q live and %q recorded", symbol.QualifiedName, kind, symbol.Kind)
		}
	}
}

func repository(t *testing.T) workspace.Repository {
	t.Helper()
	absolute, err := filepath.Abs(fixture)
	if err != nil {
		t.Fatal(err)
	}
	return workspace.Repository{Name: "basic", Path: absolute, RealPath: absolute}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	err := filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}
