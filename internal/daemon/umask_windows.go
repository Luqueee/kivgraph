//go:build windows

package daemon

import "github.com/Luqueee/kivgraph/internal/privateobject"

// withPrivateUmask runs body without narrowing what it creates, because there
// is no umask here to narrow with. What replaces it is narrowSocket, which
// acts after the fact instead of before.
func withPrivateUmask(body func() error) error { return body() }

// narrowSocket makes the socket the owner's alone.
//
// AF_UNIX works on Windows and the socket is a real file, so it arrives with
// whatever ACL its directory hands down. Measured, that is already private --
// but only because the state directory sits in a user profile that is. Point
// the state directory somewhere permissive and the socket takes that instead,
// where the Unix path creates it 0600 wherever it was told to live. The graph
// is served over this, so the guarantee should be the daemon's own and not a
// property of where somebody put it.
//
// There is a window between the bind and this call. It is not closed here
// because it cannot be: the listener creates the file, so nothing can set its
// descriptor first. What narrows it is that the file is useless to a reader
// for that instant -- no daemon is accepting on it yet -- and that the
// directory it appears in is itself private in the layout this ships.
func narrowSocket(path string) error { return privateobject.Narrow(path) }

// modeBitsAreEvidence reports whether a file's reported mode says anything
// about who can read it. Go answers 0666 for every regular file here whatever
// the ACL says, so it says nothing.
const modeBitsAreEvidence = false
