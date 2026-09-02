package sourceobservation

import (
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/topology"
)

func TestDiffRejectsInvalidManifestsBeforeComparingSources(t *testing.T) {
	valid := validManifest(t)
	cases := []struct {
		name     string
		expected Manifest
		actual   Manifest
		want     string
	}{
		{name: "invalid expected", expected: Manifest{}, actual: valid, want: "expected"},
		{name: "invalid actual", expected: valid, actual: Manifest{}, want: "current"},
		{name: "different profile", expected: valid, actual: manifestFor(t, "other", "shared", "b", "commit-2"), want: "profile changed"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Diff(test.expected, test.actual); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Diff() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDiffReportsAllChangesByRepositoryAndReason(t *testing.T) {
	before := Manifest{
		Version:             CurrentVersion,
		Profile:             "default",
		ResolverVersion:     "resolver-1",
		AnalyzerFingerprint: "analyzer-1",
		Sources: []Source{
			manifestFor(t, "default", "z-source", "z", "commit-1").Sources[0],
			manifestFor(t, "default", "a-source", "a", "commit-1").Sources[0],
		},
	}
	after := Manifest{
		Version:             CurrentVersion,
		Profile:             "default",
		ResolverVersion:     "resolver-1",
		AnalyzerFingerprint: "analyzer-1",
		Sources: []Source{
			manifestForDigest(t, "default", "z-source", "z", "commit-2", "b").Sources[0],
			manifestFor(t, "default", "new-source", "new", "commit-1").Sources[0],
		},
	}
	changes, err := Diff(before, after)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if len(changes) != 3 {
		t.Fatalf("Diff() changes = %#v, want three changes", changes)
	}
	if got := []string{changes[0].Repository, changes[1].Repository, changes[2].Repository}; strings.Join(got, ",") != "a-source,new-source,z-source" {
		t.Fatalf("Diff() repositories = %v, want sorted names", got)
	}
	if changes[2].Reason != "source content changed" {
		t.Fatalf("content change reason = %q, want source content changed", changes[2].Reason)
	}
	if changes[1].Before.Repository != "" || changes[1].After.Repository != "new-source" {
		t.Fatalf("added source = %#v, want only after source", changes[1])
	}
}

func TestDiffTurnsAnalyzerConfigurationChangesIntoProfileInvalidation(t *testing.T) {
	before := validManifest(t)
	for _, test := range []struct {
		name, reason string
		change       func(*Manifest)
	}{
		{name: "analyzer", reason: "analyzer configuration changed", change: func(manifest *Manifest) {
			manifest.AnalyzerFingerprint = "analyzer-2"
		}},
		{name: "resolver", reason: "resolver configuration changed", change: func(manifest *Manifest) {
			manifest.ResolverVersion = "resolver-2"
		}},
		{name: "both", reason: "resolver and analyzer configuration changed", change: func(manifest *Manifest) {
			manifest.ResolverVersion = "resolver-2"
			manifest.AnalyzerFingerprint = "analyzer-2"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			after := validManifest(t)
			test.change(&after)
			changes, err := Diff(before, after)
			if err != nil {
				t.Fatalf("Diff() error = %v", err)
			}
			if len(changes) != 1 || !changes[0].ProfileScoped || changes[0].Reason != test.reason {
				t.Fatalf("configuration changes = %#v, want one profile-scoped change", changes)
			}

			tracker := NewTracker()
			if err := tracker.Register("default", "000001", before); err != nil {
				t.Fatal(err)
			}
			report, err := tracker.Observe("default", after)
			if err != nil {
				t.Fatalf("Tracker.Observe() error = %v", err)
			}
			if len(report.Profiles) != 1 || report.Profiles[0] != "default" {
				t.Fatalf("configuration invalidation profiles = %#v, want only default", report.Profiles)
			}
		})
	}
}

func TestDiffReportsProfileConfigurationChangeForAnEmptySourceSet(t *testing.T) {
	before := validManifest(t)
	before.Sources = []Source{}
	after := before
	after.AnalyzerFingerprint = "analyzer-2"

	changes, err := Diff(before, after)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if len(changes) != 1 || !changes[0].ProfileScoped || changes[0].Repository != "" {
		t.Fatalf("empty-source configuration changes = %#v, want one profile-only change", changes)
	}

	tracker := NewTracker()
	if err := tracker.Register("default", "000001", before); err != nil {
		t.Fatal(err)
	}
	report, err := tracker.Observe("default", after)
	if err != nil {
		t.Fatalf("Tracker.Observe() error = %v", err)
	}
	if len(report.Profiles) != 1 || report.Profiles[0] != "default" ||
		report.Reason != "source changed: profile: analyzer configuration changed" {
		t.Fatalf("empty-source invalidation report = %#v, want the profile only", report)
	}
}

func TestDiffReportsEachSourceStateChangeReason(t *testing.T) {
	before := manifestFor(t, "default", "source", "source-main", "commit-1").Sources[0]
	tests := []struct {
		name   string
		after  Source
		reason string
	}{
		{name: "provider kind", after: func() Source { changed := before; changed.Derived = true; return changed }(), reason: "source provider kind changed"},
		{name: "worktree", after: manifestFor(t, "default", "source", "source-other", "commit-1").Sources[0], reason: "worktree selection changed"},
		{name: "content", after: manifestForDigest(t, "default", "source", "source-main", "commit-1", "b").Sources[0], reason: "source content changed"},
		{name: "commit", after: manifestFor(t, "default", "source", "source-main", "commit-2").Sources[0], reason: "source commit changed"},
		{name: "branch", after: manifestForDetails(t, "default", "source", "source-main", "commit-1", "feature", false, "a").Sources[0], reason: "source branch changed"},
		{name: "dirty", after: manifestForDetails(t, "default", "source", "source-main", "commit-1", "main", true, "a").Sources[0], reason: "source dirty state changed"},
		{name: "policy", after: func() Source { changed := before; changed.Policy.Languages = []string{"rust"}; return changed }(), reason: "source input policy changed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sourceChangeReason(before, test.after); got != test.reason {
				t.Fatalf("sourceChangeReason() = %q, want %q", got, test.reason)
			}
		})
	}
}

func TestTrackerRejectsIncompleteInvalidationRequests(t *testing.T) {
	valid := validManifest(t)
	tracker := NewTracker()
	if err := tracker.Register("other", "000001", valid); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("Register(mismatched profile) error = %v", err)
	}
	if _, err := tracker.Observe("default", valid); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("Observe(unregistered) error = %v", err)
	}
	if _, err := tracker.Invalidate("default", nil); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("Invalidate(empty) error = %v", err)
	}
	if _, err := tracker.MarkUnavailable("default", "missing"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("MarkUnavailable(unregistered) error = %v", err)
	}
	if err := tracker.RecordFailure("default", "failed"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("RecordFailure(unregistered) error = %v", err)
	}
	if _, err := tracker.Invalidate("default", []Change{{Reason: "missing repository"}}); err == nil || !strings.Contains(err.Error(), "repository") {
		t.Fatalf("Invalidate(missing repository) error = %v", err)
	}
	if _, err := tracker.Invalidate("default", []Change{{Repository: "shared"}}); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("Invalidate(missing reason) error = %v", err)
	}
	if _, err := tracker.Invalidate("default", []Change{{Repository: "source", Reason: "invalid", ProfileScoped: true}}); err == nil || !strings.Contains(err.Error(), "both source states") {
		t.Fatalf("Invalidate(profile change without states) error = %v", err)
	}
	failing := valid.Sources[0]
	failing.Repository = "other"
	if _, err := tracker.Invalidate("default", []Change{{Repository: "source", Reason: "mismatch", Before: failing}}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("Invalidate(mismatched source) error = %v", err)
	}
	if _, err := tracker.Invalidate("default", []Change{{Repository: "source", Reason: "invalid", Before: Source{Repository: "source"}}}); err == nil || !strings.Contains(err.Error(), "source observation") {
		t.Fatalf("Invalidate(invalid observation) error = %v", err)
	}
	if _, err := tracker.Invalidate("default", []Change{{Repository: "source", Reason: "missing state"}}); err == nil || !strings.Contains(err.Error(), "no source state") {
		t.Fatalf("Invalidate(missing source state) error = %v", err)
	}
}

