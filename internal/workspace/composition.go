package workspace

import (
	"context"
	"fmt"
	"strings"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/topology"
)

// NewComposedRegistry registers the repositories selected by one validated
// profile composition. The ordinary repository registry supplies provider
// configuration; the composition supplies the worktree path for each logical
// repository. Repository membership is not dependency evidence.
//
// The configured repository name is the compatibility identity used for the
// logical repository ID. A later configuration migration may add a separate
// display name, but this adapter must not guess between aliases today.
func NewComposedRegistry(ctx context.Context, source config.RepositoriesFile, composition topology.ProfileComposition) (*Registry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return newComposedRegistry(ctx, source, composition, runGit)
}

func newComposedRegistry(
	ctx context.Context,
	source config.RepositoriesFile,
	composition topology.ProfileComposition,
	git gitRunner,
) (*Registry, error) {
	if err := validateProfileComposition(composition); err != nil {
		return nil, err
	}

	configured := make(map[string]config.Repository, len(source.Repositories))
	for index, repository := range source.Repositories {
		name := strings.TrimSpace(repository.Name)
		if name == "" {
			return nil, fmt.Errorf("repositories[%d].name: must not be empty", index)
		}
		if _, exists := configured[name]; exists {
			return nil, fmt.Errorf("repositories[%d].name %q: duplicate provider metadata", index, name)
		}
		configured[name] = repository
	}

	selected := config.RepositoriesFile{
		Version:      source.Version,
		Repositories: make([]config.Repository, 0, len(composition.Repositories)),
	}
	for index, repository := range composition.Repositories {
		logicalID := string(repository.ID)
		entry, exists := configured[logicalID]
		if !exists {
			return nil, fmt.Errorf("profile %q: logical repository %q has no provider metadata", composition.Profile.ID, logicalID)
		}
		entry.Path = composition.Worktrees[index].Path
		selected.Repositories = append(selected.Repositories, entry)
	}

	registry, err := newRegistry(ctx, selected, git)
	if err != nil {
		return nil, fmt.Errorf("register composed profile %q: %w", composition.Profile.ID, err)
	}
	cloned := cloneProfileComposition(composition)
	registry.composition = &cloned
	return registry, nil
}

func validateProfileComposition(composition topology.ProfileComposition) error {
	if _, err := topology.NewProfileID(string(composition.Profile.ID)); err != nil {
		return fmt.Errorf("composition profile: %w", err)
	}
	if len(composition.Repositories) != len(composition.Worktrees) {
		return fmt.Errorf("profile %q: %w: repository/worktree count differs", composition.Profile.ID, topology.ErrInvalidTopology)
	}
	if len(composition.Profile.Worktrees) != len(composition.Worktrees) {
		return fmt.Errorf("profile %q: %w: selection/worktree count differs", composition.Profile.ID, topology.ErrInvalidTopology)
	}
	owned := make(map[topology.LogicalRepositoryID]int, len(composition.Repositories))
	for index, repository := range composition.Repositories {
		if _, err := topology.NewLogicalRepositoryID(string(repository.ID)); err != nil {
			return fmt.Errorf("composition repositories[%d].id: %w", index, err)
		}
		if previous, exists := owned[repository.ID]; exists {
			return fmt.Errorf("composition repositories[%d].id: %w: duplicate of repositories[%d]", index, topology.ErrInvalidTopology, previous)
		}
		owned[repository.ID] = index

		worktree := composition.Worktrees[index]
		if _, err := topology.NewWorktreeID(string(worktree.ID)); err != nil {
			return fmt.Errorf("composition worktrees[%d].id: %w", index, err)
		}
		if worktree.Repository != repository.ID {
			return fmt.Errorf("composition worktrees[%d].repository: %w: belongs to %q, want %q", index, topology.ErrInvalidTopology, worktree.Repository, repository.ID)
		}
		selection := composition.Profile.Worktrees[index]
		if selection.Repository != repository.ID || selection.Worktree != worktree.ID {
			return fmt.Errorf("composition profile.worktrees[%d]: %w: does not match repository %q and worktree %q", index, topology.ErrInvalidTopology, repository.ID, worktree.ID)
		}
		if strings.TrimSpace(worktree.Path) == "" {
			return fmt.Errorf("composition worktrees[%d].path: %w: must not be empty", index, topology.ErrInvalidTopology)
		}
	}
	return nil
}

func cloneProfileComposition(composition topology.ProfileComposition) topology.ProfileComposition {
	if composition.Profile.Worktrees != nil {
		composition.Profile.Worktrees = append([]topology.WorktreeSelection{}, composition.Profile.Worktrees...)
	}
	if composition.Repositories != nil {
		composition.Repositories = append([]topology.LogicalRepository{}, composition.Repositories...)
	}
	if composition.Worktrees != nil {
		composition.Worktrees = append([]topology.Worktree{}, composition.Worktrees...)
	}
	return composition
}
