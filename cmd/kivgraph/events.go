package main

import (
	"errors"
	"io"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/eventlog"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/mcp/tools"
	"github.com/Luqueee/kivgraph/internal/metrics"
	"github.com/Luqueee/kivgraph/internal/rebuild"
)

// openEventLog opens the durable record for a command that is about to do
// something worth remembering. A store it cannot open is reported once and then
// discarded: history is worth a warning, never a failed index or a server that
// refuses to serve.
func openEventLog(configuration config.Config, stderr io.Writer) *eventlog.Writer {
	writer, err := eventlog.Open(configuration.Logging.EventLogPath)
	if err != nil {
		writeWarning(stderr, "events: %v; this run will not appear in \"kivgraph logs\"", err)
		return nil
	}
	return writer
}

// publishedGenerationID answers the generation a server is about to answer
// from, or the empty string when it has none. A server with no generation is
// the shape that only serves index_project, and a log that omitted the field
// would read the same as one that never looked.
//
// It asks the store for the name rather than for the graph: this runs at
// startup, and loading a snapshot to write its number into a log line would
// undo the whole point of deferring the load. See ADR 0067.
func publishedGenerationID(store *hotsnapshot.SnapshotStore) string {
	return store.GenerationID()
}

// toolMetricsRegistry answers the registry a server observes through, wired so
// that every tool call also lands in the durable store.
//
// The registry itself stays what it was -- process-local atomics that
// graph_status reads back -- because a reader that has to parse a file cannot
// answer a tool call. The recorder is the second copy, and it is the only one
// that survives the process.
func toolMetricsRegistry(events *eventlog.Writer) *metrics.Registry {
	if events == nil {
		return metrics.NewRegistry()
	}
	return metrics.NewRegistryWithRecorder(func(observation metrics.QueryObservation) {
		event := eventlog.Event{
			Kind:    eventlog.KindTool,
			Message: observation.ToolName,
			Tool:    observation.ToolName,
			Query:   observation.Query,
			Status:  eventlog.StatusOK,
		}
		event = event.WithDuration(observation.Elapsed)
		if observation.Returned > 0 {
			event = event.WithResults(observation.Returned)
		}
		if observation.Err != nil {
			if tools.IsExpectedAbsence(observation.Err) {
				// A lookup that found nothing is a complete answer. The MCP
				// surface retains SYMBOL_NOT_FOUND for callers that branch on
				// codes, while the operator log calls the outcome what it is.
				event.Status = eventlog.StatusNotFound
				event = event.WithResults(0)
			} else {
				// The rendered error already leads with the stable tool code,
				// so the classification survives without a field of its own.
				event.Level = eventlog.LevelError
				event.Status = eventlog.StatusError
				event.Error = errorWithCause(observation.Err)
			}
		}
		events.Append(event)
	})
}

// errorWithCause renders a tool failure with the cause it wrapped.
//
// Forty-odd call sites build their failures with WrapToolError, whose doc says
// the cause is "retained for server-side diagnostics" -- and nothing was
// writing it anywhere. ToolError.Error() prints only `CODE: message`, on
// purpose, because that string is what reaches the client. This is the other
// side, the one that never leaves the machine, and it is the only place the
// cause can land.
//
// It was not academic: graph_status answered SNAPSHOT_UNAVAILABLE on every
// corpus indexed after ADR 0060, and neither the response, the log nor stderr
// said which of its four helpers had failed.
func errorWithCause(err error) string {
	rendered := err.Error()
	cause := errors.Unwrap(err)
	if cause == nil {
		return rendered
	}
	detail := cause.Error()
	if detail == "" || strings.Contains(rendered, detail) {
		return rendered
	}
	return rendered + ": " + detail
}

// recordIndexRun writes what one indexing pass did. Stages come from the report
// rather than from the progress callback because only the report knows how long
// each one took -- progress fires when a stage starts, and a duration is the
// question a reader actually has.
func recordIndexRun(
	events *eventlog.Writer,
	report rebuild.Report,
	symbols int64,
	elapsed time.Duration,
	failure error,
) {
	if events == nil {
		return
	}
	for _, stage := range report.Stages {
		milliseconds := stage.DurationMS
		event := eventlog.Event{
			Kind:       eventlog.KindIndex,
			Message:    "stage " + string(stage.Name),
			Stage:      string(stage.Name),
			Status:     eventlog.StatusOK,
			DurationMS: &milliseconds,
		}
		if !stage.Passed {
			event.Level = eventlog.LevelError
			event.Status = eventlog.StatusError
			event.Error = stage.Detail
		}
		events.Append(event)
	}

	done := eventlog.Event{
		Kind:       eventlog.KindIndex,
		Message:    "index --full finished",
		Generation: report.GenerationID,
		Status:     eventlog.StatusOK,
	}
	done = done.WithDuration(elapsed).WithSymbols(symbols)
	switch {
	case failure != nil:
		done.Level = eventlog.LevelError
		done.Status = eventlog.StatusError
		done.Message = "index --full failed"
		done.Error = failure.Error()
	case !report.Passed:
		done.Level = eventlog.LevelWarn
		done.Status = eventlog.StatusError
		done.Message = "index --full did not pass every stage"
		done.Error = rebuildFailureReason(report)
	}
	events.Append(done)
}
