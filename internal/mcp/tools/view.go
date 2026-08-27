package tools

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Views. A view is the granularity of an answer, not a different answer: the
// same edges, with the same confidence and the same provenance, spelled with or
// without the parts a row shares with every other row.
//
// The measured cost of spelling them out on every row, over `workspace`
// (`benchmarks/codebase-memory-comparison`): `confidence` plus `provenance`
// were `1.200` of the `4.236` tokens of one `find_references` page, and every
// one of the fifty rows carried the same pair. See ADR 0046.
const (
	// ViewCompact hoists whatever every row shares into the header and groups
	// rows by file. It is the default.
	ViewCompact = "compact"
	// ViewFull is the row-per-fact shape: every field on every row.
	ViewFull = "full"
	// ViewFiles answers only which files hold the facts, with a count each.
	ViewFiles = "files"
)

// normalizeView defaults to compact. filesAllowed is false for the tools whose
// answer is not a set of files, so asking for it fails instead of silently
// returning something else.
func normalizeView(value string, filesAllowed bool) (string, error) {
	switch value {
	case "":
		return ViewCompact, nil
	case ViewCompact, ViewFull:
		return value, nil
	case ViewFiles:
		if filesAllowed {
			return value, nil
		}
		return "", NewToolError(CodeInvalidArgument, fmt.Sprintf(
			"view %q is unsupported for this tool, use %q or %q",
			ViewFiles, ViewCompact, ViewFull,
		))
	default:
		return "", NewToolError(CodeInvalidArgument, fmt.Sprintf(
			"view %q is unsupported, use %q, %q or %q",
			value, ViewCompact, ViewFull, ViewFiles,
		))
	}
}

// compactEnvelope is the envelope of a compact answer. What is absent is what
// carried no information: an age nobody asked for, a `truncated` that is false,
// a cursor that does not exist, and coverage categories at zero.
type compactEnvelope[T any] struct {
	SnapshotID   *uint64          `json:"snapshot_id,omitempty"`
	Total        int              `json:"total"`
	Returned     int              `json:"returned"`
	Truncated    bool             `json:"truncated,omitempty"`
	NextCursor   *string          `json:"next_cursor,omitempty"`
	Coverage     *compactCoverage `json:"coverage,omitempty"`
	Completeness *Completeness    `json:"completeness,omitempty"`
	Guidance     string           `json:"guidance,omitempty"`
	Results      T                `json:"results"`
}

// compactCoverage drops the categories that counted nothing. A zero next to
// three other zeros says only that the tool has four counters.
type compactCoverage struct {
	Exact             int `json:"exact,omitempty"`
	Candidate         int `json:"candidate,omitempty"`
	UnresolvedRelated int `json:"unresolved_related,omitempty"`
	PackageLevel      int `json:"package_level,omitempty"`
}

func (coverage Coverage) compact() *compactCoverage {
	if coverage.Exact == 0 && coverage.Candidate == 0 &&
		coverage.UnresolvedRelated == 0 && coverage.PackageLevel == 0 {
		return nil
	}
	return &compactCoverage{
		Exact:             coverage.Exact,
		Candidate:         coverage.Candidate,
		UnresolvedRelated: coverage.UnresolvedRelated,
		PackageLevel:      coverage.PackageLevel,
	}
}

