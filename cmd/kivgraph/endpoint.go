package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/integrations"
	"github.com/Luqueee/kivgraph/internal/supervisor"
)

// endpointDeadline bounds the wait for a supervised daemon to answer.
//
// A supervisor starts the daemon asynchronously, and the daemon binds before it
// reads any graph -- since ADR 0067 the snapshot is loaded by the first query
// that needs it, so coming up is a bind and a token, not an index. Five seconds
// is generous for that and short enough that a daemon which cannot bind reports
// instead of hanging the command.
const endpointDeadline = 5 * time.Second

// stateDirectory is where a configuration keeps its generation, and therefore
// which daemon it belongs to.
//
// It lives here rather than being derived twice because the daemon writes its
// endpoint into this directory and an integration command reads it back: two
// derivations that drifted apart would have the command configure clients
// against a daemon that is not the one this configuration starts.
func stateDirectory(loaded config.Loaded) string {
	return filepath.Dir(loaded.Config.Storage.DatabasePath)
}

// transport is what an integration command decides to write into a client.
type transport int

const (
	// transportDaemon writes a url entry pointing at one supervised daemon.
	transportDaemon transport = iota
	// transportStdio writes a command entry the client spawns for itself.
	transportStdio
)

// resolveTransport decides which entry an integration command writes.
//
// The daemon is the default, and `benchmarks/daemon-cost` is why: at eight idle
// clients -- which the event log of a real machine says is the normal case, with
// 66 of 69 servers never answering a single call -- one process holds 10-13 MB
// of private pages against 77-81, and peaks at 26-29 against 179-186.
//
// What made that default safe is ADR 0068: the daemon has an owner now. A url
// entry pointing at a process nobody restarts takes every client down at once,
// which is why this used to be opt-in.
//
// The choice is deterministic, and that distinction is the whole design. It
// never depends on whether a daemon happens to be running at this moment --
// detecting one and silently writing a url would make the same command write two
// different files on two days. It does depend on the scope and the platform, and
// those are declared conditions a reader can predict, not the state of a process
// table.
func resolveTransport(options integrationOptions, stdout io.Writer) (transport, error) {
	switch {
	case options.Stdio && options.Daemon:
		return 0, errors.New("--stdio and --daemon ask for opposite entries: pass one")
	case options.Stdio:
		return transportStdio, nil
	}

	// A url entry carries a bearer token, and a project-scoped file is
	// committed. `integrations.New` refuses that combination outright, so an
	// explicit --daemon still fails there; the default writes the entry that
	// works and names why it is not the daemon.
	if options.Scope == integrations.ScopeProject && !options.Daemon {
		fmt.Fprintln(stdout, "mcp: stdio in project scope: a url entry carries a token, and this file is committed")
		return transportStdio, nil
	}
	return transportDaemon, nil
}

