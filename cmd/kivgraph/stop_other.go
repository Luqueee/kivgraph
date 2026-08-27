//go:build !unix

package main

// gracefulStopSupported reports whether this platform can ask a process this
// one did not start to exit on its own.
//
// Windows cannot. A console control event reaches only processes attached to
// the same console, and a window message reaches only something that has a
// window; a daemon started by the supervisor has neither. What is left is
// TerminateProcess, which is not a request and gives the process no chance to
// finish what it was doing.
//
// So the grace period is not shortened here, it is absent, and `stop` says
// which of the two happened rather than printing the same line for both. A
// server that is killed mid-answer is a different event from one that was
// asked and agreed, and an operator who cannot tell them apart will read a
// truncated response as a bug in the server.
const gracefulStopSupported = false
