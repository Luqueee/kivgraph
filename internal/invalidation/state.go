// Package invalidation tracks which published profiles depend on mutable
// source worktrees and records why a profile no longer describes them.
//
// The state is derived metadata. It never changes CURRENT or a published
// generation; a successful full rebuild replaces the profile record and clears
// its stale marker.
package invalidation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Luqueee/kivgraph/internal/durable"
	"github.com/Luqueee/kivgraph/internal/filelock"
	"github.com/Luqueee/kivgraph/internal/sourceobservation"
	"github.com/Luqueee/kivgraph/internal/topology"
)

const (
	// CurrentVersion is the on-disk schema version of the invalidation state.
	CurrentVersion = 1
	// StateFileName is the installation-level file containing the reverse index
	// and the stale diagnostics for every profile.
	StateFileName = "source-invalidation.json"

	maxDetailSegments = 8
	maxDetailChars    = 512
)

var (
	// ErrStateBusy means another process is updating the installation state.
	// Callers can retry; no state was changed by the refused operation.
	ErrStateBusy = errors.New("source invalidation state is busy")
	// ErrProfileNotTracked means no published source manifest has been recorded
	// for the requested profile yet.
	ErrProfileNotTracked = errors.New("profile has no tracked source generation")
)

// Reason identifies the first actionable cause of a source change.
type Reason string

const (
	ReasonContentChanged             Reason = "content_changed"
	ReasonCommitChanged              Reason = "commit_changed"
	ReasonDirtyChanged               Reason = "dirty_changed"
	ReasonBranchChanged              Reason = "branch_changed"
	ReasonPolicyChanged              Reason = "policy_changed"
	ReasonSourceAdded                Reason = "source_added"
	ReasonSourceRemoved              Reason = "source_removed"
	ReasonSourceUnavailable          Reason = "source_unavailable"
	ReasonProviderChanged            Reason = "provider_changed"
	ReasonObservationChanged         Reason = "observation_changed"
	ReasonProfileConfigurationChange Reason = "profile_configuration_changed"
	ReasonResolverChanged            Reason = "resolver_changed"
	ReasonAnalyzerChanged            Reason = "analyzer_changed"
)

// SourceChange is one observed reason a profile's published inputs differ.
// Before and After are absent for profile-wide configuration changes and After
// is absent when a source became unavailable.
type SourceChange struct {
	Worktree   topology.WorktreeID         `json:"worktree,omitempty"`
	Repository string                      `json:"repository,omitempty"`
	Reason     Reason                      `json:"reason"`
	Detail     string                      `json:"detail,omitempty"`
	Before     *topology.SourceObservation `json:"before,omitempty"`
	After      *topology.SourceObservation `json:"after,omitempty"`
}

// ProfileState is the tracked source record for one profile. Manifest is kept
// so a daemon can rebuild the reverse index and explain a stale state after a
// restart without opening the graph database.
type ProfileState struct {
	Profile    string                     `json:"profile"`
	Generation string                     `json:"generation"`
	Manifest   sourceobservation.Manifest `json:"manifest"`
	Stale      bool                       `json:"stale,omitempty"`
	Reason     string                     `json:"reason,omitempty"`
	Changes    []SourceChange             `json:"changes,omitempty"`
}

// SourceDependents is the explicit reverse relationship from one mutable
// worktree to every profile whose effective source set contains it.
type SourceDependents struct {
	Worktree topology.WorktreeID `json:"worktree"`
	Profiles []string            `json:"profiles"`
}

// State is the persisted invalidation index. Sources is redundant with the
// manifests on purpose: it makes the reverse relationship inspectable without
// decoding every profile record, while validation prevents the two views from
// diverging.
type State struct {
	Version  int                `json:"version"`
	Profiles []ProfileState     `json:"profiles"`
	Sources  []SourceDependents `json:"sources"`
}

// ProfileRecord is the publication input used to update one profile's state.
type ProfileRecord struct {
	Profile    string
	Generation string
	Manifest   sourceobservation.Manifest
}

