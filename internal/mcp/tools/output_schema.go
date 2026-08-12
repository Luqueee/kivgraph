package tools

import "github.com/google/jsonschema-go/jsonschema"

// ConciseOutputSchema is what every tool publishes in place of the full JSON
// Schema of its result type.
//
// Given no output schema, the SDK derives one from the handler's return type.
// Measured on this surface, those derivations are 87 % of everything a client
// loads before it can use the server -- `30.334` characters against `2.530`
// for the input schemas, which are the ones that actually tell a caller how to
// call. A client reads the result it receives; it does not need a description
// of that result beforehand, and a harness that loads tool schemas on demand
// pays for the description before deciding whether to look at the tool at all.
//
// Nothing else changes. Handlers stay typed, the SDK still marshals the result
// into `structuredContent`, and it still validates against this schema -- an
// object is what every response is. Only the published description is trimmed.
func ConciseOutputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{Type: "object"}
}
