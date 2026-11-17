package indexer

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

// cacheEntriesPerUnitKept is how many entries of one unit the cache keeps in
// all, whatever resolution they were analysed in.
//
// The identity of a unit includes its resolution, the map of providers it was
// analysed against, and the registry of TypeScript packages includes their
// versions. Raising the version of one package gave every TypeScript unit a new
// identity: the cache of the six-repository corpus gained `25` entries and
// `164 MB` each time, and each old identity kept its own entries for thirty days
// because the rule above counts an identity apart. Six are two or three
// resolutions that are in use together, the profiles that share a directory, with
// two contents each.
const cacheEntriesPerUnitKept = 6

// cacheResolutionSeparator divides the unit from the resolution in the identity of
// an entry. What is before it is the unit, the same across resolutions.
const cacheResolutionSeparator = "\x00resolution\x00"

// logicalUnit answers the unit of an identity, without its resolution.
func logicalUnit(identity string) string {
	unit, _, _ := strings.Cut(identity, cacheResolutionSeparator)
	return unit
}

// retain drops the entries of every unit beyond what it keeps: first the newest
// keep of each identity, then the newest perUnit of each unit across its
// resolutions. The entries the pass has just admitted are never removed. Newest is
// the last time an entry was served or written, which is what a hit sets.
//
// The identity of a unit includes the resolution it was analysed in, so the
// profiles that share a directory are counted apart by the first rule, and the
// second bounds how many of them a unit can leave behind. perUnit is never below
// keep: a unit keeps at least what one identity keeps. An entry whose header cannot
// be read is not counted and not removed: age is the only rule for it.
func (cache *factCache) retain(keep, perUnit int, admitted map[string]struct{}) {
	if cache == nil || !cache.enabled() || keep < 1 {
		return
	}
	perUnit = max(perUnit, keep)
	type entry struct {
		path     string
		modified time.Time
	}
	entries, err := os.ReadDir(cache.directory)
	if err != nil {
		return
	}
	identities := make(map[string][]entry)
	for _, item := range entries {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".json") || !isCacheFileName(item.Name()) {
			continue
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(cache.directory, item.Name())
		identity, ok := readEntryUnit(path)
		if !ok {
			continue
		}
		identities[identity] = append(identities[identity], entry{path: path, modified: info.ModTime()})
	}
	newestFirst := func(versions []entry) {
		sort.Slice(versions, func(left, right int) bool {
			return versions[left].modified.After(versions[right].modified)
		})
	}
	// drop removes what is beyond the first count of versions, and answers what
	// is left: the first count and the entries that must stay.
	drop := func(versions []entry, count int) []entry {
		if len(versions) <= count {
			return versions
		}
		newestFirst(versions)
		left := slices.Clone(versions[:count])
		for _, stale := range versions[count:] {
			if _, mine := admitted[stale.path]; mine {
				left = append(left, stale)
				continue
			}
			_ = os.Remove(stale.path)
		}
		return left
	}
	units := make(map[string][]entry)
	for identity, versions := range identities {
		unit := logicalUnit(identity)
		units[unit] = append(units[unit], drop(versions, keep)...)
	}
	for _, versions := range units {
		drop(versions, perUnit)
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
