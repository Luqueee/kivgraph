//go:build !webassets

package webassets

// Available reports whether this binary carries the versioned web bundle.
//
// The published MCP bundle is built without it on purpose, so a command that
// exists to show the viewer can say so instead of opening a server that only
// answers with the unavailable page.
func Available() bool { return false }
