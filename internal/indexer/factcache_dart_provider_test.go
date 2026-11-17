package indexer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// The facts of a Dart consumer name symbols of the package it depends on: the loader
// is handed every repository as a provider and reads the file a reference lands in.
// When the provider renames a symbol the consumer's entry is wrong though none of
// its own sources changed, and the digest of the provider's tree in the semantic
// registry is the only thing that says so. Without it the entry was served:
// `references=8` and the old symbol where the analysis gives `7` and the new one.
func TestDartConsumerIsAnalysedAgainWhenItsProviderChanges(t *testing.T) {
	if _, err := exec.LookPath("dart"); err != nil {
		t.Skip("dart SDK is not installed")
	}
	root := testsupport.TempDir(t)
	provider := filepath.Join(root, "provider")
	consumer := filepath.Join(root, "consumer")
	writeFullFixture(t, filepath.Join(provider, "pubspec.yaml"),
		"name: provider_pkg\nversion: 0.0.1\nenvironment:\n  sdk: \">=3.0.0 <4.0.0\"\n")
	source := filepath.Join(provider, "lib", "p.dart")
	writeFullFixture(t, source, "int addOne(int x) => x + 1;\n\nclass Counter {\n  int value = 0;\n  void bump() { value = addOne(value); }\n}\n")
	writeFullFixture(t, filepath.Join(consumer, "pubspec.yaml"),
		"name: consumer_pkg\nversion: 0.0.1\nenvironment:\n  sdk: \">=3.0.0 <4.0.0\"\ndependencies:\n  provider_pkg:\n    path: ../provider\n")
	writeFullFixture(t, filepath.Join(consumer, "lib", "c.dart"),
		"import 'package:provider_pkg/p.dart';\n\nint useIt() {\n  final c = Counter();\n  c.bump();\n  return addOne(c.value);\n}\n")
	// What `dart pub get` writes for a path dependency, so that the test needs no pub.
	writeFullFixture(t, filepath.Join(consumer, ".dart_tool", "package_config.json"), `{
  "configVersion": 2,
  "packages": [
    {"name": "consumer_pkg", "rootUri": "../", "packageUri": "lib/", "languageVersion": "3.0"},
    {"name": "provider_pkg", "rootUri": "../../provider", "packageUri": "lib/", "languageVersion": "3.0"}
  ]
}
`)

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cacheDirectory := testsupport.TempDir(t)
	repositories := []workspace.Repository{
		{Name: "consumer", Path: consumer, RealPath: consumer, Languages: []string{"dart"}},
		{Name: "provider", Path: provider, RealPath: provider, Languages: []string{"dart"}},
	}
	index := func() CacheReport {
		t.Helper()
		_, report, err := Full(context.Background(), FullOptions{
			Repositories:     repositories,
			DartAnalyzer:     "dart",
			DartSDKPath:      "dart",
			WorkingDirectory: workingDirectory,
			CacheMode:        CacheOn,
			CacheDirectory:   cacheDirectory,
		})
		if err != nil {
			t.Fatalf("Full() error = %v", err)
		}
		return report.Cache
	}

	if cold := index(); cold.Misses != 2 {
		t.Fatalf("cold pass cache = %+v, want both repositories analysed", cold)
	}
	// Without this the assertion below could not tell a registry that changed from a
	// cache that was never serving anything.
	if warm := index(); warm.Hits != 2 || warm.Misses != 0 {
		t.Fatalf("warm pass cache = %+v, want both repositories served", warm)
	}

	writeFullFixture(t, source, "int addTwo(int x) => x + 2;\n\nclass Counter {\n  int value = 0;\n  void bump() { value = addTwo(value); }\n}\n")
	if changed := index(); changed.Hits != 0 {
		t.Fatalf("pass after the provider renamed a symbol cache = %+v, want the consumer analysed again too: its entry names a symbol that no longer exists", changed)
	}
}
