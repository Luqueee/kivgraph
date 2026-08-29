package main

import "testing"

// The daemon and `serve` share `runConfiguredServe`, so the transport cannot be
// a literal there. Reporting `stdio` for the daemon is not a cosmetic error:
// the marker is created once per version, so the wrong row is the only row that
// version will ever produce, and the field exists to measure exactly this.
func TestTransportOfSeparatesTheDaemonFromServe(t *testing.T) {
	for command, want := range map[string]string{
		"daemon": "daemon",
		"serve":  "stdio",
	} {
		if got := transportOf(command); got != want {
			t.Errorf("transportOf(%q) = %q, want %q", command, got, want)
		}
	}
}
