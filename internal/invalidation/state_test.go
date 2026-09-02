package invalidation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/sourceobservation"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/topology"
)

func TestRecordPublishedBuildsReverseSourceIndex(t *testing.T) {
	manager, manifest := newManager(t, "default", "source-main", "commit-a", false, "digest-a")
	if err := manager.RecordPublished(context.Background(), ProfileRecord{
		Profile: "default", Generation: "000001", Manifest: manifest,
	}); err != nil {
		t.Fatal(err)
	}
	if got, want := manager.ProfilesForSource("source-main"), []string{"default"}; !equalStrings(got, want) {
		t.Fatalf("profiles for source = %v, want %v", got, want)
	}
	state := manager.Snapshot()
	if len(state.Profiles) != 1 || state.Profiles[0].Stale || len(state.Profiles[0].Changes) != 0 {
		t.Fatalf("profile state = %#v, want a fresh published record", state.Profiles)
	}
	if len(state.Sources) != 1 || !equalStrings(state.Sources[0].Profiles, []string{"default"}) {
		t.Fatalf("reverse index = %#v, want the dependent profile", state.Sources)
	}
	if _, err := os.Stat(filepath.Join(manager.root, StateFileName)); err != nil {
		t.Fatalf("state file was not persisted: %v", err)
	}
}

