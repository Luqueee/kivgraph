package indexing

import (
	"context"

	"github.com/Luqueee/kivgraph/internal/freshness"
)

// ContentFreshness reads the cached observation for the currently published
// generation. It deliberately performs no registry read, directory walk or
// content hash: graph_status is a fast status query, and a monitor or a full
// rebuild updates this cache outside the tool call.
func (service *Service) ContentFreshness(_ context.Context) freshness.Status {
	if service == nil {
		return freshness.Status{State: "unverified", Detail: "content freshness is not cached"}
	}
	if service.snapshotStore == nil {
		return freshness.Status{State: "unverified", Detail: "no snapshot store"}
	}
	generation, known := service.snapshotStore.ActiveID()
	if !known {
		return freshness.Status{State: "unverified", Detail: "no published generation"}
	}
	if service.freshnessCache == nil {
		return freshness.Status{
			Generation: generation,
			State:      "unverified",
			Detail:     "content freshness is not cached",
		}
	}
	status := service.freshnessCache.Load()
	if status.Generation != generation {
		return freshness.Status{
			Generation: generation,
			State:      "unverified",
			Detail:     "cached content freshness belongs to another generation",
		}
	}
	return status
}

func (service *Service) markFreshGeneration(generation uint64) {
	if service == nil || service.freshnessCache == nil || service.snapshotStore == nil {
		return
	}
	if served, known := service.snapshotStore.ActiveID(); !known || served != generation {
		return
	}
	service.freshnessCache.Store(freshness.Status{Generation: generation, State: "fresh"})
}
