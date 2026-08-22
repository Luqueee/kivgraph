package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/eventlog"
	"github.com/Luqueee/kivgraph/internal/mcp/tools"
	"github.com/Luqueee/kivgraph/internal/metrics"
	"github.com/Luqueee/kivgraph/internal/rebuild"
)

// This is the whole point of the recorder: an observation made inside a server
// must still be readable by a command that runs later, in another process.
func TestToolMetricsRegistryRecordsThroughToTheStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writer, err := eventlog.Open(path)
	if err != nil {
		t.Fatalf("eventlog.Open() error = %v", err)
	}
	registry := toolMetricsRegistry(writer)
	registry.ObserveQuery(metrics.QueryObservation{
		ToolName: "find_references",
		Elapsed:  8 * time.Millisecond,
		Returned: 66,
	})
	registry.ObserveQuery(metrics.QueryObservation{
		ToolName: "get_source",
		Elapsed:  2 * time.Millisecond,
		Err:      errors.New("SYMBOL_NOT_FOUND: no such symbol"),
	})
	writer.Close()

	events, err := eventlog.Read(path, eventlog.ReadOptions{})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("the store holds %d events, want 2", len(events))
	}
	answered := events[0]
	if answered.Tool != "find_references" || answered.Status != eventlog.StatusOK {
		t.Fatalf("the answered call = %+v, want an ok find_references", answered)
	}
	if elapsed, timed := answered.Duration(); !timed || elapsed != 8*time.Millisecond {
		t.Fatalf("the answered call recorded %s (timed=%v), want 8ms", elapsed, timed)
	}
	if answered.Results == nil || *answered.Results != 66 {
		t.Fatalf("the answered call lost its row count: %+v", answered)
	}
	failed := events[1]
	if !failed.Failed() || failed.Level != eventlog.LevelError {
		t.Fatalf("the failed call = %+v, want a failure", failed)
	}
	// The rendered error leads with the stable tool code, so the
	// classification survives without a field of its own.
	if !strings.HasPrefix(failed.Error, "SYMBOL_NOT_FOUND: ") {
		t.Fatalf("the failure lost its code: %q", failed.Error)
	}
	// The in-memory counters graph_status reads back must keep working.
	report := registry.Report()
	if got := report.Queries["find_references"].Calls; got != 1 {
		t.Fatalf("the registry counted %d calls to find_references, want 1", got)
	}
	if got := report.Queries["get_source"].Errors; got != 1 {
		t.Fatalf("the registry counted %d errors on get_source, want 1", got)
	}
}

// A classified failure keeps its cause out of the client's answer on purpose,
// so the durable store is the only place it can land. This is the case that was
// invisible: graph_status refused every corpus indexed after ADR 0060 and
// nothing recorded which of its helpers had failed.
func TestToolMetricsRegistryRecordsTheWrappedCause(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writer, err := eventlog.Open(path)
	if err != nil {
		t.Fatalf("eventlog.Open() error = %v", err)
	}
	registry := toolMetricsRegistry(writer)
	cause := errors.New("symbol edge from 41 has an unknown kind 12")
	registry.ObserveQuery(metrics.QueryObservation{
		ToolName: "graph_status",
		Elapsed:  time.Millisecond,
		Err: tools.WrapToolError(
			tools.CodeSnapshotUnavailable,
			"active snapshot contains invalid status metadata",
			cause,
		),
	})
	// A failure with nothing wrapped must read exactly as before.
	registry.ObserveQuery(metrics.QueryObservation{
		ToolName: "find_symbol",
		Elapsed:  time.Millisecond,
		Err:      tools.NewToolError(tools.CodeInvalidArgument, "mode \"fuzzy\" is unsupported"),
	})
	writer.Close()

	events, err := eventlog.Read(path, eventlog.ReadOptions{})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("the store holds %d events, want 2", len(events))
	}
	wrapped := events[0]
	if !strings.HasPrefix(wrapped.Error, tools.CodeSnapshotUnavailable+": ") {
		t.Fatalf("the failure lost its code: %q", wrapped.Error)
	}
	if !strings.Contains(wrapped.Error, cause.Error()) {
		t.Fatalf("the failure lost the cause it wrapped: %q", wrapped.Error)
	}
	bare := events[1]
	if bare.Error != tools.CodeInvalidArgument+": mode \"fuzzy\" is unsupported" {
		t.Fatalf("an unwrapped failure changed shape: %q", bare.Error)
	}
}

