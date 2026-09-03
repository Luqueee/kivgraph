package indexing

import (
	"context"
	"path/filepath"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/freshness"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// ContentFreshness reads the current registry rather than the startup copy.
// Filesystem stability and semantic coverage remain separate observations.
func (service *Service) ContentFreshness(ctx context.Context) freshness.Status {
	if service.snapshotStore == nil {
		return freshness.Status{State: "unverified", Detail: "no snapshot store"}
	}
	snapshot := service.snapshotStore.Load()
	if snapshot == nil {
		return freshness.Status{State: "unverified", Detail: "no published generation"}
	}
	generation := snapshot.Metadata().ID
	repositories, err := config.LoadRepositories(service.loaded.RepositoriesPath)
	if err != nil {
		return freshness.Status{Generation: generation, State: "unavailable", Detail: err.Error()}
	}
	registry, err := workspace.NewRegistry(ctx, repositories)
	if err != nil {
		return freshness.Status{Generation: generation, State: "unavailable", Detail: err.Error()}
	}
	return freshness.Check(ctx, filepath.Dir(service.loaded.Config.Storage.DatabasePath), generation, registry.List())
}
