package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/executable"
	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
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

func TestFullWithRepositoriesDoesNotAdmitFactsBeforeCommit(t *testing.T) {
	fixture := newCachedFixture(t)
	options := FullOptions{
		Repositories:      []workspace.Repository{fixture.repository},
		SyntheticWorkFile: fixture.workFile,
		CacheMode:         fixture.mode,
		CacheDirectory:    fixture.cache,
	}
	cold, report, err := FullWithRepositories(context.Background(), options, []workspace.Repository{fixture.repository})
	if err != nil {
		t.Fatalf("FullWithRepositories(root=%q cache=%q mode=%q) error = %v",
			fixture.root, fixture.cache, fixture.mode, err)
	}
	if report.Cache.Hits != 0 || report.Cache.Misses != 1 {
		t.Fatalf("cold FullWithRepositories(root=%q cache=%q mode=%q) cache = %+v, want one miss",
			fixture.root, fixture.cache, fixture.mode, report.Cache)
	}

	// A second pass before the first one commits must miss through the cache
	// lookup itself. This keeps the regression about admission, not about the
	// cache directory's current files.
	uncommitted, uncommittedReport, err := FullWithRepositories(context.Background(), options, []workspace.Repository{fixture.repository})
	if err != nil {
		t.Fatalf("uncommitted FullWithRepositories(root=%q cache=%q mode=%q) error = %v",
			fixture.root, fixture.cache, fixture.mode, err)
	}
	if uncommittedReport.Cache.Hits != 0 || uncommittedReport.Cache.Misses != 1 {
		t.Fatalf("uncommitted FullWithRepositories(root=%q cache=%q mode=%q) cache = %+v, want one miss",
			fixture.root, fixture.cache, fixture.mode, uncommittedReport.Cache)
	}
	if !reflect.DeepEqual(cold, uncommitted) {
		t.Fatalf("uncommitted FullWithRepositories(root=%q cache=%q mode=%q) returned different facts",
			fixture.root, fixture.cache, fixture.mode)
	}

	report.CommitCache()
	warm, warmReport, err := FullWithRepositories(context.Background(), options, []workspace.Repository{fixture.repository})
	if err != nil {
		t.Fatalf("committed FullWithRepositories(root=%q cache=%q mode=%q) error = %v",
			fixture.root, fixture.cache, fixture.mode, err)
	}
	if warmReport.Cache.Hits != 1 || warmReport.Cache.Misses != 0 {
		t.Fatalf("committed FullWithRepositories(root=%q cache=%q mode=%q) cache = %+v, want one hit",
			fixture.root, fixture.cache, fixture.mode, warmReport.Cache)
	}
	if !reflect.DeepEqual(cold, warm) {
		t.Fatalf("committed FullWithRepositories(root=%q cache=%q mode=%q) returned different facts",
			fixture.root, fixture.cache, fixture.mode)
	}
	if len(warm.Symbols) == 0 {
		t.Fatalf("FullWithRepositories(root=%q cache=%q mode=%q) returned no facts",
			fixture.root, fixture.cache, fixture.mode)
	}
}

func TestFactCacheStagesEntriesOnDiskUntilCommit(t *testing.T) {
	fixture := newCachedFixture(t)
	_, report, err := FullWithRepositories(context.Background(), FullOptions{
		Repositories:      []workspace.Repository{fixture.repository},
		SyntheticWorkFile: fixture.workFile,
		CacheMode:         fixture.mode,
		CacheDirectory:    fixture.cache,
	}, []workspace.Repository{fixture.repository})
	if err != nil {
		t.Fatalf("FullWithRepositories(cache=%q, repository=%q) error = %v", fixture.cache, fixture.repository.Path, err)
	}

	entries, err := os.ReadDir(fixture.cache)
	if err != nil {
		t.Fatalf("read uncommitted cache %q: %v", fixture.cache, err)
	}
	if len(entries) != 1 || !entries[0].IsDir() || !strings.HasPrefix(entries[0].Name(), ".staging-") {
		t.Fatalf("uncommitted cache %q entries = %#v, want one staging directory and no admitted entry", fixture.cache, entries)
	}
	staged, err := os.ReadDir(filepath.Join(fixture.cache, entries[0].Name()))
	if err != nil {
		t.Fatalf("read staged cache %q entry %q: %v", fixture.cache, entries[0].Name(), err)
	}
	if len(staged) != 1 || staged[0].IsDir() || filepath.Ext(staged[0].Name()) != ".json" {
		t.Fatalf("staged cache %q entries = %#v, want one JSON file", fixture.cache, staged)
	}

	report.CommitCache()
	entries, err = os.ReadDir(fixture.cache)
	if err != nil {
		t.Fatalf("read committed cache %q: %v", fixture.cache, err)
	}
	if len(entries) != 1 || entries[0].IsDir() || filepath.Ext(entries[0].Name()) != ".json" {
		t.Fatalf("committed cache entries = %#v, want one admitted JSON file and no staging directory", entries)
	}
}

