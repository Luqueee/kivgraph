//go:build !ladybug || !cgo

package ladybug

import "context"

// VerifyCanonicalIntegrity reports that the native build is required for
// integrity verification.
func VerifyCanonicalIntegrity(ctx context.Context, path string) (CanonicalIntegrityReport, error) {
	if err := validatePath(path); err != nil {
		return CanonicalIntegrityReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return CanonicalIntegrityReport{}, &Error{Op: "verify canonical integrity", Err: err}
	}
	return CanonicalIntegrityReport{}, &Error{Op: "verify canonical integrity", Err: ErrUnavailable}
}
