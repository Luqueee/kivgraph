package main

import (
	"context"
	"fmt"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/topology"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func registryForProfile(ctx context.Context, loaded config.Loaded) (*workspace.Registry, error) {
	value, present, err := config.LoadProfileTopology(loaded.ConfigPath, loaded.Profile)
	if err != nil {
		return nil, err
	}
	if !present {
		return workspace.NewRegistry(ctx, loaded.Repositories)
	}
	composition, err := value.Compose(topology.ProfileID(loaded.Profile))
	if err != nil {
		return nil, fmt.Errorf("compose profile %q: %w", loaded.Profile, err)
	}
	return workspace.NewComposedRegistry(ctx, loaded.Repositories, composition)
}
