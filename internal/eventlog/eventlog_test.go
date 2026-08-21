package eventlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWriterAppendsAndReadsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "events.jsonl")
	writer, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	moment := time.Date(2026, 8, 21, 10, 30, 0, 0, time.UTC)
	writer.Append(Event{Time: moment, Kind: KindServe, Message: "MCP server started", PID: 7})
	writer.Append(Event{
		Time:    moment.Add(time.Second),
		Kind:    KindTool,
		Message: "find_references",
		Tool:    "find_references",
		Status:  StatusOK,
		PID:     7,
	}.WithDuration(3 * time.Millisecond).WithResults(12))
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	events, err := Read(path, ReadOptions{})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	want := []Event{
		{Time: moment, Level: LevelInfo, Kind: KindServe, Message: "MCP server started", PID: 7},
		Event{
			Time:    moment.Add(time.Second),
			Level:   LevelInfo,
			Kind:    KindTool,
			Message: "find_references",
			Tool:    "find_references",
			Status:  StatusOK,
			PID:     7,
		}.WithDuration(3 * time.Millisecond).WithResults(12),
	}
	if len(events) != len(want) {
		t.Fatalf("Read() returned %d events, want %d", len(events), len(want))
	}
	for index := range want {
		if !events[index].Time.Equal(want[index].Time) {
			t.Fatalf("event %d time = %s, want %s", index, events[index].Time, want[index].Time)
		}
		events[index].Time = want[index].Time
		if got, expected := mustJSON(t, events[index]), mustJSON(t, want[index]); got != expected {
			t.Fatalf("event %d = %s, want %s", index, got, expected)
		}
	}
}

// A discarding writer is what every producer gets when the store cannot be
// opened, so it has to be usable without a branch at every call site.
func TestNilWriterDiscards(t *testing.T) {
	var writer *Writer
	writer.Append(Event{Kind: KindTool, Message: "find_symbol"})
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() on a nil writer error = %v", err)
	}
	if path := writer.Path(); path != "" {
		t.Fatalf("Path() on a nil writer = %q, want empty", path)
	}
}

func TestAppendFillsTheDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writer, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	writer.Append(Event{Kind: KindIndex, Message: "index --full started"})
	writer.Close()

	events, err := Read(path, ReadOptions{})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Read() returned %d events, want 1", len(events))
	}
	if events[0].Level != LevelInfo {
		t.Fatalf("level = %q, want %q", events[0].Level, LevelInfo)
	}
	if events[0].PID != os.Getpid() {
		t.Fatalf("pid = %d, want %d", events[0].PID, os.Getpid())
	}
	if events[0].Time.IsZero() {
		t.Fatal("an appended event carried no timestamp")
	}
}

func TestReadFiltersAndBoundsTheWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writer, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	base := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	writer.Append(Event{Time: base, Kind: KindIndex, Message: "old pass"})
	writer.Append(Event{Time: base.Add(time.Hour), Kind: KindTool, Message: "find_symbol", Tool: "find_symbol", Status: StatusOK})
	writer.Append(Event{
		Time: base.Add(2 * time.Hour), Kind: KindTool, Message: "get_source",
		Tool: "get_source", Status: StatusError, Level: LevelError, Error: "SYMBOL_NOT_FOUND: no",
	})
	writer.Close()

	cases := []struct {
		name    string
		options ReadOptions
		want    []string
	}{
		{"everything", ReadOptions{}, []string{"old pass", "find_symbol", "get_source"}},
		{"by kind", ReadOptions{Kinds: []Kind{KindTool}}, []string{"find_symbol", "get_source"}},
		{"by tool", ReadOptions{Tool: "get_source"}, []string{"get_source"}},
		{"failures only", ReadOptions{FailuresOnly: true}, []string{"get_source"}},
		{"since", ReadOptions{Since: base.Add(90 * time.Minute)}, []string{"get_source"}},
		{"newest only", ReadOptions{Limit: 1}, []string{"get_source"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			events, err := Read(path, testCase.options)
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			got := make([]string, 0, len(events))
			for _, event := range events {
				got = append(got, event.Message)
			}
			if strings.Join(got, ",") != strings.Join(testCase.want, ",") {
				t.Fatalf("Read() = %v, want %v", got, testCase.want)
			}
		})
	}
}

