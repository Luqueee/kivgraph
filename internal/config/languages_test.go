package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestEverySupportedLanguageIsWrittenInSomething is the invariant that keeps
// the two tables from drifting apart again: a language a configuration is
// allowed to declare must map to files, or a repository declaring it indexes
// and reconciles nothing while looking perfectly valid.
func TestEverySupportedLanguageIsWrittenInSomething(t *testing.T) {
	for _, language := range SupportedLanguages() {
		if extensions := SourceExtensions([]string{language}); len(extensions) == 0 {
			t.Fatalf("%q is accepted by SupportedLanguages and is written in nothing", language)
		}
	}
}

// TestSourceExtensionsIgnoresWhatItDoesNotKnow is the negative: an unknown name
// contributes nothing rather than everything, so a typo cannot widen a scan.
func TestSourceExtensionsIgnoresWhatItDoesNotKnow(t *testing.T) {
	if extensions := SourceExtensions([]string{"cobol", "", "   "}); len(extensions) != 0 {
		t.Fatalf("unknown languages produced %q", extensions)
	}
}

// TestSourceExtensionsAnswersOnceAndInOrder keeps a repository that declares
// both spellings of one language from carrying it twice.
func TestSourceExtensionsAnswersOnceAndInOrder(t *testing.T) {
	extensions := SourceExtensions([]string{"typescript", "ts", " TS ", "javascript", "js"})
	seen := map[string]int{}
	for _, extension := range extensions {
		seen[extension]++
	}
	for extension, count := range seen {
		if count > 1 {
			t.Fatalf("%s appears %d times", extension, count)
		}
	}
	for index := 1; index < len(extensions); index++ {
		if extensions[index-1] > extensions[index] {
			t.Fatalf("not sorted: %q", extensions)
		}
	}
}

// TestHasSourceExtensionAnswersOnThePathItIsGiven covers the predicate the
// watcher asks once per file, including the case it must refuse.
func TestHasSourceExtensionAnswersOnThePathItIsGiven(t *testing.T) {
	set := SourceExtensionSet([]string{"python", "dart"})
	for path, want := range map[string]bool{
		"/repo/src/module.py":   true,
		"/repo/src/module.pyi":  true,
		"/repo/lib/widget.DART": true,
		"/repo/README.md":       false,
		"/repo/src/main.go":     false,
		"/repo/src/noextension": false,
	} {
		if got := HasSourceExtension(set, path); got != want {
			t.Fatalf("HasSourceExtension(%q) = %v, want %v", path, got, want)
		}
	}
}

// TestDetectLanguagesUsesSourcesAndManifests exercises the project discovery
// used by `kivgraph index`. Generated and dependency directories are the
// negative: a vendored file must not make a project claim a language it does
// not own, and the result must be stable rather than depending on walk order.
func TestDetectLanguagesUsesSourcesAndManifests(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		"main.go", "web/component.tsx", "scripts/build.js", "src/lib.rs",
		"python/typing.pyi", "lib/widget.dart", "java/App.java", "src/Program.cs",
		"nested/package.json", "nested/pyproject.toml", "nested/build.gradle.kts",
		"nested/App.csproj",
		"node_modules/ignored.py", "vendor/ignored.java", ".kivgraph/ignored.rs",
	} {
		writeDetectionFile(t, filepath.Join(root, path))
	}

	got, err := DetectLanguages(root)
	if err != nil {
		t.Fatalf("DetectLanguages(%q) error = %v", root, err)
	}
	want := []string{"go", "typescript", "javascript", "rust", "python", "dart", "java", "csharp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DetectLanguages(%q) = %q, want %q", root, got, want)
	}
}

// TestDetectLanguagesDistinguishesAnEmptyProjectFromAnUnreadablePath keeps
// autodetection from turning both states into a successful empty index.
func TestDetectLanguagesDistinguishesAnEmptyProjectFromAnUnreadablePath(t *testing.T) {
	root := t.TempDir()
	if got, err := DetectLanguages(root); err != nil || len(got) != 0 {
		t.Fatalf("DetectLanguages(empty) = %q, %v, want an empty successful result", got, err)
	}
	if _, err := DetectLanguages(filepath.Join(root, "missing")); err == nil {
		t.Fatal("DetectLanguages(missing) succeeded, want an error")
	}
	file := filepath.Join(root, "file.go")
	writeDetectionFile(t, file)
	if _, err := DetectLanguages(file); err == nil {
		t.Fatal("DetectLanguages(file) succeeded, want a directory error")
	}
}

func writeDetectionFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