// A server that could not open the store still has to serve.
func TestToolMetricsRegistryWorksWithoutAStore(t *testing.T) {
	registry := toolMetricsRegistry(nil)
	registry.ObserveQuery(metrics.QueryObservation{ToolName: "find_symbol", Elapsed: time.Millisecond})
	if got := registry.Report().Queries["find_symbol"].Calls; got != 1 {
		t.Fatalf("the registry counted %d calls, want 1", got)
	}
}

// A store that cannot be opened is a warning, never a failed command.
func TestOpenEventLogDegradesWithAWarning(t *testing.T) {
	directory := t.TempDir()
	configuration := config.Config{}
	// A directory where the file should be is the shape of a store that
	// cannot be opened.
	configuration.Logging.EventLogPath = directory
	var stderr bytes.Buffer
	if writer := openEventLog(configuration, &stderr); writer != nil {
		t.Fatalf("openEventLog() returned a writer for %q", directory)
	}
	if !strings.Contains(stderr.String(), "will not appear in") {
		t.Fatalf("openEventLog() hid the limitation: %q", stderr.String())
	}
}

func TestRecordIndexRunWritesStagesAndTheOutcome(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writer, err := eventlog.Open(path)
	if err != nil {
		t.Fatalf("eventlog.Open() error = %v", err)
	}
	report := rebuild.Report{
		GenerationID: "000007",
		Passed:       true,
		Stages: []rebuild.Stage{
			{Name: "facts", Passed: true, DurationMS: 1200},
			{Name: "publish", Passed: true, DurationMS: 40},
		},
	}
	recordIndexRun(writer, report, 98765, 3*time.Minute, nil)
	writer.Close()

	events, err := eventlog.Read(path, eventlog.ReadOptions{})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("the store holds %d events, want two stages and one outcome", len(events))
	}
	if events[0].Stage != "facts" {
		t.Fatalf("the first stage = %q, want facts", events[0].Stage)
	}
	if elapsed, timed := events[0].Duration(); !timed || elapsed != 1200*time.Millisecond {
		t.Fatalf("the facts stage recorded %s (timed=%v), want 1.2s", elapsed, timed)
	}
	outcome := events[2]
	if outcome.Generation != "000007" || outcome.Status != eventlog.StatusOK {
		t.Fatalf("the outcome = %+v, want a passing 000007", outcome)
	}
	if outcome.Symbols == nil || *outcome.Symbols != 98765 {
		t.Fatalf("the outcome lost its symbol count: %+v", outcome)
	}
	if elapsed, timed := outcome.Duration(); !timed || elapsed != 3*time.Minute {
		t.Fatalf("the outcome recorded %s (timed=%v), want 3m", elapsed, timed)
	}
}

// A failing stage must read as a failure, and a pass that did not pass must not
// read as one.
func TestRecordIndexRunMarksWhatDidNotPass(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writer, err := eventlog.Open(path)
	if err != nil {
		t.Fatalf("eventlog.Open() error = %v", err)
	}
	report := rebuild.Report{
		GenerationID: "000008",
		Passed:       false,
		Stages: []rebuild.Stage{
			{Name: "integrity", Passed: false, Detail: "3 dangling edges", DurationMS: 10},
		},
	}
	recordIndexRun(writer, report, 0, time.Second, nil)
	writer.Close()

	events, err := eventlog.Read(path, eventlog.ReadOptions{})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("the store holds %d events, want the stage and the outcome", len(events))
	}
	if !events[0].Failed() || events[0].Error != "3 dangling edges" {
		t.Fatalf("the failing stage = %+v, want a failure carrying its detail", events[0])
	}
	if events[1].Level != eventlog.LevelWarn || !events[1].Failed() {
		t.Fatalf("the outcome = %+v, want a warning that did not pass", events[1])
	}
}

// recordIndexRun is called on every pass, including when the store is absent.
func TestRecordIndexRunWithoutAStoreIsSafe(t *testing.T) {
	recordIndexRun(nil, rebuild.Report{Stages: []rebuild.Stage{{Name: "facts"}}}, 1, time.Second, errors.New("boom"))
}
