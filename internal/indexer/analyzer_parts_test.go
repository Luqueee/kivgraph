package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Luqueee/kivgraph/internal/workspace"
)

// A refused entry has to say where the analyzer moved, or "the analyzer changed"
// is all anybody learns from a cold pass. The parts are a diagnosis: the value is
// still the hash of the same bytes in the same order, and an entry is served or
// refused by the value alone.
func observedParts(t *testing.T, options FullOptions) (string, map[string]string) {
	t.Helper()
	fingerprint, parts, err := observeAnalyzer(options)
	if err != nil {
		t.Fatalf("observeAnalyzer() error = %v", err)
	}
	return fingerprint, parts
}

func TestAnalyzerPartsNameTheOptionThatMoved(t *testing.T) {
	base := FullOptions{TypeScriptWorker: "kivgraph-ts-worker", ResolverVersion: "one"}
	baseline, baselineParts := observedParts(t, base)
	again, againParts := observedParts(t, base)
	if again != baseline || !maps.Equal(againParts, baselineParts) {
		t.Fatal("the fingerprint and its parts are not stable for the same options")
	}

	for name, test := range map[string]struct {
		change func(*FullOptions)
		want   []string
	}{
		"the resolver version": {func(o *FullOptions) { o.ResolverVersion = "two" }, []string{"resolver"}},
		"a Go build tag":       {func(o *FullOptions) { o.GoBuildTags = []string{"integration"} }, []string{"go"}},
		"Go tests":             {func(o *FullOptions) { o.IncludeTests = true }, []string{"go"}},
		"unclaimed TypeScript": {func(o *FullOptions) { o.TypeScriptIncludeUnclaimedSources = true }, []string{"typescript"}},
		"the worker command":   {func(o *FullOptions) { o.TypeScriptWorker = "another-worker" }, []string{"typescript"}},
		"the Python path":      {func(o *FullOptions) { o.PythonPath = "python3.14" }, []string{"python"}},
		"the Dart SDK":         {func(o *FullOptions) { o.DartSDKPath = "/sdk" }, []string{"dart"}},
		"the Java build tool":  {func(o *FullOptions) { o.JavaBuildTool = "gradle" }, []string{"java"}},
		"the C# project":       {func(o *FullOptions) { o.CSharpProject = "app.csproj" }, []string{"csharp"}},
		"a Rust feature":       {func(o *FullOptions) { o.RustFeatures = []string{"serde"} }, []string{"rust"}},
		"two languages at once": {func(o *FullOptions) {
			o.RustFeatures = []string{"serde"}
			o.GoBuildTags = []string{"integration"}
		}, []string{"go", "rust"}},
	} {
		t.Run(name, func(t *testing.T) {
			options := base
			test.change(&options)
			fingerprint, parts := observedParts(t, options)
			if fingerprint == baseline {
				t.Fatalf("changing %s left the fingerprint as it was", name)
			}
			if got := changedAnalyzerParts(baselineParts, parts); !slices.Equal(got, test.want) {
				t.Fatalf("changing %s moved the parts %q, want %q", name, got, test.want)
			}
		})
	}
}

