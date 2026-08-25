package hotsnapshot

import (
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
)

var (
	ErrNilSnapshot         = errors.New("cannot publish a nil snapshot")
	ErrSnapshotGeneration  = errors.New("snapshot generation is not newer than active snapshot")
	ErrSnapshotStoreClosed = errors.New("snapshot store is closed")
)

// SnapshotLoader materialises the published snapshot of one generation.
//
// It exists because mapping a snapshot is not free: the file's pages are shared
// and clean, but the lookup indexes derived from it are private, and on a real
// corpus they are some thirty megabytes per process. A server that is started
// and never asked anything pays all of it -- which is what most of them do, so a
// store can be handed the work instead of the result. See ADR 0067.
type SnapshotLoader func() (*GraphSnapshot, error)

// SnapshotStore publishes complete immutable snapshots through one atomic
// pointer. Loading a snapshot pins that pointer for the duration of a reader's
// operation; later publications do not mutate the loaded snapshot.
//
// A store may be given a loader instead of a snapshot. Then the first reader
// that needs the graph materialises it, once, and every reader after that finds
// it published. Nothing else about the store changes: what a reader gets from
// Load is the same immutable snapshot either way.
type SnapshotStore struct {
	active atomic.Pointer[GraphSnapshot]
	closed atomic.Bool

	// deferred is the work not yet done. It is read-only after construction;
	// the mutex serialises the one reader that runs it against the others,
	// which would otherwise each build a private copy of the same indexes.
	deferred     SnapshotLoader
	deferredOnce sync.Mutex
	generation   uint64
	// failure is the last refusal of the deferred load. It is kept rather than
	// retried on every call, because a query that cannot be answered should not
	// re-map a broken file each time it is asked -- and it is cleared by a
	// successful Publish, so a rebuilt generation recovers without a restart.
	failure atomic.Pointer[error]
}

// NewSnapshotStore creates a store with an optional initial snapshot.
func NewSnapshotStore(initial *GraphSnapshot) *SnapshotStore {
	store := &SnapshotStore{}
	if initial != nil {
		store.active.Store(initial)
	}
	return store
}

// NewDeferredSnapshotStore creates a store that holds the work rather than the
// graph. The generation is the one the loader will materialise, and it is known
// without doing any of that work: a caller that has to log or report which
// generation a server answers from must not force the load to find out.
func NewDeferredSnapshotStore(generation uint64, load SnapshotLoader) *SnapshotStore {
	return &SnapshotStore{deferred: load, generation: generation}
}

// Load returns the published snapshot, materialising a deferred one on the way.
// It answers nil before the first successful publication, after Close, and when
// a deferred load has been refused.
//
// The load happens here rather than at startup because this is the first moment
// anything needs the graph. A caller that only wants to know whether a query
// could be answered must ask Available instead: asking Load would do the very
// work being deferred.
func (store *SnapshotStore) Load() *GraphSnapshot {
	if store.closed.Load() {
		return nil
	}
	if snapshot := store.active.Load(); snapshot != nil {
		return snapshot
	}
	return store.materialise()
}

// materialise runs the deferred load under the mutex, so N readers arriving at
// once build one set of indexes rather than N.
func (store *SnapshotStore) materialise() *GraphSnapshot {
	if store.deferred == nil {
		return nil
	}
	store.deferredOnce.Lock()
	defer store.deferredOnce.Unlock()
	if snapshot := store.active.Load(); snapshot != nil {
		return snapshot
	}
	if store.failure.Load() != nil {
		return nil
	}
	snapshot, err := store.deferred()
	if err == nil && snapshot == nil {
		err = ErrNilSnapshot
	}
	if err != nil {
		store.failure.Store(&err)
		return nil
	}
	if publishErr := store.Publish(snapshot); publishErr != nil {
		store.failure.Store(&publishErr)
		return nil
	}
	return snapshot
}

// Available answers whether a query has a graph to read, without materialising
// one. A deferred store that has not been asked anything yet is available: the
// work is pending, not missing.
func (store *SnapshotStore) Available() bool {
	if store == nil || store.closed.Load() {
		return false
	}
	if store.active.Load() != nil {
		return true
	}
	return store.deferred != nil && store.failure.Load() == nil
}

// LoadFailure is why the deferred load was refused, or nil. It is what keeps a
// refusal nameable: a reader that only saw Load answer nil could not tell a
// server with no generation from one whose generation it could not map.
func (store *SnapshotStore) LoadFailure() error {
	if store == nil {
		return nil
	}
	if failure := store.failure.Load(); failure != nil {
		return *failure
	}
	return nil
}

// ActiveID names the generation this store answers from, without materialising
// it. It is what a caller that only compares generations must use: a reconciler
// polling for a newer one would otherwise map the graph on its first tick, and
// every deferred server would load itself after one interval of doing nothing.
func (store *SnapshotStore) ActiveID() (uint64, bool) {
	if store == nil || store.closed.Load() {
		return 0, false
	}
	if snapshot := store.active.Load(); snapshot != nil {
		return snapshot.Metadata().ID, true
	}
	if store.deferred == nil || store.failure.Load() != nil {
		return 0, false
	}
	return store.generation, true
}

// GenerationID is ActiveID rendered for a log line or a report.
func (store *SnapshotStore) GenerationID() string {
	id, known := store.ActiveID()
	if !known {
		return ""
	}
	return strconv.FormatUint(id, 10)
}

// Publish atomically replaces the active snapshot. A candidate must be
// non-nil and have a strictly newer snapshot ID than the active generation.
// Compare-and-swap prevents a stale concurrent publisher from overwriting a
// generation that won the race.
func (store *SnapshotStore) Publish(candidate *GraphSnapshot) error {
	if candidate == nil {
		return ErrNilSnapshot
	}
	candidateID := candidate.Metadata().ID
	for {
		if store.closed.Load() {
			return ErrSnapshotStoreClosed
		}
		current := store.active.Load()
		if current != nil && candidateID <= current.Metadata().ID {
			return ErrSnapshotGeneration
		}
		if store.active.CompareAndSwap(current, candidate) {
			if store.closed.Load() {
				store.active.CompareAndSwap(candidate, nil)
				return ErrSnapshotStoreClosed
			}
			// A published generation retires the refusal of the previous one:
			// whatever could not be mapped is no longer what a reader would be
			// asking for, so a rebuild recovers a server without a restart.
			store.failure.Store(nil)
			return nil
		}
	}
}

// Close removes the active snapshot and prevents future publications. It is
// idempotent; readers holding an earlier pointer may still finish safely.
func (store *SnapshotStore) Close() {
	if store.closed.Swap(true) {
		return
	}
	store.active.Store(nil)
}
