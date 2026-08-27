// Package privateobject keeps a named Windows object to the account that made
// it.
//
// Two things this repository creates have names the whole machine can see: the
// pipe the TypeScript worker writes its frames into, and the socket the daemon
// serves the graph over. On Unix neither is a problem -- one is anonymous and
// reachable only through an inherited descriptor, and the other is created
// under a narrowed umask, which holds wherever the caller pointed it. Windows
// has no umask and both objects live in namespaces, so the privacy has to be
// stated rather than inherited, and stated the same way in both places.
//
// It is stated once, here. The package has no exported surface off Windows
// because there is no question to answer there.
package privateobject
