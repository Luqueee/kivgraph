package ladybug

import (
	"errors"
	"fmt"
)

var (
	// ErrUnavailable reports that the native LadybugDB build is not enabled.
	ErrUnavailable = errors.New("LadybugDB native support is unavailable")
	// ErrClosed reports an operation attempted after database closure.
	ErrClosed = errors.New("LadybugDB database is closed")
	// ErrInvalidPath reports an empty database path.
	ErrInvalidPath = errors.New("LadybugDB database path is empty")
	// ErrInvalidStableKey reports an empty symbol stable key.
	ErrInvalidStableKey = errors.New("LadybugDB symbol stable key is empty")
	// ErrInvalidLimit reports a non-positive or excessive result limit.
	ErrInvalidLimit = errors.New("LadybugDB query limit is outside the supported range")
	// ErrInvalidDepth reports a traversal depth outside the supported range.
	ErrInvalidDepth = errors.New("LadybugDB traversal depth is outside the supported range")
	// ErrReadOnly reports a writer requested from a read-only database.
	ErrReadOnly = errors.New("LadybugDB database is read-only")
	// ErrWriterOpen reports that the database already owns its single logical writer.
	ErrWriterOpen = errors.New("LadybugDB writer is already open")
	// ErrInvalidMutation reports a structurally invalid incremental delta.
	ErrInvalidMutation = errors.New("LadybugDB incremental mutation is invalid")
	// ErrAlreadyExists reports an attempted insertion of an existing entity.
	ErrAlreadyExists = errors.New("LadybugDB entity already exists")
	// ErrNotFound reports an update or deletion whose target does not exist.
	ErrNotFound = errors.New("LadybugDB entity was not found")
)

// Error wraps a native or lifecycle failure with the Ladygraph operation that failed.
type Error struct {
	Op  string
	Err error
}

func (err *Error) Error() string {
	if err == nil {
		return "<nil>"
	}
	return fmt.Sprintf("ladybug %s: %v", err.Op, err.Err)
}

func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}
