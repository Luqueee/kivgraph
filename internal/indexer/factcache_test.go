package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/testsupport"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

// cachedFixture is one Go repository indexed through a fact cache. Every test
// below asks the same question in a different way: after the world changes,
// does the cache still answer what a fresh analysis would answer?
type cachedFixture struct {
	t          *testing.T
	root       string
	cache      string
	workFile   string
	repository workspace.Repository
	mode       CacheMode
}

func newCachedFixture(t *testing.T) *cachedFixture {
	t.Helper()
	root := testsupport.TempDir(t)
	writeFullFixture(t, filepath.Join(root, "go.mod"), "module example.com/cached\n\ngo 1.24\n")
	writeFullFixture(t, filepath.Join(root, "fixture.go"), `package fixture

// Greeting is the definition every assertion below counts.
func Greeting() string { return "hello" }
`)
	return &cachedFixture{
		t:        t,
		root:     root,
		cache:    testsupport.TempDir(t),
		workFile: filepath.Join(testsupport.TempDir(t), "go.work"),
		repository: workspace.Repository{
			Name: "cached", Path: root, RealPath: root, Languages: []string{"go"},
		},
		mode: CacheOn,
	}
}

func (fixture *cachedFixture) index() (facts.Set, FullReport) {
	fixture.t.Helper()
	set, report, err := Full(context.Background(), FullOptions{
		Repositories:      []workspace.Repository{fixture.repository},
		SyntheticWorkFile: fixture.workFile,
		CacheMode:         fixture.mode,
		CacheDirectory:    fixture.cache,
	})
	if err != nil {
		fixture.t.Fatalf("Full() error = %v", err)
	}
	return set, report
}

func encodedFacts(t *testing.T, set facts.Set) string {
	t.Helper()
	encoded, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return string(encoded)
}

// TestFactCacheServesTheFactsTheAnalysisWouldProduce is the whole promise: a
// hit and a miss are the same facts. Everything else here is about when a hit
// is allowed.
func TestFactCacheServesTheFactsTheAnalysisWouldProduce(t *testing.T) {
	fixture := newCachedFixture(t)

	cold, coldReport := fixture.index()
	if coldReport.Cache.Hits != 0 || coldReport.Cache.Misses != 1 {
		t.Fatalf("cold cache = %+v, want one miss", coldReport.Cache)
	}

	warm, warmReport := fixture.index()
	if warmReport.Cache.Hits != 1 || warmReport.Cache.Misses != 0 {
		t.Fatalf("warm cache = %+v, want one hit", warmReport.Cache)
	}
	if encodedFacts(t, cold) != encodedFacts(t, warm) {
		t.Fatalf("the cache answered facts the analysis did not produce")
	}
}

// TestFactCacheMissesWhenASourceFileChanges keeps the obvious case honest.
func TestFactCacheMissesWhenASourceFileChanges(t *testing.T) {
	fixture := newCachedFixture(t)
	cold, _ := fixture.index()

	writeFullFixture(t, filepath.Join(fixture.root, "fixture.go"), `package fixture

func Greeting() string { return "hello" }

// Farewell is the definition the second pass must discover.
func Farewell() string { return "bye" }
`)

	warm, report := fixture.index()
	if report.Cache.Hits != 0 {
		t.Fatalf("cache = %+v, want no hit after the source changed", report.Cache)
	}
	if len(warm.Symbols) <= len(cold.Symbols) {
		t.Fatalf("symbols = %d, want more than the %d of the first pass",
			len(warm.Symbols), len(cold.Symbols))
	}
}

// TestFactCacheMissesWhenAFileIsAdded is the case a recorded list of inputs
// would get wrong: nothing the first pass read has changed, and yet the facts
// have. The fingerprint is taken over the walk, not over the files the last
// analysis happened to open.
func TestFactCacheMissesWhenAFileIsAdded(t *testing.T) {
	fixture := newCachedFixture(t)
	cold, _ := fixture.index()

	writeFullFixture(t, filepath.Join(fixture.root, "added.go"), `package fixture

func Added() string { return "added" }
`)

	warm, report := fixture.index()
	if report.Cache.Hits != 0 {
		t.Fatalf("cache = %+v, want no hit after a file was added", report.Cache)
	}
	if len(warm.Symbols) <= len(cold.Symbols) {
		t.Fatalf("symbols = %d, want the added definition on top of %d",
			len(warm.Symbols), len(cold.Symbols))
	}
}

