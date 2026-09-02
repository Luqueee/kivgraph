package sourceobservation

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Luqueee/kivgraph/internal/topology"
)

// Change identifies one source input whose observed state differs between two
// manifests. Before and After may be zero for a source that was removed or
// added, respectively.
type Change struct {
	Repository    string
	Before        Source
	After         Source
	Reason        string
	ProfileScoped bool
}

// Diff compares manifests by repository identity and returns every changed
// source in deterministic order. Comparing by position would turn a profile
// registry reorder into a false source change and would hide which input moved.
func Diff(expected, actual Manifest) ([]Change, error) {
	if err := expected.Validate(); err != nil {
		return nil, fmt.Errorf("validate expected source observations: %w", err)
	}
	if err := actual.Validate(); err != nil {
		return nil, fmt.Errorf("validate current source observations: %w", err)
	}
	if expected.Profile != actual.Profile {
		return nil, fmt.Errorf("%w: profile changed from %q to %q", ErrChanged, expected.Profile, actual.Profile)
	}
	configurationReason := sourceConfigurationChangeReason(expected, actual)

	before := sourcesByRepository(expected.Sources)
	after := sourcesByRepository(actual.Sources)
	names := make([]string, 0, len(before)+len(after))
	seen := make(map[string]struct{}, len(before)+len(after))
	for name := range before {
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for name := range after {
		if _, exists := seen[name]; !exists {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	changes := make([]Change, 0)
	for _, name := range names {
		oldSource, hadBefore := before[name]
		newSource, hadAfter := after[name]
		switch {
		case !hadBefore:
			changes = append(changes, Change{Repository: name, After: newSource, Reason: "source was added"})
		case !hadAfter:
			changes = append(changes, Change{Repository: name, Before: oldSource, Reason: "source was removed"})
		default:
			if reason := sourceChangeReason(oldSource, newSource); reason != "" {
				changes = append(changes, Change{Repository: name, Before: oldSource, After: newSource, Reason: reason})
			}
		}
	}
	if configurationReason != "" {
		changes = append(changes, Change{Reason: configurationReason, ProfileScoped: true})
	}
	return changes, nil
}

func sourceConfigurationChangeReason(expected, actual Manifest) string {
	resolverChanged := expected.ResolverVersion != actual.ResolverVersion
	analyzerChanged := expected.AnalyzerFingerprint != actual.AnalyzerFingerprint
	switch {
	case resolverChanged && analyzerChanged:
		return "resolver and analyzer configuration changed"
	case resolverChanged:
		return "resolver configuration changed"
	case analyzerChanged:
		return "analyzer configuration changed"
	default:
		return ""
	}
}

func sourcesByRepository(sources []Source) map[string]Source {
	result := make(map[string]Source, len(sources))
	for _, source := range sources {
		result[source.Repository] = source
	}
	return result
}

func sourceChangeReason(before, after Source) string {
	if before.Derived != after.Derived {
		return "source provider kind changed"
	}
	if before.Observation.Worktree != after.Observation.Worktree {
		return "worktree selection changed"
	}
	if before.Observation.ContentDigest != after.Observation.ContentDigest {
		return "source content changed"
	}
	if before.Observation.Commit != after.Observation.Commit {
		return "source commit changed"
	}
	if before.Observation.Branch != after.Observation.Branch {
		return "source branch changed"
	}
	if before.Observation.Dirty != after.Observation.Dirty {
		return "source dirty state changed"
	}
	if !bytes.Equal(mustEncode(before.Policy), mustEncode(after.Policy)) {
		return "source input policy changed"
	}
	return ""
}

// ProfileState is the last valid generation and the source manifest that
// produced it. The manifest remains unchanged while a replacement rebuild is
// stale or failing, so diagnostics always point at the last published truth.
type ProfileState struct {
	Profile    string
	Generation string
	Manifest   Manifest
	Stale      bool
	Reason     string
}

// ProfileStatus is the read-only diagnostic projection of one tracked profile.
type ProfileStatus struct {
	Profile    string
	Generation string
	Stale      bool
	Reason     string
}

// InvalidationReport describes one coalesced source change and every profile
// whose effective source set may now be stale.
type InvalidationReport struct {
	TriggerProfile string
	Sources        []Change
	Profiles       []string
	Reason         string
}

type trackedProfile struct {
	ProfileState
	worktrees map[topology.WorktreeID]struct{}
}

// Tracker maintains the reverse relationship from a shared worktree to every
// dependent profile. It is intentionally in-memory: published generations and
// their source manifests remain the durable source of truth, while this index
// is rebuilt when a serving process starts.
type Tracker struct {
	mu         sync.Mutex
	profiles   map[string]*trackedProfile
	dependents map[topology.WorktreeID]map[string]struct{}
}

// NewTracker creates an empty source dependency tracker.
func NewTracker() *Tracker {
	return &Tracker{
		profiles:   make(map[string]*trackedProfile),
		dependents: make(map[topology.WorktreeID]map[string]struct{}),
	}
}

// Register records the source manifest of a profile. Register replaces an old
// record for the same profile and rebuilds its reverse-index entries.
func (tracker *Tracker) Register(profile, generation string, manifest Manifest) error {
	if tracker == nil {
		return errors.New("source invalidation tracker is nil")
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("register source observations: %w", err)
	}
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return errors.New("source invalidation profile is required")
	}
	if manifest.Profile != profile {
		return fmt.Errorf("source invalidation profile %q does not match manifest profile %q", profile, manifest.Profile)
	}

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.removeLocked(profile)
	state := &trackedProfile{
		ProfileState: ProfileState{Profile: profile, Generation: generation, Manifest: cloneManifest(manifest)},
		worktrees:    make(map[topology.WorktreeID]struct{}),
	}
	for _, source := range manifest.Sources {
		worktree := source.Observation.Worktree
		state.worktrees[worktree] = struct{}{}
		dependents := tracker.dependents[worktree]
		if dependents == nil {
			dependents = make(map[string]struct{})
			tracker.dependents[worktree] = dependents
		}
		dependents[profile] = struct{}{}
	}
	tracker.profiles[profile] = state
	return nil
}

// Observe compares a new source observation with the last valid generation and
// marks all profiles sharing a changed source stale. It does not advance any
// profile state; Commit does that only after a rebuild was published.
func (tracker *Tracker) Observe(profile string, actual Manifest) (InvalidationReport, error) {
	if tracker == nil {
		return InvalidationReport{}, errors.New("source invalidation tracker is nil")
	}
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return InvalidationReport{}, errors.New("source invalidation profile is required")
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	state, exists := tracker.profiles[profile]
	if !exists {
		return InvalidationReport{}, fmt.Errorf("source invalidation profile %q is not registered", profile)
	}
	changes, err := Diff(state.Manifest, actual)
	if err != nil {
		return InvalidationReport{}, err
	}
	if len(changes) == 0 {
		return InvalidationReport{}, nil
	}
	return tracker.invalidateLocked(profile, changes), nil
}

// Invalidate marks a profile and every profile sharing one of the changed
// worktrees stale. Multiple changes supplied in one call are one report, which
// is the coalescing boundary for a single source-observation window.
func (tracker *Tracker) Invalidate(profile string, changes []Change) (InvalidationReport, error) {
	if tracker == nil {
		return InvalidationReport{}, errors.New("source invalidation tracker is nil")
	}
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return InvalidationReport{}, errors.New("source invalidation profile is required")
	}
	if len(changes) == 0 {
		return InvalidationReport{}, errors.New("source invalidation requires at least one source change")
	}
	if err := validateChanges(changes); err != nil {
		return InvalidationReport{}, err
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if _, exists := tracker.profiles[profile]; !exists {
		return InvalidationReport{}, fmt.Errorf("source invalidation profile %q is not registered", profile)
	}
	return tracker.invalidateLocked(profile, changes), nil
}

func (tracker *Tracker) invalidateLocked(profile string, changes []Change) InvalidationReport {
	affected := map[string]struct{}{profile: {}}
	for _, change := range changes {
		if change.ProfileScoped {
			continue
		}
		for _, source := range []Source{change.Before, change.After} {
			worktree := source.Observation.Worktree
			for dependent := range tracker.dependents[worktree] {
				affected[dependent] = struct{}{}
			}
		}
	}
	profiles := make([]string, 0, len(affected))
	for name := range affected {
		profiles = append(profiles, name)
	}
	sort.Strings(profiles)
	reason := invalidationReason(changes)
	for _, name := range profiles {
		if state := tracker.profiles[name]; state != nil {
			state.Stale = true
			state.Reason = reason
		}
	}
	return InvalidationReport{
		TriggerProfile: profile,
		Sources:        cloneChanges(changes),
		Profiles:       profiles,
		Reason:         reason,
	}
}

// Commit installs the source state of a successfully published generation and
// clears its stale diagnostic. A failed rebuild must not call Commit.
func (tracker *Tracker) Commit(profile, generation string, manifest Manifest) error {
	return tracker.Register(profile, generation, manifest)
}

// RecordFailure retains the previous generation and names why the replacement
// is still stale. The dependency reverse index is not changed.
func (tracker *Tracker) RecordFailure(profile, reason string) error {
	if tracker == nil {
		return errors.New("source invalidation tracker is nil")
	}
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return errors.New("source invalidation profile is required")
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	state, exists := tracker.profiles[profile]
	if !exists {
		return fmt.Errorf("source invalidation profile %q is not registered", profile)
	}
	state.Stale = true
	state.Reason = strings.TrimSpace(reason)
	if state.Reason == "" {
		state.Reason = "profile rebuild failed"
	}
	return nil
}

// MarkUnavailable marks the profile and every profile sharing any of its
// sources stale when a source cannot be observed. The last valid manifest is
// retained so a later recovery can compare against it.
func (tracker *Tracker) MarkUnavailable(profile, reason string) (InvalidationReport, error) {
	if tracker == nil {
		return InvalidationReport{}, errors.New("source invalidation tracker is nil")
	}
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return InvalidationReport{}, errors.New("source invalidation profile is required")
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	state, exists := tracker.profiles[profile]
	if !exists {
		return InvalidationReport{}, fmt.Errorf("source invalidation profile %q is not registered", profile)
	}
	affected := map[string]struct{}{profile: {}}
	for worktree := range state.worktrees {
		for dependent := range tracker.dependents[worktree] {
			affected[dependent] = struct{}{}
		}
	}
	profiles := make([]string, 0, len(affected))
	for name := range affected {
		profiles = append(profiles, name)
	}
	sort.Strings(profiles)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "source observation unavailable"
	}
	for _, name := range profiles {
		if current := tracker.profiles[name]; current != nil {
			current.Stale = true
			current.Reason = reason
		}
	}
	return InvalidationReport{TriggerProfile: profile, Profiles: profiles, Reason: reason}, nil
}

// Statuses returns a deterministic snapshot of all profile diagnostics.
func (tracker *Tracker) Statuses() []ProfileStatus {
	if tracker == nil {
		return nil
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	result := make([]ProfileStatus, 0, len(tracker.profiles))
	for _, state := range tracker.profiles {
		result = append(result, ProfileStatus{
			Profile: state.Profile, Generation: state.Generation,
			Stale: state.Stale, Reason: state.Reason,
		})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Profile < result[right].Profile })
	return result
}

func (tracker *Tracker) removeLocked(profile string) {
	old := tracker.profiles[profile]
	if old == nil {
		return
	}
	for worktree := range old.worktrees {
		dependents := tracker.dependents[worktree]
		delete(dependents, profile)
		if len(dependents) == 0 {
			delete(tracker.dependents, worktree)
		}
	}
	delete(tracker.profiles, profile)
}

func invalidationReason(changes []Change) string {
	details := make([]string, 0, len(changes))
	for _, change := range changes {
		if change.Repository == "" {
			details = append(details, fmt.Sprintf("profile: %s", change.Reason))
			continue
		}
		details = append(details, fmt.Sprintf("%s: %s", change.Repository, change.Reason))
	}
	sort.Strings(details)
	return "source changed: " + strings.Join(details, "; ")
}

func cloneChanges(changes []Change) []Change {
	result := make([]Change, len(changes))
	for index, change := range changes {
		result[index] = change
		result[index].Before = cloneSource(change.Before)
		result[index].After = cloneSource(change.After)
	}
	return result
}

func validateChanges(changes []Change) error {
	for index, change := range changes {
		repository := strings.TrimSpace(change.Repository)
		profileOnly := change.ProfileScoped && repository == "" &&
			change.Before.Repository == "" && change.After.Repository == ""
		if repository == "" && !profileOnly {
			return fmt.Errorf("source invalidation changes[%d].repository is required", index)
		}
		if strings.TrimSpace(change.Reason) == "" {
			return fmt.Errorf("source invalidation changes[%d].reason is required", index)
		}
		if change.ProfileScoped && !profileOnly {
			if change.Before.Repository == "" || change.After.Repository == "" {
				return fmt.Errorf("source invalidation changes[%d] profile-scoped change requires both source states", index)
			}
		}
		for _, side := range []struct {
			name   string
			source Source
		}{
			{name: "before", source: change.Before},
			{name: "after", source: change.After},
		} {
			source := side.source
			if source.Repository == "" {
				continue
			}
			if source.Repository != repository {
				return fmt.Errorf("source invalidation changes[%d].%s repository %q does not match %q", index, side.name, source.Repository, repository)
			}
			if err := source.Observation.Validate(); err != nil {
				return fmt.Errorf("source invalidation changes[%d].%s: %w", index, side.name, err)
			}
		}
		if change.Before.Repository == "" && change.After.Repository == "" && !profileOnly {
			return fmt.Errorf("source invalidation changes[%d] has no source state", index)
		}
	}
	return nil
}

func cloneManifest(manifest Manifest) Manifest {
	result := manifest
	result.Sources = make([]Source, len(manifest.Sources))
	for index, source := range manifest.Sources {
		result.Sources[index] = cloneSource(source)
	}
	return result
}

func cloneSource(source Source) Source {
	result := source
	result.Policy.Languages = append([]string(nil), source.Policy.Languages...)
	result.Policy.Manifests = append([]string(nil), source.Policy.Manifests...)
	result.Policy.Roots = append([]string(nil), source.Policy.Roots...)
	result.Policy.Exclusions = append([]string(nil), source.Policy.Exclusions...)
	return result
}
