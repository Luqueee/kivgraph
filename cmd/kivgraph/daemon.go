package main

import (
	"context"
	"log/slog"

	"golang.org/x/sync/errgroup"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/eventlog"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/indexing"
)

// runDaemon serves MCP over a socket and over HTTP, from one process.
//
// What it changes against `serve` is only who owns the snapshot. A client that
// spawns `serve` gets a process of its own, and at the load a real editor
// produces eight of them cost `323`-`330 MB` against `60`-`62` for one daemon --
// `77`-`81` against `10`-`13` when nobody asks anything, which is what 48 of 51
// real servers do.
// Measured in `benchmarks/daemon-cost`.
//
// Two transports because a client reaches one or the other and not both: an
// editor's configuration takes an executable or a `url`, never a socket path, so
// HTTP is what makes the saving reachable at all. The socket stays because it is
// the narrower door -- its mode is the key, where HTTP needs a token.
// The flags arrive as a pointer, not as a built value, and that is the fix for a
// real defect: this runner is constructed before `runConfiguredServe` parses the
// command line, so a value built at construction time read every flag as its
// zero. `--addr` and `--allow-remote` were declared, documented and discarded --
// the daemon could only ever bind `127.0.0.1:7788`. Reading them here reads them
// after the parse.
func runDaemon(logger *slog.Logger, flags *daemonOptions) configuredMCPRunner {
	return func(
		ctx context.Context,
		loaded config.Loaded,
		store *hotsnapshot.SnapshotStore,
		indexer indexing.ProjectIndexer,
		events *eventlog.Writer,
	) error {
		// Profiles own generations, but the daemon owns every profile in one
		// installation. Keep its endpoint, token and socket at installation scope:
		// this is the same directory mcp install, serve and update read.
		options := daemon.Options{
			StateDirectory: stateDirectory(loaded),
			SnapshotStore:  store,
			Registry:       toolMetricsRegistry(events),
			Indexer:        indexer,
			OnSession: func(event string, err error) {
				if err != nil {
					logger.Error("daemon session "+event, "command", "daemon", "error", err)
					return
				}
				logger.Info("daemon session "+event, "command", "daemon")
			},
		}
		httpOptions := daemon.HTTPOptions{
			Address:     flags.Address,
			AllowRemote: flags.AllowRemote,
			OnWarning: func(message string) {
				logger.Warn(message, "command", "daemon")
			},
		}

		// HTTP first, and the order is load-bearing. A unix socket accepts
		// connections the moment it is bound, before anyone calls Accept, so a
		// client that treats a successful dial as readiness would reach a daemon
		// whose endpoint file does not exist yet. Publishing HTTP first makes
		// the socket the later signal, so reaching it implies the endpoint is
		// there. `benchmarks/daemon-cost` found this by failing on it.
		served, err := daemon.ListenHTTP(options, httpOptions)
		if err != nil {
			return err
		}

		listener, err := daemon.Listen(options)
		if err != nil {
			// Withdraw the endpoint: it claims a daemon is answering, and this
			// one is about to not exist.
			_ = served.Close()
			return err
		}
		socket := listener.Addr().String()
		logger.Info("MCP daemon listening",
			"command", "daemon", "socket", socket, "http", served.Addr().String())
		defer logger.Info("MCP daemon stopped listening", "command", "daemon", "socket", socket)

		// Both halves end together: whichever fails first cancels the other, so
		// a daemon never keeps answering on one transport after losing the
		// other and telling nobody.
		group, groupCtx := errgroup.WithContext(ctx)
		group.Go(func() error {
			// Serve owns the listener from here: it closes it when the context
			// is cancelled, and that close unlinks the socket file because this
			// listener is the one that created it.
			return daemon.Serve(groupCtx, listener, options)
		})
		group.Go(func() error { return served.Serve(groupCtx) })
		return group.Wait()
	}
}