// TestFactCacheMissesWhenTheManifestChanges covers the input that is not
// source: what a module declares decides what its code even means.
func TestFactCacheMissesWhenTheManifestChanges(t *testing.T) {
	fixture := newCachedFixture(t)
	fixture.index()

	writeFullFixture(t, filepath.Join(fixture.root, "go.mod"), "module example.com/cached\n\ngo 1.25\n")

	_, report := fixture.index()
	if report.Cache.Hits != 0 {
		t.Fatalf("cache = %+v, want no hit after go.mod changed", report.Cache)
	}
}

// TestFactCacheMissesWhenTheAnalyzerChanges keeps facts produced under one set
// of build constraints from being served under another. The same source
// declares different files to read, so it is a different question with a
// different answer.
func TestFactCacheMissesWhenTheAnalyzerChanges(t *testing.T) {
	fixture := newCachedFixture(t)
	fixture.index()

	set, report, err := Full(context.Background(), FullOptions{
		Repositories:      []workspace.Repository{fixture.repository},
		SyntheticWorkFile: fixture.workFile,
		CacheMode:         CacheOn,
		CacheDirectory:    fixture.cache,
		GoBuildTags:       []string{"integration"},
	})
	if err != nil {
		t.Fatalf("Full() error = %v", err)
	}
	if report.Cache.Hits != 0 {
		t.Fatalf("cache = %+v, want no hit under different build tags", report.Cache)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("facts validation error = %v", err)
	}
}

// goPair writes a consumer that requires a provider without replacing it.
// Two modules share a synthetic workspace exactly when one reaches the other,
// which is what makes the provider's source part of the consumer's type
// information -- and therefore an input of the consumer's entry.
func goPair(t *testing.T) (consumer, provider string) {
	t.Helper()
	root := testsupport.TempDir(t)
	consumer = filepath.Join(root, "consumer")
	provider = filepath.Join(root, "provider")
	writeFullFixture(t, filepath.Join(provider, "go.mod"), "module example.com/provider\n\ngo 1.24\n")
	writeFullFixture(t, filepath.Join(provider, "value.go"), "package provider\n\nconst Value = 41\n")
	writeFullFixture(t, filepath.Join(consumer, "go.mod"),
		"module example.com/consumer\n\ngo 1.24\n\nrequire example.com/provider v0.0.0\n")
	writeFullFixture(t, filepath.Join(consumer, "main.go"),
		"package consumer\n\nimport \"example.com/provider\"\n\nfunc Total() int { return provider.Value }\n")
	return consumer, provider
}

// TestFactCacheMissesWhenAWorkspaceSiblingChanges is the dangerous case.
// Nothing inside the consumer changed: what changed is the module it reads.
// Its cached facts name symbols that no longer exist, and an entry that kept
// answering them would be a graph that looks correct and is not.
func TestFactCacheMissesWhenAWorkspaceSiblingChanges(t *testing.T) {
	consumerRoot, providerRoot := goPair(t)
	cache := testsupport.TempDir(t)
	workFile := filepath.Join(testsupport.TempDir(t), "go.work")
	repositories := []workspace.Repository{
		{Name: "consumer", Path: consumerRoot, RealPath: consumerRoot, Languages: []string{"go"}},
		{Name: "provider", Path: providerRoot, RealPath: providerRoot, Languages: []string{"go"}},
	}
	index := func() FullReport {
		t.Helper()
		_, report, err := Full(context.Background(), FullOptions{
			Repositories:      repositories,
			SyntheticWorkFile: workFile,
			CacheMode:         CacheOn,
			CacheDirectory:    cache,
		})
		if err != nil {
			t.Fatalf("Full() error = %v", err)
		}
		return report
	}

	index()
	if warm := index(); warm.Cache.Hits != 2 {
		t.Fatalf("cache = %+v, want both modules served from their entries", warm.Cache)
	}

	writeFullFixture(t, filepath.Join(providerRoot, "value.go"), "package provider\n\nconst Value = 42\n\nconst Extra = 1\n")
	if changed := index(); changed.Cache.Hits != 0 {
		t.Fatalf("cache = %+v, want the consumer reanalysed when its provider changed", changed.Cache)
	}
}

