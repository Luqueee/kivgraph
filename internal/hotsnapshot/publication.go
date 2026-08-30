package hotsnapshot

import (
	"errors"
	"fmt"
	"sort"
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
	generation   atomic.Uint64
	// failure is the last refusal of the deferred load. It is kept rather than
	// retried on every call, because a query that cannot be answered should not
	// re-map a broken file each time it is asked -- and it is cleared by a
	// successful Publish, so a rebuilt generation recovers without a restart.
	failure atomic.Pointer[error]

	// profiles is set only on the installation-level aggregate. Each child
	// retains its own publication synchronization and deferred loader. The map
	// changes only when index_project creates a profile in a running daemon.
	profilesMu      sync.RWMutex
	profiles        map[string]*SnapshotStore
	profileNames    []string
	defaultProfile  string
	maxOpenProfiles int
	profileLRU      []string
	onLoad          func()
}

// ProfileStore is one named graph selected from an installation store.
type ProfileStore struct {
	Name  string
	Store *SnapshotStore
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
	store := &SnapshotStore{deferred: load}
	store.generation.Store(generation)
	return store
}

// NewProfileSnapshotStore groups independently published stores behind one
// default-compatible entry. The input map is copied and canonicalised.
func NewProfileSnapshotStore(defaultProfile string, profiles map[string]*SnapshotStore) (*SnapshotStore, error) {
	if defaultProfile == "" {
		return nil, errors.New("snapshot profiles: default profile is required")
	}
	copyProfiles := make(map[string]*SnapshotStore, len(profiles))
	names := make([]string, 0, len(profiles))
	for name, profile := range profiles {
		if name == "" || profile == nil {
			return nil, fmt.Errorf("snapshot profiles: invalid profile %q", name)
		}
		copyProfiles[name] = profile
		names = append(names, name)
	}
	if _, found := copyProfiles[defaultProfile]; !found {
		return nil, fmt.Errorf("snapshot profiles: default profile %q does not exist", defaultProfile)
	}
	sort.Strings(names)
	store := &SnapshotStore{
		profiles: copyProfiles, profileNames: names, defaultProfile: defaultProfile,
		maxOpenProfiles: len(copyProfiles),
	}
	store.installProfileLoadHooks()
	return store, nil
}

// SetMaxOpenProfiles bounds how many deferred profile snapshots remain
// materialised. Eviction only drops the aggregate's cached pointer; readers
// that already loaded an immutable snapshot keep using it safely.
func (store *SnapshotStore) SetMaxOpenProfiles(limit int) error {
	if store == nil || limit < 1 {
		return errors.New("snapshot profiles: max open profiles must be at least 1")
	}
	store.profilesMu.Lock()
	defer store.profilesMu.Unlock()
	if store.profiles == nil {
		return errors.New("snapshot profiles: store is not an aggregate")
	}
	store.maxOpenProfiles = limit
	store.evictProfilesLocked("")
	return nil
}

func (store *SnapshotStore) installProfileLoadHooks() {
	for name, profile := range store.profiles {
		profileName := name
		profile.onLoad = func() { store.touchProfile(profileName) }
	}
}

func (store *SnapshotStore) touchProfile(name string) {
	store.profilesMu.Lock()
	defer store.profilesMu.Unlock()
	for index, current := range store.profileLRU {
		if current == name {
			store.profileLRU = append(store.profileLRU[:index], store.profileLRU[index+1:]...)
			break
		}
	}
	store.profileLRU = append(store.profileLRU, name)
	store.evictProfilesLocked(name)
}

func (store *SnapshotStore) evictProfilesLocked(current string) {
	for len(store.profileLRU) > store.maxOpenProfiles {
		name := store.profileLRU[0]
		store.profileLRU = store.profileLRU[1:]
		if name == current {
			store.profileLRU = append(store.profileLRU, name)
			continue
		}
		store.profiles[name].unload()
	}
}

func (store *SnapshotStore) unload() {
	if store != nil && store.deferred != nil {
		store.active.Store(nil)
	}
}

// ResolveProfiles selects stores in canonical name order. Omitted selects the
// default; `*` alone selects all profiles.
func (store *SnapshotStore) ResolveProfiles(requested []string) ([]ProfileStore, error) {
	if store == nil {
		return nil, errors.New("snapshot profiles: no store")
	}
	store.profilesMu.RLock()
	defer store.profilesMu.RUnlock()
	if store.profiles == nil {
		if len(requested) == 0 {
			return []ProfileStore{{Store: store}}, nil
		}
		if len(requested) == 1 && (requested[0] == "default" || requested[0] == "*") {
			return []ProfileStore{{Name: "default", Store: store}}, nil
		}
		return nil, fmt.Errorf("snapshot profiles: profile does not exist: %s", requested[0])
	}
	names := append([]string(nil), requested...)
	if len(names) == 0 {
		names = []string{store.defaultProfile}
	} else if len(names) == 1 && names[0] == "*" {
		names = append([]string(nil), store.profileNames...)
	} else {
		for _, name := range names {
			if name == "*" {
				return nil, errors.New("snapshot profiles: * cannot be combined with another profile")
			}
		}
		sort.Strings(names)
	}
	selected := make([]ProfileStore, 0, len(names))
	for index, name := range names {
		if index > 0 && name == names[index-1] {
			return nil, fmt.Errorf("snapshot profiles: duplicate profile %q", name)
		}
		profile, found := store.profiles[name]
		if !found {
			return nil, fmt.Errorf("snapshot profiles: profile does not exist: %s", name)
		}
		selected = append(selected, ProfileStore{Name: name, Store: profile})
	}
	return selected, nil
}

