package indexer

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// indexing.fact_cache_path is any directory the user names, and nothing makes
// it a dedicated one. prune may expire only what the cache itself wrote: an old
// file of somebody else's is not a stale entry.
func TestPruneRemovesOnlyWhatTheCacheWrote(t *testing.T) {
	directory := testsupport.TempDir(t)
	entry := strings.Repeat("a", 64) + cacheEntrySuffix
	fresh := strings.Repeat("b", 64) + cacheEntrySuffix
	files := []string{
		entry, fresh, ".entry-123.tmp",
		"notes.txt", "report.json", strings.Repeat("a", 63) + cacheEntrySuffix, strings.Repeat("G", 64) + cacheEntrySuffix,
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	directories := []string{".staging-1", "empty-directory", ".staging-file-holder"}
	for _, name := range directories {
		if err := os.Mkdir(filepath.Join(directory, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// A regular file may not borrow the staging prefix to be removed.
	if err := os.WriteFile(filepath.Join(directory, ".staging-notes"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-31 * 24 * time.Hour)
	for _, name := range append(append([]string{".staging-notes"}, files...), directories...) {
		if name == fresh {
			continue
		}
		if err := os.Chtimes(filepath.Join(directory, name), old, old); err != nil {
			t.Fatal(err)
		}
	}

	(&factCache{directory: directory, mode: CacheOn}).prune(30 * 24 * time.Hour)

	remaining, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, item := range remaining {
		got = append(got, item.Name())
	}
	want := []string{
		".staging-notes", strings.Repeat("G", 64) + cacheEntrySuffix, strings.Repeat("a", 63) + cacheEntrySuffix,
		fresh, "empty-directory", "notes.txt", "report.json",
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("after prune the directory holds\n%q\nwant\n%q", got, want)
	}
}

// An entry of the JSON format can never be read again, and a directory of them is
// most of a gigabyte: they go at once, fresh or not. A file that only looks like
// one -- not a digest, not lower case -- is not the cache's to remove.
func TestPruneRemovesTheEntriesOfTheRetiredFormatAtOnce(t *testing.T) {
	directory := testsupport.TempDir(t)
	legacy := strings.Repeat("c", 64) + legacyCacheEntrySuffix
	keep := []string{"report.json", strings.Repeat("c", 63) + legacyCacheEntrySuffix, strings.Repeat("G", 64) + legacyCacheEntrySuffix}
	for _, name := range append([]string{legacy}, keep...) {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	(&factCache{directory: directory, mode: CacheOn}).prune(30 * 24 * time.Hour)

	if _, err := os.Stat(filepath.Join(directory, legacy)); !os.IsNotExist(err) {
		t.Fatalf("a fresh entry of the retired format survived the prune: %v", err)
	}
	for _, name := range keep {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Errorf("%s was removed, and it is not an entry the cache wrote: %v", name, err)
		}
	}
}