// TestFactCacheMissesWhenTheProviderRepositoryChanges covers the input that is
// not a file at all. The same module is provided by a repository under a new
// name, and every cross-repository edge the consumer holds names the old one
// in its target key.
func TestFactCacheMissesWhenTheProviderRepositoryChanges(t *testing.T) {
	consumerRoot, providerRoot := goPair(t)
	cache := testsupport.TempDir(t)
	workFile := filepath.Join(testsupport.TempDir(t), "go.work")
	index := func(providerName string) (facts.Set, FullReport) {
		t.Helper()
		set, report, err := Full(context.Background(), FullOptions{
			Repositories: []workspace.Repository{
				{Name: "consumer", Path: consumerRoot, RealPath: consumerRoot, Languages: []string{"go"}},
				{Name: providerName, Path: providerRoot, RealPath: providerRoot, Languages: []string{"go"}},
			},
			SyntheticWorkFile: workFile,
			CacheMode:         CacheOn,
			CacheDirectory:    cache,
		})
		if err != nil {
			t.Fatalf("Full() error = %v", err)
		}
		return set, report
	}

	before, _ := index("provider")
	after, report := index("vendored-provider")
	if report.Cache.Hits != 0 {
		t.Fatalf("cache = %+v, want the consumer reanalysed when the provider registry changed", report.Cache)
	}
	if countTargets(before, "vendored-provider") != 0 {
		t.Fatalf("the first pass already named the renamed repository")
	}
	if countTargets(after, "vendored-provider") == 0 {
		t.Fatalf("the second pass does not name the renamed repository: the cache served stale keys")
	}
}

func countTargets(set facts.Set, repository string) int {
	count := 0
	for _, edge := range set.Edges {
		if strings.Contains(edge.TargetKey, repository) {
			count++
		}
	}
	return count
}

// TestFactCacheVerifyRejectsAnEntryThatDisagrees is the mode that makes the
// rest believable. A poisoned entry passes every fingerprint check -- the
// world really is unchanged -- and is caught only because the pass compares
// it against the analysis it just ran.
func TestFactCacheVerifyRejectsAnEntryThatDisagrees(t *testing.T) {
	fixture := newCachedFixture(t)
	fixture.index()
	poisonCacheEntry(t, fixture.cache)

	fixture.mode = CacheVerify
	_, _, err := Full(context.Background(), FullOptions{
		Repositories:      []workspace.Repository{fixture.repository},
		SyntheticWorkFile: fixture.workFile,
		CacheMode:         CacheVerify,
		CacheDirectory:    fixture.cache,
	})
	if !errors.Is(err, ErrCacheDiverged) {
		t.Fatalf("Full() error = %v, want %v", err, ErrCacheDiverged)
	}
}

// TestFactCacheVerifyPublishesTheAnalysedFacts keeps verification from being
// the thing that breaks a pass: with an honest entry it changes nothing but
// the counters.
func TestFactCacheVerifyPublishesTheAnalysedFacts(t *testing.T) {
	fixture := newCachedFixture(t)
	cold, _ := fixture.index()

	fixture.mode = CacheVerify
	verified, report := fixture.index()
	if report.Cache.Verified != 1 || report.Cache.Hits != 0 {
		t.Fatalf("cache = %+v, want one verified entry and no hit", report.Cache)
	}
	if encodedFacts(t, cold) != encodedFacts(t, verified) {
		t.Fatalf("verification changed the published facts")
	}
}

// poisonCacheEntry rewrites the stored facts of the only entry, leaving every
// recorded input untouched. It is what a wrong cache looks like from the
// outside: plausible facts nobody can reproduce.
func poisonCacheEntry(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", directory, err)
	}
	poisoned := 0
	for _, file := range entries {
		path := filepath.Join(directory, file.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		var entry cacheEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			t.Fatalf("Unmarshal(%q) error = %v", path, err)
		}
		if len(entry.Set.Symbols) == 0 {
			continue
		}
		entry.Set.Symbols = entry.Set.Symbols[:len(entry.Set.Symbols)-1]
		entry.Set.Edges = nil
		rewritten, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		if err := os.WriteFile(path, rewritten, 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
		poisoned++
	}
	if poisoned == 0 {
		t.Fatalf("no entry to poison in %q", directory)
	}
}