// AddProfile publishes one empty or deferred child into a running aggregate.
// It does not replace an existing child: the caller that created a profile
// must keep indexing the store already visible to readers.
func (store *SnapshotStore) AddProfile(name string, profile *SnapshotStore) error {
	if store == nil || profile == nil || name == "" {
		return errors.New("snapshot profiles: name and store are required")
	}
	store.profilesMu.Lock()
	defer store.profilesMu.Unlock()
	if store.closed.Load() {
		return ErrSnapshotStoreClosed
	}
	if store.profiles == nil {
		return errors.New("snapshot profiles: store is not an aggregate")
	}
	if _, found := store.profiles[name]; found {
		return fmt.Errorf("snapshot profiles: profile already exists: %s", name)
	}
	store.profiles[name] = profile
	profileName := name
	profile.onLoad = func() { store.touchProfile(profileName) }
	store.profileNames = append(store.profileNames, name)
	sort.Strings(store.profileNames)
	return nil
}

// ProfileCount reports the number of independently published stores.
func (store *SnapshotStore) ProfileCount() int {
	if store == nil {
		return 0
	}
	store.profilesMu.RLock()
	defer store.profilesMu.RUnlock()
	if store.profiles == nil {
		return 1
	}
	return len(store.profileNames)
}

// DefaultProfileName returns the ordinary profile used by unscoped calls.
func (store *SnapshotStore) DefaultProfileName() string {
	if store == nil {
		return ""
	}
	store.profilesMu.RLock()
	defer store.profilesMu.RUnlock()
	if store.profiles == nil {
		return ""
	}
	return store.defaultProfile
}

func (store *SnapshotStore) defaultStore() *SnapshotStore {
	if store != nil {
		store.profilesMu.RLock()
		defer store.profilesMu.RUnlock()
		if store.profiles != nil {
			return store.profiles[store.defaultProfile]
		}
	}
	return store
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
	store = store.defaultStore()
	if store == nil {
		return nil
	}
	if store.closed.Load() {
		return nil
	}
	if store.onLoad != nil {
		store.onLoad()
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
	if publishErr := store.publishMaterialised(snapshot); publishErr != nil {
		store.failure.Store(&publishErr)
		return nil
	}
	return snapshot
}

func (store *SnapshotStore) publishMaterialised(snapshot *GraphSnapshot) error {
	if store.closed.Load() {
		return ErrSnapshotStoreClosed
	}
	candidateID := snapshot.Metadata().ID
	if known := store.generation.Load(); known != 0 && candidateID < known {
		return ErrSnapshotGeneration
	}
	if !store.active.CompareAndSwap(nil, snapshot) {
		return nil
	}
	store.generation.Store(candidateID)
	return nil
}

// Available answers whether a query has a graph to read, without materialising
// one. A deferred store that has not been asked anything yet is available: the
// work is pending, not missing.
func (store *SnapshotStore) Available() bool {
	store = store.defaultStore()
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
	store = store.defaultStore()
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
	store = store.defaultStore()
	if store == nil || store.closed.Load() {
		return 0, false
	}
	if snapshot := store.active.Load(); snapshot != nil {
		return snapshot.Metadata().ID, true
	}
	if store.deferred == nil || store.failure.Load() != nil {
		return 0, false
	}
	return store.generation.Load(), true
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
	store = store.defaultStore()
	if store == nil {
		return ErrSnapshotStoreClosed
	}
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
		if current == nil {
			if known := store.generation.Load(); known != 0 && candidateID <= known {
				return ErrSnapshotGeneration
			}
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
			store.generation.Store(candidateID)
			return nil
		}
	}
}

// Close removes the active snapshot and prevents future publications. It is
// idempotent; readers holding an earlier pointer may still finish safely.
func (store *SnapshotStore) Close() {
	if store == nil {
		return
	}
	store.profilesMu.RLock()
	if store.profiles != nil {
		if store.closed.Swap(true) {
			store.profilesMu.RUnlock()
			return
		}
		profiles := make([]*SnapshotStore, 0, len(store.profiles))
		for _, profile := range store.profiles {
			profiles = append(profiles, profile)
		}
		store.profilesMu.RUnlock()
		for _, profile := range profiles {
			profile.Close()
		}
		return
	}
	store.profilesMu.RUnlock()
	if store.closed.Swap(true) {
		return
	}
	store.active.Store(nil)
}
