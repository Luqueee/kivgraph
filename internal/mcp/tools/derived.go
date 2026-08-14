package tools

import (
	"fmt"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

// derivedFilter decides whether rows from a provider Ladygraph derived from the
// machine belong in an answer.
//
// They are withheld by default, and that default is the difference between a
// usable answer and an unusable one: with the standard library in the graph,
// `find_references` on `Clone` or `Debug` reaches most of the corpus, and a page
// of `core` is not what a caller asking about their own code wants. Two things
// override it: asking for them, and naming one by repository, because an
// explicit filter is a request and not an accident.
type derivedFilter struct {
	include bool
	// repository is the caller's `repo` filter. Naming a derived provider is
	// how one asks about it without a second flag.
	repository string
}

func newDerivedFilter(include bool, repository string) derivedFilter {
	// The `repo` filter of this surface is matched against the repository's
	// durable key, so a caller naming a derived provider spells it either way.
	return derivedFilter{include: include, repository: facts.RepositoryNameFromKey(repository)}
}

// keepsAll reports a filter that withholds nothing, so a caller can skip the
// per-row work entirely.
func (filter derivedFilter) keepsAll() bool {
	return filter.include || facts.IsSyntheticRepository(filter.repository)
}

// keepsRepository reports whether rows of one repository belong in the answer.
func (filter derivedFilter) keepsRepository(name string) bool {
	if filter.keepsAll() {
		return true
	}
	return !facts.IsSyntheticRepository(name)
}

// keepsSymbol reports whether one symbol of the snapshot belongs in the answer.
func (filter derivedFilter) keepsSymbol(snapshot *hotsnapshot.GraphSnapshot, id hotsnapshot.SymbolID) (bool, error) {
	if filter.keepsAll() {
		return true, nil
	}
	repository, _, err := symbolRepositoryAndLanguages(snapshot, id)
	if err != nil {
		return false, err
	}
	name, ok := snapshot.Strings().String(repository.Name)
	if !ok {
		return false, fmt.Errorf("repository has an invalid name: %v", repository)
	}
	return !facts.IsSyntheticRepository(name), nil
}
