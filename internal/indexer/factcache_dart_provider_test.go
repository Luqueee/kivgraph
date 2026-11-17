package indexer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// The facts of a Dart consumer name symbols of the package it depends on: the loader
// is handed every repository as a provider and reads the file a reference lands in.
// When the provider renames a symbol the consumer's entry is wrong though none of
// its own sources changed, and something has to say so: the digest of the provider's
// tree in the semantic registry when the provider declares a semantic language, and
// the trees a unit takes from its package config when it does not. Without them the
// entry was served: `references=8` and the old symbol where the analysis gives `7`
// and the new one.
type dartLab struct {
	t          *testing.T
	root       string
	provider   string
	consumer   string
	unrelated  string
	source     string
	cache      string
	workingDir string
}

func newDartLab(t *testing.T) *dartLab {
	t.Helper()
	if _, err := exec.LookPath("dart"); err != nil {
		t.Skip("dart SDK is not installed")
	}
	root := testsupport.TempDir(t)
	lab := &dartLab{
		t: t, root: root,
		provider:  filepath.Join(root, "provider"),
		consumer:  filepath.Join(root, "consumer"),
		unrelated: filepath.Join(root, "unrelated"),
		cache:     testsupport.TempDir(t),
	}
	writeFullFixture(t, filepath.Join(lab.provider, "pubspec.yaml"),
		"name: provider_pkg\nversion: 0.0.1\nenvironment:\n  sdk: \">=3.0.0 <4.0.0\"\n")
	lab.source = filepath.Join(lab.provider, "lib", "p.dart")
	writeFullFixture(t, lab.source, "int addOne(int x) => x + 1;\n\nclass Counter {\n  int value = 0;\n  void bump() { value = addOne(value); }\n}\n")
	writeFullFixture(t, filepath.Join(lab.consumer, "pubspec.yaml"),
		"name: consumer_pkg\nversion: 0.0.1\nenvironment:\n  sdk: \">=3.0.0 <4.0.0\"\ndependencies:\n  provider_pkg:\n    path: ../provider\n")
	writeFullFixture(t, filepath.Join(lab.consumer, "lib", "c.dart"),
		"import 'package:provider_pkg/p.dart';\n\nint useIt() {\n  final c = Counter();\n  c.bump();\n  return addOne(c.value);\n}\n")
	// What `dart pub get` writes for a path dependency, so that the test needs no pub.
	writeFullFixture(t, filepath.Join(lab.consumer, ".dart_tool", "package_config.json"), `{
  "configVersion": 2,
  "packages": [
    {"name": "consumer_pkg", "rootUri": "../", "packageUri": "lib/", "languageVersion": "3.0"},
    {"name": "provider_pkg", "rootUri": "../../provider", "packageUri": "lib/", "languageVersion": "3.0"}
  ]
}
`)
	// A repository the consumer has nothing to do with, under a language that is not
	// semantic: it is in nobody's package config.
	writeFullFixture(t, filepath.Join(lab.unrelated, "lib.rs"), "pub fn unrelated() {}\n")
	var err error
	if lab.workingDir, err = os.Getwd(); err != nil {
		t.Fatal(err)
	}
	return lab
}

func (lab *dartLab) repository(name, path, language string) workspace.Repository {
	return workspace.Repository{Name: name, Path: path, RealPath: path, Languages: []string{language}}
}

func (lab *dartLab) index(repositories ...workspace.Repository) CacheReport {
	lab.t.Helper()
	_, report, err := Full(context.Background(), FullOptions{
		Repositories: repositories,
		DartAnalyzer: "dart",
		DartSDKPath:  "dart",
		// A repository registered as Rust needs the command named, though one
		// without a Cargo manifest never starts it.
		RustAnalyzer:        "rust-analyzer",
		RustTargetDirectory: filepath.Join(lab.root, "rust-target"),
		WorkingDirectory:    lab.workingDir,
		CacheMode:           CacheOn,
		CacheDirectory:      lab.cache,
	})
	if err != nil {
		lab.t.Fatalf("Full() error = %v", err)
	}
	return report.Cache
}

