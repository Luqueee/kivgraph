package hotsnapshot

import (
	"errors"
	"strings"
)

const MaxExactResults = 500

var (
	ErrInvalidExactOffset = errors.New("exact search offset is invalid")
	ErrInvalidExactLimit  = errors.New("exact search limit is outside the supported range")
)

// SymbolPage is one bounded exact-name result page.
type SymbolPage struct {
	IDs     []SymbolID
	Offset  int
	Limit   int
	Total   int
	HasMore bool
}

// SymbolFilter narrows a page after the name matched. Every field is
// optional and an empty one matches everything. Filtering costs nothing new
// on the scanning searches, which already read every interned name once.
type SymbolFilter struct {
	Kind           string
	RepositoryName string
	PathPrefix     string
}

// Empty reports whether the filter would keep every symbol.
func (filter SymbolFilter) Empty() bool {
	return filter.Kind == "" && filter.RepositoryName == "" && filter.PathPrefix == ""
}

// SearchSymbolsByName returns one exact interned-name page. It never performs
// substring, case-folding, or nominal matching.
func (snapshot *GraphSnapshot) SearchSymbolsByName(name InternedString, filter SymbolFilter, offset, limit int) (SymbolPage, error) {
	return exactSymbolPage(snapshot.keepMatching(snapshot.symbolsByName[name], filter), offset, limit)
}

// SearchSymbolsByQName returns one exact interned-qualified-name page.
func (snapshot *GraphSnapshot) SearchSymbolsByQName(name InternedString, filter SymbolFilter, offset, limit int) (SymbolPage, error) {
	return exactSymbolPage(snapshot.keepMatching(snapshot.symbolsByQName[name], filter), offset, limit)
}

// SearchSymbolsByNamePrefix returns symbols whose unqualified name starts
// with prefix. Results retain the snapshot's stable-key order.
func (snapshot *GraphSnapshot) SearchSymbolsByNamePrefix(prefix string, filter SymbolFilter, offset, limit int) (SymbolPage, error) {
	return snapshot.scanSymbolNames(prefix, false, filter, offset, limit)
}

// SearchSymbolsByNameSubstring returns symbols whose unqualified name
// contains needle. It is the same walk as the prefix search and costs the
// same: an agent that knows a fragment of a name should not have to guess
// where the name begins.
func (snapshot *GraphSnapshot) SearchSymbolsByNameSubstring(needle string, filter SymbolFilter, offset, limit int) (SymbolPage, error) {
	return snapshot.scanSymbolNames(needle, true, filter, offset, limit)
}

func (snapshot *GraphSnapshot) scanSymbolNames(needle string, substring bool, filter SymbolFilter, offset, limit int) (SymbolPage, error) {
	if offset < 0 {
		return SymbolPage{}, ErrInvalidExactOffset
	}
	if limit < 1 || limit > MaxExactResults {
		return SymbolPage{}, ErrInvalidExactLimit
	}
	ids := make([]SymbolID, 0)
	for index, symbol := range snapshot.symbols {
		name, found := snapshot.strings.String(symbol.Name)
		if !found {
			continue
		}
		if substring {
			if !strings.Contains(name, needle) {
				continue
			}
		} else if !strings.HasPrefix(name, needle) {
			continue
		}
		id := SymbolID(index)
		if !snapshot.symbolPassesFilter(id, filter) {
			continue
		}
		ids = append(ids, id)
	}
	return exactSymbolPage(ids, offset, limit)
}

// keepMatching applies a filter to an index result. An empty filter returns
// the index slice untouched, which is the common case; exactSymbolPage copies
// the page it hands back, so nothing internal escapes either way.
func (snapshot *GraphSnapshot) keepMatching(ids []SymbolID, filter SymbolFilter) []SymbolID {
	if filter.Empty() {
		return ids
	}
	kept := make([]SymbolID, 0, len(ids))
	for _, id := range ids {
		if snapshot.symbolPassesFilter(id, filter) {
			kept = append(kept, id)
		}
	}
	return kept
}

// symbolPassesFilter resolves only what the filter asked about: a query with
// no repository or path filter never touches the file or repository tables.
func (snapshot *GraphSnapshot) symbolPassesFilter(id SymbolID, filter SymbolFilter) bool {
	if filter.Empty() {
		return true
	}
	symbol, found := snapshot.Symbol(id)
	if !found {
		return false
	}
	if filter.Kind != "" {
		kind, kindFound := snapshot.strings.String(symbol.Kind)
		if !kindFound || kind != filter.Kind {
			return false
		}
	}
	if filter.RepositoryName == "" && filter.PathPrefix == "" {
		return true
	}
	file, fileFound := snapshot.File(symbol.File)
	if !fileFound {
		return false
	}
	if filter.PathPrefix != "" {
		path, pathFound := snapshot.strings.String(file.Path)
		if !pathFound || !strings.HasPrefix(path, filter.PathPrefix) {
			return false
		}
	}
	if filter.RepositoryName != "" {
		repository, repositoryFound := snapshot.Repository(file.Repository)
		if !repositoryFound {
			return false
		}
		name, nameFound := snapshot.strings.String(repository.Name)
		if !nameFound || name != filter.RepositoryName {
			return false
		}
	}
	return true
}

func exactSymbolPage(ids []SymbolID, offset, limit int) (SymbolPage, error) {
	if offset < 0 {
		return SymbolPage{}, ErrInvalidExactOffset
	}
	if limit < 1 || limit > MaxExactResults {
		return SymbolPage{}, ErrInvalidExactLimit
	}
	page := SymbolPage{Offset: offset, Limit: limit, Total: len(ids)}
	if offset >= len(ids) {
		return page, nil
	}
	end := offset + limit
	if end < offset || end > len(ids) {
		end = len(ids)
	}
	page.IDs = append([]SymbolID(nil), ids[offset:end]...)
	page.HasMore = end < len(ids)
	return page, nil
}