// Manager owns one installation-level invalidation file. Its in-process mutex
// protects the cached state; the sidecar lock makes read-modify-write safe
// when separate profile indexers update the same installation concurrently.
type Manager struct {
	root  string
	path  string
	mu    sync.Mutex
	state State
}

// Open loads the installation invalidation state. A missing file means no
// published profile has been registered yet. The root itself is not created
// until the first mutating operation.
func Open(root string) (*Manager, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("source invalidation root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve source invalidation root: %w", err)
	}
	if info, statErr := os.Stat(absolute); statErr == nil && !info.IsDir() {
		return nil, fmt.Errorf("source invalidation root %q is not a directory", absolute)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect source invalidation root: %w", statErr)
	}
	state, err := read(filepath.Join(absolute, StateFileName))
	if err != nil {
		return nil, err
	}
	return &Manager{root: absolute, path: filepath.Join(absolute, StateFileName), state: state}, nil
}

// Snapshot returns a detached copy of the last state read or written by the
// manager. Call Refresh when another process may have published a newer file.
func (manager *Manager) Snapshot() State {
	if manager == nil {
		return State{}
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return cloneState(manager.state)
}

// Refresh reloads the atomically replaced state file. A missing file resets
// the manager to an empty state.
func (manager *Manager) Refresh(ctx context.Context) error {
	if manager == nil {
		return errors.New("source invalidation manager is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	state, err := read(manager.path)
	if err != nil {
		return err
	}
	manager.state = state
	return nil
}

// RecordPublished records a successful generation and clears stale state for
// that profile. It does not clear stale markers belonging to other profiles
// that depend on the same worktree.
func (manager *Manager) RecordPublished(ctx context.Context, record ProfileRecord) error {
	if manager == nil {
		return errors.New("source invalidation manager is nil")
	}
	profile, err := validProfileRecord(record)
	if err != nil {
		return err
	}
	return manager.mutate(ctx, func(state *State) error {
		for index := range state.Profiles {
			if state.Profiles[index].Profile == profile.Profile {
				state.Profiles[index] = profile
				return nil
			}
		}
		state.Profiles = append(state.Profiles, profile)
		return nil
	})
}

// ProfilesForSource returns the profiles that use worktree in canonical name
// order. It reads the explicit reverse index, not repository names or paths.
func (manager *Manager) ProfilesForSource(worktree topology.WorktreeID) []string {
	if manager == nil {
		return nil
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for _, source := range manager.state.Sources {
		if source.Worktree != worktree {
			continue
		}
		return append([]string(nil), source.Profiles...)
	}
	return nil
}

// CompareManifests explains every source difference between a published
// manifest and a newly observed one. The result is sorted by WorktreeID and is
// suitable for both state persistence and diagnostics.
func CompareManifests(expected, actual sourceobservation.Manifest) ([]SourceChange, error) {
	if err := expected.Validate(); err != nil {
		return nil, fmt.Errorf("validate expected source manifest: %w", err)
	}
	if err := actual.Validate(); err != nil {
		return nil, fmt.Errorf("validate actual source manifest: %w", err)
	}
	if expected.Profile != actual.Profile {
		return nil, fmt.Errorf("source manifest profile changed from %q to %q", expected.Profile, actual.Profile)
	}
	changes := make([]SourceChange, 0)
	if expected.ResolverVersion != actual.ResolverVersion {
		changes = append(changes, SourceChange{
			Reason: ReasonResolverChanged,
			Detail: fmt.Sprintf("resolver changed from %q to %q", expected.ResolverVersion, actual.ResolverVersion),
		})
	}
	if expected.AnalyzerFingerprint != actual.AnalyzerFingerprint {
		changes = append(changes, SourceChange{
			Reason: ReasonAnalyzerChanged,
			Detail: "analyzer fingerprint changed",
		})
	}

	before, err := sourceMap(expected)
	if err != nil {
		return nil, err
	}
	after, err := sourceMap(actual)
	if err != nil {
		return nil, err
	}
	worktrees := make([]string, 0, len(before)+len(after))
	seen := make(map[string]struct{}, len(before)+len(after))
	for worktree := range before {
		key := string(worktree)
		seen[key] = struct{}{}
		worktrees = append(worktrees, key)
	}
	for worktree := range after {
		key := string(worktree)
		if _, exists := seen[key]; !exists {
			worktrees = append(worktrees, key)
		}
	}
	sort.Strings(worktrees)
	for _, key := range worktrees {
		worktree := topology.WorktreeID(key)
		oldSource, wasPresent := before[worktree]
		newSource, isPresent := after[worktree]
		switch {
		case !wasPresent:
			changes = append(changes, SourceChange{
				Worktree: worktree, Repository: newSource.Repository,
				Reason: ReasonSourceAdded, After: observationCopy(&newSource.Observation),
				Detail: "source was added to the effective provider set",
			})
		case !isPresent:
			changes = append(changes, SourceChange{
				Worktree: worktree, Repository: oldSource.Repository,
				Reason: ReasonSourceRemoved, Before: observationCopy(&oldSource.Observation),
				Detail: "source was removed from the effective provider set",
			})
		default:
			if change, changed := compareSource(oldSource, newSource); changed {
				changes = append(changes, change)
			}
		}
	}
	return changes, nil
}

// Invalidate compares a profile's newly observed manifest and marks every
// profile that depends on each changed worktree. A profile-wide configuration
// change only affects the profile that supplied it.
func (manager *Manager) Invalidate(ctx context.Context, profile string, actual sourceobservation.Manifest) error {
	if manager == nil {
		return errors.New("source invalidation manager is nil")
	}
	profile = strings.TrimSpace(profile)
	if _, err := topology.NewProfileID(profile); err != nil {
		return fmt.Errorf("invalidation profile: %w", err)
	}
	return manager.mutate(ctx, func(state *State) error {
		tracked, found := findProfile(state, profile)
		if !found {
			return fmt.Errorf("profile %q: %w", profile, ErrProfileNotTracked)
		}
		changes, err := CompareManifests(tracked.Manifest, actual)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			return nil
		}
		markStale(state, profile, changes)
		return nil
	})
}

// MarkStale marks all profiles depending on the supplied worktree. It is the
// path for a watcher that knows a source became unavailable before it can make
// a complete manifest. The detail must preserve the observed failure reason.
func (manager *Manager) MarkStale(ctx context.Context, worktree topology.WorktreeID, repository string, reason Reason, detail string) error {
	if manager == nil {
		return errors.New("source invalidation manager is nil")
	}
	if _, err := topology.NewWorktreeID(string(worktree)); err != nil {
		return fmt.Errorf("invalidation worktree: %w", err)
	}
	if reason == "" {
		return errors.New("invalidation reason is required")
	}
	if reason == ReasonSourceAdded || reason == ReasonSourceRemoved {
		return errors.New("source membership changes require a profile manifest")
	}
	if strings.TrimSpace(detail) == "" {
		return errors.New("invalidation detail is required")
	}
	return manager.mutate(ctx, func(state *State) error {
		change := SourceChange{
			Worktree: worktree, Repository: strings.TrimSpace(repository),
			Reason: reason, Detail: strings.TrimSpace(detail),
		}
		markStale(state, "", []SourceChange{change})
		return nil
	})
}

func (manager *Manager) mutate(ctx context.Context, mutate func(*State) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	lock, acquired, err := filelock.Acquire(manager.path + ".lock")
	if err != nil {
		return fmt.Errorf("lock source invalidation state: %w", err)
	}
	if !acquired {
		return ErrStateBusy
	}
	defer func() { _ = lock.Release() }()

	state, err := read(manager.path)
	if err != nil {
		return err
	}
	before := cloneState(state)
	if err := mutate(&state); err != nil {
		return err
	}
	if equalJSON(before, state) {
		manager.state = state
		return nil
	}
	if err := rebuildSources(&state); err != nil {
		return err
	}
	if err := write(manager.root, manager.path, state); err != nil {
		return err
	}
	manager.state = state
	return nil
}

func markStale(state *State, requestedProfile string, changes []SourceChange) {
	affected := make(map[string]struct{})
	if requestedProfile != "" {
		affected[requestedProfile] = struct{}{}
	}
	for _, change := range changes {
		if change.Worktree == "" {
			continue
		}
		if change.Reason == ReasonSourceAdded || change.Reason == ReasonSourceRemoved {
			continue
		}
		for _, source := range state.Sources {
			if source.Worktree != change.Worktree {
				continue
			}
			for _, profile := range source.Profiles {
				affected[profile] = struct{}{}
			}
		}
	}
	profiles := make([]string, 0, len(affected))
	for profile := range affected {
		profiles = append(profiles, profile)
	}
	sort.Strings(profiles)
	for _, profile := range profiles {
		tracked, found := findProfile(state, profile)
		if !found {
			continue
		}
		tracked.Stale = true
		for _, change := range changes {
			profileChange := change
			if (profileChange.Reason == ReasonSourceAdded || profileChange.Reason == ReasonSourceRemoved) &&
				profile != requestedProfile {
				continue
			}
			if profileChange.Worktree == "" {
				if profile != requestedProfile {
					continue
				}
			} else {
				matched := false
				for _, source := range tracked.Manifest.Sources {
					if source.Observation.Worktree == profileChange.Worktree {
						matched = true
						profileChange.Repository = source.Repository
						profileChange.Before = observationCopy(&source.Observation)
						break
					}
				}
				if !matched && profile != requestedProfile {
					continue
				}
			}
			mergeChange(&tracked.Changes, profileChange)
		}
		tracked.Reason = staleReason(tracked.Changes)
	}
}

func staleReason(changes []SourceChange) string {
	if len(changes) == 0 {
		return ""
	}
	change := changes[0]
	for _, candidate := range changes[1:] {
		if reasonPriority(candidate.Reason) > reasonPriority(change.Reason) {
			change = candidate
		}
	}
	if change.Repository == "" {
		return string(change.Reason)
	}
	return fmt.Sprintf("%s: %s", change.Reason, change.Repository)
}

func mergeChange(changes *[]SourceChange, incoming SourceChange) {
	incoming.Detail = boundDetail(incoming.Detail)
	for index := range *changes {
		current := &(*changes)[index]
		if current.Worktree != incoming.Worktree || current.Repository != incoming.Repository ||
			(incoming.Worktree == "" && current.Reason != incoming.Reason) {
			continue
		}
		if current.Before == nil {
			current.Before = observationCopy(incoming.Before)
		}
		if incoming.After != nil {
			current.After = observationCopy(incoming.After)
		}
		if reasonPriority(incoming.Reason) > reasonPriority(current.Reason) {
			current.Reason = incoming.Reason
		}
		if current.Reason == ReasonSourceUnavailable {
			current.After = nil
		}
		current.Detail = joinDetail(current.Detail, incoming.Detail)
		return
	}
	*changes = append(*changes, cloneChange(incoming))
	sort.SliceStable(*changes, func(left, right int) bool {
		if (*changes)[left].Worktree != (*changes)[right].Worktree {
			return (*changes)[left].Worktree < (*changes)[right].Worktree
		}
		if (*changes)[left].Repository != (*changes)[right].Repository {
			return (*changes)[left].Repository < (*changes)[right].Repository
		}
		return (*changes)[left].Reason < (*changes)[right].Reason
	})
}

func joinDetail(first, second string) string {
	first, second = strings.TrimSpace(first), strings.TrimSpace(second)
	if first == "" {
		return boundDetail(second)
	}
	if second == "" || first == second || strings.Contains(first, second) {
		return boundDetail(first)
	}

	segments := detailSegments(first)
	for _, candidate := range detailSegments(second) {
		if len(segments) >= maxDetailSegments || detailContained(segments, candidate) {
			continue
		}
		joined := append(append([]string(nil), segments...), candidate)
		if len([]rune(strings.Join(joined, "; "))) > maxDetailChars {
			break
		}
		segments = joined
	}
	return boundDetail(strings.Join(segments, "; "))
}

func detailSegments(detail string) []string {
	parts := strings.Split(strings.TrimSpace(detail), "; ")
	segments := make([]string, 0, min(len(parts), maxDetailSegments))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		segments = append(segments, part)
		if len(segments) == maxDetailSegments {
			break
		}
	}
	return segments
}

func detailContained(segments []string, candidate string) bool {
	for _, segment := range segments {
		if segment == candidate || strings.Contains(segment, candidate) {
			return true
		}
	}
	return false
}

func boundDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	runes := []rune(detail)
	if len(runes) <= maxDetailChars {
		return detail
	}
	return string(runes[:maxDetailChars-1]) + "…"
}

func reasonPriority(reason Reason) int {
	switch reason {
	case ReasonSourceUnavailable:
		return 100
	case ReasonContentChanged:
		return 90
	case ReasonCommitChanged:
		return 80
	case ReasonDirtyChanged:
		return 70
	case ReasonSourceRemoved, ReasonSourceAdded:
		return 60
	case ReasonPolicyChanged, ReasonProviderChanged, ReasonObservationChanged:
		return 50
	case ReasonBranchChanged:
		return 40
	default:
		return 10
	}
}

func compareSource(before, after sourceobservation.Source) (SourceChange, bool) {
	details := make([]string, 0, 5)
	if before.Repository != after.Repository {
		details = append(details, "provider identity changed")
	}
	if before.Derived != after.Derived {
		details = append(details, "derived-provider status changed")
	}
	if before.Observation.Commit != after.Observation.Commit {
		details = append(details, "commit changed")
	}
	if before.Observation.Dirty != after.Observation.Dirty {
		details = append(details, "dirty state changed")
	}
	if before.Observation.ContentDigest != after.Observation.ContentDigest {
		details = append(details, "content digest changed")
	}
	if before.Observation.Branch != after.Observation.Branch {
		details = append(details, "branch changed")
	}
	if !equalJSON(before.Policy, after.Policy) {
		details = append(details, "provider policy changed")
	}
	if len(details) == 0 && before.Observation.ID == after.Observation.ID {
		return SourceChange{}, false
	}
	reason := ReasonBranchChanged
	switch {
	case before.Observation.ContentDigest != after.Observation.ContentDigest:
		reason = ReasonContentChanged
	case before.Observation.Commit != after.Observation.Commit:
		reason = ReasonCommitChanged
	case before.Observation.Dirty != after.Observation.Dirty:
		reason = ReasonDirtyChanged
	case before.Repository != after.Repository || before.Derived != after.Derived:
		reason = ReasonProviderChanged
	case !equalJSON(before.Policy, after.Policy):
		reason = ReasonPolicyChanged
	case before.Observation.ID != after.Observation.ID:
		reason = ReasonObservationChanged
	}
	if len(details) == 0 {
		details = append(details, "source observation identity changed")
	}
	return SourceChange{
		Worktree: before.Observation.Worktree, Repository: after.Repository,
		Reason: reason, Detail: strings.Join(details, "; "),
		Before: observationCopy(&before.Observation), After: observationCopy(&after.Observation),
	}, true
}

func sourceMap(manifest sourceobservation.Manifest) (map[topology.WorktreeID]sourceobservation.Source, error) {
	result := make(map[topology.WorktreeID]sourceobservation.Source, len(manifest.Sources))
	for _, source := range manifest.Sources {
		worktree := source.Observation.Worktree
		if _, exists := result[worktree]; exists {
			return nil, fmt.Errorf("source manifest has duplicate worktree %q", worktree)
		}
		result[worktree] = source
	}
	return result, nil
}

func findProfile(state *State, profile string) (*ProfileState, bool) {
	for index := range state.Profiles {
		if state.Profiles[index].Profile == profile {
			return &state.Profiles[index], true
		}
	}
	return nil, false
}

func validProfileRecord(record ProfileRecord) (ProfileState, error) {
	profile := strings.TrimSpace(record.Profile)
	if _, err := topology.NewProfileID(profile); err != nil {
		return ProfileState{}, fmt.Errorf("invalidation profile: %w", err)
	}
	generation := strings.TrimSpace(record.Generation)
	if _, err := topology.NewGenerationID(generation); err != nil {
		return ProfileState{}, fmt.Errorf("invalidation generation: %w", err)
	}
	if err := record.Manifest.Validate(); err != nil {
		return ProfileState{}, fmt.Errorf("invalidation manifest: %w", err)
	}
	if record.Manifest.Profile != profile {
		return ProfileState{}, fmt.Errorf("invalidation manifest profile %q does not match %q", record.Manifest.Profile, profile)
	}
	return ProfileState{Profile: profile, Generation: generation, Manifest: cloneManifest(record.Manifest)}, nil
}

func rebuildSources(state *State) error {
	profiles := make(map[string]struct{}, len(state.Profiles))
	dependents := make(map[topology.WorktreeID]map[string]struct{})
	for index := range state.Profiles {
		profile := &state.Profiles[index]
		if _, exists := profiles[profile.Profile]; exists {
			return fmt.Errorf("duplicate invalidation profile %q", profile.Profile)
		}
		if _, err := topology.NewProfileID(profile.Profile); err != nil {
			return fmt.Errorf("invalidation profile: %w", err)
		}
		if _, err := topology.NewGenerationID(profile.Generation); err != nil {
			return fmt.Errorf("invalidation generation: %w", err)
		}
		if err := profile.Manifest.Validate(); err != nil {
			return fmt.Errorf("invalidation profile %q manifest: %w", profile.Profile, err)
		}
		if profile.Manifest.Profile != profile.Profile {
			return fmt.Errorf("invalidation profile %q manifest profile is %q", profile.Profile, profile.Manifest.Profile)
		}
		profiles[profile.Profile] = struct{}{}
		for _, source := range profile.Manifest.Sources {
			worktree := source.Observation.Worktree
			if dependents[worktree] == nil {
				dependents[worktree] = make(map[string]struct{})
			}
			dependents[worktree][profile.Profile] = struct{}{}
		}
	}
	state.Sources = state.Sources[:0]
	worktrees := make([]string, 0, len(dependents))
	for worktree := range dependents {
		worktrees = append(worktrees, string(worktree))
	}
	sort.Strings(worktrees)
	for _, value := range worktrees {
		profileNames := make([]string, 0, len(dependents[topology.WorktreeID(value)]))
		for profile := range dependents[topology.WorktreeID(value)] {
			profileNames = append(profileNames, profile)
		}
		sort.Strings(profileNames)
		state.Sources = append(state.Sources, SourceDependents{
			Worktree: topology.WorktreeID(value), Profiles: profileNames,
		})
	}
	state.Version = CurrentVersion
	return nil
}

func read(path string) (State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{Version: CurrentVersion, Profiles: []ProfileState{}, Sources: []SourceDependents{}}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read source invalidation state: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state State
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("decode source invalidation state: %w", err)
	}
	if state.Profiles == nil {
		state.Profiles = []ProfileState{}
	}
	if state.Sources == nil {
		state.Sources = []SourceDependents{}
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return State{}, errors.New("decode source invalidation state: multiple documents")
		}
		return State{}, fmt.Errorf("decode source invalidation state: %w", err)
	}
	if err := validateState(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func write(root, path string, state State) error {
	if err := validateState(state); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create source invalidation root: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode source invalidation state: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(root, ".source-invalidation-*.tmp")
	if err != nil {
		return fmt.Errorf("create source invalidation state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure source invalidation state: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write source invalidation state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close source invalidation state: %w", err)
	}
	if err := durable.File(temporaryPath); err != nil {
		return fmt.Errorf("sync source invalidation state: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish source invalidation state: %w", err)
	}
	if err := durable.Directory(root); err != nil {
		return fmt.Errorf("sync source invalidation root: %w", err)
	}
	return nil
}

func validateState(state State) error {
	if state.Version != CurrentVersion {
		return fmt.Errorf("source invalidation state version %d: want %d", state.Version, CurrentVersion)
	}
	copyState := cloneState(state)
	if err := rebuildSources(&copyState); err != nil {
		return err
	}
	if !equalJSON(state.Sources, copyState.Sources) {
		return errors.New("source invalidation reverse index does not match profile manifests")
	}
	for _, profile := range state.Profiles {
		if !profile.Stale && len(profile.Changes) != 0 {
			return fmt.Errorf("invalidation profile %q has changes without stale state", profile.Profile)
		}
		for _, change := range profile.Changes {
			if err := validateChange(change); err != nil {
				return fmt.Errorf("invalidation profile %q change: %w", profile.Profile, err)
			}
		}
	}
	return nil
}

func validateChange(change SourceChange) error {
	switch change.Reason {
	case ReasonContentChanged, ReasonCommitChanged, ReasonDirtyChanged,
		ReasonBranchChanged, ReasonPolicyChanged, ReasonSourceAdded,
		ReasonSourceRemoved, ReasonSourceUnavailable, ReasonProviderChanged,
		ReasonObservationChanged, ReasonProfileConfigurationChange,
		ReasonResolverChanged, ReasonAnalyzerChanged:
	default:
		return fmt.Errorf("unknown reason %q", change.Reason)
	}
	if change.Worktree != "" {
		if _, err := topology.NewWorktreeID(string(change.Worktree)); err != nil {
			return err
		}
	}
	for _, observation := range []*topology.SourceObservation{change.Before, change.After} {
		if observation == nil {
			continue
		}
		if err := observation.Validate(); err != nil {
			return err
		}
		if change.Worktree != "" && observation.Worktree != change.Worktree {
			return errors.New("observation worktree does not match change")
		}
	}
	return nil
}

func equalJSON(left, right any) bool {
	leftData, leftErr := json.Marshal(left)
	rightData, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftData, rightData)
}