func TestInvalidateFansOutToEveryDependentProfile(t *testing.T) {
	manager, first := newManager(t, "default", "shared", "commit-a", false, "digest-a")
	second := manifestFor(t, "other", "shared", "commit-a", false, "digest-a")
	for _, record := range []ProfileRecord{
		{Profile: "default", Generation: "000001", Manifest: first},
		{Profile: "other", Generation: "000001", Manifest: second},
	} {
		if err := manager.RecordPublished(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}

	actual := manifestFor(t, "default", "shared", "commit-b", true, "digest-b")
	if err := manager.Invalidate(context.Background(), "default", actual); err != nil {
		t.Fatal(err)
	}
	state := manager.Snapshot()
	if len(state.Profiles) != 2 {
		t.Fatalf("profiles = %#v, want stale records for default and other", state.Profiles)
	}
	for _, profile := range state.Profiles {
		if !profile.Stale || len(profile.Changes) != 1 {
			t.Fatalf("profile %q state = %#v, want one stale shared-source change", profile.Profile, profile)
		}
		if profile.Changes[0].Reason != ReasonContentChanged || profile.Changes[0].Repository != "shared" {
			t.Fatalf("profile %q change = %#v, want content change for shared", profile.Profile, profile.Changes[0])
		}
		if profile.Changes[0].Before == nil || profile.Changes[0].After == nil {
			t.Fatalf("profile %q change = %#v, want before and after observations", profile.Profile, profile.Changes[0])
		}
	}
}

func TestInvalidateMarksTheRequestedProfileWhenAProviderIsAdded(t *testing.T) {
	manager, err := Open(testsupport.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	empty := sourceobservation.Manifest{
		Version: sourceobservation.CurrentVersion, Profile: "default",
		ResolverVersion: "resolver-1", AnalyzerFingerprint: "analyzer-1",
		Sources: []sourceobservation.Source{},
	}
	if err := manager.RecordPublished(context.Background(), ProfileRecord{
		Profile: "default", Generation: "000001", Manifest: empty,
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.RecordPublished(context.Background(), ProfileRecord{
		Profile: "other", Generation: "000001",
		Manifest: manifestFor(t, "other", "shared", "commit-a", false, "digest-a"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Invalidate(context.Background(), "default",
		manifestFor(t, "default", "shared", "commit-a", false, "digest-a")); err != nil {
		t.Fatal(err)
	}
	state := manager.Snapshot()
	if len(state.Profiles) != 2 {
		t.Fatalf("profiles = %#v, want requested and shared dependent records", state.Profiles)
	}
	for _, profile := range state.Profiles {
		if profile.Profile == "default" {
			if !profile.Stale || len(profile.Changes) != 1 || profile.Changes[0].Reason != ReasonSourceAdded {
				t.Fatalf("requested profile state = %#v, want source_added", profile)
			}
		}
		if profile.Profile == "other" && profile.Stale {
			t.Fatalf("unrelated profile became stale after source membership change: %#v", profile)
		}
	}
}

func TestCompareManifestsDistinguishesCommitAndDirtyChanges(t *testing.T) {
	expected := manifestFor(t, "default", "source", "commit-a", false, "digest-a")
	commitOnly := manifestFor(t, "default", "source", "commit-b", false, "digest-a")
	changes, err := CompareManifests(expected, commitOnly)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Reason != ReasonCommitChanged {
		t.Fatalf("commit-only changes = %#v, want commit_changed", changes)
	}

	dirtyOnly := manifestFor(t, "default", "source", "commit-a", true, "digest-a")
	changes, err = CompareManifests(expected, dirtyOnly)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Reason != ReasonDirtyChanged {
		t.Fatalf("dirty-only changes = %#v, want dirty_changed", changes)
	}
}

func TestInvalidateRejectsUntrackedProfileAndInvalidManifest(t *testing.T) {
	manager, manifest := newManager(t, "default", "source", "commit-a", false, "digest-a")
	if err := manager.Invalidate(context.Background(), "default", manifest); !errors.Is(err, ErrProfileNotTracked) {
		t.Fatalf("Invalidate() error = %v, want untracked profile refusal", err)
	}
	if err := manager.RecordPublished(context.Background(), ProfileRecord{
		Profile: "default", Generation: "000001", Manifest: manifest,
	}); err != nil {
		t.Fatal(err)
	}
	invalid := manifestFor(t, "default", "source", "commit-a", false, "digest-a")
	invalid.Sources[0].Observation.ContentDigest = "tampered"
	before := manager.Snapshot()
	if err := manager.Invalidate(context.Background(), "default", invalid); err == nil ||
		!errors.Is(err, topology.ErrInvalidSourceObservation) {
		t.Fatalf("Invalidate() error = %v, want invalid-manifest refusal", err)
	}
	if after := manager.Snapshot(); !equalJSON(before, after) {
		t.Fatalf("state after invalidation refusal = %#v, want unchanged %#v", after, before)
	}
}

func TestMarkStaleRequiresActionableUnavailableDetail(t *testing.T) {
	manager, manifest := newManager(t, "default", "source", "commit-a", false, "digest-a")
	if err := manager.RecordPublished(context.Background(), ProfileRecord{
		Profile: "default", Generation: "000001", Manifest: manifest,
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Invalidate(context.Background(), "default",
		manifestFor(t, "default", "source", "commit-b", false, "digest-b")); err != nil {
		t.Fatal(err)
	}
	if err := manager.MarkStale(context.Background(), "source", "source", ReasonSourceUnavailable, ""); err == nil {
		t.Fatal("MarkStale() accepted an empty unavailable detail")
	}
	if err := manager.MarkStale(context.Background(), "source", "source", ReasonSourceUnavailable, "worktree was removed"); err != nil {
		t.Fatal(err)
	}
	state := manager.Snapshot()
	if !state.Profiles[0].Stale || state.Profiles[0].Changes[0].Reason != ReasonSourceUnavailable ||
		state.Profiles[0].Changes[0].After != nil || !strings.Contains(state.Profiles[0].Changes[0].Detail, "removed") {
		t.Fatalf("state = %#v, want unavailable source diagnostic", state.Profiles[0])
	}
}

func TestRecordPublishedClearsOnlyTheRebuiltProfile(t *testing.T) {
	manager, first := newManager(t, "default", "shared", "commit-a", false, "digest-a")
	second := manifestFor(t, "other", "shared", "commit-a", false, "digest-a")
	if err := manager.RecordPublished(context.Background(), ProfileRecord{Profile: "default", Generation: "000001", Manifest: first}); err != nil {
		t.Fatal(err)
	}
	if err := manager.RecordPublished(context.Background(), ProfileRecord{Profile: "other", Generation: "000001", Manifest: second}); err != nil {
		t.Fatal(err)
	}
	if err := manager.MarkStale(context.Background(), "shared", "shared", ReasonCommitChanged, "HEAD moved"); err != nil {
		t.Fatal(err)
	}
	updated := manifestFor(t, "default", "shared", "commit-b", false, "digest-b")
	if err := manager.RecordPublished(context.Background(), ProfileRecord{Profile: "default", Generation: "000002", Manifest: updated}); err != nil {
		t.Fatal(err)
	}
	state := manager.Snapshot()
	if len(state.Profiles) != 2 {
		t.Fatalf("profiles = %#v, want records for default and other", state.Profiles)
	}
	seen := map[string]bool{}
	for _, profile := range state.Profiles {
		seen[profile.Profile] = true
		if profile.Profile == "default" && profile.Stale {
			t.Fatalf("rebuilt profile stayed stale: %#v", profile)
		}
		if profile.Profile == "other" && !profile.Stale {
			t.Fatalf("unrebuilt dependent profile was cleared: %#v", profile)
		}
	}
	if !seen["default"] || !seen["other"] {
		t.Fatalf("observed profiles = %v, want default and other", seen)
	}
}

func TestOpenRefreshesStateFromAnotherManager(t *testing.T) {
	root := testsupport.TempDir(t)
	first, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest := manifestFor(t, "default", "source", "commit-a", false, "digest-a")
	if err := first.RecordPublished(context.Background(), ProfileRecord{Profile: "default", Generation: "000001", Manifest: manifest}); err != nil {
		t.Fatal(err)
	}
	if len(second.Snapshot().Profiles) != 0 {
		t.Fatal("second manager unexpectedly observed an unrefreshed external write")
	}
	if err := second.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := second.ProfilesForSource("source"); !equalStrings(got, []string{"default"}) {
		t.Fatalf("ProfilesForSource(%q) after Refresh = %v, want %v", "source", got, []string{"default"})
	}
}

func TestOpenNormalizesNullCollectionsInAnEmptyState(t *testing.T) {
	root := testsupport.TempDir(t)
	data := []byte("{\"version\":1,\"profiles\":null,\"sources\":null}\n")
	if err := os.WriteFile(filepath.Join(root, StateFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	state := manager.Snapshot()
	if state.Profiles == nil || state.Sources == nil {
		t.Fatalf("state = %#v, want non-nil empty collections", state)
	}
	if len(state.Profiles) != 0 || len(state.Sources) != 0 {
		t.Fatalf("state = %#v, want empty collections", state)
	}
}

func newManager(t *testing.T, profile, worktree, commit string, dirty bool, digest string) (*Manager, sourceobservation.Manifest) {
	t.Helper()
	manager, err := Open(testsupport.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	return manager, manifestFor(t, profile, worktree, commit, dirty, digest)
}

func manifestFor(t *testing.T, profile, worktree, commit string, dirty bool, digest string) sourceobservation.Manifest {
	t.Helper()
	digestHash := sha256.Sum256([]byte(digest))
	digest = hex.EncodeToString(digestHash[:])
	observation, err := topology.NewSourceObservation(topology.WorktreeID(worktree), commit, "main", dirty, digest)
	if err != nil {
		t.Fatal(err)
	}
	return sourceobservation.Manifest{
		Version: sourceobservation.CurrentVersion, Profile: profile,
		ResolverVersion: "resolver-1", AnalyzerFingerprint: "analyzer-1",
		Sources: []sourceobservation.Source{{Repository: worktree, Observation: observation}},
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
