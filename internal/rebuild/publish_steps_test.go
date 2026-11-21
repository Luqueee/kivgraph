package rebuild

import (
	"context"
	"reflect"
	"testing"

	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// TestPublishStepsCarvesVerificationOutOfValidateAndKeepsTheSum is the property the
// breakdown stands on: the verification of the sources runs inside the validate
// closure, so it is taken out of that step and listed on its own, and the list
// still adds up to what it was plus the pruning.
func TestPublishStepsCarvesVerificationOutOfValidateAndKeepsTheSum(t *testing.T) {
	stored := []generation.PublishStep{
		{Name: "setup", DurationMS: 10},
		{Name: "build", DurationMS: 100},
		{Name: "sync candidate", DurationMS: 5},
		{Name: "validate", DurationMS: 50},
		{Name: "sync validated", DurationMS: 4},
		{Name: "commit", DurationMS: 3},
	}
	got := publishSteps(stored, 20, 7)
	want := []generation.PublishStep{
		{Name: "setup", DurationMS: 10},
		{Name: "build", DurationMS: 100},
		{Name: "sync candidate", DurationMS: 5},
		{Name: "validate", DurationMS: 30},
		{Name: "verify sources", DurationMS: 20},
		{Name: "sync validated", DurationMS: 4},
		{Name: "commit", DurationMS: 3},
		{Name: "prune", DurationMS: 7},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("publishSteps() = %+v, want %+v", got, want)
	}
}

// A publication that has no steps gets only the pruning, and the input is not
// modified: the report keeps the store's own list.
func TestPublishStepsWithNothingStoredStillReportsThePruning(t *testing.T) {
	stored := []generation.PublishStep{{Name: "validate", DurationMS: 9}}
	got := publishSteps(stored, 0, 1)
	want := []generation.PublishStep{
		{Name: "validate", DurationMS: 9}, {Name: "verify sources", DurationMS: 0}, {Name: "prune", DurationMS: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("publishSteps() = %+v, want %+v", got, want)
	}
	if stored[0].DurationMS != 9 || len(stored) != 1 {
		t.Fatalf("the store's list was modified: %+v", stored)
	}
	if only := publishSteps(nil, 0, 2); !reflect.DeepEqual(only, []generation.PublishStep{{Name: "prune", DurationMS: 2}}) {
		t.Fatalf("publishSteps(nil) = %+v, want only the pruning", only)
	}
}

// TestRunReportsPublishStepsInsideThePublishStage runs the real pipeline: every
// step is named, in order, and together they fit inside the stage they split.
func TestRunReportsPublishStepsInsideThePublishStage(t *testing.T) {
	root := t.TempDir()
	report, err := Run(context.Background(), buildOptions(t, root, "000001", sampleFacts()))
	testsupport.RequireSpaceOrSkip(t, err)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var names []string
	total := 0.0
	for _, step := range report.PublishSteps {
		names = append(names, step.Name)
		total += step.DurationMS
	}
	want := []string{"setup", "build", "sync candidate", "validate", "verify sources", "sync validated", "commit", "prune"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("PublishSteps = %q, want %q", names, want)
	}
	publish, ok := stageByName(report.Stages, StagePublish)
	if !ok {
		t.Fatal("no publish stage")
	}
	if total > publish.DurationMS {
		t.Fatalf("steps add up to %v ms, more than the %v ms of the stage they split", total, publish.DurationMS)
	}
}