func TestFactCacheDiscardRemovesStagedEntries(t *testing.T) {
	fixture := newCachedFixture(t)
	_, report, err := FullWithRepositories(context.Background(), FullOptions{
		Repositories:      []workspace.Repository{fixture.repository},
		SyntheticWorkFile: fixture.workFile,
		CacheMode:         fixture.mode,
		CacheDirectory:    fixture.cache,
	}, []workspace.Repository{fixture.repository})
	if err != nil {
		t.Fatalf("FullWithRepositories(cache=%q, repository=%q) error = %v",
			fixture.cache, fixture.repository.Path, err)
	}

	report.DiscardCache()
	entries, err := os.ReadDir(fixture.cache)
	if err != nil {
		t.Fatalf("read discarded cache %q: %v", fixture.cache, err)
	}
	if len(entries) != 0 {
		t.Fatalf("discarded cache entries = %#v, want none", entries)
	}
	report.CommitCache()
	entries, err = os.ReadDir(fixture.cache)
	if err != nil {
		t.Fatalf("read cache %q after late commit: %v", fixture.cache, err)
	}
	if len(entries) != 0 {
		t.Fatalf("cache entries after late commit = %#v, want none", entries)
	}
}

func (fixture *cachedFixture) index() (facts.Set, FullReport) {
	return fixture.indexProfile("")
}

