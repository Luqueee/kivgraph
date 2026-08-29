package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/eventlog"
	"github.com/Luqueee/kivgraph/internal/filelock"
	"github.com/Luqueee/kivgraph/internal/integrations"
	"github.com/Luqueee/kivgraph/internal/logging"
	"github.com/Luqueee/kivgraph/internal/relay"
	"github.com/Luqueee/kivgraph/internal/supervisor"
)

// endpointDeadline bounds the wait for a supervised daemon to answer.
//
// A supervisor starts the daemon asynchronously, and the daemon binds before it
// reads any graph -- since ADR 0067 the snapshot is loaded by the first query
// that needs it, so coming up is a bind and a token, not an index. Five seconds
// is generous for that and short enough that a daemon which cannot bind reports
// instead of hanging the command.
var endpointDeadline = 5 * time.Second

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

// serveInProcessEnv forces `serve` to answer from its own graph even when a
// daemon is running.
//
// It exists because the relay is the new default on a path a client spawns
// unattended: without an escape, a machine where the relay misbehaves has no
// way back that does not involve stopping the daemon other clients are using.
const serveInProcessEnv = "KIVGRAPH_SERVE_IN_PROCESS"

// relayToTheDaemon forwards this `serve` to a running daemon and answers
// whether it did.
//
// ADR 0084: the stdio *entry* is permanent -- the `.mcpb` manifest has no field
// for a url, and a url in a committed project file would carry the token -- but
// the stdio *server* is not, and it is the one that costs. Measured on
// generation `000091`, a `serve` answering costs `68.9`-`70.3 MB` per client
// against a relay's `8.7`-`9.8`, and peaks at `2.5 GB` across eight clients
// against `0.44`. See `benchmarks/relay-cost`.
//
// Every way of declining returns false and no error, because each leaves the
// caller's existing behaviour correct: this is `daemon` or `ui` rather than
// `serve`, the escape hatch is set, `--introspection` asked about this server
// in particular, no daemon published an endpoint, or nothing answered where one
// said it would. The last is the fallback ADR 0084 promised for a platform with
// no supervisor, and on those two paths nothing is worse than it was.
func relayToTheDaemon(
	ctx context.Context,
	command, configPath string,
	loaded config.Loaded,
	provision daemonProvisioner,
	introspection bool,
) (bool, error) {
	if command != "serve" {
		return false, nil
	}
	logger := logging.New(os.Stderr)
	if os.Getenv(serveInProcessEnv) != "" {
		logger.Info("serving in process because "+serveInProcessEnv+" is set", "command", command)
		return false, nil
	}
	// Four: --introspection asks what *this* server publishes, and a daemon
	// that nobody asked for introspection would answer a different question --
	// with no generation published, by publishing no query tool at all, which
	// is precisely the state the flag exists to look past. Relaying it would
	// be a silent no-op, and a flag that does nothing is worse than one that
	// is refused.
	if introspection {
		logger.Info("serving in process because --introspection asks what this server publishes", "command", command)
		return false, nil
	}
	// Probed rather than trusted, and here rather than inside the relay,
	// because only this side can fall back: once the relay has read the
	// agent's handshake there is no in-process server left to hand it to. A
	// daemon that dies between this probe and the connection is a declared
	// race -- the relay fails, and the client that spawned this process starts
	// another.
	endpoint, answering := reachableDaemon(ctx, loaded)
	if !answering {
		if provision == nil {
			return false, nil
		}
		provisioned, ok := provision(ctx, command, configPath, loaded, logger)
		if !ok {
			return false, nil
		}
		endpoint = provisioned
	}

	events := openEventLog(loaded.Config, os.Stderr)
	started := time.Now()
	// The message says which arrangement served, because nothing else does and
	// the fallback rate is the number that decides whether provisioning is
	// worth building. A relay and an in-process server answer identically.
	events.Append(eventlog.Event{Kind: eventlog.KindServe, Message: command + " started as a relay to the daemon"})
	defer func() {
		events.Append(eventlog.Event{
			Kind:    eventlog.KindServe,
			Message: command + " stopped",
		}.WithDuration(time.Since(started)))
		if err := events.Close(); err != nil {
			writeWarning(os.Stderr, "events: close: %v", err)
		}
	}()
	logger.Info("relaying to the daemon", "command", command, "endpoint", endpoint.URL)
	announceRelayedFirstRun(loaded)
	return true, relay.Run(ctx, endpoint)
}

// provisionLockName is the file a burst of relays contends on.
//
// It lives in the state directory because that is what a daemon belongs to:
// two configurations get two daemons, so they must be allowed to provision at
// the same time without seeing each other.
const provisionLockName = "daemon-provision.lock"

// reachableDaemon answers the daemon of this configuration, if one is
// answering right now.
func reachableDaemon(ctx context.Context, loaded config.Loaded) (daemon.Endpoint, bool) {
	endpoint, err := daemon.ReadEndpoint(stateDirectory(loaded))
	if err != nil {
		// A daemon writes this before it serves, so its absence is the answer
		// and not a failure to get one.
		return daemon.Endpoint{}, false
	}
	if err := relay.Reachable(ctx, endpoint); err != nil {
		return daemon.Endpoint{}, false
	}
	return endpoint, true
}

