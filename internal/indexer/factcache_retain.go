package indexer

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// cacheEntriesKept is how many entries of one unit the cache keeps.
//
// The address of an entry holds the content of the unit, so editing a file writes
// a new entry and leaves the old one where it was, and age alone expires it: an
// edit of one TypeScript package added `105` to `115 MB` to the cache of the
// six-repository corpus, measured, and nothing took it away for thirty days. Three
// are the one in use, the one before it and one more, which is what going back to
// another branch or reverting an edit needs.
const cacheEntriesKept = 3

// cacheHeaderBytes is how much of an entry is read to learn which unit it is
// about. The unit comes second in an entry, after the version, so the facts that
// make an entry large are never read.
const cacheHeaderBytes = 4096

var cacheUnitHeader = regexp.MustCompile(`"unit":("(?:[^"\\]|\\.)*")`)

// retain drops the entries of every unit beyond the newest keep, the entries the
// pass has just admitted among the newest. Newest is the last time an entry was
// served or written, which is what a hit sets.
//
// The identity of a unit includes the resolution it was analysed in, so the
// profiles that share a directory are counted apart. An entry whose header cannot
// be read is not counted and not removed: age is the only rule for it.
func (cache *factCache) retain(keep int, admitted map[string]struct{}) {
	if cache == nil || !cache.enabled() || keep < 1 {
		return
	}
	type entry struct {
		path     string
		modified time.Time
	}
	entries, err := os.ReadDir(cache.directory)
	if err != nil {
		return
	}
	units := make(map[string][]entry)
	for _, item := range entries {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".json") || !isCacheFileName(item.Name()) {
			continue
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(cache.directory, item.Name())
		unit, ok := readEntryUnit(path)
		if !ok {
			continue
		}
		units[unit] = append(units[unit], entry{path: path, modified: info.ModTime()})
	}
	for _, versions := range units {
		if len(versions) <= keep {
			continue
		}
		sort.Slice(versions, func(left, right int) bool {
			return versions[left].modified.After(versions[right].modified)
		})
		for _, stale := range versions[keep:] {
			if _, mine := admitted[stale.path]; mine {
				continue
			}
			_ = os.Remove(stale.path)
		}
	}
}

// readEntryUnit answers the unit an entry is about from its first bytes.
func readEntryUnit(path string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	header := make([]byte, cacheHeaderBytes)
	read, err := io.ReadFull(file, header)
	if err != nil && read == 0 {
		return "", false
	}
	match := cacheUnitHeader.FindSubmatch(header[:read])
	if match == nil {
		return "", false
	}
	var unit string
	if json.Unmarshal(match[1], &unit) != nil || unit == "" {
		return "", false
	}
	return unit, true
}
