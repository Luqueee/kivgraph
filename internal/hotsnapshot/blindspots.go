package hotsnapshot

import "strings"

// A recorded resolution failure is one of two very different things, and the
// snapshot already tells them apart without being asked: a row that names a
// file is one reference the resolver could not follow, and a row without one
// is a scope it could not read at all -- a package excluded by build tags, a
// module the loader never loaded.
//
// The first bounds an answer about a symbol. The second bounds every answer
// about that repository, whatever it was asked.

// UnresolvedNamingSymbol returns the failures whose requested symbol is name,
// or ends in it after a dot. The resolver records what it looked for, so a
// failed `Connection.Close` is a failure that concerns `Close`.
//
// total is the number of matches before limit, so a caller can say how much it
// is not showing instead of implying it showed everything.
func (snapshot *GraphSnapshot) UnresolvedNamingSymbol(name string, limit int) ([]UnresolvedReferenceRecord, int) {
	if snapshot == nil || name == "" || limit < 0 {
		return nil, 0
	}
	matched := make([]UnresolvedReferenceRecord, 0)
	total := 0
	for _, reference := range snapshot.unresolved {
		if reference.File == InvalidFileID {
			continue
		}
		requested, found := snapshot.strings.String(reference.RequestedSymbol)
		if !found || !requestedNames(requested, name) {
			continue
		}
		total++
		if len(matched) < limit {
			matched = append(matched, reference)
		}
	}
	return matched, total
}

// requestedNames reports whether a requested symbol concerns name. The
// resolver stores the selector it tried, which for a member access is
// `Owner.Member`, so an exact match and a trailing member both count. A
// substring match would not: `Closer` is not `Close`.
func requestedNames(requested, name string) bool {
	if requested == name {
		return true
	}
	return strings.HasSuffix(requested, "."+name)
}

// UnresolvedFromSymbol returns the failures the resolver recorded while reading
// one symbol: references it makes that go nowhere the graph holds.
//
// This is the outward direction, and it is a different question from
// UnresolvedNamingSymbol. "Who calls X" is bounded by failures that asked for
// X; "what does X reach" is bounded by failures X itself made. A traversal that
// answers nothing while its root has these is a lower bound, not an absence.
func (snapshot *GraphSnapshot) UnresolvedFromSymbol(symbol SymbolID, limit int) ([]UnresolvedReferenceRecord, int) {
	if snapshot == nil || symbol == InvalidSymbolID || limit < 0 {
		return nil, 0
	}
	matched := make([]UnresolvedReferenceRecord, 0)
	total := 0
	for _, reference := range snapshot.unresolved {
		if reference.Source != symbol {
			continue
		}
		total++
		if len(matched) < limit {
			matched = append(matched, reference)
		}
	}
	return matched, total
}

// UnresolvedScopeGroup is one invisible scope and how many recorded failures
// share it. The identity is what a reader can act on -- reason, repository,
// requested package, detail -- and never one failure's own position: a scope is
// a package or a module, so a row per failure inside it repeats a single fact
// and charges every answer for the repetition. Twenty cgo symbols of one
// package are one unreadable package.
type UnresolvedScopeGroup struct {
	Reference   UnresolvedReferenceRecord
	Occurrences int
}

// UnresolvedScopeGroups returns the scopes the index could not read at all,
// rather than one reference, one row per distinct scope. Passing
// InvalidRepositoryID returns them for every repository.
//
// total is the number of distinct scopes before limit, so a cut list still
// reports the size of the problem instead of the size of the page.
func (snapshot *GraphSnapshot) UnresolvedScopeGroups(repository RepositoryID, limit int) ([]UnresolvedScopeGroup, int) {
	if snapshot == nil || limit < 0 {
		return nil, 0
	}
	// Interned identifiers compare directly, so grouping costs one map probe
	// per failure and never builds a key string.
	type identity struct {
		repository       RepositoryID
		reason           InternedString
		requestedPackage InternedString
		detail           InternedString
	}
	const notShown = -1
	groups := make([]UnresolvedScopeGroup, 0)
	placed := make(map[identity]int)
	total := 0
	for _, reference := range snapshot.unresolved {
		if reference.File != InvalidFileID {
			continue
		}
		if repository != InvalidRepositoryID && reference.Repository != repository {
			continue
		}
		key := identity{
			repository:       reference.Repository,
			reason:           reference.Reason,
			requestedPackage: reference.RequestedPackage,
			detail:           reference.Detail,
		}
		if index, seen := placed[key]; seen {
			if index != notShown {
				groups[index].Occurrences++
			}
			continue
		}
		total++
		if len(groups) >= limit {
			// Counted as a distinct scope, and remembered so its later
			// failures never inflate a scope that is shown.
			placed[key] = notShown
			continue
		}
		placed[key] = len(groups)
		groups = append(groups, UnresolvedScopeGroup{Reference: reference, Occurrences: 1})
	}
	return groups, total
}

// UnresolvedInRepository counts every recorded failure of one repository,
// scope-wide ones included. It is the number behind "this answer is a lower
// bound": a repository with none of them can be answered exactly.
func (snapshot *GraphSnapshot) UnresolvedInRepository(repository RepositoryID) int {
	if snapshot == nil {
		return 0
	}
	total := 0
	for _, reference := range snapshot.unresolved {
		if reference.Repository == repository {
			total++
		}
	}
	return total
}
