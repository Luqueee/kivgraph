package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/topology"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func registryForProfile(ctx context.Context, loaded config.Loaded) (*workspace.Registry, error) {
	if strings.TrimSpace(loaded.ConfigPath) == "" {
		return workspace.NewRegistry(ctx, loaded.Repositories)
	}
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

// repositoriesThatRegister splits a registry into the entries that can be
// registered on their own and a line for each one that cannot.
//
// A directory that was deleted, one that is not a Git checkout or a path with
// a symlink in it failed the registry, and with it the pass: every other
// repository lost its graph to an entry that had nothing to do with it. Such
// an entry is left out and named. What involves two entries -- a duplicate
// name or path -- is not decided here and still fails.
func repositoriesThatRegister(ctx context.Context, source config.RepositoriesFile) (kept []config.Repository, unregistered []string) {
	for _, repository := range source.Repositories {
		alone := config.RepositoriesFile{Version: source.Version, Repositories: []config.Repository{repository}}
		if _, err := workspace.NewRegistry(ctx, alone); err != nil {
			unregistered = append(unregistered, fmt.Sprintf("%s: %v", repository.Name, err))
			continue
		}
		kept = append(kept, repository)
	}
	return kept, unregistered
}

// registryOfWhatRegisters is registryForProfile for a caller that goes on
// without the entries that cannot be registered: a pass, and the watcher that
// schedules one. It answers a line for each entry left out. A registry where
// nothing registers, or that fails for a reason no single entry explains, is
// the error it always was.
func registryOfWhatRegisters(ctx context.Context, loaded config.Loaded) (*workspace.Registry, []string, error) {
	registry, err := registryForProfile(ctx, loaded)
	if err == nil {
		return registry, nil, nil
	}
	kept, unregistered := repositoriesThatRegister(ctx, loaded.Repositories)
	if len(unregistered) == 0 || len(kept) == 0 {
		return nil, nil, err
	}
	loaded.Repositories.Repositories = kept
	registry, err = registryForProfile(ctx, loaded)
	if err != nil {
		return nil, nil, err
	}
	return registry, unregistered, nil
}
