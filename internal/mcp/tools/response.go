package tools

// Response is the common envelope returned by every query tool.
//
// A nil snapshot identifier and age mean that no immutable snapshot has been
// published yet. Optional metadata is still encoded as JSON null: clients can
// rely on all envelope fields being present without confusing an empty graph
// with a missing response field.
type Response[T any] struct {
	SnapshotID    *uint64  `json:"snapshot_id"`
	SnapshotAgeMS *int64   `json:"snapshot_age_ms"`
	Total         int      `json:"total"`
	Returned      int      `json:"returned"`
	Truncated     bool     `json:"truncated"`
	NextCursor    *string  `json:"next_cursor"`
	Coverage      Coverage `json:"coverage"`
	// Completeness is present when the tool checked how far its answer
	// reaches. Absent means it did not check, which is not the same as
	// checking and finding nothing.
	Completeness *Completeness `json:"completeness,omitempty"`
	// Guidance is what the count means and what to call next. It is present only
	// when the answer alone would mislead: zero rows read as "no such thing"
	// unless something says otherwise, and a truncated page does not say whether
	// the rows that mattered are in it.
	Guidance string `json:"guidance,omitempty"`
	Results  T      `json:"results"`
}

// Coverage counts how confidently the response can account for related graph
// facts. Exact and candidate facts are disjoint; unresolved_related records
// related references that could not be classified.
//
// PackageLevel is disjoint from all three: it counts facts about a package
// rather than about the symbol asked for. A package dependency proves the
// consumer depends on the provider package, never that it uses the queried
// symbol, so counting one as exact would report a use nobody observed.
type Coverage struct {
	Exact             int `json:"exact"`
	Candidate         int `json:"candidate"`
	UnresolvedRelated int `json:"unresolved_related"`
	PackageLevel      int `json:"package_level"`
}
