package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/invalidation"
	"github.com/Luqueee/kivgraph/internal/sourceobservation"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/topology"
)

func TestInvalidationSchedulerRebuildsEachStaleProfileOnce(t *testing.T) {
	manager, err := invalidation.Open(testsupport.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	first := schedulerTestManifest("default", "commit-a")
	second := schedulerTestManifest("other", "commit-a")
	for _, record := range []invalidation.ProfileRecord{
		{Profile: "default", Generation: "000001", Manifest: first},
		{Profile: "other", Generation: "000001", Manifest: second},
	} {
		if err := manager.RecordPublished(context.Background(), record); err != nil {
			t.Fatalf("RecordPublished(%q) error = %v", record.Profile, err)
		}
	}
	if err := manager.MarkStale(context.Background(), "shared", "shared", invalidation.ReasonContentChanged, "source changed"); err != nil {
		t.Fatal(err)
	}

	fake := &schedulerTestReindexer{manager: manager, manifests: map[string]sourceobservation.Manifest{
		"default": schedulerTestManifest("default", "commit-b"),
		"other":   schedulerTestManifest("other", "commit-b"),
	}, calls: make(chan string, 4)}
	scheduler := newInvalidationScheduler(context.Background(), manager, fake, nil)
	scheduler.enqueueStale()
	scheduler.enqueueStale()
	defer scheduler.Close()

	seen := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case profile := <-fake.calls:
			if seen[profile] {
				t.Fatalf("profile %q was scheduled more than once", profile)
			}
			seen[profile] = true
		case <-deadline:
			t.Fatalf("scheduled profiles = %v, want default and other", seen)
		}
	}
	scheduler.Close()
	state := manager.Snapshot()
	for _, profile := range state.Profiles {
		if profile.Stale {
			t.Fatalf("profile %q stayed stale after its rebuild: %#v", profile.Profile, profile)
		}
	}
}

func TestInvalidationSchedulerRetriesWhenReindexLeavesAProfileStale(t *testing.T) {
	manager := schedulerTestStaleManager(t)
	var logs bytes.Buffer
	fake := &schedulerTestReindexer{calls: make(chan string, 1)}
	scheduler := newInvalidationScheduler(context.Background(), manager, fake,
		slog.New(slog.NewTextHandler(&logs, nil)))
	defer scheduler.Close()

	scheduler.reindex("default")
	if profile := <-fake.calls; profile != "default" {
		t.Fatalf("ReindexProfile() profile = %q, want default", profile)
	}
	if !profileIsStale(manager.Snapshot(), "default") {
		t.Fatalf("profile %q stopped being stale without a published replacement", "default")
	}
	scheduler.mu.Lock()
	attempts := scheduler.attempts["default"]
	scheduler.mu.Unlock()
	if attempts != 1 {
		t.Fatalf("stale reindex attempts = %d, want 1", attempts)
	}
	if !strings.Contains(logs.String(), "rebuild completed without clearing stale source state") {
		t.Fatalf("scheduler log = %q, want the stale-state rebuild reason", logs.String())
	}
}

func TestInvalidationSchedulerGivesUpAfterTheRetryBudget(t *testing.T) {
	manager := schedulerTestStaleManager(t)
	var logs bytes.Buffer
	fake := &schedulerTestReindexer{calls: make(chan string, 1), err: errors.New("index failed")}
	scheduler := newInvalidationScheduler(context.Background(), manager, fake,
		slog.New(slog.NewTextHandler(&logs, nil)))
	defer scheduler.Close()
	scheduler.mu.Lock()
	scheduler.attempts["default"] = profileInvalidationAttempts - 1
	scheduler.mu.Unlock()

	scheduler.reindex("default")
	if profile := <-fake.calls; profile != "default" {
		t.Fatalf("ReindexProfile() profile = %q, want default", profile)
	}
	scheduler.mu.Lock()
	attempts := scheduler.attempts["default"]
	_, pending := scheduler.pending["default"]
	scheduler.mu.Unlock()
	if attempts != profileInvalidationAttempts || pending {
		t.Fatalf("retry state = attempts:%d pending:%t, want exhausted without a retry", attempts, pending)
	}
	if !strings.Contains(logs.String(), "gave up rebuilding a stale profile") ||
		!strings.Contains(logs.String(), "kivgraph index --full") {
		t.Fatalf("scheduler log = %q, want the give-up remedy", logs.String())
	}
}

func schedulerTestStaleManager(t *testing.T) *invalidation.Manager {
	t.Helper()
	manager, err := invalidation.Open(testsupport.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	manifest := schedulerTestManifest("default", "commit-a")
	if err := manager.RecordPublished(context.Background(), invalidation.ProfileRecord{
		Profile: "default", Generation: "000001", Manifest: manifest,
	}); err != nil {
		t.Fatalf("RecordPublished(default) error = %v", err)
	}
	if err := manager.MarkStale(context.Background(), "shared", "shared", invalidation.ReasonContentChanged, "source changed"); err != nil {
		t.Fatalf("MarkStale() error = %v", err)
	}
	return manager
}

type schedulerTestReindexer struct {
	manager   *invalidation.Manager
	manifests map[string]sourceobservation.Manifest
	calls     chan string
	err       error
}

func (reindexer *schedulerTestReindexer) ReindexProfile(ctx context.Context, profile string) error {
	if reindexer.err != nil {
		reindexer.calls <- profile
		return reindexer.err
	}
	if reindexer.manager == nil {
		reindexer.calls <- profile
		return nil
	}
	if err := reindexer.manager.RecordPublished(ctx, invalidation.ProfileRecord{
		Profile: profile, Generation: "000002", Manifest: reindexer.manifests[profile],
	}); err != nil {
		return err
	}
	reindexer.calls <- profile
	return nil
}

func schedulerTestManifest(profile, commit string) sourceobservation.Manifest {
	observation, err := topology.NewSourceObservation(
		"shared", commit, "main", false,
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	if err != nil {
		panic(err)
	}
	return sourceobservation.Manifest{
		Version:             sourceobservation.CurrentVersion,
		Profile:             profile,
		ResolverVersion:     "resolver",
		AnalyzerFingerprint: "analyzer",
		Sources: []sourceobservation.Source{{
			Repository:  "shared",
			Observation: observation,
		}},
	}
}
