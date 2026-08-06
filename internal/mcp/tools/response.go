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
	Results       T        `json:"results"`
}

// Coverage counts how confidently the response can account for related graph
// facts. Exact and candidate facts are disjoint; unresolved_related records
// related references that could not be classified.
type Coverage struct {
	Exact             int `json:"exact"`
	Candidate         int `json:"candidate"`
	UnresolvedRelated int `json:"unresolved_related"`
}
