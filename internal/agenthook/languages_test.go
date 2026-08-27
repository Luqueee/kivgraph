package agenthook

import "testing"

// TestExtensionsForAnswersEveryLanguageAConfigurationMayName is the reason this
// table is wider than the watcher's: a repository declaring Python or Dart is
// valid configuration, and a mapping that did not know them would leave the
// files branch silent for those repositories with no sign that it had.
func TestExtensionsForAnswersEveryLanguageAConfigurationMayName(t *testing.T) {
	for _, language := range []string{
		"go", "typescript", "ts", "javascript", "js", "rust", "rs", "python", "py", "dart",
	} {
		if len(ExtensionsFor([]string{language})) == 0 {
			t.Fatalf("language %q maps to no extension", language)
		}
	}
}

// TestExtensionsForDeduplicatesAcrossLanguages keeps a repository that declares
// both spellings of one language from gating twice on the same extension.
func TestExtensionsForDeduplicatesAcrossLanguages(t *testing.T) {
	extensions := ExtensionsFor([]string{"typescript", "ts", " TS ", "javascript"})
	seen := map[string]int{}
	for _, extension := range extensions {
		seen[extension]++
	}
	for extension, count := range seen {
		if count > 1 {
			t.Fatalf("%s appears %d times", extension, count)
		}
	}
	if !IndexedExtensions(extensions)("**/*.tsx") || !IndexedExtensions(extensions)("a.mjs") {
		t.Fatalf("extensions %q do not cover what those languages are written in", extensions)
	}
}

// TestExtensionsForIgnoresWhatItDoesNotKnow is the negative: an unknown
// language contributes nothing rather than everything.
func TestExtensionsForIgnoresWhatItDoesNotKnow(t *testing.T) {
	if extensions := ExtensionsFor([]string{"cobol", "", "  "}); len(extensions) != 0 {
		t.Fatalf("unknown languages produced %q", extensions)
	}
}
