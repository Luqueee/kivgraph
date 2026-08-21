package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/eventlog"
	"github.com/Luqueee/kivgraph/internal/procstat"
)

// noProcesses is the lister for a machine running nothing of ours.
func noProcesses() ([]procstat.Process, error) { return nil, nil }

// initConfig installs an isolated configuration. A configuration outside the
// default location relocates every state path beside it, which is what keeps a
// test from writing into the real installation's event store.
func initConfig(t *testing.T, configPath string) {
	t.Helper()
	if _, err := config.Initialize(config.InitOptions{
		ConfigPath:       configPath,
		RepositoriesPath: filepath.Join(filepath.Dir(configPath), "repositories.yaml"),
	}); err != nil {
		t.Fatalf("config.Initialize() error = %v", err)
	}
	loaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("config.LoadConfig() error = %v", err)
	}
	if want := filepath.Join(filepath.Dir(configPath), "state", "events.jsonl"); loaded.Logging.EventLogPath != want {
		t.Fatalf("the isolated configuration points at %q, want %q",
			loaded.Logging.EventLogPath, want)
	}
}

func logEvent(offset time.Duration, kind eventlog.Kind, message string) eventlog.Event {
	base := time.Date(2026, 8, 21, 13, 10, 57, 0, time.UTC)
	return eventlog.Event{Time: base.Add(offset), Level: eventlog.LevelInfo, Kind: kind, Message: message, PID: 4242}
}

