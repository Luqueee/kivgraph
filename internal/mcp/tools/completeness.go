package tools

import (
	"fmt"

	"strings"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

// Verdicts a caller can branch on without reading anything else.
const (
	// VerdictComplete means nothing the index recorded could add to this
	// answer: the list is the whole list.
	VerdictComplete = "COMPLETE"
	// VerdictLowerBound means the answer is a minimum. The index recorded
	// places it could not read that this query reaches, and they are listed.
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
	// They bound every answer about their repository, whatever was asked.
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
func completenessFor(
	snapshot *hotsnapshot.GraphSnapshot,
	symbolName string,
	repository hotsnapshot.RepositoryID,
) (Completeness, int, error) {
	naming, namingTotal := snapshot.UnresolvedNamingSymbol(symbolName, MaximumBlindSpots)
	scopes, scopeTotal := snapshot.UnresolvedScopes(repository, MaximumBlindSpots)
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
	for _, reference := range scopes {
		spot, err := blindSpot(snapshot, reference)
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
	return result, namingTotal + scopeTotal, nil
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
func scopeDirectory(detail string) string {
	marker := " in "
	index := strings.LastIndex(detail, marker)
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(detail[index+len(marker):])
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
