package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/workspace"
)

// An entry is dropped when nothing has used it for thirty days. It was dropped
// thirty days after it was written instead: a hit did not count as a use, so
// the units that never change -- the ones a cache pays for -- were analysed
// from scratch once a month, in the pass right after the one that served them.
func TestFactCacheKeepsAnEntryThatIsStillServed(t *testing.T) {
	fixture := newCachedFixture(t)
	options := FullOptions{
		Repositories:      []workspace.Repository{fixture.repository},
		SyntheticWorkFile: fixture.workFile,
		CacheMode:         fixture.mode,
		CacheDirectory:    fixture.cache,
	}
	pass := func(label string) CacheReport {
		t.Helper()
		_, report, err := FullWithRepositories(context.Background(), options, []workspace.Repository{fixture.repository})
		if err != nil {
			t.Fatalf("%s pass error = %v", label, err)
		}
		report.CommitCache()
		return report.Cache
	}
	if cold := pass("cold"); cold.Misses != 1 {
		t.Fatalf("cold pass cache = %+v, want one miss", cold)
	}

	entries, err := filepath.Glob(filepath.Join(fixture.cache, "*"+cacheEntrySuffix))
	if err != nil || len(entries) != 1 {
		t.Fatalf("admitted entries = %v, %v, want one", entries, err)
	}
	written := time.Now().Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(entries[0], written, written); err != nil {
		t.Fatal(err)
	}

	// Served, and the same pass then prunes what is older than thirty days.
	if served := pass("served"); served.Hits != 1 {
		t.Fatalf("pass over a forty-day-old entry cache = %+v, want it served", served)
	}
	if again := pass("after the prune"); again.Hits != 1 || again.Misses != 0 {
		t.Fatalf("pass after the entry was served cache = %+v, want a hit: serving it is using it", again)
	}

	// Left alone, it still goes: nothing used it.
	if err := os.Chtimes(entries[0], written, written); err != nil {
		t.Fatal(err)
	}
	(&factCache{directory: fixture.cache, mode: CacheOn}).prune(30 * 24 * time.Hour)
	if _, err := os.Stat(entries[0]); !os.IsNotExist(err) {
		t.Fatalf("an entry nothing used for forty days survived the prune: %v", err)
	}
}
