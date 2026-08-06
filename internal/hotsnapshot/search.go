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

// SearchSymbolsByName returns one exact interned-name page. It never performs
// substring, case-folding, or nominal matching.
func (snapshot *GraphSnapshot) SearchSymbolsByName(name InternedString, offset, limit int) (SymbolPage, error) {
	return exactSymbolPage(snapshot.symbolsByName[name], offset, limit)
}

// SearchSymbolsByQName returns one exact interned-qualified-name page.
func (snapshot *GraphSnapshot) SearchSymbolsByQName(name InternedString, offset, limit int) (SymbolPage, error) {
	return exactSymbolPage(snapshot.symbolsByQName[name], offset, limit)
}

// SearchSymbolsByNamePrefix returns symbols whose unqualified name starts
// with prefix. Results retain the snapshot's stable-key order.
func (snapshot *GraphSnapshot) SearchSymbolsByNamePrefix(prefix string, offset, limit int) (SymbolPage, error) {
	if offset < 0 {
		return SymbolPage{}, ErrInvalidExactOffset
	}
	if limit < 1 || limit > MaxExactResults {
		return SymbolPage{}, ErrInvalidExactLimit
	}
	ids := make([]SymbolID, 0)
	for index, symbol := range snapshot.symbols {
		name, found := snapshot.strings.String(symbol.Name)
		if found && strings.HasPrefix(name, prefix) {
			ids = append(ids, SymbolID(index))
		}
	}
	return exactSymbolPage(ids, offset, limit)
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