func observationCopy(observation *topology.SourceObservation) *topology.SourceObservation {
	if observation == nil {
		return nil
	}
	clone := *observation
	return &clone
}

func cloneChange(change SourceChange) SourceChange {
	clone := change
	clone.Before = nil
	clone.After = nil
	if change.Before != nil {
		before := *change.Before
		clone.Before = &before
	}
	if change.After != nil {
		after := *change.After
		clone.After = &after
	}
	return clone
}

func cloneManifest(manifest sourceobservation.Manifest) sourceobservation.Manifest {
	clone := manifest
	clone.Sources = make([]sourceobservation.Source, len(manifest.Sources))
	for index, source := range manifest.Sources {
		clone.Sources[index] = source
		clone.Sources[index].Policy.Languages = append([]string(nil), source.Policy.Languages...)
		clone.Sources[index].Policy.Manifests = append([]string(nil), source.Policy.Manifests...)
		clone.Sources[index].Policy.Roots = append([]string(nil), source.Policy.Roots...)
		clone.Sources[index].Policy.Exclusions = append([]string(nil), source.Policy.Exclusions...)
	}
	return clone
}

func cloneState(state State) State {
	clone := state
	clone.Profiles = make([]ProfileState, len(state.Profiles))
	for index, profile := range state.Profiles {
		clone.Profiles[index] = profile
		clone.Profiles[index].Manifest = cloneManifest(profile.Manifest)
		clone.Profiles[index].Changes = make([]SourceChange, len(profile.Changes))
		for changeIndex, change := range profile.Changes {
			clone.Profiles[index].Changes[changeIndex] = cloneChange(change)
		}
	}
	clone.Sources = make([]SourceDependents, len(state.Sources))
	for index, source := range state.Sources {
		clone.Sources[index] = source
		clone.Sources[index].Profiles = append([]string(nil), source.Profiles...)
	}
	return clone
}