// writeEventStore installs a configuration whose state, including the event
// store, lives under the test directory, and fills that store with events.
func writeEventStore(t *testing.T, events ...eventlog.Event) string {
	t.Helper()
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.yaml")
	initConfig(t, configPath)
	storePath := filepath.Join(directory, "state", "events.jsonl")
	writer, err := eventlog.Open(storePath)
	if err != nil {
		t.Fatalf("eventlog.Open() error = %v", err)
	}
	for _, event := range events {
		writer.Append(event)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return configPath
}

func TestLogsRendersTheRecordedHistory(t *testing.T) {
	configPath := writeEventStore(t,
		logEvent(0, eventlog.KindServe, "MCP server started"),
		eventlog.Event{
			Time: logEvent(time.Second, eventlog.KindTool, "").Time, Level: eventlog.LevelInfo,
			Kind: eventlog.KindTool, Message: "find_references", Tool: "find_references",
			Status: eventlog.StatusOK, PID: 4242,
		}.WithDuration(8*time.Millisecond).WithResults(66),
	)
	var stdout, stderr bytes.Buffer
	if code := runLogs([]string{"--config", configPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("runLogs() = %d, stderr=%q", code, stderr.String())
	}
	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("runLogs() printed %d lines, want 2:\n%s", len(lines), stdout.String())
	}
	if !strings.Contains(lines[0], "13:10:57") || !strings.Contains(lines[0], "INFO") {
		t.Fatalf("the serve line lost its time or its badge: %q", lines[0])
	}
	// A tool line carries the badge of its kind, the elapsed time and the
	// row count -- the three things the command exists to answer.
	for _, want := range []string{"TOOL", "find_references", "took=8ms", "results=66", "pid=4242"} {
		if !strings.Contains(lines[1], want) {
			t.Fatalf("the tool line lost %q: %q", want, lines[1])
		}
	}
	// The message of a tool event is the tool name, so a tool= field would
	// spend a column repeating it.
	if strings.Contains(lines[1], "tool=") {
		t.Fatalf("the tool line repeated the tool name as a field: %q", lines[1])
	}
}

// A redirected or piped view stays plain text; this is the same invariant the
// two Bubble Tea views are held to.
func TestLogsRendersWithoutColourWhenRedirected(t *testing.T) {
	configPath := writeEventStore(t, logEvent(0, eventlog.KindServe, "MCP server started"))
	var stdout, stderr bytes.Buffer
	if code := runLogs([]string{"--config", configPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("runLogs() = %d, stderr=%q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "\x1b[") {
		t.Fatalf("a redirected view carried ANSI escapes: %q", stdout.String())
	}
}

// The badge is a solid-background column, which is new: the rest of the
// non-TUI surface has foreground colours only.
func TestLogBadgeIsAColouredFixedWidthColumn(t *testing.T) {
	plain := logStyles{}
	coloured := logStyles{color: true}
	for _, badge := range []logBadge{logBadgeInfo, logBadgeWarn, logBadgeError, logBadgeTool, logBadgeIndex} {
		rendered := plain.badge(badge)
		if len(rendered) != logBadgeWidth {
			t.Fatalf("badge %q rendered %d columns, want %d: %q",
				badge.text, len(rendered), logBadgeWidth, rendered)
		}
		painted := coloured.badge(badge)
		if !strings.Contains(painted, "48;5;"+badge.background) {
			t.Fatalf("badge %q lost its background: %q", badge.text, painted)
		}
	}
}

func TestLogBadgeNamesWhatHappened(t *testing.T) {
	cases := []struct {
		name  string
		event eventlog.Event
		want  string
	}{
		{"a call that answered", eventlog.Event{Kind: eventlog.KindTool, Status: eventlog.StatusOK}, "TOOL"},
		{"a pass", eventlog.Event{Kind: eventlog.KindIndex}, "INDEX"},
		{"a lifecycle line", eventlog.Event{Kind: eventlog.KindServe}, "INFO"},
		// A failing call is an error first and a call second: the reader is
		// scanning for the failure, not for the subsystem.
		{"a call that failed", eventlog.Event{Kind: eventlog.KindTool, Status: eventlog.StatusError}, "ERROR"},
		{"a degraded pass", eventlog.Event{Kind: eventlog.KindIndex, Level: eventlog.LevelWarn}, "WARN"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := logBadgeFor(testCase.event).text; got != testCase.want {
				t.Fatalf("logBadgeFor() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// Twelve identical calls are one row carrying their mean, not twelve rows to
// scroll past. The duration is deliberately outside the identity, or nothing
// with a duration would ever collapse.
func TestCollapseFoldsARunAndAveragesIt(t *testing.T) {
	base := time.Date(2026, 8, 21, 18, 5, 42, 0, time.UTC)
	call := func(offset time.Duration, elapsed time.Duration) eventlog.Event {
		return eventlog.Event{
			Time: base.Add(offset), Kind: eventlog.KindTool, Message: "find_symbol",
			Tool: "find_symbol", Status: eventlog.StatusOK,
		}.WithDuration(elapsed)
	}
	lines := collapseLogEvents([]eventlog.Event{
		call(0, 10*time.Millisecond),
		call(time.Second, 20*time.Millisecond),
		call(2*time.Second, 30*time.Millisecond),
		{Time: base.Add(3 * time.Second), Kind: eventlog.KindServe, Message: "MCP server stopped"},
	})
	if len(lines) != 2 {
		t.Fatalf("collapseLogEvents() produced %d rows, want 2", len(lines))
	}
	if lines[0].repeat != 3 {
		t.Fatalf("the run collapsed to repeat=%d, want 3", lines[0].repeat)
	}
	if lines[0].mean != 20*time.Millisecond {
		t.Fatalf("mean = %s, want 20ms", lines[0].mean)
	}
	// The row reports the newest occurrence: the question a repeated line
	// raises is whether it is still happening.
	if !lines[0].event.Time.Equal(base.Add(2 * time.Second)) {
		t.Fatalf("the collapsed row is stamped %s, want the newest occurrence", lines[0].event.Time)
	}
	rendered := renderLogLine(logStyles{}, lines[0])
	if !strings.Contains(rendered, "(×3)") || !strings.Contains(rendered, "mean=20ms") {
		t.Fatalf("the collapsed row hid its count or its mean: %q", rendered)
	}
}

// A run of records that differ in something a reader can see must not collapse.
func TestCollapseKeepsDistinguishableRecordsApart(t *testing.T) {
	base := time.Date(2026, 8, 21, 18, 5, 42, 0, time.UTC)
	lines := collapseLogEvents([]eventlog.Event{
		{Time: base, Kind: eventlog.KindTool, Message: "find_symbol", Tool: "find_symbol", Status: eventlog.StatusOK},
		{Time: base.Add(time.Second), Kind: eventlog.KindTool, Message: "find_symbol", Tool: "find_symbol",
			Status: eventlog.StatusError, Error: "AMBIGUOUS_SYMBOL: two"},
		{Time: base.Add(2 * time.Second), Kind: eventlog.KindTool, Message: "find_symbol", Tool: "find_symbol", Status: eventlog.StatusOK},
	})
	if len(lines) != 3 {
		t.Fatalf("collapseLogEvents() produced %d rows, want 3: a failure must not fold into a success", len(lines))
	}
}

func TestLogsFiltersAndEmitsJSON(t *testing.T) {
	configPath := writeEventStore(t,
		logEvent(0, eventlog.KindServe, "MCP server started"),
		logEvent(time.Second, eventlog.KindIndex, "index --full finished"),
		logEvent(2*time.Second, eventlog.KindTool, "find_symbol"),
	)
	var stdout, stderr bytes.Buffer
	if code := runLogs([]string{"--config", configPath, "--kind", "index", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runLogs() = %d, stderr=%q", code, stderr.String())
	}
	var events []eventlog.Event
	if err := json.Unmarshal(stdout.Bytes(), &events); err != nil {
		t.Fatalf("the JSON view did not parse: %v\n%s", err, stdout.String())
	}
	if len(events) != 1 || events[0].Message != "index --full finished" {
		t.Fatalf("--kind index returned %v, want only the pass", events)
	}
}

func TestLogsRejectsWhatItCannotAnswer(t *testing.T) {
	configPath := writeEventStore(t)
	cases := [][]string{
		{"--config", configPath, "--kind", "nonsense"},
		{"--config", configPath, "--limit", "-1"},
		{"--config", configPath, "--since", "-1h"},
		// A follower can never close a JSON array, so the two flags
		// describe different documents.
		{"--config", configPath, "--json", "--follow"},
		{"--config", configPath, "unexpected"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if code := runLogs(args, &stdout, &stderr); code != 2 {
			t.Fatalf("runLogs(%v) = %d, want 2 (stderr=%q)", args, code, stderr.String())
		}
	}
}

// A fresh install has no store, and that is not a failure.
func TestLogsOnAnEmptyStoreSucceeds(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.yaml")
	initConfig(t, configPath)
	if _, err := os.Stat(filepath.Join(directory, "state", "events.jsonl")); err == nil {
		t.Fatal("the fixture wrote a store this test needs to be missing")
	}
	var stdout, stderr bytes.Buffer
	if code := runLogs([]string{"--config", configPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("runLogs() = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "nothing recorded yet") {
		t.Fatalf("runLogs() said nothing about the empty store: %q", stdout.String())
	}
}

func TestFormatLogDurationKeepsItsScale(t *testing.T) {
	cases := map[time.Duration]string{
		0:                       "0ms",
		500 * time.Microsecond:  "500µs",
		8 * time.Millisecond:    "8ms",
		1500 * time.Millisecond: "1.50s",
		95 * time.Second:        "1m35s",
	}
	for elapsed, want := range cases {
		if got := formatLogDuration(elapsed); got != want {
			t.Fatalf("formatLogDuration(%s) = %q, want %q", elapsed, got, want)
		}
	}
}
