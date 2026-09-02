package indexer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// repositoryNamed is the one repository every unit of the identity test shares,
// which is what makes a collision between two kinds visible.
func repositoryNamed(name string) workspace.Repository {
	return workspace.Repository{Name: name, Path: "/repositories/" + name, RealPath: "/repositories/" + name}
}

// TestAnalyzerFingerprintWatchesTheJavaIndexerBinary is the Java half of what
// TestAnalyzerFingerprintWatchesThePythonProducerThatRuns pins for Python.
//
// Java's producer is an executable this repository does not ship, so what
// identifies it is the binary the PATH resolves. A scip-java upgraded in place
// emits a different index from the same sources, and an entry written by the
// old one describes a graph this pass would not produce. Naming only the
// command string would miss that entirely: the string does not change.
func TestAnalyzerFingerprintWatchesTheJavaIndexerBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture relies on a unix executable bit")
	}
	directory := testsupport.TempDir(t)
	indexer := filepath.Join(directory, "scip-java-fixture")
	if err := os.WriteFile(indexer, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))

	options := FullOptions{JavaIndexerCommand: "scip-java-fixture"}
	base := analyzerFingerprint(options)
	if analyzerFingerprint(options) != base {
		t.Fatal("fingerprint is not stable across two reads")
	}

	if err := os.WriteFile(indexer, []byte("#!/bin/sh\necho changed\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if analyzerFingerprint(options) == base {
		t.Fatal("fingerprint ignored a change to the indexer binary this pass runs")
	}
}

// TestAnalyzerFingerprintSeparatesTheJavaOptions guards the options that change
// what the indexer is asked to do. Two passes that disagree on any of them
// observe different code, so neither may be served the other's entry.
func TestAnalyzerFingerprintSeparatesTheJavaOptions(t *testing.T) {
	base := FullOptions{JavaIndexerCommand: "scip-java", JavaTargetDirectory: "/tmp/a"}
	reference := analyzerFingerprint(base)

	for name, mutate := range map[string]func(*FullOptions){
		"build tool":        func(o *FullOptions) { o.JavaBuildTool = "gradle" },
		"include tests":     func(o *FullOptions) { o.JavaIncludeTests = true },
		"include generated": func(o *FullOptions) { o.JavaIncludeGenerated = true },
		"target directory":  func(o *FullOptions) { o.JavaTargetDirectory = "/tmp/b" },
		"indexer command":   func(o *FullOptions) { o.JavaIndexerCommand = "scip-java --verbose" },
	} {
		options := base
		mutate(&options)
		if analyzerFingerprint(options) == reference {
			t.Errorf("changing the %s did not change the fingerprint", name)
		}
	}
}

// TestUnitIdentitySeparatesEveryKind is the regression for a defect the kind
// refactor uncovered: unitIdentity had no branch for Rust, so every Rust
// workspace was keyed as a TypeScript package with an empty name. One
// workspace per repository hid it; two shared an entry. Profile is not part
// of this identity: the effective registry is added by factCache.identity.
func TestUnitIdentitySeparatesEveryKind(t *testing.T) {
	identities := map[string]string{}
	for name, unit := range map[string]analysisUnit{
		"go":          {kind: unitGo, repository: repositoryNamed("shared")},
		"typescript":  {kind: unitTypeScript, repository: repositoryNamed("shared")},
		"rust":        {kind: unitRust, repository: repositoryNamed("shared")},
		"python":      {kind: unitSemantic, language: "python", repository: repositoryNamed("shared")},
		"dart":        {kind: unitSemantic, language: "dart", repository: repositoryNamed("shared")},
		"java":        {kind: unitSemantic, language: "java", repository: repositoryNamed("shared")},
		"unspecified": {repository: repositoryNamed("shared")},
	} {
		identity := unitIdentity(unit)
		if previous, clash := identities[identity]; clash {
			t.Errorf("%s and %s share the cache identity %q", name, previous, identity)
		}
		identities[identity] = name
	}
}

func TestFactCacheIdentityUsesTheEffectiveResolutionContext(t *testing.T) {
	cache := &factCache{
		goRegistry:         "go-context",
		rustRegistry:       "rust-context",
		semanticRegistry:   "semantic-context",
		typeScriptRegistry: "typescript-context",
	}
	for name, unit := range map[string]analysisUnit{
		"go":          {kind: unitGo},
		"rust":        {kind: unitRust},
		"semantic":    {kind: unitSemantic},
		"typescript":  {kind: unitTypeScript},
		"unspecified": {},
	} {
		if got, want := cache.registryContext(unit), name+"-context"; name == "unspecified" {
			if got != "none" {
				t.Errorf("registryContext(%s) = %q, want none", name, got)
			}
		} else if got != want {
			t.Errorf("registryContext(%s) = %q, want %q", name, got, want)
		}
	}
}

