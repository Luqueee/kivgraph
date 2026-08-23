package main

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/eventlog"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/indexing"
)

// runDaemon serves MCP over a socket in the state directory.
//
// What it changes against `serve` is only who owns the snapshot. A client that
// spawns `serve` gets a process of its own, and eight clients get eight copies
// of the same graph in private pages -- `566 MB` on `kena`, measured in
// `benchmarks/load-cost-resident` and flat in the number of clients, because
// each process decodes the same tables for itself. One daemon decodes them once.
func runDaemon(logger *slog.Logger) configuredMCPRunner {
	return func(
		ctx context.Context,
		loaded config.Loaded,
		store *hotsnapshot.SnapshotStore,
		indexer indexing.ProjectIndexer,
		events *eventlog.Writer,
	) error {
		// The state directory is where the generation lives, so it is the
		// identity a daemon belongs to: two configurations pointing elsewhere
		// must never answer each other's clients.
		options := daemon.Options{
			StateDirectory: filepath.Dir(loaded.Config.Storage.DatabasePath),
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
		listener, err := daemon.Listen(options)
		if err != nil {
			return err
		}
		socket := listener.Addr().String()
		logger.Info("MCP daemon listening", "command", "daemon", "socket", socket)
		defer logger.Info("MCP daemon stopped listening", "command", "daemon", "socket", socket)
		// Serve owns the listener from here: it closes it when the context is
		// cancelled, and that close unlinks the socket file because this
		// listener is the one that created it.
		return daemon.Serve(ctx, listener, options)
	}
}