func TestChangedAnalyzerPartsSaysNothingItCannotKnow(t *testing.T) {
	parts := map[string]string{"go": "a", "rust": "b"}
	for name, test := range map[string]struct {
		recorded, current map[string]string
		want              []string
	}{
		"an entry written before the parts were kept": {nil, parts, []string{"unrecorded"}},
		"a value that was not computed here":          {parts, nil, []string{"unrecorded"}},
		"neither side has parts":                      {nil, nil, []string{"unrecorded"}},
		// The values differed or the cache would not be here, so two equal parts
		// are a gap in the record and not a reason to say nothing.
		"equal parts under different values": {parts, maps.Clone(parts), []string{"unrecorded"}},
		"a part the entry never had":         {map[string]string{"go": "a"}, parts, []string{"rust"}},
		"a part the pass no longer has":      {parts, map[string]string{"go": "a"}, []string{"rust"}},
		"a part with another value":          {parts, map[string]string{"go": "a", "rust": "c"}, []string{"rust"}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := changedAnalyzerParts(test.recorded, test.current); !slices.Equal(got, test.want) {
				t.Fatalf("changedAnalyzerParts() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAnalyzerChangedNoteIsEmptyUnlessAnEntryWasRefusedForIt(t *testing.T) {
	for name, test := range map[string]struct {
		changed map[string]int
		want    string
	}{
		"nothing refused":     {nil, ""},
		"an empty record":     {map[string]int{}, ""},
		"one part":            {map[string]int{"binary": 39}, "binary:39"},
		"several, in order":   {map[string]int{"rust": 1, "binary": 39, "go": 2}, "binary:39,go:2,rust:1"},
		"an unrecorded entry": {map[string]int{"unrecorded": 4}, "unrecorded:4"},
	} {
		if got := (CacheReport{AnalyzerChanged: test.changed}).AnalyzerChangedNote(); got != test.want {
			t.Errorf("%s: AnalyzerChangedNote() = %q, want %q", name, got, test.want)
		}
	}
}

// The memory of parts is by value and bounded: a process that computes the
// fingerprint again and again, a watcher, does not collect them.
func TestRememberedAnalyzerPartsAreBounded(t *testing.T) {
	first := "bounded-fingerprint-0"
	for index := range recentAnalyzerPartsKept + 1 {
		rememberAnalyzerParts(fmt.Sprintf("bounded-fingerprint-%d", index), map[string]string{"go": fmt.Sprint(index)})
	}
	if analyzerPartsOf(first) != nil {
		t.Fatal("the oldest fingerprint is still remembered past the bound")
	}
	last := fmt.Sprintf("bounded-fingerprint-%d", recentAnalyzerPartsKept)
	if got := analyzerPartsOf(last); got["go"] != fmt.Sprint(recentAnalyzerPartsKept) {
		t.Fatalf("the newest fingerprint is not remembered: %v", got)
	}
	if analyzerPartsOf("never computed here") != nil {
		t.Fatal("parts were invented for a fingerprint nobody computed")
	}
}

// The report of a pass says which part of the analyzer refused its entries, for
// entries that recorded their parts and for the ones that did not.
func TestCacheReportNamesThePartsOfTheAnalyzerThatRefusedEntries(t *testing.T) {
	fixture := newCachedFixture(t)
	index := func(resolver string, tags ...string) FullReport {
		t.Helper()
		_, report, err := Full(context.Background(), FullOptions{
			ResolverVersion:   resolver,
			GoBuildTags:       tags,
			Repositories:      []workspace.Repository{fixture.repository},
			SyntheticWorkFile: fixture.workFile,
			CacheMode:         fixture.mode,
			CacheDirectory:    fixture.cache,
		})
		if err != nil {
			t.Fatalf("Full(resolver=%q tags=%q) error = %v", resolver, tags, err)
		}
		return report
	}

	if report := index("one"); len(report.Cache.AnalyzerChanged) != 0 {
		t.Fatalf("cold pass AnalyzerChanged = %v, want none: nothing was refused", report.Cache.AnalyzerChanged)
	}
	if report := index("one"); report.Cache.Hits != 1 || len(report.Cache.AnalyzerChanged) != 0 {
		t.Fatalf("warm pass cache = %+v, want a hit and nothing refused", report.Cache)
	}
	if got := index("two").Cache.AnalyzerChanged; !maps.Equal(got, map[string]int{"resolver": 1}) {
		t.Fatalf("after a resolver change AnalyzerChanged = %v, want only the resolver", got)
	}
	if got := index("three", "integration").Cache.AnalyzerChanged; !maps.Equal(got, map[string]int{"resolver": 1, "go": 1}) {
		t.Fatalf("after a resolver change and a build tag AnalyzerChanged = %v, want both", got)
	}

	// An entry written before the parts were kept can only be said to differ.
	entries, err := filepath.Glob(filepath.Join(fixture.cache, "*.json"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("no entry to strip: %v %v", entries, err)
	}
	for _, path := range entries {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var entry map[string]any
		if err := json.Unmarshal(content, &entry); err != nil {
			t.Fatal(err)
		}
		if _, recorded := entry["analyzer_parts"]; !recorded {
			t.Fatalf("%s kept no parts, so there is nothing to strip", filepath.Base(path))
		}
		delete(entry, "analyzer_parts")
		stripped, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, stripped, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := index("four", "integration").Cache.AnalyzerChanged; !maps.Equal(got, map[string]int{"unrecorded": 1}) {
		t.Fatalf("an entry without parts AnalyzerChanged = %v, want it unrecorded", got)
	}
}
