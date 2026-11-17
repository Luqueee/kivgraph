package generation

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// TestPublishNamesEveryStepAndTheirSumStaysInsideTheCall pins what a reader of
// the `publish` stage can rely on: the steps follow one another in the order
// Publish runs them, the two closures the caller supplies are charged to their own
// steps, and nothing is counted twice.
func TestPublishNamesEveryStepAndTheirSumStaysInsideTheCall(t *testing.T) {
	store := newTestStore(t)
	const buildPause, validatePause = 20 * time.Millisecond, 30 * time.Millisecond

	started := time.Now()
	publication, err := store.Publish(context.Background(), PublishRequest{
		ID: "000001",
		Build: func(_ context.Context, path string) error {
			time.Sleep(buildPause)
			return writeTestGeneration(path, "stable")
		},
		Validate: func(ctx context.Context, generation Generation) error {
			time.Sleep(validatePause)
			return validateTestGeneration("stable")(ctx, generation)
		},
	})
	call := time.Since(started)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	var names []string
	total := 0.0
	durations := map[string]float64{}
	for _, step := range publication.Steps {
		names = append(names, step.Name)
		durations[step.Name] = step.DurationMS
		if step.DurationMS < 0 {
			t.Errorf("step %q took %v ms, want a duration that is not negative", step.Name, step.DurationMS)
		}
		total += step.DurationMS
	}
	want := []string{"setup", "build", "sync candidate", "validate", "sync validated", "commit"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("steps = %q, want %q", names, want)
	}
	if got, least := durations["build"], float64(buildPause/time.Millisecond); got < least {
		t.Errorf("build step = %v ms, want at least the %v ms its closure slept", got, least)
	}
	if got, least := durations["validate"], float64(validatePause/time.Millisecond); got < least {
		t.Errorf("validate step = %v ms, want at least the %v ms its closure slept", got, least)
	}
	if limit := float64(call) / float64(time.Millisecond); total > limit {
		t.Errorf("steps add up to %v ms, more than the %v ms the call took: something is counted twice", total, limit)
	}
}

// A Publish that fails has no steps to report: a partial list would read as a
// publication that stopped where the list does.
func TestAFailedPublishReportsNoSteps(t *testing.T) {
	store := newTestStore(t)
	validationErr := errors.New("golden probe failed")
	publication, err := store.Publish(context.Background(), PublishRequest{
		ID:       "000001",
		Build:    func(_ context.Context, path string) error { return writeTestGeneration(path, "candidate") },
		Validate: func(context.Context, Generation) error { return validationErr },
	})
	if !errors.Is(err, validationErr) {
		t.Fatalf("Publish() error = %v, want the validation failure", err)
	}
	if len(publication.Steps) != 0 {
		t.Fatalf("Steps = %+v, want none from a failed Publish", publication.Steps)
	}
}
