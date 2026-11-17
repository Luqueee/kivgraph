package eventlog

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// Two writers on one path stand for two processes: the daemon holds the log
// for its whole life, and index runs and relays open and close it around it.

// rotationEvent is a record whose encoded size does not depend on its label,
// so a limit of n records means exactly n records for every writer.
func rotationEvent(label string, number int) Event {
	return Event{
		Time:    time.Date(2026, 8, 21, 9, 0, number, 0, time.UTC),
		Kind:    KindTool,
		Message: fmt.Sprintf("%.4s-%03d", label+"____", number),
		Tool:    "find_references",
		Status:  StatusOK,
	}
}

// rotationRecordSize is what one such record occupies on disk, measured by
// writing one rather than computed: the writer fills in fields of its own.
func rotationRecordSize(t *testing.T) int64 {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.jsonl")
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	writer.Append(rotationEvent("probe", 0))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func openRotationWriter(t *testing.T, path string, limit int64) *Writer {
	t.Helper()
	writer, err := OpenWithLimit(path, limit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	return writer
}

func messagesIn(t *testing.T, path string) []string {
	t.Helper()
	events, err := Read(path, ReadOptions{})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	messages := make([]string, 0, len(events))
	for _, event := range events {
		messages = append(messages, event.Message)
	}
	return messages
}

// A writer kept its descriptor for life. Once another process had rotated
// twice, that descriptor named a file with no path: every write succeeded and
// went nowhere, so a long-lived daemon stopped appearing in `kivgraph logs`
// until it restarted.
func TestWriterFollowsARotationAnotherProcessMade(t *testing.T) {
	limit := rotationRecordSize(t) * 3
	path := filepath.Join(t.TempDir(), "events.jsonl")
	longLived := openRotationWriter(t, path, limit)
	other := openRotationWriter(t, path, limit)

	// Enough for the other process to rotate twice over.
	for index := range 8 {
		other.Append(rotationEvent("othr", index))
	}
	longLived.Append(rotationEvent("daem", 20))
	longLived.Append(rotationEvent("daem", 21))

	got := messagesIn(t, path)
	for _, want := range []string{"daem-020", "daem-021"} {
		if !slices.Contains(got, want) {
			t.Fatalf("the long-lived writer's %q is not in the store after another writer rotated twice; store holds %v", want, got)
		}
	}
}

// A writer also kept its own idea of the file's size: what it was when opened,
// plus what it wrote itself. After another process rotated, that number
// described a file that was no longer live, and the next append rotated a
// fresh, nearly empty file over the full rotation -- the history that one
// rotation is kept for was gone at once.
func TestWriterDoesNotRotateOnASizeAnotherProcessMadeStale(t *testing.T) {
	limit := rotationRecordSize(t) * 3
	path := filepath.Join(t.TempDir(), "events.jsonl")
	first := openRotationWriter(t, path, limit)
	// Exactly the limit: the next record, whoever writes it, rotates.
	for index := range 3 {
		first.Append(rotationEvent("frst", index))
	}
	second := openRotationWriter(t, path, limit)
	second.Append(rotationEvent("scnd", 10))
	if _, err := os.Stat(path + rotatedSuffix); err != nil {
		t.Fatalf("the second writer did not rotate a full file, so the test proves nothing: %v", err)
	}

	// The live file now holds one record. Nothing here needs rotating.
	first.Append(rotationEvent("frst", 11))

	want := []string{"frst-000", "frst-001", "frst-002", "scnd-010", "frst-011"}
	if got := messagesIn(t, path); !slices.Equal(got, want) {
		t.Fatalf("store holds %v, want %v: the retained rotation was replaced by a nearly empty file", got, want)
	}
}

// The size that decides a rotation is the file's, not one writer's share of
// it. Two writers that opened an empty file and each stayed under the limit by
// their own count wrote past it together, and the store that promises to cost
// at most twice the limit did not.
func TestWritersRotateOnWhatTheyWroteTogether(t *testing.T) {
	limit := rotationRecordSize(t) * 4
	path := filepath.Join(t.TempDir(), "events.jsonl")
	a := openRotationWriter(t, path, limit)
	b := openRotationWriter(t, path, limit)
	for index := range 3 {
		a.Append(rotationEvent("aaaa", index))
	}
	for index := range 3 {
		b.Append(rotationEvent("bbbb", 10+index))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > limit {
		t.Fatalf("the live file is %d bytes, over the %d-byte limit both writers were given", info.Size(), limit)
	}
	// Rotating must not have cost a record.
	want := []string{"aaaa-000", "aaaa-001", "aaaa-002", "bbbb-010", "bbbb-011", "bbbb-012"}
	if got := messagesIn(t, path); !slices.Equal(got, want) {
		t.Fatalf("store holds %v, want %v", got, want)
	}
}
