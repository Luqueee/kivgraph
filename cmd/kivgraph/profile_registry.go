package main

import (
	"context"
	"fmt"
	"io"

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

// writeProfileDiagnostics reports the effective repository universe selected
// for a human-readable full index. Membership and worktree paths explain what
// the pass read; they do not claim that any dependency edge exists.
func writeProfileDiagnostics(stdout io.Writer, profile string, registry *workspace.Registry) {
	if registry == nil {
		return
	}
	composition, present := registry.Composition()
	if !present {
		writeInfo(stdout, "index.profile: name=%s composition=legacy repositories=%d",
			profile, len(registry.List()))
		return
	}
	writeInfo(stdout, "index.profile: name=%s composition=topology repositories=%d",
		composition.Profile.ID, len(composition.Repositories))
	for index, repository := range composition.Repositories {
		worktree := composition.Worktrees[index]
		writeInfo(stdout, "index.profile.worktree: repository=%s worktree=%s path=%s",
			repository.ID, worktree.ID, worktree.Path)
	}
}
