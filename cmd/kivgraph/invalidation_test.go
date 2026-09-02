package main

import (
	"context"
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
			t.Fatal(err)
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

type schedulerTestReindexer struct {
	manager   *invalidation.Manager
	manifests map[string]sourceobservation.Manifest
	calls     chan string
}

func (reindexer *schedulerTestReindexer) ReindexProfile(ctx context.Context, profile string) error {
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
