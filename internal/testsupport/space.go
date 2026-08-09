package testsupport

import (
	"errors"
	"testing"

	"github.com/Luqueee/ladygraph/internal/storage/generation"
)

// RequireSpaceOrSkip turns the one failure that belongs to the host rather
// than to the code under test into a skip.
//
// Publishing a generation refuses a filesystem that is more than 85% full,
// whatever the size of the database, so on such a host a test that needs a
// published generation never reaches the behaviour it asserts. The policy
// itself is covered by the generation package, which builds its store with an
// explicit one.
func RequireSpaceOrSkip(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, generation.ErrInsufficientSpace) {
		t.Skipf("host filesystem cannot satisfy the default space policy: %v", err)
	}
}
