package tools

import "time"

// Observer receives the elapsed time spent in an MCP tool handler.
type Observer func(toolName string, elapsed time.Duration)

// CallObservation describes the common metadata available after a typed MCP
// tool handler returns. It deliberately excludes the response payload itself:
// result counts and truncation are enough for metrics and avoid retaining
// potentially large graph results.
type CallObservation struct {
	ToolName          string
	Elapsed           time.Duration
	Total             int
	Returned          int
	Truncated         bool
	UnresolvedRelated int
	SnapshotID        *uint64
	SnapshotAgeMS     *int64
	Err               error
}

// CallObserver receives the metadata of one completed MCP tool handler.
type CallObserver func(CallObservation)

func observe[T any](
	observer Observer,
	callObserver CallObserver,
	toolName string,
	start time.Time,
	response Response[T],
	err error,
) {
	elapsed := time.Since(start)
	if observer != nil {
		observer(toolName, elapsed)
	}
	if callObserver != nil {
		callObserver(CallObservation{
			ToolName:          toolName,
			Elapsed:           elapsed,
			Total:             response.Total,
			Returned:          response.Returned,
			Truncated:         response.Truncated,
			UnresolvedRelated: response.Coverage.UnresolvedRelated,
			SnapshotID:        response.SnapshotID,
			SnapshotAgeMS:     response.SnapshotAgeMS,
			Err:               err,
		})
	}
}

// observeCall times a tool whose result is not a paginated Response, which is
// every mutating tool. Without it index_project would be the one call a client
// can make that no counter and no log ever sees -- and it is the slowest one.
func observeCall(
	observer Observer,
	callObserver CallObserver,
	toolName string,
	start time.Time,
	err error,
) {
	elapsed := time.Since(start)
	if observer != nil {
		observer(toolName, elapsed)
	}
	if callObserver != nil {
		callObserver(CallObservation{ToolName: toolName, Elapsed: elapsed, Err: err})
	}
}

func firstCallObserver(observers []CallObserver) CallObserver {
	if len(observers) == 0 {
		return nil
	}
	return observers[0]
}
