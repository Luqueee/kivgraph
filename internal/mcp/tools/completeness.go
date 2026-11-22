package tools

import (
	"fmt"

	"strings"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// Verdicts a caller can branch on without reading anything else.
const (
	// VerdictComplete means nothing the index recorded could add to this
	// answer: the list is the whole list.
	VerdictComplete = "COMPLETE"
	// VerdictLowerBound means the answer is a minimum: the index recorded
	// places it could not read that this query reaches, and they are listed,
	// or a traversal stopped at its node budget before it finished.
	VerdictLowerBound = "LOWER_BOUND"
)

// MaximumBlindSpots bounds what one response enumerates. The count is always
// exact even when the list is cut: a truncated warning must not read as a
// smaller problem than it is.
const MaximumBlindSpots = 20

// Completeness states how far an answer reaches.
//
// Every other index in this space answers with a list and lets the reader
// assume it is the whole one. This one refuses to guess, so it knows where it
// failed -- with a file, a line and a reason -- and can say so. An answer
// that cannot be vouched for is a lower bound, and it says which places it
// could not read.
type Completeness struct {
	Verdict string `json:"verdict"`
	// BlindSpots are recorded references the resolver could not follow that
	// could belong to this answer.
	BlindSpots     []BlindSpot `json:"blind_spots,omitempty"`
	MoreBlindSpots int         `json:"more_blind_spots,omitempty"`
	// InvisibleScopes are whole packages or modules the index could not read.
	// They bound every answer about their repository, whatever was asked, and
	// they are one row per scope: the unit is the package, so a reader learns
	// nothing from the same package repeated once per failure inside it.
	InvisibleScopes     []BlindSpot `json:"invisible_scopes,omitempty"`
	MoreInvisibleScopes int         `json:"more_invisible_scopes,omitempty"`
	// Fallback closes the gap. A warning without the recovery action forces a
	// whole-repository sweep, which costs more than not warning at all.
	Fallback *Fallback `json:"fallback,omitempty"`
}

// BlindSpot is one place the index knows it could not resolve. It is evidence
// about a failed request and never an inferred relationship: the requested
// names are the strings the resolver used, not graph keys.
type BlindSpot struct {
	Reason           string `json:"reason"`
	Repository       string `json:"repository"`
	FilePath         string `json:"file_path,omitempty"`
	StartLine        uint32 `json:"start_line,omitempty"`
	RequestedSymbol  string `json:"requested_symbol,omitempty"`
	RequestedPackage string `json:"requested_package,omitempty"`
	Detail           string `json:"detail,omitempty"`
	// Occurrences is how many recorded failures share an invisible scope. It
	// is absent from a failed reference, which is one occurrence by
	// definition.
	Occurrences int `json:"occurrences,omitempty"`
}

// Fallback is the bounded search that covers what the graph could not.
type Fallback struct {
	Pattern string   `json:"pattern"`
	Paths   []string `json:"paths,omitempty"`
}

// completenessFor builds the verdict for an answer about a symbol name inside
// one repository.
//
// The three checks are the three observable ways a failure can reach this
// query: a failure that named the same symbol, a failure anywhere in the same
// repository that could not be read at all, and -- through the second -- a
// package the index never opened. Passing every one of them is what makes
// silence mean something.
//
// language is the language of the declaration the answer is about, and empty
// when the question is about a name and not about one declaration.
//
// implementations says whether a relation of implementing, extending or
// overriding could be part of the answer: see answersWithImplementations.
func completenessFor(
	snapshot *hotsnapshot.GraphSnapshot,
	symbolName string,
	language string,
	repository hotsnapshot.RepositoryID,
	implementations bool,
) (Completeness, int, error) {
	naming, namingTotal := snapshot.UnresolvedNamingSymbol(symbolName, language, MaximumBlindSpots)
	scopes, scopeTotal := snapshot.UnresolvedScopeGroups(repository, language, implementations, MaximumBlindSpots)
	if namingTotal == 0 && scopeTotal == 0 {
		return Completeness{Verdict: VerdictComplete}, 0, nil
	}

	result := Completeness{Verdict: VerdictLowerBound}
	paths := make(map[string]struct{})
	for _, reference := range naming {
		spot, err := blindSpot(snapshot, reference)
		if err != nil {
			return Completeness{}, 0, err
		}
		result.BlindSpots = append(result.BlindSpots, spot)
		if spot.FilePath != "" {
			paths[spot.FilePath] = struct{}{}
		}
	}
	for _, group := range scopes {
		spot, err := invisibleScope(snapshot, group)
		if err != nil {
			return Completeness{}, 0, err
		}
		result.InvisibleScopes = append(result.InvisibleScopes, spot)
		if directory := scopeDirectory(spot.Detail); directory != "" {
			paths[directory] = struct{}{}
		}
	}
	result.MoreBlindSpots = namingTotal - len(result.BlindSpots)
	result.MoreInvisibleScopes = scopeTotal - len(result.InvisibleScopes)
	result.Fallback = &Fallback{
		Pattern: `\b` + regexpQuoteWord(symbolName) + `\b`,
		Paths:   sortedKeys(paths),
	}
	// The second value is `coverage.unresolved_related`, and the four coverage
	// counters are documented as disjoint counts of what touches this query. A
	// scope failure does not: it is why the answer may be short, not evidence
	// about the name asked for, and it is already reported in full by
	// InvisibleScopes and MoreInvisibleScopes. Adding it here once made
	// find_symbol report 29 related records for a name nothing referenced.
	// Ver ADR 0063.
	return result, namingTotal, nil
}

// neverImplemented are the kinds of declaration nothing implements, extends or
// overrides. The list is of what is certain and everything else counts: a
// kind this does not know keeps the bound, which is the safe way to be wrong.
var neverImplemented = map[string]bool{
	"function": true, "variable": true, "enum": true, "enum_member": true,
	"namespace": true, "parameter": true, "type_parameter": true,
	"import": true, "export": true, "module": true,
}

// answersWithImplementations reports whether the direct uses of a declaration
// can include an implementation relation: a class that extends it, a type
// that implements it, a member that overrides it.
//
// It decides whether a package whose implementation relations are incomplete
// bounds the answer. Who calls a function is not among what such a package
// left out; who uses a class is.
func answersWithImplementations(snapshot *hotsnapshot.GraphSnapshot, symbol hotsnapshot.SymbolID) bool {
	record, found := snapshot.Symbol(symbol)
	if !found {
		return true
	}
	kind, known := snapshot.Strings().String(record.Kind)
	return !known || !neverImplemented[kind]
}

// completenessOutwardFor builds the verdict for an answer about what a symbol
// reaches, which is bounded by a different set of failures than an answer about
// who reaches it: the ones the resolver recorded while reading that symbol.
//
// Asking the naming question here would be wrong in both directions. A failure
// that asked for this symbol's name does not bound what the symbol reaches, and
// a reference it makes that goes nowhere does -- and would be missed.
func completenessOutwardFor(
	snapshot *hotsnapshot.GraphSnapshot,
	symbol hotsnapshot.SymbolID,
	symbolName string,
	repository hotsnapshot.RepositoryID,
) (Completeness, int, error) {
	sourced, sourcedTotal := snapshot.UnresolvedFromSymbol(symbol, MaximumBlindSpots)
	// A walk outward crosses any relation, an implementation among them.
	scopes, scopeTotal := snapshot.UnresolvedScopeGroups(repository, symbolLanguage(snapshot, symbol), true, MaximumBlindSpots)
	if sourcedTotal == 0 && scopeTotal == 0 {
		return Completeness{Verdict: VerdictComplete}, 0, nil
	}

	result := Completeness{Verdict: VerdictLowerBound}
	paths := make(map[string]struct{})
	for _, reference := range sourced {
		spot, err := blindSpot(snapshot, reference)
		if err != nil {
			return Completeness{}, 0, err
		}
		result.BlindSpots = append(result.BlindSpots, spot)
		if spot.FilePath != "" {
			paths[spot.FilePath] = struct{}{}
		}
	}
	for _, group := range scopes {
		spot, err := invisibleScope(snapshot, group)
		if err != nil {
			return Completeness{}, 0, err
		}
		result.InvisibleScopes = append(result.InvisibleScopes, spot)
		if directory := scopeDirectory(spot.Detail); directory != "" {
			paths[directory] = struct{}{}
		}
	}
	result.MoreBlindSpots = sourcedTotal - len(result.BlindSpots)
	result.MoreInvisibleScopes = scopeTotal - len(result.InvisibleScopes)
	result.Fallback = &Fallback{
		Pattern: `\b` + regexpQuoteWord(symbolName) + `\b`,
		Paths:   sortedKeys(paths),
	}
	// Only the symbol's own unresolved references touch this query; the scopes
	// bound the answer without being evidence about it. See completenessFor.
	return result, sourcedTotal, nil
}

// completenessScopes builds the verdict for an answer about a place rather than
// a symbol -- what is declared under this path. There is no name to have been
// asked for, so the only failure that can bound it is a scope the index could
// not read: a package excluded by build tags declares nothing in the graph and
// everything in the source.
//
// No fallback pattern: without a symbol there is nothing to grep for, and
// inventing one would be advice the caller cannot use.
//
// It reports no coverage counter. Every failure it finds is a scope, and a
// scope is not one of the four disjoint things `coverage` counts about an
// answer; a reader learns of it from InvisibleScopes.
//
// language is the language of the one file the answer is about, and empty for
// a directory: what is missing under a directory can be in any language.
func completenessScopes(
	snapshot *hotsnapshot.GraphSnapshot,
	repository hotsnapshot.RepositoryID,
	language string,
) (Completeness, error) {
	// What a file declares is not a relation between two declarations.
	scopes, scopeTotal := snapshot.UnresolvedScopeGroups(repository, language, false, MaximumBlindSpots)
	if scopeTotal == 0 {
		return Completeness{Verdict: VerdictComplete}, nil
	}
	result := Completeness{Verdict: VerdictLowerBound}
	for _, group := range scopes {
		spot, err := invisibleScope(snapshot, group)
		if err != nil {
			return Completeness{}, err
		}
		result.InvisibleScopes = append(result.InvisibleScopes, spot)
	}
	result.MoreInvisibleScopes = scopeTotal - len(result.InvisibleScopes)
	return result, nil
}

// invisibleScope builds the row for one scope the index could not read. It
// drops the symbol and the line of the failure it was grouped from: a scope is
// a package, so naming one of its failures reads as if that one mattered, and
// the count is what tells a reader how much is behind it.
func invisibleScope(
	snapshot *hotsnapshot.GraphSnapshot,
	group hotsnapshot.UnresolvedScopeGroup,
) (BlindSpot, error) {
	spot, err := blindSpot(snapshot, group.Reference)
	if err != nil {
		return BlindSpot{}, err
	}
	spot.RequestedSymbol = ""
	spot.StartLine = 0
	spot.Occurrences = group.Occurrences
	return spot, nil
}

func blindSpot(
	snapshot *hotsnapshot.GraphSnapshot,
	reference hotsnapshot.UnresolvedReferenceRecord,
) (BlindSpot, error) {
	table := snapshot.Strings()
	reason, reasonOK := table.String(reference.Reason)
	requestedSymbol, requestedSymbolOK := table.String(reference.RequestedSymbol)
	requestedPackage, requestedPackageOK := table.String(reference.RequestedPackage)
	detail, detailOK := table.String(reference.Detail)
	if !reasonOK || !requestedSymbolOK || !requestedPackageOK || !detailOK {
		return BlindSpot{}, fmt.Errorf(
			"unresolved reference has invalid strings (reason_ok=%t requested_symbol_ok=%t requested_package_ok=%t detail_ok=%t)",
			reasonOK, requestedSymbolOK, requestedPackageOK, detailOK,
		)
	}
	spot := BlindSpot{
		Reason:           reason,
		RequestedSymbol:  requestedSymbol,
		RequestedPackage: requestedPackage,
		Detail:           detail,
		StartLine:        reference.StartLine,
	}
	if repository, found := snapshot.Repository(reference.Repository); found {
		if name, ok := table.String(repository.Name); ok {
			spot.Repository = name
		}
	}
	if reference.File != hotsnapshot.InvalidFileID {
		if file, found := snapshot.File(reference.File); found {
			if path, ok := table.String(file.Path); ok {
				spot.FilePath = path
			}
		}
	}
	return spot, nil
}

// scopeDirectory recovers the directory a scope failure names. The loader
// writes it into the detail, which is the only place it exists: a package the
// build excluded has no File row to point at.
//
// Not every detail names one, and the marker alone cannot tell: "declared in a
// Go build cache entry: ..." yields prose. The fallback is a recovery action, so
// a path that does not exist is worse than no path at all -- it sends an agent
// to sweep something that is not there. Only an absolute path is accepted,
// which is what the loader writes.
func scopeDirectory(detail string) string {
	marker := " in "
	index := strings.LastIndex(detail, marker)
	if index < 0 {
		return ""
	}
	candidate := strings.TrimSpace(detail[index+len(marker):])
	if !strings.HasPrefix(candidate, "/") {
		return ""
	}
	return candidate
}

// regexpQuoteWord escapes what a symbol name may legally contain and a regular
// expression would otherwise read as syntax.
func regexpQuoteWord(name string) string {
	var builder strings.Builder
	builder.Grow(len(name))
	for _, character := range name {
		if strings.ContainsRune(`\.+*?()|[]{}^$`, character) {
			builder.WriteByte('\\')
		}
		builder.WriteRune(character)
	}
	return builder.String()
}