// TestTreeFingerprintSeesEveryKindOfChange pins the primitive the entries rest
// on. A fingerprint that ignores a rename, an addition or a deletion would let
// every test above pass and still serve stale facts.
func TestTreeFingerprintSeesEveryKindOfChange(t *testing.T) {
	root := testsupport.TempDir(t)
	writeFullFixture(t, filepath.Join(root, "a.go"), "package a\n")
	writeFullFixture(t, filepath.Join(root, "nested", "b.ts"), "export const b = 1\n")
	base := treeFingerprint(root)

	if treeFingerprint(root) != base {
		t.Fatalf("fingerprint is not stable across two reads")
	}

	writeFullFixture(t, filepath.Join(root, "README.md"), "documentation is not source\n")
	if treeFingerprint(root) != base {
		t.Fatalf("fingerprint changed for a file no analysis reads")
	}

	// node_modules is an input the lockfile stands for: walking it would
	// cost more than the analysis it saves.
	writeFullFixture(t, filepath.Join(root, "node_modules", "dep", "index.js"), "module.exports = 1\n")
	if treeFingerprint(root) != base {
		t.Fatalf("fingerprint walked node_modules")
	}

	for _, mutation := range []struct {
		name   string
		mutate func()
	}{
		{"content changed", func() {
			writeFullFixture(t, filepath.Join(root, "a.go"), "package a\n\nvar Added = 1\n")
		}},
		{"file added", func() {
			writeFullFixture(t, filepath.Join(root, "c.go"), "package a\n")
		}},
		{"file renamed", func() {
			if err := os.Rename(filepath.Join(root, "c.go"), filepath.Join(root, "d.go")); err != nil {
				t.Fatalf("Rename() error = %v", err)
			}
		}},
		{"file removed", func() {
			if err := os.Remove(filepath.Join(root, "d.go")); err != nil {
				t.Fatalf("Remove() error = %v", err)
			}
		}},
	} {
		previous := treeFingerprint(root)
		mutation.mutate()
		if treeFingerprint(root) == previous {
			t.Fatalf("fingerprint did not see %s", mutation.name)
		}
	}

	if treeFingerprint(filepath.Join(root, "missing")) != "absent" {
		t.Fatalf("a directory that is not there must fingerprint as absent")
	}
}

// TestAnalyzerIdentityFollowsTheGoCommand keeps entries from surviving a
// change in the toolchain that produces them. go/types is linked into this
// binary, but the standard library it checks against and the versions the
// build list selects are the go command's answers.
func TestAnalyzerIdentityFollowsTheGoCommand(t *testing.T) {
	options := FullOptions{TypeScriptWorker: "ladygraph-ts-worker"}
	before := analyzerFingerprint(options)
	t.Setenv("GOFLAGS", "-mod=mod")
	if after := analyzerFingerprint(options); after == before {
		t.Fatalf("analyzer identity ignored the go environment")
	}
}

// TestAnalyzerIdentityFollowsTheWorker covers the other half of the analyser:
// the same command line runs whatever the last build left in dist.
func TestAnalyzerIdentityFollowsTheWorker(t *testing.T) {
	worker := filepath.Join(testsupport.TempDir(t), "worker")
	writeFullFixture(t, worker, "#!/bin/sh\necho first\n")
	if err := os.Chmod(worker, 0o700); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	options := FullOptions{TypeScriptWorker: worker}
	before := analyzerFingerprint(options)

	writeFullFixture(t, worker, "#!/bin/sh\necho second\n")
	if err := os.Chmod(worker, 0o700); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	if after := analyzerFingerprint(options); after == before {
		t.Fatalf("analyzer identity ignored a rebuilt worker")
	}
}

