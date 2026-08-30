package main

import (
	"context"
	"os"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/telemetry"
	"github.com/Luqueee/kivgraph/internal/version"
)

// firstRunOptions builds what the emitter is handed for one serving command.
//
// The transport is derived here and cannot be passed in, which is the fix for
// the defect that made this a function: `runConfiguredServe` runs both `serve`
// and `daemon`, its call site passed the literal `"stdio"`, and the daemon's
// first run was recorded as a stdio one. The marker is created once per
// version, so that wrong row was the only row that version would ever have
// produced -- in the one field that exists to tell those two apart.
func firstRunOptions(loaded config.Loaded, command string) telemetry.Options {
	executable, err := os.Executable()
	if err != nil {
		// Without it there is no layout to read, so `Announce` declines. Which
		// is the right answer: a binary this process cannot locate is not one
		// whose channel it can report.
		executable = ""
	}
	transport := "stdio"
	if command == "daemon" {
		transport = "daemon"
	}
	return telemetry.Options{
		StateDirectory: stateDirectory(loaded),
		Version:        version.Value,
		Transport:      transport,
		Executable:     executable,
		// Never os.Stdout. `serve` speaks MCP over it.
		Notice: os.Stderr,
	}
}

// announceFirstRun reports the first run of this version, off the path of the
// command that triggered it.
//
// It is called from the two places that reach a serving command -- the tail of
// `runConfiguredServe`, which is `serve` in process and the daemon, and the
// relay, which never returns -- and from nowhere else. What Layer 1 of ADR
// 0083 measures is *machines that ran the server*, and `kivgraph index`
// running in a terminal is a different fact; it also has no transport, which is
// a field the endpoint requires on a binary row rather than defaulting.
//
// The goroutine is what keeps it off the path. `telemetry.Announce` bounds
// itself to two seconds and drops every error, so the worst it can cost a
// session is a goroutine that outlives its usefulness by that long.
func announceFirstRun(loaded config.Loaded, command string) {
	go telemetry.Announce(context.Background(), firstRunOptions(loaded, command))
}

// announceRelayedFirstRun reports a `serve` that is about to relay.
//
// Separate from the above because the command is `serve` and the arrangement
// that will answer is the daemon: the one case where the two disagree, and the
// reason the transport is a field at all.
func announceRelayedFirstRun(loaded config.Loaded) {
	go telemetry.Announce(context.Background(), relayedFirstRunOptions(loaded))
}

// relayedFirstRunOptions is that case as a value, so a test can hold it.
func relayedFirstRunOptions(loaded config.Loaded) telemetry.Options {
	options := firstRunOptions(loaded, "serve")
	options.Transport = "daemon"
	return options
}

// supervisorInstallOptions builds what the emitter is handed when `daemon
// install` succeeds.
//
// It does not reuse `firstRunOptions`: that function derives a transport from
// a command name, and there is no arrangement to name here -- a supervisor
// entry was registered, and nothing is serving yet.
func supervisorInstallOptions(loaded config.Loaded) telemetry.Options {
	executable, err := os.Executable()
	if err != nil {
		executable = ""
	}
	return telemetry.Options{
		StateDirectory: stateDirectory(loaded),
		Version:        version.Value,
		Executable:     executable,
		// Never os.Stdout, for the same reason as the two above, even though
		// `daemon install` does not itself speak a protocol over it: the
		// notice is one function shared with the paths that do.
		Notice: os.Stderr,
	}
}

// announceSupervisorInstall reports that `daemon install` gave this machine a
// supervisor entry for this version, off the path of the command.
//
// Called once, from the success tail of `runSupervisorCommand`'s `"install"`
// case, and from nowhere else: `daemon remove` and `daemon status` change or
// read that entry but do not create it, and are not the fact this reports.
func announceSupervisorInstall(loaded config.Loaded) {
	go telemetry.AnnounceSupervisorInstall(context.Background(), supervisorInstallOptions(loaded))
}
