package scip

import (
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
)

// reconstructEnclosingRanges gives every declaration a span when the producer
// gave none.
//
// SCIP's `enclosing_range` is optional. scip-java sets it, so a Java symbol
// spans its whole declaration and a reference is attributed to the innermost
// one containing it. scip-dotnet sets no enclosing range at all: every
// declaration would span its own name, nothing could contain anything, and
// every reference in the language would be sourced at the file's module
// symbol.
//
// So the spans are rebuilt from what is there: the definition positions and
// the nesting the SCIP descriptors already encode. `Coverage/Circle#Area().`
// is inside `Coverage/Circle#` because its descriptor path says so, not
// because of where it sits. Walking the definitions in source order with a
// stack of open ancestors closes each one at the next declaration that is not
// its descendant.
//
// This is a reconstruction and it is named as one. It assumes declarations are
// contiguous and appear in source order within a document, which is true of C#
// and of every language whose members are written inside their type. Where the
// assumption fails the cost is bounded: a declaration's span reaches to the
// next declaration instead of to its own closing brace, so a reference sitting
// between the two -- a trailing comment, a closing brace -- is attributed to
// the last member rather than to its parent. It never changes which symbol a
// reference points *at*, only which one it is counted *from*.
//
// The Dart loader already does the same thing for the same reason; see ADR
// 0048's container-symbol-by-position fallback.
func reconstructEnclosingRanges(declarations []declaration, offsets *offsetTable) []declaration {
	for _, entry := range declarations {
		if entry.enclosing.Present {
			// The producer knows better than any reconstruction.
			return declarations
		}
	}
	if len(declarations) == 0 {
		return declarations
	}

	ordered := append([]declaration(nil), declarations...)
	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left].selection.StartLine != ordered[right].selection.StartLine {
			return ordered[left].selection.StartLine < ordered[right].selection.StartLine
		}
		return ordered[left].selection.StartCharacter < ordered[right].selection.StartCharacter
	})

	ends := make([]scipwire.Range, len(ordered))
	var stack []int
	closeTo := func(index int, at scipwire.Range) {
		ends[index] = at
	}
	for position, entry := range ordered {
		for len(stack) > 0 && !descends(entry.symbol, ordered[stack[len(stack)-1]].symbol) {
			closeTo(stack[len(stack)-1], entry.selection)
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, position)
	}
	endOfFile := scipwire.Range{
		StartLine:      int32(offsets.lastLine()),
		StartCharacter: 0,
		Present:        true,
	}
	for _, index := range stack {
		closeTo(index, endOfFile)
	}

	for position := range ordered {
		end := ends[position]
		ordered[position].enclosing = scipwire.Range{
			StartLine:      ordered[position].selection.StartLine,
			StartCharacter: 0,
			EndLine:        end.StartLine,
			EndCharacter:   end.StartCharacter,
			Present:        true,
		}
		// A declaration that closes on its own line -- the last one before a
		// sibling on the very next line -- would span nothing. Keep at least
		// its own selection so it can still contain a use on that line.
		if ordered[position].enclosing.EndLine < ordered[position].selection.EndLine {
			ordered[position].enclosing.EndLine = ordered[position].selection.EndLine
			ordered[position].enclosing.EndCharacter = ordered[position].selection.EndCharacter
		}
	}
	return ordered
}

// descends reports whether `inner` is nested inside `outer` by descriptor path.
//
// It is a prefix test on the descriptors and not on the whole symbol, because
// the package coordinates of a member and its type are identical anyway, and a
// producer that writes `.` for both would make a whole-symbol prefix match two
// unrelated projects.
func descends(inner, outer string) bool {
	innerIdentity, innerErr := parseSymbol(inner)
	outerIdentity, outerErr := parseSymbol(outer)
	if innerErr != nil || outerErr != nil {
		return false
	}
	innerPath := strings.TrimSpace(innerIdentity.descriptors)
	outerPath := strings.TrimSpace(outerIdentity.descriptors)
	if innerPath == outerPath || outerPath == "" {
		return false
	}
	return strings.HasPrefix(innerPath, outerPath)
}
