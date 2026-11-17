package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/workspace"
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

// TestEveryLanguagesManifestsInvalidateAnEntry covers the other half: a build
// file can change what the facts are without a single source changing.
func TestEveryLanguagesManifestsInvalidateAnEntry(t *testing.T) {
	for _, manifest := range []string{
		"go.mod", "go.sum", "go.work", "package.json", "tsconfig.json",
		"Cargo.toml", "Cargo.lock", "pyproject.toml", "requirements.txt",
		"requirements-dev.txt", "pubspec.yaml", "analysis_options.yaml", "pom.xml",
		"build.gradle", "build.gradle.kts", "app.csproj", "pnpm-lock.yaml",
		"tsconfig.build.json", filepath.Join(".dart_tool", "package_config.json"),
		"rust-toolchain.toml", "rust-toolchain",
	} {
		if !isFingerprintedSource(manifest) {
			t.Fatalf("%s does not invalidate a cache entry", manifest)
		}
	}
}

// TestFactCacheMissesWhenAnExplicitOrAnalyzerManifestChanges covers inputs
// that can change facts without a source edit. The explicit manifest is
// relative to the repository on purpose: describeInputs has to resolve it the
// same way as source observation does, or the cache would fingerprint the
// process's working directory instead.
func TestFactCacheMissesWhenAnExplicitOrAnalyzerManifestChanges(t *testing.T) {
	fixture := newCachedFixture(t)
	fixture.repository.Manifests = []string{"project.settings"}
	writeFullFixture(t, filepath.Join(fixture.root, "project.settings"), "before\n")
	writeFullFixture(t, filepath.Join(fixture.root, "build.gradle"), "plugins { id 'java' }\n")
	fixture.index()
	if _, report := fixture.index(); report.Cache.Hits == 0 {
		t.Fatalf("cache = %+v, want a hit when project.settings and build.gradle are unchanged", report.Cache)
	}

	writeFullFixture(t, filepath.Join(fixture.root, "project.settings"), "after\n")
	if _, report := fixture.index(); report.Cache.Hits != 0 {
		t.Fatalf("cache = %+v, want no hit after project.settings changed", report.Cache)
	}

	writeFullFixture(t, filepath.Join(fixture.root, "build.gradle"), "plugins { id 'java-library' }\n")
	if _, report := fixture.index(); report.Cache.Hits != 0 {
		t.Fatalf("cache = %+v, want no hit after build.gradle changed", report.Cache)
	}
}

// A repository's pinned Rust toolchain decides which compiler and which standard
// library its facts come from, and editing the file that pins it changes neither
// a source nor a manifest of the unit.
func TestFactCacheMissesWhenTheRustToolchainPinChanges(t *testing.T) {
	fixture := newCachedFixture(t)
	writeFullFixture(t, filepath.Join(fixture.root, "rust-toolchain.toml"), "[toolchain]\nchannel = \"1.80.0\"\n")
	fixture.index()
	if _, report := fixture.index(); report.Cache.Hits == 0 {
		t.Fatalf("cache = %+v, want a hit when the pin is unchanged", report.Cache)
	}
	writeFullFixture(t, filepath.Join(fixture.root, "rust-toolchain.toml"), "[toolchain]\nchannel = \"1.90.0\"\n")
	if _, report := fixture.index(); report.Cache.Hits != 0 {
		t.Fatalf("cache = %+v, want no hit after the pinned channel changed", report.Cache)
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

// A caller that has just observed the analyzer fingerprint hands it over, and the
// cache keys its entries with that value: computing it again hashes the
// executable and the worker and starts every toolchain, for a result the caller
// already holds. The resolver version is what tells the two apart, because it is
// part of the value a computation would produce.
func TestFactCacheKeysEntriesWithTheFingerprintTheCallerObserved(t *testing.T) {
	fixture := newCachedFixture(t)
	index := func(resolver, observed string) FullReport {
		t.Helper()
		_, report, err := Full(context.Background(), FullOptions{
			ResolverVersion:     resolver,
			AnalyzerFingerprint: observed,
			Repositories:        []workspace.Repository{fixture.repository},
			SyntheticWorkFile:   fixture.workFile,
			CacheMode:           fixture.mode,
			CacheDirectory:      fixture.cache,
		})
		if err != nil {
			t.Fatalf("Full(resolver=%q observed=%q) error = %v", resolver, observed, err)
		}
		return report
	}

	if report := index("one", ""); report.Cache.Hits != 0 {
		t.Fatalf("cold pass cache = %+v, want no hit", report.Cache)
	}
	// Without the observed value the resolver version is part of what the pass
	// computes, so another one is another analyzer. This is what makes the next
	// assertion say something.
	if report := index("two", ""); report.Cache.Hits != 0 {
		t.Fatalf("pass with another resolver cache = %+v, want no hit when the fingerprint is computed", report.Cache)
	}
	// A blank value is no observation.
	if report := index("three", "  \t"); report.Cache.Hits != 0 {
		t.Fatalf("pass with a blank observed fingerprint cache = %+v, want it computed and no hit", report.Cache)
	}
	if report := index("five", "  \t"); report.Cache.Hits != 0 {
		t.Fatalf("second pass with a blank observed fingerprint cache = %+v, want it computed again and no hit: blank was used as the key", report.Cache)
	}

	if report := index("one", "observed"); report.Cache.Hits != 0 {
		t.Fatalf("first pass under an observed fingerprint cache = %+v, want no hit", report.Cache)
	}
	if report := index("four", "observed"); report.Cache.Hits == 0 {
		t.Fatalf("pass under the same observed fingerprint cache = %+v, want a hit: the value was computed again", report.Cache)
	}
	if report := index("four", "another"); report.Cache.Hits != 0 {
		t.Fatalf("pass under another observed fingerprint cache = %+v, want no hit", report.Cache)
	}
}
