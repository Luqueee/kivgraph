package tools

// Response is the common envelope returned by every query tool.
//
// A nil snapshot identifier and age mean that no immutable snapshot has been
// published yet. Under `view: "full"` optional metadata is still encoded as
// JSON null, so a client can rely on every envelope field being present
// without confusing an empty graph with a missing response field. Under the
// compact views the fields that carried nothing are absent instead; see
// ADR 0046 and `MarshalJSON` in `view.go`.
type Response[T any] struct {
	SnapshotID *uint64 `json:"snapshot_id"`
	// Profile scopes a single-profile answer when the installation contains
	// several profiles. It is omitted for the compatibility shape.
	Profile string `json:"profile,omitempty"`
	// Profiles replaces snapshot_id when several profiles were queried.
	Profiles []ProfileSnapshot `json:"profiles,omitempty"`
	// CrossProfileEdges states that a union contains no edges resolved between
	// its independently indexed graphs.
	CrossProfileEdges string   `json:"cross_profile_edges,omitempty"`
	SnapshotAgeMS     *int64   `json:"snapshot_age_ms"`
	Total             int      `json:"total"`
	Returned          int      `json:"returned"`
	Truncated         bool     `json:"truncated"`
	NextCursor        *string  `json:"next_cursor"`
	Coverage          Coverage `json:"coverage"`
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
	// View is the granularity the caller asked for. It shapes the envelope and
	// never travels in it.
	View string `json:"-"`
}

// ProfileSnapshot identifies one independently published graph in a union.
type ProfileSnapshot struct {
	Name         string        `json:"name"`
	SnapshotID   uint64        `json:"snapshot_id"`
	Completeness *Completeness `json:"completeness,omitempty"`
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
