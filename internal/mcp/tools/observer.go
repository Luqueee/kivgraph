package tools

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Observer receives the elapsed time spent in an MCP tool handler.
type Observer func(toolName string, elapsed time.Duration)

// CallObservation describes the common metadata available after a typed MCP
// tool handler returns. It deliberately excludes the response payload itself:
// result counts and truncation are enough for metrics and avoid retaining
// potentially large graph results.
type CallObservation struct {
	ToolName string
	// Query is the bounded, user-facing projection of the request that belongs
	// in the durable operator log. It deliberately omits cursors, confirmations,
	// opaque keys and arguments a future tool may add without opting into logs.
	Query             string
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
	request *sdkmcp.CallToolRequest,
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
			Query:             logQueryForRequest(toolName, request),
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
	request *sdkmcp.CallToolRequest,
	start time.Time,
	err error,
) {
	elapsed := time.Since(start)
	if observer != nil {
		observer(toolName, elapsed)
	}
	if callObserver != nil {
		callObserver(CallObservation{
			ToolName: toolName,
			Query:    logQueryForRequest(toolName, request),
			Elapsed:  elapsed,
			Err:      err,
		})
	}
}

// The durable log is an operator view, not a second copy of an MCP request.
// A new argument therefore stays out until it is deliberately useful to a
// person reading `kivgraph logs`. Cursors are opaque pagination state,
// confirmations are consent state, and index_project paths are absolute local
// paths; none explains what a query was about.
var logQueryFields = map[string][]string{
	graphStatusToolName:            {"profile"},
	repositoryQueryToolName:        {"profile"},
	findSymbolToolName:             {"name", "mode", "kind", "repo", "path_prefix", "profile"},
	findByIntentToolName:           {"intent", "keywords", "repo", "path_prefix", "kind", "profile"},
	getSymbolToolName:              {"qualified_name", "repository", "path", "profile"},
	getSourceToolName:              {"symbols", "context_lines", "profile"},
	fileOutlineToolName:            {"repository", "path", "kind", "include_members", "profile"},
	findReferencesToolName:         {"qualified_name", "name", "repository", "path", "direction", "repo", "language", "edge_kinds", "confidence", "profile"},
	findCrossRepoConsumersToolName: {"qualified_name", "repository", "path", "repo", "language", "profile"},
	traceDependenciesToolName:      {"qualified_name", "repository", "path", "to", "to_path", "depth", "max_nodes", "edge_kinds", "confidence", "repo", "language", "profile"},
	blastRadiusToolName:            {"qualified_name", "repository", "path", "depth", "max_nodes", "edge_kinds", "confidence", "kinds", "profile"},
	unresolvedReferencesToolName:   {"repo", "package", "requested_package", "requested_symbol", "reason", "language"},
	indexProjectToolName:           {"profile", "name", "languages"},
}

// maxLoggedQueryRunes retains enough intent to identify a call while keeping
// each append well below the atomic-write bound of the event log.
const maxLoggedQueryRunes = 320

func logQueryForRequest(toolName string, request *sdkmcp.CallToolRequest) string {
	if request == nil || request.Params == nil {
		return ""
	}
	return summarizeLogQuery(toolName, request.Params.Arguments)
}

// summarizeLogQuery returns a stable, allow-listed rendering of arguments.
// Its input is raw JSON because the observer is shared by every typed handler;
// parsing it here avoids a second field on every public tool input.
func summarizeLogQuery(toolName string, rawArguments []byte) string {
	fields := logQueryFields[toolName]
	if len(fields) == 0 || len(rawArguments) == 0 {
		return ""
	}
	arguments := map[string]json.RawMessage{}
	if err := json.Unmarshal(rawArguments, &arguments); err != nil {
		return ""
	}
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		raw, ok := arguments[field]
		if !ok || len(raw) == 0 {
			continue
		}
		value, ok := logQueryValue(field, raw)
		if !ok {
			continue
		}
		parts = append(parts, field+"="+value)
	}
	return truncateLoggedQuery(strings.Join(parts, " "))
}

func logQueryValue(field string, raw json.RawMessage) (string, bool) {
	if field == "symbols" {
		var symbols []SourceRequest
		if err := json.Unmarshal(raw, &symbols); err != nil {
			return "", false
		}
		labels := make([]string, 0, len(symbols))
		for _, symbol := range symbols {
			switch {
			case symbol.QualifiedName != "":
				labels = append(labels, symbol.QualifiedName)
			case symbol.Repository != "" && symbol.Path != "":
				labels = append(labels, symbol.Repository+":"+symbol.Path)
			default:
				// A durable key is an opaque implementation identifier, not a
				// useful query to an operator. Do not copy it into a log.
				labels = append(labels, "[stable key]")
			}
		}
		encoded, err := json.Marshal(labels)
		if err != nil {
			return "", false
		}
		return string(encoded), true
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return "", false
	}
	return compact.String(), true
}

func truncateLoggedQuery(query string) string {
	runes := []rune(query)
	if len(runes) <= maxLoggedQueryRunes {
		return query
	}
	return string(runes[:maxLoggedQueryRunes-1]) + "…"
}

func firstCallObserver(observers []CallObserver) CallObserver {
	if len(observers) == 0 {
		return nil
	}
	return observers[0]
}