func (fixture *cachedFixture) indexProfile(profile string) (facts.Set, FullReport) {
	fixture.t.Helper()
	set, report, err := Full(context.Background(), FullOptions{
		Profile:           profile,
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

func (fixture *cachedFixture) indexResolver(resolver string) (facts.Set, FullReport) {
	fixture.t.Helper()
	set, report, err := Full(context.Background(), FullOptions{
		ResolverVersion:   resolver,
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

func TestFactCacheKeepsAlternatingProfilesWarm(t *testing.T) {
	fixture := newCachedFixture(t)
	for index, profile := range []string{"backend", "frontend", "backend", "frontend"} {
		_, report := fixture.indexProfile(profile)
		if index == 0 && (report.Cache.Hits != 0 || report.Cache.Misses != 1) {
			t.Fatalf("first %s cache = %+v, want one miss", profile, report.Cache)
		}
		if index > 0 && (report.Cache.Hits != 1 || report.Cache.Misses != 0) {
			t.Fatalf("warm %s cache = %+v, want one hit", profile, report.Cache)
		}
	}
}

func TestFactCacheMissesWhenResolverChanges(t *testing.T) {
	fixture := newCachedFixture(t)
	fixture.indexResolver("resolver-a")

	if _, report := fixture.indexResolver("resolver-b"); report.Cache.Hits != 0 || report.Cache.Misses != 1 || report.Cache.Refusals[CacheRefusalAnalyzer] != 1 {
		t.Fatalf("changed resolver cache = %+v, want one analyzer refusal and one miss", report.Cache)
	}
	if _, report := fixture.indexResolver("resolver-b"); report.Cache.Hits != 1 || report.Cache.Misses != 0 {
		t.Fatalf("same resolver cache = %+v, want the new resolver entry", report.Cache)
	}
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

	// newCachedFixture gives this test a private worktree. Rewriting that
	// worktree in place is intentional: sourceIdentity includes RealPath, so
	// separate copies would have different cache identities and could not prove
	// that restoring the content reuses the original address.
	writeFullFixture(t, filepath.Join(fixture.root, "fixture.go"), `package fixture

func Greeting() string { return "hello" }

// Farewell is the definition the second pass must discover.
func Farewell() string { return "bye" }
`)

	warm, report := fixture.index()
	if report.Cache.Hits != 0 {
		t.Fatalf("cache = %+v, want no hit after the source changed", report.Cache)
	}
	if report.Cache.Refusals[CacheRefusalNoEntry] != 1 {
		t.Fatalf("cache refusals after editing %q = %+v, want one content-address miss", filepath.Join(fixture.root, "fixture.go"), report.Cache.Refusals)
	}
	if len(warm.Symbols) <= len(cold.Symbols) {
		t.Fatalf("symbols = %d, want more than the %d of the first pass",
			len(warm.Symbols), len(cold.Symbols))
	}

	writeFullFixture(t, filepath.Join(fixture.root, "fixture.go"), `package fixture

// Greeting is the definition every assertion below counts.
func Greeting() string { return "hello" }
`)
	restored, report := fixture.index()
	if report.Cache.Hits != 1 || report.Cache.Misses != 0 {
		t.Fatalf("restored source cache = %+v, want the previous content address to be warm", report.Cache)
	}
	if encodedFacts(t, restored) != encodedFacts(t, cold) {
		t.Fatalf("restored source facts for %q differ from the original facts", filepath.Join(fixture.root, "fixture.go"))
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
	if restored, report := index("provider"); report.Cache.Hits != 2 || report.Cache.Misses != 0 {
		t.Fatalf("restored cache = %+v, want the original registry context to remain warm", report.Cache)
	} else if countTargets(restored, "vendored-provider") != 0 {
		t.Fatalf("restored context retained the renamed provider edge")
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

func TestFactCacheReportsRefusalReasons(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
		want   CacheRefusalReason
		valid  bool
	}{
		{name: "missing", mutate: func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatalf("Remove() error = %v", err)
			}
		}, want: CacheRefusalNoEntry},
		{name: "unreadable", mutate: func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatalf("Remove() error = %v", err)
			}
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatalf("Mkdir() error = %v", err)
			}
		}, want: CacheRefusalUnreadable},
		{name: "malformed", mutate: func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
		}, want: CacheRefusalMalformed},
		{name: "incompatible", mutate: mutateCacheEntry(func(entry *cacheEntry) {
			entry.Version = cacheEntryVersion - 1
		}), want: CacheRefusalIncompatible},
		{name: "analyzer", mutate: mutateCacheEntry(func(entry *cacheEntry) {
			entry.Analyzer = "other-analyzer"
		}), want: CacheRefusalAnalyzer},
		{name: "local address", mutate: mutateCacheEntry(func(entry *cacheEntry) {
			entry.LocalAddress = "other-local"
		}), want: CacheRefusalLocalContent},
		{name: "dependency", mutate: mutateCacheEntry(func(entry *cacheEntry) {
			entry.Inputs = append(entry.Inputs, cacheInput{Kind: inputProvider, Name: "provider", Fingerprint: "present"})
		}), want: CacheRefusalDependency},
		{name: "registry", mutate: mutateCacheEntry(func(entry *cacheEntry) {
			for index := range entry.Inputs {
				if entry.Inputs[index].Kind == inputRegistry {
					entry.Inputs[index].Fingerprint = "stale"
					return
				}
			}
		}), want: CacheRefusalRegistry},
		{name: "local input", mutate: mutateCacheEntry(func(entry *cacheEntry) {
			entry.Inputs = append(entry.Inputs, cacheInput{Kind: inputFile, Name: "missing", Fingerprint: "present"})
		}), want: CacheRefusalLocalContent},
		{name: "valid", valid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCachedFixture(t)
			if _, report := fixture.index(); report.Cache.Misses != 1 {
				t.Fatalf("setup cache %q = %+v, want one miss", test.name, report.Cache)
			}
			path := cacheEntryForFixture(t, fixture)
			if test.mutate != nil {
				test.mutate(t, path)
			}
			_, report := fixture.index()
			if test.valid {
				if report.Cache.Hits != 1 || len(report.Cache.Refusals) != 0 {
					t.Fatalf("valid cache %q = %+v, want one hit and no refusals", test.name, report.Cache)
				}
				return
			}
			if report.Cache.Hits != 0 || report.Cache.Misses != 1 || len(report.Cache.Refusals) != 1 || report.Cache.Refusals[test.want] != 1 {
				t.Fatalf("refused cache %q = %+v, want one miss, no hit, and only one %q refusal", test.name, report.Cache, test.want)
			}
		})
	}
}

func cacheEntryForFixture(t *testing.T, fixture *cachedFixture) string {
	t.Helper()
	entries, err := os.ReadDir(fixture.cache)
	if err != nil {
		t.Fatalf("ReadDir(%q) for %q error = %v", fixture.cache, t.Name(), err)
	}
	unitPrefix := "go\x00" + fixture.repository.Name + "\x00" + sourceIdentity(fixture.repository) + "\x00"
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(fixture.cache, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cached cacheEntry
		if err := json.Unmarshal(data, &cached); err != nil {
			continue
		}
		if cached.Version == cacheEntryVersion && strings.HasPrefix(cached.Unit, unitPrefix) {
			return path
		}
	}
	t.Fatalf("cache entry for unit prefix %q not found in %q for %q", unitPrefix, fixture.cache, t.Name())
	return ""
}

func mutateCacheEntry(mutate func(*cacheEntry)) func(*testing.T, string) {
	return func(t *testing.T, path string) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) for %q error = %v", path, t.Name(), err)
		}
		var entry cacheEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			t.Fatalf("Unmarshal(%q) for %q error = %v", path, t.Name(), err)
		}
		mutate(&entry)
		data, err = json.Marshal(entry)
		if err != nil {
			t.Fatalf("Marshal(%q) for %q error = %v", path, t.Name(), err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("WriteFile(%q) for %q error = %v", path, t.Name(), err)
		}
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

// TestAnalyzerFingerprintWatchesThePythonProducerThatRuns pins what the cache
// is for. The fingerprint used to name `python-worker/index.py` by hand, so the
// exact adapter was unwatched: editing it changed no key, a rebuild reused the
// previous producer's facts, and the published generation was one the current
// code would not produce.
func TestAnalyzerFingerprintWatchesThePythonProducerThatRuns(t *testing.T) {
	root := testsupport.TempDir(t)
	fallback := filepath.Join(root, "python-worker", "index.py")
	exact := filepath.Join(root, "python-worker", "pyright_index.py")
	writeFullFixture(t, fallback, "print('fallback')\n")
	writeFullFixture(t, exact, "print('exact')\n")

	options := FullOptions{
		WorkingDirectory:   root,
		PythonIndexer:      "kivgraph-python-worker",
		PythonAnalyzer:     "kivgraph-python-pyright",
		PythonAnalyzerMode: "exact",
		PythonPath:         "python3",
	}
	base := analyzerFingerprint(options)
	if analyzerFingerprint(options) != base {
		t.Fatal("fingerprint is not stable across two reads")
	}

	// The mode decides which producer runs, so it decides which file is
	// watched: in exact mode the fallback is not the one whose facts land.
	writeFullFixture(t, fallback, "print('fallback changed')\n")
	if analyzerFingerprint(options) != base {
		t.Fatal("fingerprint changed for a producer this mode does not run")
	}

	writeFullFixture(t, exact, "print('exact changed')\n")
	if analyzerFingerprint(options) == base {
		t.Fatal("fingerprint ignored a change to the producer this pass runs")
	}

	// And the other way round: in fallback mode the bundled worker is the one
	// that decides, and the exact adapter is not.
	options.PythonAnalyzerMode = "fallback"
	base = analyzerFingerprint(options)
	writeFullFixture(t, exact, "print('exact changed again')\n")
	if analyzerFingerprint(options) != base {
		t.Fatal("fallback mode watched the exact adapter")
	}
	writeFullFixture(t, fallback, "print('fallback changed again')\n")
	if analyzerFingerprint(options) == base {
		t.Fatal("fallback mode ignored a change to the worker it runs")
	}
}

func TestObservedAnalyzerFingerprintIsStable(t *testing.T) {
	worker := "kivgraph-ts-worker"
	first, err := AnalyzerFingerprint(FullOptions{TypeScriptWorker: worker})
	if err != nil {
		t.Fatalf("AnalyzerFingerprint(TypeScriptWorker=%q): %v", worker, err)
	}
	second, err := AnalyzerFingerprint(FullOptions{TypeScriptWorker: worker})
	if err != nil {
		t.Fatalf("second AnalyzerFingerprint(TypeScriptWorker=%q): %v", worker, err)
	}
	if first != second {
		t.Fatalf("AnalyzerFingerprint() = %q then %q, want stable observations", first, second)
	}
}

// TestAnalyzerIdentityFollowsTheGoCommand keeps entries from surviving a
// change in the toolchain that produces them. go/types is linked into this
// binary, but the standard library it checks against and the versions the
// build list selects are the go command's answers.
func TestAnalyzerIdentityFollowsTheGoCommand(t *testing.T) {
	options := FullOptions{TypeScriptWorker: "kivgraph-ts-worker"}
	before := analyzerFingerprint(options)
	t.Setenv("GOFLAGS", "-mod=mod")
	if after := analyzerFingerprint(options); after == before {
		t.Fatalf("analyzer identity ignored the go environment")
	}
}

// TestAnalyzerIdentityFollowsTheWorker covers the other half of the analyser:
// the same command line runs whatever the last build left in dist.
func TestAnalyzerIdentityFollowsTheWorker(t *testing.T) {
	// The name carries the platform's program suffix. factsCommand resolves a
	// configured worker with exec.LookPath and refuses one it cannot run, and
	// on Windows a file with no extension is not runnable -- so the fixture
	// took the "worker command is not executable" path, the fingerprint fell
	// back to the constant "unavailable", and the test read a fixture Windows
	// cannot run as an analyser identity that ignores its worker.
	worker := filepath.Join(testsupport.TempDir(t), executable.Name("worker"))
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
	workspace := filepath.Join(root, "workspace")
	repository := filepath.Join(workspace, "services", "go-svc-b")
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
