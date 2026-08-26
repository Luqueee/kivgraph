// Package tools implements the MCP tools Kivgraph answers questions with,
// along with their arguments, cursors, error codes and response shapes.
package tools

import sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

// alwaysLoadMeta promotes a tool past a client's schema deferral.
//
// Claude Code holds only tool names until its tool search loads a schema, and
// tokensave measured what that does to a graph server: its graph tools stopped
// being called until it marked a handful always-loaded (their issue #333). Four
// are marked here -- the ones that answer the question this server exists for --
// and the rest stay deferred, because every promoted schema is paid in every
// request of every session.
//
// Oh My Pi ignores the hint: it mounts each tool as a device and reads the schema
// on demand. The hint costs a few bytes there and changes nothing, which is the
// right trade for the one client where it is the difference between being called
// and not.
func alwaysLoadMeta() sdkmcp.Meta {
	return sdkmcp.Meta{"anthropic/alwaysLoad": true}
}

// boundedResultMeta raises the ceiling a client puts on one tool result.
//
// Claude Code warns above 10.000 tokens and truncates at 25.000. A traversal over
// a large corpus can legitimately exceed that, and a truncated answer is worse
// than a paged one: the caller cannot tell what it did not see. The tools that
// page already declare it in their own response, so the ceiling here only has to
// be wide enough that the page arrives whole.
func boundedResultMeta(maximumChars int) sdkmcp.Meta {
	return sdkmcp.Meta{"anthropic/maxResultSizeChars": maximumChars}
}

// traversalMeta is both hints at once for a tool that is worth promoting and can
// legitimately return a large page.
func traversalMeta(maximumChars int) sdkmcp.Meta {
	meta := alwaysLoadMeta()
	for key, value := range boundedResultMeta(maximumChars) {
		meta[key] = value
	}
	return meta
}

// MaximumTraversalResultChars is wide enough for a full page of either traversal
// tool and well inside the 500.000 ceiling a client will accept.
const MaximumTraversalResultChars = 120_000
