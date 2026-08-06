//go:build !ladybug || !cgo

package ladybug

import "context"

// ScanCanonical reports that the native build is required to read the
// definitive graph.
func ScanCanonical(ctx context.Context, path string) (CanonicalGraph, error) {
	if err := validatePath(path); err != nil {
		return CanonicalGraph{}, err
	}
	if err := ctx.Err(); err != nil {
		return CanonicalGraph{}, &Error{Op: "scan canonical", Err: err}
	}
	return CanonicalGraph{}, &Error{Op: "scan canonical", Err: ErrUnavailable}
}
