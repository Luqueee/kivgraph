package hotsnapshot

import "strings"

// RepositoryByName returns the repository carrying this exact name. Names are
// compared exactly: a repository name is an identifier and never a path
// component, and two repositories differing only in case are two repositories.
func (snapshot *GraphSnapshot) RepositoryByName(name string) (RepositoryID, bool) {
	if snapshot == nil || name == "" {
		return 0, false
	}
	for index, repository := range snapshot.repositories {
		value, found := snapshot.strings.String(repository.Name)
		if found && value == name {
			return RepositoryID(index), true
		}
	}
	return 0, false
}

// FilesUnder returns the files of one repository at exactly path, or beneath
// it as a directory. The same argument answers "this file" and "this
// directory" because it is the same question at two granularities: an agent
// holding a path does not know, and should not have to say, which it has.
//
// A directory match requires the separator, so `internal/facts` never matches
// `internal/factsheet`.
func (snapshot *GraphSnapshot) FilesUnder(repository RepositoryID, path string) []FileID {
	if snapshot == nil || path == "" {
		return nil
	}
	if id, found := snapshot.FileByRepoPath(RepoPathKey{
		Repository: repository,
		Path:       snapshot.internedPath(path),
	}); found {
		return []FileID{id}
	}
	prefix := strings.TrimSuffix(path, "/") + "/"
	matched := make([]FileID, 0)
	for index, file := range snapshot.files {
		if file.Repository != repository {
			continue
		}
		value, found := snapshot.strings.String(file.Path)
		if found && strings.HasPrefix(value, prefix) {
			matched = append(matched, FileID(index))
		}
	}
	return matched
}

// internedPath resolves a path to its interned identifier, or to the invalid
// one when the snapshot never saw that string. Looking up the invalid
// identifier simply misses, which is the correct answer.
func (snapshot *GraphSnapshot) internedPath(path string) InternedString {
	id, found := snapshot.strings.Lookup(path)
	if !found {
		return InvalidInternedString
	}
	return id
}

// SymbolKindFilter decides which symbols a page is about. Nil keeps every one.
type SymbolKindFilter func(kind string) bool

// SearchSymbolsInFiles returns the symbols declared in any of files, in
// snapshot order. It is one walk over the symbol table, the same cost as a
// name search, so an outline of a whole directory costs no more than an
// outline of one file.
//
// keep is applied on that same walk, before paging, so the page's Total and
// its rows describe one set. Filtering after paging instead reported a file as
// twice its size -- every enum variant and struct field counted in the total
// and then dropped from the answer -- with Truncated false and no cursor, so
// the half a reader went looking for had never existed.
func (snapshot *GraphSnapshot) SearchSymbolsInFiles(
	files []FileID, offset, limit int, keep SymbolKindFilter,
) (SymbolPage, error) {
	if offset < 0 {
		return SymbolPage{}, ErrInvalidExactOffset
	}
	if limit < 1 || limit > MaxExactResults {
		return SymbolPage{}, ErrInvalidExactLimit
	}
	if snapshot == nil || len(files) == 0 {
		return SymbolPage{Offset: offset, Limit: limit}, nil
	}
	wanted := make(map[FileID]struct{}, len(files))
	for _, file := range files {
		wanted[file] = struct{}{}
	}
	ids := make([]SymbolID, 0)
	for index, symbol := range snapshot.symbols {
		if _, found := wanted[symbol.File]; !found {
			continue
		}
		if keep != nil {
			kind, ok := snapshot.strings.String(symbol.Kind)
			if !ok || !keep(kind) {
				continue
			}
		}
		ids = append(ids, SymbolID(index))
	}
	return exactSymbolPage(ids, offset, limit)
}
