//go:build windows

package workspace

import (
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// Windows has two path shapes Unix does not, and filepath.IsAbs reports false
// for both: `\outside` is rooted on the current drive but names no volume, and
// `C:outside` is relative to that drive's own working directory. A validator
// that asks only IsAbs joins them to the repository root and concludes they
// are inside it -- which is how the shared table's "manifest escape" case
// passed here while asserting a refusal, with `/outside/manifest.json` landing
// under the temporary directory instead of at the root of C:.
//
// These are the cases the shared table cannot carry, because on Unix a
// backslash is an ordinary character in a file name and `C:outside` is a
// directory called `C:`. Refusing them there would be refusing a legal path.
func TestValidateScopedPathRefusesPathsRootedOutsideTheRepository(t *testing.T) {
	base := testsupport.TempDir(t)

	for _, rawPath := range []string{
		`/outside/manifest.json`,
		`\outside\manifest.json`,
		`C:outside\manifest.json`,
		`..\outside`,
	} {
		t.Run(rawPath, func(t *testing.T) {
			err := validateScopedPath(base, rawPath)
			if err == nil {
				t.Fatalf("validateScopedPath(%q) = nil: a path the operating system resolves "+
					"outside the repository was accepted as one inside it", rawPath)
			}
			if !strings.Contains(err.Error(), "escapes repository realpath") {
				t.Fatalf("validateScopedPath(%q) error = %v, want the containment refusal", rawPath, err)
			}
		})
	}
}

// The tightening must not refuse what it was always meant to allow: a plain
// relative path under the root, and an absolute path that genuinely names
// something inside it.
func TestValidateScopedPathStillAcceptsPathsInsideTheRepository(t *testing.T) {
	base := testsupport.TempDir(t)

	for _, rawPath := range []string{
		`packages\api`,
		`packages/api`,
		`a\..\b`,
		base + `\packages\api`,
	} {
		t.Run(rawPath, func(t *testing.T) {
			if err := validateScopedPath(base, rawPath); err != nil {
				t.Fatalf("validateScopedPath(%q) error = %v, want it accepted", rawPath, err)
			}
		})
	}
}
