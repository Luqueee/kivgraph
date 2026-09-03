//go:build linux

package supervisor

import "testing"

// renderedUnit is what an install would write, so a test can compare against the
// real file rather than a fixture that drifts from it.
func renderedUnit(t *testing.T, spec Spec) []byte {
	t.Helper()
	return []byte(unit(spec, daemonPath()))
}
