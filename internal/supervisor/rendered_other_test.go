//go:build !linux && !darwin && !windows

package supervisor

import "testing"

// renderedUnit has nothing to render here: the platform declares its absence
// through ErrUnsupportedPlatform, and the tests that ask for a unit skip. It
// exists so the test package still builds on a platform the project does not
// distribute to -- a package that fails to compile there would hide the absence
// instead of declaring it.
func renderedUnit(t *testing.T, _ Spec) []byte {
	t.Helper()
	t.Fatal("this platform renders no supervisor unit")
	return nil
}