func (lab *dartLab) renameProviderSymbol() {
	lab.t.Helper()
	writeFullFixture(lab.t, lab.source, "int addTwo(int x) => x + 2;\n\nclass Counter {\n  int value = 0;\n  void bump() { value = addTwo(value); }\n}\n")
}

func TestDartConsumerIsAnalysedAgainWhenItsProviderChanges(t *testing.T) {
	lab := newDartLab(t)
	repositories := []workspace.Repository{
		lab.repository("consumer", lab.consumer, "dart"),
		lab.repository("provider", lab.provider, "dart"),
	}
	if cold := lab.index(repositories...); cold.Misses != 2 {
		t.Fatalf("cold pass cache = %+v, want both repositories analysed", cold)
	}
	// Without this the assertion below could not tell a registry that changed from a
	// cache that was never serving anything.
	if warm := lab.index(repositories...); warm.Hits != 2 || warm.Misses != 0 {
		t.Fatalf("warm pass cache = %+v, want both repositories served", warm)
	}

	lab.renameProviderSymbol()
	if changed := lab.index(repositories...); changed.Hits != 0 {
		t.Fatalf("pass after the provider renamed a symbol cache = %+v, want the consumer analysed again too: its entry names a symbol that no longer exists", changed)
	}
}

// A provider the semantic registry does not know: registered under a language that is
// not semantic, so that no digest of its tree is in the registry. The consumer reads
// it all the same, through its package config.
func TestDartConsumerIsAnalysedAgainWhenItsProviderIsRegisteredUnderAnotherLanguage(t *testing.T) {
	lab := newDartLab(t)
	repositories := []workspace.Repository{
		lab.repository("consumer", lab.consumer, "dart"),
		lab.repository("provider", lab.provider, "rust"),
	}
	if cold := lab.index(repositories...); cold.Misses != 1 {
		t.Fatalf("cold pass cache = %+v, want the consumer analysed", cold)
	}
	if warm := lab.index(repositories...); warm.Hits != 1 || warm.Misses != 0 {
		t.Fatalf("warm pass cache = %+v, want the consumer served", warm)
	}

	lab.renameProviderSymbol()
	if changed := lab.index(repositories...); changed.Hits != 0 {
		t.Fatalf("pass after the provider renamed a symbol cache = %+v, want the consumer analysed again: it was served with the old symbol", changed)
	}
}

// The same provider, registered after the consumer was analysed: the references that
// were unresolved are attributed to it now.
func TestDartConsumerIsAnalysedAgainWhenItsProviderIsRegisteredLater(t *testing.T) {
	lab := newDartLab(t)
	consumer := lab.repository("consumer", lab.consumer, "dart")
	if cold := lab.index(consumer); cold.Misses != 1 {
		t.Fatalf("cold pass cache = %+v, want the consumer analysed", cold)
	}
	if warm := lab.index(consumer); warm.Hits != 1 {
		t.Fatalf("warm pass cache = %+v, want the consumer served", warm)
	}
	if later := lab.index(consumer, lab.repository("provider", lab.provider, "rust")); later.Hits != 0 {
		t.Fatalf("pass after the provider was registered cache = %+v, want the consumer analysed again", later)
	}
}