// integrationManagerOptions builds the manager for an integration command.
//
// provision separates a writer from a reader, and the separation is the point.
// `mcp install` may bring a daemon up; `mcp status` may not. A status that
// installed a supervisor as a side effect of being asked a question would be a
// surprise, and a read-only command has no business starting a process.
//
// When a writer chooses the daemon this ensures one exists: it installs the
// supervisor if it is missing, waits for the endpoint to answer, and reports each
// step. Ensuring rather than detecting is what keeps the outcome deterministic --
// and it is also the only honest reading of "the daemon is the default", because
// a default that fails on a machine which has never run one is not a default.
//
// A platform with no supervisor writes stdio and says so. That is a declared
// platform limitation, not a moment in time: the same command on the same
// machine keeps writing the same file.
func integrationManagerOptions(options integrationOptions, provision bool, stdout io.Writer) (integrations.Options, error) {
	chosen, err := resolveTransport(options, stdout)
	if err != nil {
		return integrations.Options{}, err
	}
	if chosen == transportStdio {
		return integrations.Options{}, nil
	}
	loaded, err := config.Load("")
	if err != nil {
		// No configuration means no state directory, so there is no daemon this
		// command could point at -- and `mcp install` worked here yesterday, so
		// it cannot start failing today. It is a declared condition like an
		// unsupported platform, not a moment in time: the same machine keeps
		// writing the same entry until somebody runs `kivgraph init`.
		if options.Daemon {
			return integrations.Options{}, fmt.Errorf("--daemon: read the configuration: %w", err)
		}
		fmt.Fprintln(stdout, "mcp: stdio: no configuration yet, so no daemon to point at: run `kivgraph init` first")
		return integrations.Options{}, nil
	}
	directory := stateDirectory(loaded)
	published, readErr := daemon.ReadEndpoint(directory)

	if !provision {
		// A reader compares shapes, so it wants the endpoint's identity and not
		// its liveness: the token survives a restart, so a published file
		// describes the entry this configuration installs even while the daemon
		// is down. With no file at all there is no url to compare against, and
		// the stdio shape is what an install would have written.
		if readErr != nil {
			fmt.Fprintln(stdout, "mcp: no daemon endpoint is published, so this compares against the stdio entry")
			return integrations.Options{}, nil
		}
		return endpointOptions(published), nil
	}

	// An endpoint that already answers is the common case, and it costs one
	// dial. Nothing is installed or started when a daemon is already serving.
	if readErr == nil && daemon.Reachable(context.Background(), published, time.Second) == nil {
		return endpointOptions(published), nil
	}
	endpoint, err := ensureDaemon(loaded.ConfigPath, directory, options, stdout)
	var fallback errStdioFallback
	if errors.As(err, &fallback) {
		fmt.Fprintf(stdout, "mcp: stdio: %s\n", fallback.reason)
		return integrations.Options{}, nil
	}
	if err != nil {
		return integrations.Options{}, err
	}
	return endpointOptions(endpoint), nil
}

func endpointOptions(endpoint daemon.Endpoint) integrations.Options {
	return integrations.Options{
		Endpoint: integrations.Endpoint{URL: endpoint.URL, Token: endpoint.Token},
	}
}

// ensureDaemon brings a supervised daemon up and returns its endpoint.
func ensureDaemon(configPath, directory string, options integrationOptions, stdout io.Writer) (daemon.Endpoint, error) {
	executable, err := os.Executable()
	if err != nil {
		return daemon.Endpoint{}, fmt.Errorf("resolve this executable: %w", err)
	}
	// The resolved configuration is recorded, not left empty. A supervisor
	// starts the daemon outside this shell, so a daemon that resolved its own
	// configuration would resolve it against the supervisor's environment and
	// could serve a different state directory than the one this command just
	// pointed clients at.
	spec := supervisor.Spec{Executable: executable, StateDirectory: directory, ConfigPath: configPath}

	report, err := supervisor.Install(spec)
	switch {
	case errors.Is(err, supervisor.ErrUnsupportedPlatform):
		// Not a failure: the platform declares it has no supervisor. An
		// explicit --daemon still refuses, because the caller asked for the
		// thing this machine cannot keep running.
		if options.Daemon {
			return daemon.Endpoint{}, fmt.Errorf(
				"--daemon: %s: start one with `kivgraph daemon` and re-run", report.Detail)
		}
		return daemon.Endpoint{}, errStdioFallback{reason: report.Detail}
	case err != nil:
		return daemon.Endpoint{}, fmt.Errorf(
			"install the daemon's supervisor: %w\npass --stdio to register a per-client `serve` instead", err)
	}
	fmt.Fprintf(stdout, "mcp: daemon supervised by %s (%s)\n", report.Label, report.Path)

	endpoint, err := daemon.WaitReachable(context.Background(), directory, endpointDeadline)
	if err != nil {
		// The supervisor is installed and the daemon is not answering. Writing
		// a url here would hand every client a dead address, and writing stdio
		// would hide a daemon that cannot start.
		return daemon.Endpoint{}, fmt.Errorf(
			"the supervised daemon did not answer: %w\ncheck `kivgraph daemon status`, or pass --stdio", err)
	}
	fmt.Fprintf(stdout, "mcp: daemon endpoint %s\n", endpoint.URL)
	return endpoint, nil
}

// errStdioFallback carries a declared reason to write stdio instead of a url. It
// is a value rather than a bare bool so the reason reaches the operator: a
// silent downgrade is the defect this whole seam exists to avoid.
type errStdioFallback struct {
	reason string
}

func (err errStdioFallback) Error() string { return err.reason }
