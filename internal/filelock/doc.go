// Package filelock is the exclusive right to do one thing per state
// directory, held across processes.
//
// It is an advisory lock on a file and not a leader election: whoever holds it
// does the work and everyone else notices through whatever the work publishes.
// The kernel releases it if the holder dies, which is the property a pid file
// cannot offer, and it is the reason this is written against each platform's
// own primitive rather than stubbed out.
//
// It was `internal/indexing`'s, used by one caller. It moved here when a second
// wanted it: `serve` installing the supervisor unit, where eight editors
// starting at once would otherwise run eight `systemctl daemon-reload`s to
// arrive at the one daemon systemd was going to start anyway.
//
// `internal/storage/generation` still carries a third copy with a different
// contract -- it takes the lock or fails, where this one refuses without
// waiting -- and has not been migrated.
package filelock
