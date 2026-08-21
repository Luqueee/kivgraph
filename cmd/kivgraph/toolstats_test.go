package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/eventlog"
)

func toolCallEvent(offset time.Duration, tool string, elapsed time.Duration, failure string) eventlog.Event {
	base := time.Date(2026, 8, 21, 13, 10, 57, 0, time.UTC)
	event := eventlog.Event{
		Time:    base.Add(offset),
		Level:   eventlog.LevelInfo,
		Kind:    eventlog.KindTool,
		Message: tool,
		Tool:    tool,
		Status:  eventlog.StatusOK,
	}
	if failure != "" {
		event.Level = eventlog.LevelError
		event.Status = eventlog.StatusError
		event.Error = failure
	}
	return event.WithDuration(elapsed)
}

func TestToolStatsReportsCostAndFailures(t *testing.T) {
	configPath := writeEventStore(t,
		toolCallEvent(0, "find_references", 10*time.Millisecond, ""),
		toolCallEvent(time.Second, "find_references", 30*time.Millisecond, ""),
		toolCallEvent(2*time.Second, "find_references", 20*time.Millisecond, "CURSOR_INVALID: stale"),
		toolCallEvent(3*time.Second, "find_symbol", 5*time.Millisecond, ""),
		// A pass is not a call and must not be counted as one.
		logEvent(4*time.Second, eventlog.KindIndex, "index --full finished"),
	)
	var stdout, stderr bytes.Buffer
	if code := runToolStats([]string{"--config", configPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("runToolStats() = %d, stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"TOOL", "CALLS", "OK", "FAIL", "MEAN", "P95", "MAX",
		"find_references", "find_symbol",
		"calls=4 ok=3 failed=1",
		// The last failure is what turns a count into something actionable.
		"tool-stats.failure: find_references: CURSOR_INVALID: stale",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("runToolStats() output lost %q:\n%s", want, output)
		}
	}
	// The busiest tool leads, because that is the row a reader came for.
	if strings.Index(output, "find_references") > strings.Index(output, "find_symbol") {
		t.Fatalf("find_symbol was reported before the busier find_references:\n%s", output)
	}
}

func TestToolStatsJSONCarriesTheDerivedLatencies(t *testing.T) {
	configPath := writeEventStore(t,
		toolCallEvent(0, "get_source", 4*time.Millisecond, ""),
		toolCallEvent(time.Second, "get_source", 6*time.Millisecond, ""),
	)
	var stdout, stderr bytes.Buffer
	if code := runToolStats([]string{"--config", configPath, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runToolStats() = %d, stderr=%q", code, stderr.String())
	}
	var summary eventlog.Summary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("the JSON summary did not parse: %v\n%s", err, stdout.String())
	}
	if len(summary.Tools) != 1 {
		t.Fatalf("summary reported %d tools, want 1", len(summary.Tools))
	}
	entry := summary.Tools[0]
	if entry.Calls != 2 || entry.OK != 2 || entry.Failed != 0 {
		t.Fatalf("summary = %+v, want two answered calls", entry)
	}
	if entry.Mean != 5*time.Millisecond {
		t.Fatalf("mean = %s, want 5ms", entry.Mean)
	}
	if entry.Max != 6*time.Millisecond {
		t.Fatalf("max = %s, want 6ms", entry.Max)
	}
}

func TestToolStatsNarrowsByToolAndWindow(t *testing.T) {
	configPath := writeEventStore(t,
		toolCallEvent(0, "find_symbol", time.Millisecond, ""),
		toolCallEvent(time.Second, "get_symbol", time.Millisecond, ""),
	)
	var stdout, stderr bytes.Buffer
	if code := runToolStats([]string{"--config", configPath, "--tool", "get_symbol"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runToolStats() = %d, stderr=%q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "find_symbol") {
		t.Fatalf("--tool get_symbol reported another tool:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "get_symbol") {
		t.Fatalf("--tool get_symbol reported nothing:\n%s", stdout.String())
	}
}

// A store whose records are all older than the window is an honest empty
// answer, not an error.
func TestToolStatsOnAnEmptyWindowSucceeds(t *testing.T) {
	stale := eventlog.Event{
		Time:    time.Now().Add(-24 * time.Hour),
		Level:   eventlog.LevelInfo,
		Kind:    eventlog.KindTool,
		Message: "find_symbol",
		Tool:    "find_symbol",
		Status:  eventlog.StatusOK,
	}.WithDuration(time.Millisecond)
	configPath := writeEventStore(t, stale)
	var stdout, stderr bytes.Buffer
	if code := runToolStats([]string{"--config", configPath, "--since", "1m"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runToolStats() = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no tool call recorded yet") {
		t.Fatalf("runToolStats() said nothing about the empty window: %q", stdout.String())
	}
}

func TestToolStatsRejectsWhatItCannotAnswer(t *testing.T) {
	configPath := writeEventStore(t)
	for _, args := range [][]string{
		{"--config", configPath, "--since", "-1h"},
		{"--config", configPath, "unexpected"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runToolStats(args, &stdout, &stderr); code != 2 {
			t.Fatalf("runToolStats(%v) = %d, want 2 (stderr=%q)", args, code, stderr.String())
		}
	}
}

// A configuration that cannot be read is a failure, not an empty report: a
// silent zero would read as "nothing ever ran".
func TestToolStatsFailsOnAnUnreadableConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{"--config", filepath.Join(t.TempDir(), "absent.yaml")}
	if code := runToolStats(args, &stdout, &stderr); code != 1 {
		t.Fatalf("runToolStats() = %d, want 1 (stderr=%q)", code, stderr.String())
	}
}
