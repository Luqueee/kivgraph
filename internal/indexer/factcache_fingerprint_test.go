package indexer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
)

// TestATreeFingerprintChangesWithEverySupportedLanguage is the regression, and
// the number it defends is not a percentage: a unit whose tree fingerprint does
// not move when its sources move is served from a stale cache entry, which is
// facts nobody can reproduce.
//
// Rust was that unit. isFingerprintedSource listed nine extensions across four
// languages and `.rs` was not one of them, and neither Cargo.toml nor
// Cargo.lock was a manifest it knew, so treeFingerprint over a crate matched no
// file at all and hashed the empty string -- the same constant for every crate,
// before and after every edit.
func TestATreeFingerprintChangesWithEverySupportedLanguage(t *testing.T) {
	for _, testCase := range []struct{ language, source string }{
		{"go", "main.go"},
		{"typescript", filepath.Join("src", "index.ts")},
		{"javascript", filepath.Join("src", "index.js")},
		{"rust", filepath.Join("src", "lib.rs")},
		{"python", filepath.Join("pkg", "module.py")},
		{"dart", filepath.Join("lib", "widget.dart")},
	} {
		t.Run(testCase.language, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, testCase.source)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("before\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			before := treeFingerprint(root)
			if before == emptyTreeFingerprint {
				t.Fatalf("a %s source tree fingerprints as if it were empty",
					testCase.language)
			}
			if err := os.WriteFile(path, []byte("after\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if after := treeFingerprint(root); after == before {
				t.Fatalf("editing %s did not change the fingerprint of a %s tree",
					testCase.source, testCase.language)
			}
		})
	}
}

// emptyTreeFingerprint is the hash of nothing, which is what a tree whose files
// the walk all skipped produces.
const emptyTreeFingerprint = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// TestFactCacheMissesWhenAnExplicitOrAnalyzerManifestChanges covers inputs
// that can change facts without a source edit. The explicit manifest is
// relative to the repository on purpose: describeInputs has to resolve it the
// same way as freshness does, or the cache would fingerprint the process's
// working directory instead.
func TestFactCacheMissesWhenAnExplicitOrAnalyzerManifestChanges(t *testing.T) {
	fixture := newCachedFixture(t)
	fixture.repository.Manifests = []string{"project.settings"}
	writeFullFixture(t, filepath.Join(fixture.root, "project.settings"), "before\n")
	writeFullFixture(t, filepath.Join(fixture.root, "build.gradle"), "plugins { id 'java' }\n")
	fixture.index()
	if _, report := fixture.index(); report.Cache.Hits == 0 {
		t.Fatalf("cache = %+v, want a hit when no input changed", report.Cache)
	}

	writeFullFixture(t, filepath.Join(fixture.root, "project.settings"), "after\n")
	if _, report := fixture.index(); report.Cache.Hits != 0 {
		t.Fatalf("cache = %+v, want no hit after explicit manifest changed", report.Cache)
	}

	writeFullFixture(t, filepath.Join(fixture.root, "build.gradle"), "plugins { id 'java-library' }\n")
	if _, report := fixture.index(); report.Cache.Hits != 0 {
		t.Fatalf("cache = %+v, want no hit after analyzer manifest changed", report.Cache)
	}
}

// TestTheFingerprintIgnoresWhatNoLanguageDeclares is the negative: the walk
// over-approximates on purpose, but not to the point of hashing the world.
func TestTheFingerprintIgnoresWhatNoLanguageDeclares(t *testing.T) {
	for _, name := range []string{"README.md", "LICENSE", "notes.txt", "image.png", "a.out"} {
		if isFingerprintedSource(name) {
			t.Fatalf("%s is hashed and no language declares it", name)
		}
	}
}

// TestFingerprintedExtensionsFollowTheSupportedLanguages keeps the next
// language from arriving the way Rust was left: accepted by configuration and
// invisible to the cache.
func TestFingerprintedExtensionsFollowTheSupportedLanguages(t *testing.T) {
	for _, language := range config.SupportedLanguages() {
		for _, extension := range config.SourceExtensions([]string{language}) {
			if !isFingerprintedSource("probe" + extension) {
				t.Fatalf("%s is written in %s and %s is not fingerprinted",
					language, extension, extension)
			}
		}
	}
}