// MarshalJSON writes the full envelope unless the response was built for a
// compact view. View is not serialized: it is the argument that produced the
// shape, and a client that has to read it back has already paid for it.
func (response Response[T]) MarshalJSON() ([]byte, error) {
	type fullEnvelope struct {
		SnapshotID    *uint64       `json:"snapshot_id"`
		SnapshotAgeMS *int64        `json:"snapshot_age_ms"`
		Total         int           `json:"total"`
		Returned      int           `json:"returned"`
		Truncated     bool          `json:"truncated"`
		NextCursor    *string       `json:"next_cursor"`
		Coverage      Coverage      `json:"coverage"`
		Completeness  *Completeness `json:"completeness,omitempty"`
		Guidance      string        `json:"guidance,omitempty"`
		Results       T             `json:"results"`
	}
	if response.View == ViewFull || response.View == "" {
		return json.Marshal(fullEnvelope{
			SnapshotID:    response.SnapshotID,
			SnapshotAgeMS: response.SnapshotAgeMS,
			Total:         response.Total,
			Returned:      response.Returned,
			Truncated:     response.Truncated,
			NextCursor:    response.NextCursor,
			Coverage:      response.Coverage,
			Completeness:  response.Completeness,
			Guidance:      response.Guidance,
			Results:       response.Results,
		})
	}
	return json.Marshal(compactEnvelope[T]{
		SnapshotID:   response.SnapshotID,
		Total:        response.Total,
		Returned:     response.Returned,
		Truncated:    response.Truncated,
		NextCursor:   response.NextCursor,
		Coverage:     response.Coverage.compact(),
		Completeness: response.Completeness,
		Guidance:     response.Guidance,
		Results:      response.Results,
	})
}

// hoistString returns the value shared by every row, or "" when the rows
// disagree or there are none. It is what turns a column into a header.
func hoistString(rows int, at func(int) string) string {
	if rows == 0 {
		return ""
	}
	first := at(0)
	if first == "" {
		return ""
	}
	for index := 1; index < rows; index++ {
		if at(index) != first {
			return ""
		}
	}
	return first
}

// locationLabel is `repository:path:line`, the triple every tool accepts,
// written once instead of as three fields.
func locationLabel(repository, path string, line uint32) string {
	return repository + ":" + path + ":" + strconv.FormatUint(uint64(line), 10)
}

// symbolAtLine names a symbol and where its declaration starts. The line is
// the declaration of the symbol holding the fact, never the token position:
// the snapshot never observed the latter.
func symbolAtLine(qualifiedName string, line uint32) string {
	return qualifiedName + "@" + strconv.FormatUint(uint64(line), 10)
}

// compactRowTail appends the fields a row still needs because they were not
// hoisted. An entry is a bare string when nothing varies, and an array when
// something does, so the shape says which columns are per-row.
func compactRowTail(label string, varying ...string) any {
	tail := make([]string, 0, len(varying))
	for _, value := range varying {
		if value != "" {
			tail = append(tail, value)
		}
	}
	if len(tail) == 0 {
		return label
	}
	row := make([]any, 0, len(tail)+1)
	row = append(row, label)
	for _, value := range tail {
		row = append(row, value)
	}
	return row
}

// groupByResidual buckets rows by the exact tuple `residual` returns for each
// one, in first-seen order. It is the second tier of hoisting: hoistString
// lifts a column to the page header when every row agrees, and this groups
// the rows that do not agree by what they still have in common.
//
// Measured on a real 66-row page where `confidence` and `provenance` already
// hoisted but `kind` and `edge_kind` did not -- one dissenting export among 65
// calls was enough to put both columns back on every row -- the residual
// collapsed to three tuples, one of them covering 62 of the 66 rows.
//
// A page where every row disagrees on everything produces one group per row,
// which is more bytes than the per-row tail it would replace: each group pays
// for its own object and field names where a tail paid only for values. So
// grouping is a candidate, not a decision -- the caller marshals it against
// the ungrouped page and keeps whichever is smaller; see the callers in
// find_references.go and trace_dependencies.go for that comparison.
func groupByResidual[T any](rows []T, residual func(T) []string) [][]T {
	index := make(map[string]int, len(rows))
	groups := make([][]T, 0, len(rows))
	for _, row := range rows {
		key := strings.Join(residual(row), "\x00")
		position, seen := index[key]
		if !seen {
			position = len(groups)
			index[key] = position
			groups = append(groups, nil)
		}
		groups[position] = append(groups[position], row)
	}
	return groups
}
