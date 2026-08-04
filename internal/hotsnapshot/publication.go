package hotsnapshot

import (
	"errors"
	"sync/atomic"
)

var (
	ErrNilSnapshot         = errors.New("cannot publish a nil snapshot")
	ErrSnapshotGeneration  = errors.New("snapshot generation is not newer than active snapshot")
	ErrSnapshotStoreClosed = errors.New("snapshot store is closed")
)

// SnapshotStore publishes complete immutable snapshots through one atomic
// pointer. Loading a snapshot pins that pointer for the duration of a reader's
// operation; later publications do not mutate the loaded snapshot.
type SnapshotStore struct {
	active atomic.Pointer[GraphSnapshot]
	closed atomic.Bool
}

// NewSnapshotStore creates a store with an optional initial snapshot.
func NewSnapshotStore(initial *GraphSnapshot) *SnapshotStore {
	store := &SnapshotStore{}
	if initial != nil {
		store.active.Store(initial)
	}
	return store
}

// Load returns the currently published snapshot, or nil before the first
// successful publication or after Close.
func (store *SnapshotStore) Load() *GraphSnapshot {
	snapshot := store.active.Load()
	if store.closed.Load() {
		return nil
	}
	return snapshot
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
