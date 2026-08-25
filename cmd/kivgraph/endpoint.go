package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/integrations"
)

// stateDirectory is where a configuration keeps its generation, and therefore
// which daemon it belongs to.
//
// It lives here rather than being derived twice because the daemon writes its
// endpoint into this directory and `mcp install --daemon` reads it back: two
// derivations that drifted apart would have the command configure clients
// against a daemon that is not the one this configuration starts.
func stateDirectory(loaded config.Loaded) string {
	return filepath.Dir(loaded.Config.Storage.DatabasePath)
}

// integrationManagerOptions builds the manager for an integration command,
// resolving a running daemon's endpoint when the caller asked for one.
//
// Asking is deliberate. Detecting a daemon and silently writing a url would make
// the installed entry depend on whether a daemon happened to be running at that
// moment, and the same command would then write two different files on two days.
func integrationManagerOptions(useDaemon bool) (integrations.Options, error) {
	if !useDaemon {
		return integrations.Options{}, nil
	}
	loaded, err := config.Load("")
	if err != nil {
		return integrations.Options{}, fmt.Errorf("read the configuration: %w", err)
	}
	directory := stateDirectory(loaded)
	endpoint, err := daemon.ReadEndpoint(directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return integrations.Options{}, fmt.Errorf(
				"no daemon has published an endpoint in %s: start one with `kivgraph daemon`", directory)
		}
		return integrations.Options{}, fmt.Errorf("read the daemon endpoint: %w", err)
	}
	return integrations.Options{
		Endpoint: integrations.Endpoint{URL: endpoint.URL, Token: endpoint.Token},
	}, nil
}
