package invalidation

import (
	"context"
	"testing"
)

// A file saved after a pass last verified its sources is not in the generation
// that pass publishes. The watcher has already seen it and marked the profile
// stale; publishing must not erase that, or the graph lacks the edit, the
// state says fresh and nothing ever reports the edit again.
func TestRecordPublishedKeepsAChangeTheGenerationDoesNotContain(t *testing.T) {
	manager := newManager(t)
	record := func(generation, digest string) {
		t.Helper()
		if err := manager.RecordPublished(context.Background(), ProfileRecord{
			Profile: "default", Generation: generation,
			Manifest: manifestFor(t, "default", "source", "commit-a", true, digest),
		}); err != nil {
			t.Fatal(err)
		}
	}
	observe := func(digest string) {
		t.Helper()
		if err := manager.Invalidate(context.Background(), "default",
			manifestFor(t, "default", "source", "commit-a", true, digest)); err != nil {
			t.Fatal(err)
		}
	}
	profile := func() ProfileState {
		t.Helper()
		state := manager.Snapshot()
		if len(state.Profiles) != 1 {
			t.Fatalf("profiles = %#v, want one", state.Profiles)
		}
		return state.Profiles[0]
	}

	record("000001", "first")
	// The pass that will publish 000002 captured "second"; the watcher then
	// saw "third", which the pass never read.
	observe("third")
	record("000002", "second")

	published := manifestFor(t, "default", "source", "commit-a", true, "second").Sources[0].Observation
	observed := manifestFor(t, "default", "source", "commit-a", true, "third").Sources[0].Observation
	got := profile()
	if !got.Stale || got.Generation != "000002" || len(got.Changes) != 1 {
		t.Fatalf("state after publishing without the last edit = %#v, want generation 000002 still stale with one change", got)
	}
	change := got.Changes[0]
	if change.Reason != ReasonContentChanged || got.Reason == "" ||
		change.Before == nil || *change.Before != published ||
		change.After == nil || *change.After != observed {
		t.Fatalf("surviving change = %#v (reason %q), want content changed from the published observation to the observed one",
			change, got.Reason)
	}

	// The next pass reads "third" and publishes it: now the mark is accounted for.
	record("000003", "third")
	if got := profile(); got.Stale || len(got.Changes) != 0 || got.Reason != "" {
		t.Fatalf("state after publishing the edit = %#v, want fresh", got)
	}
}

// The ordinary sequence must still end fresh: the watcher sees an edit, the
// pass reads the same edit and publishes it.
func TestRecordPublishedClearsAChangeTheGenerationContains(t *testing.T) {
	manager := newManager(t)
	for _, step := range []struct {
		generation, digest string
	}{{"000001", "first"}, {"000002", "second"}} {
		if step.generation == "000002" {
			if err := manager.Invalidate(context.Background(), "default",
				manifestFor(t, "default", "source", "commit-a", true, "second")); err != nil {
				t.Fatal(err)
			}
			if !manager.Snapshot().Profiles[0].Stale {
				t.Fatal("the edit did not mark the profile stale, so the test proves nothing")
			}
		}
		if err := manager.RecordPublished(context.Background(), ProfileRecord{
			Profile: "default", Generation: step.generation,
			Manifest: manifestFor(t, "default", "source", "commit-a", true, step.digest),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := manager.Snapshot().Profiles[0]; got.Stale || len(got.Changes) != 0 {
		t.Fatalf("state after publishing the observed edit = %#v, want fresh", got)
	}
}