// A store that has never been written is a machine that has not run anything,
// not a failure: `kivgraph logs` on a fresh install must not report an error.
func TestReadMissingStoreIsEmpty(t *testing.T) {
	events, err := Read(filepath.Join(t.TempDir(), "absent.jsonl"), ReadOptions{})
	if err != nil {
		t.Fatalf("Read() on a missing store error = %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("Read() returned %d events, want none", len(events))
	}
}

// Several processes write this file, and one of them may be a version that
// knows a field this one does not. Refusing to show the rest would turn a
// harmless unknown record into an outage of the only view a reader has.
func TestReadSkipsUnparseableLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	contents := strings.Join([]string{
		`{"ts":"2026-08-21T09:00:00Z","level":"info","kind":"tool","msg":"find_symbol","tool":"find_symbol"}`,
		`this is not JSON at all`,
		`{"ts":"2026-08-21T09:00:01Z","level":"info","kind":"tool","msg":"get_symbol","tool":"get_symbol","future_field":42}`,
		``,
	}, "\n")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	events, err := Read(path, ReadOptions{})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Read() returned %d events, want the 2 parseable ones", len(events))
	}
	if events[0].Tool != "find_symbol" || events[1].Tool != "get_symbol" {
		t.Fatalf("Read() = %v, want find_symbol then get_symbol", events)
	}
}

// Rotation must not lose history a single retained file can still hold: the
// reader has to see the rotated file and the live one as one chronological
// store. Only one rotation is kept, so the threshold here is sized from a real
// record -- a store that outgrows twice the threshold does drop its oldest
// records, and that is the documented cost of keeping exactly one.
func TestRotationKeepsTheHistoryReadable(t *testing.T) {
	base := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	const total = 12
	sample := Event{
		Time:    base,
		Level:   LevelInfo,
		Kind:    KindTool,
		Message: "find_references",
		Tool:    "find_references",
		Status:  StatusOK,
		PID:     os.Getpid(),
	}
	recordSize := int64(len(mustJSON(t, sample)) + 1)

	path := filepath.Join(t.TempDir(), "events.jsonl")
	writer, err := OpenWithLimit(path, recordSize*(total-4))
	if err != nil {
		t.Fatalf("OpenWithLimit() error = %v", err)
	}
	for index := range total {
		writer.Append(Event{
			Time:    base.Add(time.Duration(index) * time.Second),
			Kind:    KindTool,
			Message: "find_references",
			Tool:    "find_references",
			Status:  StatusOK,
		})
	}
	writer.Close()

	if _, err := os.Stat(path + rotatedSuffix); err != nil {
		t.Fatalf("the store never rotated: %v", err)
	}
	events, err := Read(path, ReadOptions{})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(events) != total {
		t.Fatalf("Read() returned %d events across the rotation, want %d", len(events), total)
	}
	for index := 1; index < len(events); index++ {
		if events[index].Time.Before(events[index-1].Time) {
			t.Fatalf("event %d is older than its predecessor: %s before %s",
				index, events[index].Time, events[index-1].Time)
		}
	}
}

// The recorder runs inside a tool call, and a server answers several at once.
func TestConcurrentAppendsKeepEveryLineWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writer, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	const writers, each = 8, 40
	group := sync.WaitGroup{}
	group.Add(writers)
	for range writers {
		go func() {
			defer group.Done()
			for range each {
				writer.Append(Event{Kind: KindTool, Message: "find_symbol", Tool: "find_symbol", Status: StatusOK})
			}
		}()
	}
	group.Wait()
	writer.Close()

	events, err := Read(path, ReadOptions{})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(events) != writers*each {
		t.Fatalf("Read() returned %d events, want %d; a line was interleaved or lost",
			len(events), writers*each)
	}
}

func TestOpenRejectsAnEmptyPath(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("Open(\"\") was accepted")
	}
	if _, err := OpenWithLimit(filepath.Join(t.TempDir(), "events.jsonl"), 0); err == nil {
		t.Fatal("OpenWithLimit() accepted a non-positive threshold")
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return string(encoded)
}
