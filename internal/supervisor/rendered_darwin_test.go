//go:build darwin

package supervisor

import "testing"

// renderedUnit is what an install would write, so a test can compare against the
// real file rather than a fixture that drifts from it.
func renderedUnit(t *testing.T, spec Spec) []byte {
	t.Helper()
	label, err := spec.Label()
	if err != nil {
		t.Fatalf("Label() error = %v", err)
	}
	rendered, err := plist(spec, label)
	if err != nil {
		t.Fatalf("plist() error = %v", err)
	}
	return rendered
}
