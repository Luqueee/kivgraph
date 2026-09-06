package freshness

import "sync"

// Cache holds the latest content-freshness observation for a published
// generation. It deliberately contains no filesystem-derived mutable state:
// graph_status can read it without walking or hashing a repository.
type Cache struct {
	mutex  sync.RWMutex
	status Status
}

// NewCache creates a cache with an initial, already classified observation.
func NewCache(initial Status) *Cache {
	return &Cache{status: initial}
}

// Load returns the last observation. A nil cache is treated as an unverified
// observation so an optional fast-path integration fails closed.
func (cache *Cache) Load() Status {
	if cache == nil {
		return Status{State: "unverified", Detail: "content freshness is not cached"}
	}
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	return cache.status
}

// Store replaces the observation after a complete inventory check or a
// successful publication.
func (cache *Cache) Store(status Status) {
	if cache == nil {
		return
	}
	cache.mutex.Lock()
	cache.status = status
	cache.mutex.Unlock()
}

// StoreIfUnverified publishes a background verification result only while no
// watcher event or successful rebuild has superseded it. This prevents a scan
// started at server setup from turning an already observed change back into
// fresh.
func (cache *Cache) StoreIfUnverified(status Status) bool {
	if cache == nil {
		return false
	}
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if cache.status.State != "unverified" || cache.status.Generation != status.Generation {
		return false
	}
	cache.status = status
	return true
}

// MarkStale invalidates the cached result after a watched registered input
// changes. The generation remains attached so callers can distinguish a stale
// graph from an observation for a different generation.
func (cache *Cache) MarkStale(detail string) {
	if cache == nil {
		return
	}
	cache.mutex.Lock()
	if cache.status.State == "stale" {
		cache.mutex.Unlock()
		return
	}
	cache.status.State = "stale"
	cache.status.Detail = detail
	cache.mutex.Unlock()
}

// MarkUnavailable fails closed when the watcher can no longer observe the
// registered trees. It is not equivalent to fresh content.
func (cache *Cache) MarkUnavailable(detail string) {
	if cache == nil {
		return
	}
	cache.mutex.Lock()
	cache.status.State = "unavailable"
	cache.status.Detail = detail
	cache.mutex.Unlock()
}