func TestFactCacheSourceIdentityUsesWorktreeAndPath(t *testing.T) {
	withWorktree := workspace.Repository{Worktree: "service-main", RealPath: "/checkouts/service"}
	worktreeIdentity := sourceIdentity(withWorktree)
	otherWorktree := workspace.Repository{Worktree: "service-feature", RealPath: "/checkouts/service"}
	if otherIdentity := sourceIdentity(otherWorktree); worktreeIdentity == otherIdentity {
		t.Fatalf("sourceIdentity() reused facts across worktrees: %q and %q for %#v and %#v", worktreeIdentity, otherIdentity, withWorktree, otherWorktree)
	}
	otherPath := workspace.Repository{Worktree: "service-main", RealPath: "/checkouts/other"}
	if otherIdentity := sourceIdentity(otherPath); worktreeIdentity == otherIdentity {
		t.Fatalf("sourceIdentity() reused facts across paths: %q and %q for %#v and %#v", worktreeIdentity, otherIdentity, withWorktree, otherPath)
	}
	withoutRealPath := workspace.Repository{Path: "/checkouts/fallback"}
	withFallback := workspace.Repository{RealPath: "/checkouts/fallback"}
	if fallbackIdentity := sourceIdentity(withoutRealPath); fallbackIdentity != sourceIdentity(withFallback) {
		t.Fatalf("sourceIdentity() did not fall back to Path: %q and %q for %#v and %#v", fallbackIdentity, sourceIdentity(withFallback), withoutRealPath, withFallback)
	}
}

func TestFactCacheLocalAddressIncludesInputNames(t *testing.T) {
	cache := &factCache{trees: newFingerprintMemo()}
	first := analysisUnit{
		kind:       unitSemantic,
		language:   "python",
		repository: workspace.Repository{RealPath: "/missing/source-one"},
	}
	second := first
	second.repository.RealPath = "/missing/source-two"
	firstAddress := cache.localAddress(first, FullOptions{}, analysisInputs{})
	secondAddress := cache.localAddress(second, FullOptions{}, analysisInputs{})
	if firstAddress == secondAddress {
		t.Fatalf("localAddress() reused identical absent fingerprints for %q and %q", first.repository.RealPath, second.repository.RealPath)
	}
}

func TestTypeScriptRegistryAddressIsDeterministic(t *testing.T) {
	packages := []typeScriptPackageUnit{
		{
			repository: workspace.Repository{Name: "frontend", Worktree: "frontend-main"},
			packageValue: workspace.TypeScriptPackage{
				Name: "@example/ui", Version: "1.0.0", RootPath: "/src/ui",
				ManifestPath: "/src/ui/package.json", ProjectPath: "/src/ui/tsconfig.json",
			},
		},
		{
			repository: workspace.Repository{Name: "shared", Worktree: "shared-main"},
			packageValue: workspace.TypeScriptPackage{
				Name: "@example/config", Version: "2.0.0", RootPath: "/src/config",
				ManifestPath: "/src/config/package.json", ProjectPath: "/src/config/tsconfig.json",
			},
		},
	}
	first := typeScriptRegistryName(packages)
	reversed := append([]typeScriptPackageUnit(nil), packages[1], packages[0])
	if second := typeScriptRegistryName(reversed); first != second {
		t.Fatalf("registry address changed with package order: %q then %q", first, second)
	}
	packages[0].packageValue.Version = "1.0.1"
	if changed := typeScriptRegistryName(packages); changed == first {
		t.Fatal("registry address ignored a provider version change")
	}
}

func TestFactCacheRefusalReportIgnoresNoopRefusals(t *testing.T) {
	// Full cannot create a nil cache or pass an empty refusal reason; keep these
	// guards tested directly so a future caller cannot turn a no-op into a panic.
	var nilCache *factCache
	nilCache.refuse("")
	cache := &factCache{refusals: make(map[CacheRefusalReason]int)}
	cache.refuse("")
	cache.refuse(CacheRefusalNoEntry)
	if report := cache.report(); report.Refusals[CacheRefusalNoEntry] != 1 || len(report.Refusals) != 1 {
		t.Fatalf("refusal report = %+v, want only one recorded refusal", report.Refusals)
	}
}
