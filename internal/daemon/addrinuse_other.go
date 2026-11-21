//go:build !windows

package daemon

import (
	"errors"
	"syscall"
)

// isAddressInUse reports whether a bind failed because something already
// listens on the address.
func isAddressInUse(err error) bool { return errors.Is(err, syscall.EADDRINUSE) }
