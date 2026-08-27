//go:build unix

package main

// gracefulStopSupported reports whether this platform can ask a process this
// one did not start to exit on its own.
//
// A signal is exactly that, so here it can.
const gracefulStopSupported = true
