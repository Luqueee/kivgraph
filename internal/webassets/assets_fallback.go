//go:build !webassets

package webassets

import "net/http"

// New returns the explicit response used by binaries built without web assets.
func New() http.Handler {
	return http.HandlerFunc(serveUnavailable)
}
