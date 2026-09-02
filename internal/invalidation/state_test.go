package invalidation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	manager := newManager(t)
	manifest := manifestFor(t, "default", "source-main", "commit-a", false, "digest-a")
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
	manager := newManager(t)
	first := manifestFor(t, "default", "shared", "commit-a", false, "digest-a")
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
	seen := map[string]bool{}
	for _, profile := range state.Profiles {
		if seen[profile.Profile] {
			t.Fatalf("profile %q was recorded more than once", profile.Profile)
		}
		seen[profile.Profile] = true
		if profile.Profile != "default" && profile.Profile != "other" {
			t.Fatalf("unexpected profile record = %#v", profile)
		}
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
	manager := newManager(t)
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
	seen := map[string]bool{}
	for _, profile := range state.Profiles {
		seen[profile.Profile] = true
		switch profile.Profile {
		case "default":
			if !profile.Stale || len(profile.Changes) != 1 || profile.Changes[0].Reason != ReasonSourceAdded {
				t.Fatalf("profile %q state = %#v, want source_added", profile.Profile, profile)
			}
		case "other":
			if profile.Stale || len(profile.Changes) != 0 {
				t.Fatalf("profile %q state = %#v, want a fresh profile", profile.Profile, profile)
			}
		default:
			t.Fatalf("unexpected profile record = %#v", profile)
		}
	}
	if !seen["default"] || !seen["other"] {
		t.Fatalf("observed profiles = %v, want default and other", seen)
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
	manager := newManager(t)
	manifest := manifestFor(t, "default", "source", "commit-a", false, "digest-a")
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

func TestInvalidateAcceptsAnUnchangedManifest(t *testing.T) {
	manager := newManager(t)
	manifest := manifestFor(t, "default", "source", "commit-a", false, "digest-a")
	if err := manager.RecordPublished(context.Background(), ProfileRecord{
		Profile: "default", Generation: "000001", Manifest: manifest,
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Invalidate(context.Background(), "default", manifest); err != nil {
		t.Fatal(err)
	}
	state := manager.Snapshot()
	if len(state.Profiles) != 1 || state.Profiles[0].Stale || len(state.Profiles[0].Changes) != 0 {
		t.Fatalf("profile state = %#v, want a fresh unchanged publication", state.Profiles)
	}
}

func TestMarkStaleRequiresActionableUnavailableDetail(t *testing.T) {
	manager := newManager(t)
	manifest := manifestFor(t, "default", "source", "commit-a", false, "digest-a")
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
	if len(state.Profiles) != 1 || len(state.Profiles[0].Changes) != 1 {
		t.Fatalf("state = %#v, want one stale profile with one change for worktree %q", state.Profiles, "source")
	}
	if !state.Profiles[0].Stale || state.Profiles[0].Changes[0].Reason != ReasonSourceUnavailable ||
		state.Profiles[0].Changes[0].After != nil || !strings.Contains(state.Profiles[0].Changes[0].Detail, "removed") {
		t.Fatalf("state = %#v, want unavailable source diagnostic", state.Profiles[0])
	}
	before := manager.Snapshot()
	for _, reason := range []Reason{ReasonSourceAdded, ReasonSourceRemoved} {
		if err := manager.MarkStale(context.Background(), "source", "source", reason, "membership changed"); err == nil {
			t.Fatalf("MarkStale() accepted membership reason %q", reason)
		}
		if after := manager.Snapshot(); !equalJSON(before, after) {
			t.Fatalf("state after membership refusal = %#v, want unchanged %#v", after, before)
		}
	}
}

func TestInvalidateScopesSourceMembershipChangesToRequestingProfile(t *testing.T) {
	manager := newManager(t)
	defaultManifest := manifestFor(t, "default", "shared", "commit-a", false, "digest-a")
	removed := manifestFor(t, "default", "removed", "commit-a", false, "digest-removed")
	defaultManifest.Sources = append(defaultManifest.Sources, removed.Sources[0])
	otherManifest := manifestFor(t, "other", "shared", "commit-a", false, "digest-a")
	for _, record := range []ProfileRecord{
		{Profile: "default", Generation: "000001", Manifest: defaultManifest},
		{Profile: "other", Generation: "000001", Manifest: otherManifest},
	} {
		if err := manager.RecordPublished(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}

	if err := manager.Invalidate(context.Background(), "default",
		manifestFor(t, "default", "shared", "commit-b", false, "digest-b")); err != nil {
		t.Fatal(err)
	}
	state := manager.Snapshot()
	seen := map[string]bool{}
	for _, profile := range state.Profiles {
		if seen[profile.Profile] {
			t.Fatalf("profile %q was recorded more than once", profile.Profile)
		}
		seen[profile.Profile] = true
		switch profile.Profile {
		case "default":
			if len(profile.Changes) != 2 {
				t.Fatalf("profile %q changes = %#v, want shared and removed changes", profile.Profile, profile.Changes)
			}
			reasons := map[topology.WorktreeID]Reason{}
			for _, change := range profile.Changes {
				reasons[change.Worktree] = change.Reason
			}
			if reasons["shared"] != ReasonContentChanged || reasons["removed"] != ReasonSourceRemoved {
				t.Fatalf("profile %q change reasons = %v, want shared=%q and removed=%q",
					profile.Profile, reasons, ReasonContentChanged, ReasonSourceRemoved)
			}
		case "other":
			if len(profile.Changes) != 1 || profile.Changes[0].Reason != ReasonContentChanged ||
				profile.Changes[0].Repository != "shared" {
				t.Fatalf("profile %q changes = %#v, want only the shared-source change", profile.Profile, profile.Changes)
			}
		default:
			t.Fatalf("unexpected profile record = %#v", profile)
		}
	}
	if !seen["default"] || !seen["other"] {
		t.Fatalf("observed profiles = %v, want default and other", seen)
	}
}

func TestInvalidateScopesProfileWideChangesToRequestingProfile(t *testing.T) {
	manager := newManager(t)
	defaultManifest := manifestFor(t, "default", "shared", "commit-a", false, "digest-a")
	otherManifest := manifestFor(t, "other", "shared", "commit-a", false, "digest-a")
	for _, record := range []ProfileRecord{
		{Profile: "default", Generation: "000001", Manifest: defaultManifest},
		{Profile: "other", Generation: "000001", Manifest: otherManifest},
	} {
		if err := manager.RecordPublished(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}

	actual := manifestFor(t, "default", "shared", "commit-a", false, "digest-a")
	actual.ResolverVersion = "resolver-2"
	actual.AnalyzerFingerprint = "analyzer-2"
	if err := manager.Invalidate(context.Background(), "default", actual); err != nil {
		t.Fatal(err)
	}
	state := manager.Snapshot()
	seen := map[string]bool{}
	for _, profile := range state.Profiles {
		seen[profile.Profile] = true
		switch profile.Profile {
		case "default":
			if !profile.Stale || len(profile.Changes) != 2 {
				t.Fatalf("profile %q state = %#v, want resolver and analyzer changes", profile.Profile, profile)
			}
			reasons := map[Reason]bool{}
			for _, change := range profile.Changes {
				if change.Worktree != "" || change.Repository != "" {
					t.Fatalf("profile %q change = %#v, want a profile-wide change", profile.Profile, change)
				}
				reasons[change.Reason] = true
			}
			if !reasons[ReasonResolverChanged] || !reasons[ReasonAnalyzerChanged] {
				t.Fatalf("profile %q change reasons = %v, want resolver and analyzer changes", profile.Profile, reasons)
			}
		case "other":
			if profile.Stale || len(profile.Changes) != 0 {
				t.Fatalf("profile %q state = %#v, want a fresh dependent profile", profile.Profile, profile)
			}
		default:
			t.Fatalf("unexpected profile record = %#v", profile)
		}
	}
	if !seen["default"] || !seen["other"] {
		t.Fatalf("observed profiles = %v, want default and other", seen)
	}
}

func TestJoinDetailBoundsSegmentsAndCharacters(t *testing.T) {
	manager := newManager(t)
	manifest := manifestFor(t, "default", "source", "commit-a", false, "digest-a")
	if err := manager.RecordPublished(context.Background(), ProfileRecord{
		Profile: "default", Generation: "000001", Manifest: manifest,
	}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxDetailSegments+3; index++ {
		if err := manager.MarkStale(context.Background(), "source", "source", ReasonCommitChanged,
			"observation-"+string(rune('a'+index))); err != nil {
			t.Fatal(err)
		}
	}
	state := manager.Snapshot()
	if len(state.Profiles) != 1 || len(state.Profiles[0].Changes) != 1 {
		t.Fatalf("state = %#v, want one profile with one merged change", state)
	}
	detail := state.Profiles[0].Changes[0].Detail
	if got := strings.Count(detail, "; ") + 1; got != maxDetailSegments {
		t.Fatalf("detail segments after merging %d details = %d, want %d: %q",
			maxDetailSegments+3, got, maxDetailSegments, detail)
	}
	if !strings.Contains(detail, "observation-a") {
		t.Fatalf("detail = %q, want the first merged detail to be kept", detail)
	}
	longManager := newManager(t)
	longManifest := manifestFor(t, "default", "source", "commit-a", false, "digest-a")
	if err := longManager.RecordPublished(context.Background(), ProfileRecord{
		Profile: "default", Generation: "000001", Manifest: longManifest,
	}); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("x", maxDetailChars+100)
	if err := longManager.MarkStale(context.Background(), "source", "source", ReasonCommitChanged, long); err != nil {
		t.Fatal(err)
	}
	longState := longManager.Snapshot()
	if len(longState.Profiles) != 1 || len(longState.Profiles[0].Changes) != 1 {
		t.Fatalf("long-detail state = %#v, want one profile with one change", longState)
	}
	got := longState.Profiles[0].Changes[0].Detail
	if len([]rune(got)) > maxDetailChars {
		t.Fatalf("detail length after merging a %d-character detail = %d, want at most %d",
			len(long), len([]rune(got)), maxDetailChars)
	}
}

func TestRecordPublishedClearsOnlyTheRebuiltProfile(t *testing.T) {
	manager := newManager(t)
	first := manifestFor(t, "default", "shared", "commit-a", false, "digest-a")
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
	if profiles := second.Snapshot().Profiles; len(profiles) != 0 {
		t.Fatalf("second manager profiles before Refresh = %#v, want none until Refresh", profiles)
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

func TestOpenRejectsMalformedOrInconsistentState(t *testing.T) {
	tests := []struct {
		name      string
		wantError string
		write     func(*testing.T, string)
	}{
		{
			name: "unsupported version", wantError: "source invalidation state version",
			write: func(t *testing.T, root string) {
				writeRawState(t, root, `{"version":2,"profiles":[],"sources":[]}`)
			},
		},
		{
			name: "divergent reverse index", wantError: "reverse index",
			write: func(t *testing.T, root string) {
				state := publishedState(t, root)
				state.Sources[0].Profiles = []string{"other"}
				writeState(t, root, state)
			},
		},
		{
			name: "changes without stale", wantError: "changes without stale state",
			write: func(t *testing.T, root string) {
				state := publishedState(t, root)
				state.Profiles[0].Changes = []SourceChange{{Reason: ReasonCommitChanged}}
				writeState(t, root, state)
			},
		},
		{
			name: "unknown field", wantError: "decode source invalidation state",
			write: func(t *testing.T, root string) {
				writeRawState(t, root, `{"version":1,"profiles":[],"sources":[],"unexpected":true}`)
			},
		},
		{
			name: "trailing document", wantError: "multiple documents",
			write: func(t *testing.T, root string) {
				writeRawState(t, root, `{"version":1,"profiles":[],"sources":[]} {"version":1}`)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := testsupport.TempDir(t)
			test.write(t, root)
			if _, err := Open(root); err == nil {
				t.Fatalf("Open(%q) succeeded for %s", root, test.name)
			} else if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Open(%q) error = %v, want substring %q", root, err, test.wantError)
			}
		})
	}
}

func newManager(t *testing.T) *Manager {
	t.Helper()
	manager, err := Open(testsupport.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	return manager
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

func publishedState(t *testing.T, root string) State {
	t.Helper()
	manager, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RecordPublished(context.Background(), ProfileRecord{
		Profile: "default", Generation: "000001",
		Manifest: manifestFor(t, "default", "source", "commit-a", false, "digest-a"),
	}); err != nil {
		t.Fatal(err)
	}
	return manager.Snapshot()
}

func writeState(t *testing.T, root string, state State) {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writeRawState(t, root, string(data))
}

func writeRawState(t *testing.T, root, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, StateFileName), []byte(data+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
