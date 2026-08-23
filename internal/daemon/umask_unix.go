//go:build unix

package daemon

import (
	"sync"
	"syscall"
)

// umaskMutex serialises the process-wide umask this file borrows.
//
// A library changing a process's umask is a real side effect, and the honest
// mitigation is the narrow one: it is held for exactly one bind, restored on
// every path out, and Listen is called once per daemon at startup. The
// alternative -- chmod after binding -- fails on filesystems that refuse it for a
// socket, which is a worse trade: an inability to start instead of a moment of
// borrowed state.
var umaskMutex sync.Mutex

// withPrivateUmask runs body with a umask that makes anything it creates
// readable and writable by the owner alone.
func withPrivateUmask(body func() error) error {
	umaskMutex.Lock()
	defer umaskMutex.Unlock()
	previous := syscall.Umask(0o177)
	defer syscall.Umask(previous)
	return body()
}