// daemonProvisioner installs a supervised daemon for this configuration and
// answers its endpoint, or declines.
//
// It is injected for the reason `supervisedDaemonRestart` is: a test must never
// reach the developer's own supervisor. This one is sharper still -- before it
// was a parameter, two tests of the *decline* paths were running `systemctl`
// against the real user manager to arrive at "no", which is a side effect no
// test may have and which a passing suite said nothing about.
//
// A nil provisioner installs nothing, which is what every caller that is not
// `serve` wants.
type daemonProvisioner func(
	ctx context.Context,
	command, configPath string,
	loaded config.Loaded,
	logger *slog.Logger,
) (daemon.Endpoint, bool)

// provisionDaemon installs the supervisor for this configuration's daemon and
// waits for it to publish an endpoint, so that a `.mcpb` installation gets the
// daemon it can never be configured for.
//
// It is not `ensureDaemon` above, which serves the integration commands: that
// one is an operator asking for a daemon and is entitled to fail loudly at
// them. This one runs unattended inside a server a client spawned, so every
// way it can decline ends in serving as before rather than in an error nobody
// is there to read.
//
// The principle is `ensureConfiguration`'s and was argued there: an MCP client
// spawns its servers itself, so a server that exits because nobody ran a
// terminal command reports only that it failed. This is the same trade one
// step further out.
//
// **It acts only when no unit is installed at all.** An installed unit whose
// daemon is not answering is somebody who ran `kivgraph stop`, and starting it
// again would make that command unable to stop anything -- which is the exact
// argument ADR 0068 gives for why the unit is `Restart=on-failure` and not
// `Restart=always`. A hand-edited unit is left alone for the reason `Status`
// reports rather than repairs it, and a platform with no supervisor is the
// fallback ADR 0084 promised.
//
// Everything it declines is `false` with no error: the caller then serves in
// process, which is what it did before any of this existed.
func provisionDaemon(
	ctx context.Context,
	command, configPath string,
	loaded config.Loaded,
	logger *slog.Logger,
) (daemon.Endpoint, bool) {
	spec, err := supervisorSpec(configPath, supervisorOptions{})
	if err != nil {
		logger.Info("serving in process: no daemon could be described",
			"command", command, "reason", err)
		return daemon.Endpoint{}, false
	}
	report, err := supervisor.Status(spec)
	if err != nil || report.State != supervisor.StateAbsent {
		logger.Info("serving in process: not installing a daemon",
			"command", command, "state", string(report.State), "detail", report.Detail)
		return daemon.Endpoint{}, false
	}

	// Eight editors starting at once all find no daemon. Without this they run
	// eight `systemctl daemon-reload`s to arrive at the one daemon systemd was
	// going to start anyway; the losers wait for that daemon instead. The lock
	// is advisory and the kernel drops it if the holder dies, so a relay killed
	// mid-install does not leave the next one waiting forever.
	lock, held, err := filelock.Acquire(filepath.Join(stateDirectory(loaded), provisionLockName))
	if err != nil {
		logger.Info("serving in process: could not take the provisioning lock",
			"command", command, "reason", err)
		return daemon.Endpoint{}, false
	}
	if !held {
		endpoint, ok := awaitDaemon(ctx, loaded)
		if !ok {
			logger.Info("serving in process: another server is installing the daemon",
				"command", command, "waited", endpointDeadline)
		}
		return endpoint, ok
	}
	defer func() {
		if err := lock.Release(); err != nil {
			writeWarning(os.Stderr, "serve: release the provisioning lock: %v", err)
		}
	}()

	installed, err := supervisor.Install(spec)
	if err != nil {
		logger.Info("serving in process: the daemon could not be installed",
			"command", command, "reason", err)
		return daemon.Endpoint{}, false
	}
	endpoint, ok := awaitDaemon(ctx, loaded)
	if !ok {
		logger.Warn("serving in process: the installed daemon published no endpoint",
			"command", command, "label", installed.Label, "waited", endpointDeadline)
		return daemon.Endpoint{}, false
	}
	// Said once, and out loud. A machine that installed a `.mcpb` extension
	// acquires a supervised background service it never asked for in a
	// terminal, so the line that tells it so also names the way out.
	logger.Info("installed a background daemon for this configuration; remove it with \"kivgraph daemon remove\"",
		"command", command, "label", installed.Label, "unit", installed.Path)
	return endpoint, true
}

// awaitDaemon waits for a daemon to publish an endpoint and answer on it.
//
// It polls rather than watching, because the thing being waited for is another
// process binding a port and writing a file, and there is no readiness signal
// between them. `endpointDeadline` is what bounds it: a supervisor starts the
// daemon asynchronously, and since ADR 0067 coming up is a bind and a token
// rather than an index.
func awaitDaemon(ctx context.Context, loaded config.Loaded) (daemon.Endpoint, bool) {
	deadline := time.Now().Add(endpointDeadline)
	for {
		if endpoint, ok := reachableDaemon(ctx, loaded); ok {
			return endpoint, true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return daemon.Endpoint{}, false
		}
		select {
		case <-ctx.Done():
			return daemon.Endpoint{}, false
		case <-time.After(50 * time.Millisecond):
		}
	}
}
