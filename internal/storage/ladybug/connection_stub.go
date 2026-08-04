//go:build !ladybug || !cgo

package ladybug

import "context"

// Open reports that the native build is required for database access.
func Open(ctx context.Context, path string, _ Config) (Database, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, &Error{Op: "open", Err: err}
	}
	return nil, &Error{Op: "open", Err: ErrUnavailable}
}
