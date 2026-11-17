//go:build windows

package daemon

import (
	"errors"
	"syscall"
)

// wsaeAddrInUse is WSAEADDRINUSE. The syscall package's EADDRINUSE on Windows is
// a number of its own and no bind ever returns it, so the check the other
// platforms use never matches a busy port here and the daemon would refuse to
// start instead of choosing another one.
const wsaeAddrInUse syscall.Errno = 10048

// isAddressInUse reports whether a bind failed because something already
// listens on the address.
func isAddressInUse(err error) bool { return errors.Is(err, wsaeAddrInUse) }