// What the unit reads is what it depends on, and nothing else: a repository that is
// in no package config does not cost the consumer its entry. The registry that this
// adds to would have, were it to take every repository.
func TestDartConsumerIsNotAnalysedAgainWhenARepositoryItDoesNotReadChanges(t *testing.T) {
	lab := newDartLab(t)
	repositories := []workspace.Repository{
		lab.repository("consumer", lab.consumer, "dart"),
		lab.repository("provider", lab.provider, "rust"),
		lab.repository("unrelated", lab.unrelated, "rust"),
	}
	lab.index(repositories...)
	if warm := lab.index(repositories...); warm.Hits != 1 || warm.Misses != 0 {
		t.Fatalf("warm pass cache = %+v, want the consumer served", warm)
	}

	writeFullFixture(t, filepath.Join(lab.unrelated, "lib.rs"), "pub fn unrelated() {}\npub fn another() {}\n")
	if changed := lab.index(repositories...); changed.Hits != 1 || changed.Misses != 0 {
		t.Fatalf("pass after an unrelated repository changed cache = %+v, want the consumer served", changed)
	}
}

func TestDartProviderRootsAreTheRegisteredRepositoriesThePackageConfigReaches(t *testing.T) {
	root := testsupport.TempDir(t)
	for _, directory := range []string{
		"consumer", "provider", "provider2", filepath.Join("mono", "packages", "inner"), "unrelated", "elsewhere",
	} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// A package reached through a link is the repository it points into.
	if err := os.Symlink(filepath.Join(root, "elsewhere"), filepath.Join(root, "linked")); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}
	config := func(roots ...string) {
		t.Helper()
		packages := `{"name": "consumer_pkg", "rootUri": "../", "packageUri": "lib/"}`
		for index, packageRoot := range roots {
			packages += fmt.Sprintf(`, {"name": "p%d", "rootUri": %q, "packageUri": "lib/"}`, index, packageRoot)
		}
		writeFullFixture(t, filepath.Join(root, "consumer", ".dart_tool", "package_config.json"),
			`{"configVersion": 2, "packages": [`+packages+`]}`)
	}
	repository := func(name, directory string) workspace.Repository {
		path := filepath.Join(root, directory)
		return workspace.Repository{Name: name, Path: path, RealPath: path, Languages: []string{"rust"}}
	}
	options := FullOptions{Repositories: []workspace.Repository{
		repository("consumer", "consumer"), repository("provider", "provider"), repository("mono", "mono"),
		repository("unrelated", "unrelated"), repository("elsewhere", "elsewhere"),
		// Its root is the first characters of the root of "provider".
		repository("provide", "provide"),
	}}
	unit := analysisUnit{kind: unitSemantic, repository: repository("consumer", "consumer")}
	rootsOf := func(composed map[string]composedTarget) []string {
		t.Helper()
		var names []string
		for _, got := range dartProviderRoots(unit, options, composed) {
			relative, err := filepath.Rel(root, got)
			if err != nil {
				t.Fatal(err)
			}
			names = append(names, relative)
		}
		return names
	}

	// No package config, nothing to read.
	if got := rootsOf(nil); len(got) != 0 {
		t.Fatalf("a repository with no package config depends on %q", got)
	}

	config("../../provider", "../../provider2", "../../mono/packages/inner", "../../linked")
	// The repository itself is never its own provider, a package inside another
	// repository is that repository, a directory that only shares its first characters
	// with a repository is not inside it, and a link is followed.
	if got, want := rootsOf(nil), []string{"elsewhere", "mono", "provider"}; !slices.Equal(got, want) {
		t.Fatalf("roots = %q, want %q", got, want)
	}

	// A target the facts name is a dependency whether or not the package config says
	// so, and a repository is listed once.
	composed := map[string]composedTarget{"one": {Repository: "unrelated"}, "two": {Repository: "provider"}}
	if got, want := rootsOf(composed), []string{"elsewhere", "mono", "provider", "unrelated"}; !slices.Equal(got, want) {
		t.Fatalf("roots with composed targets = %q, want %q", got, want)
	}
	// A target in the repository itself, or in one nobody registered, is none.
	if got, want := rootsOf(map[string]composedTarget{"self": {Repository: "consumer"}, "gone": {Repository: "nobody"}}), []string{"elsewhere", "mono", "provider"}; !slices.Equal(got, want) {
		t.Fatalf("roots with targets in no other repository = %q, want %q", got, want)
	}
}
