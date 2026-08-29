package main

import (
	"context"
	"os"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/telemetry"
	"github.com/Luqueee/kivgraph/internal/version"
)

// transportOf names the arrangement a command is about to serve with.
//
// `runConfiguredServe` runs both `serve` and `daemon`, which is what made this
// a function rather than a literal: reporting `stdio` for every command that
// reached it recorded the daemon's first run as a stdio one, and the marker is
// exclusive, so that wrong row was the only row that version would ever
// produce. `serve` relaying reports for itself before this is reached, because
// it never comes back.
func transportOf(command string) string {
	if command == "daemon" {
		return "daemon"
	}
	return "stdio"
}

// announceFirstRun reports the first run of this version, off the path of the
// command that triggered it.
//
// It is called from the two places that reach a serving command -- the tail of
// `runConfiguredServe`, which is `serve` in process and the daemon, and the
// relay, which never returns -- and from nowhere else. What Layer 1 of ADR 0083 measures is *machines that ran the server*,
// and `kivgraph index` running in a terminal is a different fact; it also has
// no transport, which is a field the endpoint requires on a binary row rather
// than defaulting.
//
// The goroutine is what keeps it off the path. `telemetry.Announce` bounds
// itself to two seconds and drops every error, so the worst it can cost a
// session is a goroutine that outlives its usefulness by that long.
func announceFirstRun(loaded config.Loaded, transport string) {
	executable, err := os.Executable()
	if err != nil {
		// Without it there is no layout to read, so `Announce` declines. Which
		// is the right answer: a binary this process cannot locate is not one
		// whose channel it can report.
		executable = ""
	}
	options := telemetry.Options{
		StateDirectory: stateDirectory(loaded),
		Version:        version.Value,
		Transport:      transport,
		Executable:     executable,
		// Never os.Stdout. `serve` speaks MCP over it.
		Notice: os.Stderr,
	}
	go telemetry.Announce(context.Background(), options)
}