// TestFactCacheNeverStoresAModuleThatCouldNotLoad keeps a failure out of the
// cache. Whether a module loads depends on the module cache, which no
// fingerprint of the source describes: storing the failure would make
// "download the dependencies and index again" a no-op.
func TestFactCacheNeverStoresAModuleThatCouldNotLoad(t *testing.T) {
	root := testsupport.TempDir(t)
	writeFullFixture(t, filepath.Join(root, "go.mod"),
		"module example.com/missing\n\ngo 1.24\n\nrequire example.com/nobody/provides v1.2.3\n")
	writeFullFixture(t, filepath.Join(root, "main.go"),
		"package missing\n\nimport \"example.com/nobody/provides\"\n\nvar _ = provides.Thing\n")
	cache := testsupport.TempDir(t)

	index := func() FullReport {
		t.Helper()
		_, report, err := Full(context.Background(), FullOptions{
			Repositories: []workspace.Repository{
				{Name: "missing", Path: root, RealPath: root, Languages: []string{"go"}},
			},
			SyntheticWorkFile: filepath.Join(testsupport.TempDir(t), "go.work"),
			CacheMode:         CacheOn,
			CacheDirectory:    cache,
		})
		if err != nil {
			t.Fatalf("Full() error = %v", err)
		}
		return report
	}

	if first := index(); first.GoModulesNotLoaded != 1 {
		t.Fatalf("report = %+v, want the module declared as not loaded", first)
	}
	second := index()
	if second.Cache.Hits != 0 {
		t.Fatalf("cache = %+v, want the load retried rather than served", second.Cache)
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("cache holds %d entries, want none for a module that did not load", len(entries))
	}
}

// TestFactCacheRefusesAnUnusableDirectory keeps a misconfiguration from
// turning into a pass that quietly runs slower than it was asked to.
func TestFactCacheRefusesAnUnusableDirectory(t *testing.T) {
	blocked := filepath.Join(testsupport.TempDir(t), "blocked")
	writeFullFixture(t, blocked, "this is a file, not a directory\n")

	fixture := newCachedFixture(t)
	_, _, err := Full(context.Background(), FullOptions{
		Repositories:      []workspace.Repository{fixture.repository},
		SyntheticWorkFile: fixture.workFile,
		CacheMode:         CacheOn,
		CacheDirectory:    filepath.Join(blocked, "cache"),
	})
	if err == nil || !strings.Contains(err.Error(), "fact cache") {
		t.Fatalf("Full() error = %v, want a named fact cache failure", err)
	}
}

// TestLockfilePathsClimbToTheWorkspaceRoot defends the only control the entry
// has over installed dependencies. node_modules is never hashed, so an entry
// that cannot name the lockfile cannot tell that `pnpm install` changed what
// the analysis read -- and in the layout this is written for, one lockfile at
// the workspace root above many registered repositories, a search confined to
// the repository's own root names nothing at all.
func TestLockfilePathsClimbToTheWorkspaceRoot(t *testing.T) {
	root := testsupport.TempDir(t)
	workspace := filepath.Join(root, "kena")
	repository := filepath.Join(workspace, "services", "api-music")
	lockfile := filepath.Join(workspace, "pnpm-lock.yaml")
	writeFullFixture(t, lockfile, "lockfileVersion: '9.0'\n")

	paths := lockfilePaths(repository)
	if len(paths) == 0 {
		t.Fatal("lockfilePaths() named nothing")
	}
	found := false
	for _, candidate := range paths {
		if candidate == lockfile {
			found = true
		}
	}
	if !found {
		t.Fatalf("the workspace lockfile %s is not among %v", lockfile, paths)
	}

	// Every level is named whether or not a file sits there today: the entry
	// records what to re-measure, so a lockfile added closer to the
	// repository afterwards has to invalidate it as well.
	nearer := filepath.Join(repository, "pnpm-lock.yaml")
	if !slices.Contains(paths, nearer) {
		t.Errorf("the repository's own lockfile path %s is not among %v", nearer, paths)
	}

	// The chain is finite and reaches the filesystem root exactly once.
	if seen := len(paths); seen != len(slices.Compact(slices.Clone(paths))) {
		t.Errorf("lockfilePaths() repeats a candidate: %v", paths)
	}
	if lockfilePaths("  ") != nil {
		t.Error("a repository with no path named a lockfile")
	}
}
