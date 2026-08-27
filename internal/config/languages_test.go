package config

import "testing"

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