func TestTrackerCopiesRegisteredManifestBeforeIndexingIt(t *testing.T) {
	manifest := validManifest(t)
	tracker := NewTracker()
	if err := tracker.Register("default", "000001", manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Sources[0].Policy.Languages[0] = "rust"
	actual := validManifest(t)
	actual.Sources[0].Policy.Languages[0] = "rust"

	report, err := tracker.Observe("default", actual)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if report.Reason != "source changed: source: source input policy changed" {
		t.Fatalf("Observe() reason = %q, want the registered manifest to stay unchanged", report.Reason)
	}
}

func TestTrackerObserveLeavesAProfileCleanWhenTheManifestIsUnchanged(t *testing.T) {
	manifest := validManifest(t)
	tracker := NewTracker()
	if err := tracker.Register("default", "000001", manifest); err != nil {
		t.Fatal(err)
	}
	report, err := tracker.Observe("default", manifest)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if len(report.Profiles) != 0 || tracker.Statuses()[0].Stale {
		t.Fatalf("unchanged observation = %#v, statuses = %#v", report, tracker.Statuses())
	}
}

func TestTrackerInvalidatesEveryProfileSharingAWorktree(t *testing.T) {
	tracker := NewTracker()
	defaultManifest := manifestFor(t, "default", "shared", "shared", "commit-1")
	otherManifest := manifestFor(t, "other", "shared", "shared", "commit-1")
	if err := tracker.Register("default", "000001", defaultManifest); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Register("other", "000004", otherManifest); err != nil {
		t.Fatal(err)
	}
	changed := manifestForDigest(t, "default", "shared", "shared", "commit-2", "b")
	report, err := tracker.Observe("default", changed)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if got := strings.Join(report.Profiles, ","); got != "default,other" {
		t.Fatalf("invalidated profiles = %q, want default,other", got)
	}
	if report.Reason != "source changed: shared: source content changed" {
		t.Fatalf("invalidation reason = %q", report.Reason)
	}
	statuses := tracker.Statuses()
	if len(statuses) != 2 || !statuses[0].Stale || !statuses[1].Stale {
		t.Fatalf("statuses = %#v, want both profiles stale", statuses)
	}
	if statuses[0].Generation != "000001" || statuses[1].Generation != "000004" {
		t.Fatalf("statuses = %#v, want last valid generations retained", statuses)
	}
}

func TestTrackerRemovesOldReverseDependenciesWhenAProfileCommits(t *testing.T) {
	tracker := NewTracker()
	if err := tracker.Register("default", "000001", manifestFor(t, "default", "shared", "shared", "commit-1")); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Register("other", "000002", manifestFor(t, "other", "shared", "shared", "commit-1")); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Commit("other", "000003", manifestFor(t, "other", "private", "private", "commit-1")); err != nil {
		t.Fatal(err)
	}

	report, err := tracker.Observe("default", manifestForDigest(t, "default", "shared", "shared", "commit-2", "b"))
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if got := strings.Join(report.Profiles, ","); got != "default" {
		t.Fatalf("invalidated profiles = %q, want old dependency removed", got)
	}
}

func TestTrackerFailedRebuildKeepsStaleGenerationUntilCommit(t *testing.T) {
	tracker := NewTracker()
	before := manifestFor(t, "default", "shared", "shared", "commit-1")
	if err := tracker.Register("default", "000001", before); err != nil {
		t.Fatal(err)
	}
	if err := tracker.RecordFailure("default", "rebuild failed: analyzer unavailable"); err != nil {
		t.Fatal(err)
	}
	status := tracker.Statuses()[0]
	if !status.Stale || status.Generation != "000001" || !strings.Contains(status.Reason, "analyzer unavailable") {
		t.Fatalf("status after failure = %#v, want stale generation with reason", status)
	}
	after := manifestFor(t, "default", "shared", "shared", "commit-2")
	if err := tracker.Commit("default", "000002", after); err != nil {
		t.Fatal(err)
	}
	status = tracker.Statuses()[0]
	if status.Stale || status.Generation != "000002" || status.Reason != "" {
		t.Fatalf("status after commit = %#v, want current generation", status)
	}
}

func TestTrackerUnavailableSourceMarksSharedDependents(t *testing.T) {
	tracker := NewTracker()
	for _, profile := range []string{"default", "other"} {
		if err := tracker.Register(profile, "000001", manifestFor(t, profile, "shared", "shared", "commit-1")); err != nil {
			t.Fatal(err)
		}
	}
	report, err := tracker.MarkUnavailable("default", "source input is absent: shared")
	if err != nil {
		t.Fatalf("MarkUnavailable() error = %v", err)
	}
	if got := strings.Join(report.Profiles, ","); got != "default,other" {
		t.Fatalf("unavailable profiles = %q, want both dependents", got)
	}
}

func manifestFor(t *testing.T, profile, repository, worktree, commit string) Manifest {
	return manifestForDigest(t, profile, repository, worktree, commit, "a")
}

func manifestForDigest(t *testing.T, profile, repository, worktree, commit, digestCharacter string) Manifest {
	return manifestForDetails(t, profile, repository, worktree, commit, "main", false, digestCharacter)
}

func manifestForDetails(t *testing.T, profile, repository, worktree, commit, branch string, dirty bool, digestCharacter string) Manifest {
	t.Helper()
	observation, err := topology.NewSourceObservation(topology.WorktreeID(worktree), commit, branch, dirty, strings.Repeat(digestCharacter, 64))
	if err != nil {
		t.Fatal(err)
	}
	return Manifest{
		Version:             CurrentVersion,
		Profile:             profile,
		ResolverVersion:     "resolver-1",
		AnalyzerFingerprint: "analyzer-1",
		Sources: []Source{{
			Repository:  repository,
			Observation: observation,
			Policy:      Policy{Languages: []string{"go"}},
		}},
	}
}
