//go:build !ladybug || !cgo

package ladybug

import (
	"context"

	"github.com/Luqueee/kivgraph/internal/facts"
)

// ApplyCanonicalDelta reports that the native build is required to mutate
// the canonical graph.
func ApplyCanonicalDelta(ctx context.Context, path string, _ facts.Delta, _ CanonicalLoadOptions) (CanonicalMutationResult, error) {
	if err := validatePath(path); err != nil {
		return CanonicalMutationResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return CanonicalMutationResult{}, &Error{Op: "apply canonical delta", Err: err}
	}
	return CanonicalMutationResult{}, &Error{Op: "apply canonical delta", Err: ErrUnavailable}
}
