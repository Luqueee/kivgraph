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
)

// Error wraps a native or lifecycle failure with the Luque operation that failed.
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
