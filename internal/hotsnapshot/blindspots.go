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

// UnresolvedScopes returns the failures that describe something the index
// could not read at all, rather than one reference. Passing
// InvalidRepositoryID returns them for every repository.
func (snapshot *GraphSnapshot) UnresolvedScopes(repository RepositoryID, limit int) ([]UnresolvedReferenceRecord, int) {
	if snapshot == nil || limit < 0 {
		return nil, 0
	}
	matched := make([]UnresolvedReferenceRecord, 0)
	total := 0
	for _, reference := range snapshot.unresolved {
		if reference.File != InvalidFileID {
			continue
		}
		if repository != InvalidRepositoryID && reference.Repository != repository {
			continue
		}
		total++
		if len(matched) < limit {
			matched = append(matched, reference)
		}
	}
	return matched, total
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
