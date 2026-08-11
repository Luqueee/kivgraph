package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestNewWritesJSONRecord(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output)
	logger.Error("operation failed", "command", "serve", "error", errors.New("boom"))

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("JSON log = %q: %v", output.String(), err)
	}
	if record["level"] != "ERROR" || record["msg"] != "operation failed" {
		t.Fatalf("record = %#v, want ERROR operation failed", record)
	}
	if record["command"] != "serve" || record["error"] != "boom" {
		t.Fatalf("record fields = %#v, want command and error", record)
	}
	if _, ok := record["time"]; !ok {
		t.Fatalf("record = %#v, want timestamp", record)
	}
}

// TestNewCommandWriterRecordsProgressAsInformation keeps the level meaning
// something. Everything a command writes to stderr used to be recorded as an
// error, including every line of progress of a pass that published cleanly.
func TestNewCommandWriterRecordsProgressAsInformation(t *testing.T) {
	var output bytes.Buffer
	writer := NewCommandWriter(New(&output))
	message := "[  1.2s] rebuild publish"
	if _, err := writer.Write([]byte(message + "\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("JSON log = %q: %v", output.String(), err)
	}
	if record["level"] != "INFO" || record["msg"] != message {
		t.Fatalf("record = %#v, want the progress line at INFO", record)
	}
}

// TestNewCommandWriterRecordsAStatedFailure covers the other half: a caller
// that knows the line is a failure says so, and the line itself is the
// message a reader greps for.
func TestNewCommandWriterRecordsAStatedFailure(t *testing.T) {
	var output bytes.Buffer
	writer := NewCommandWriter(New(&output))
	message := "doctor storage: invalid database"
	writer.WriteLevel(slog.LevelError, message)

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("JSON log = %q: %v", output.String(), err)
	}
	if record["level"] != "ERROR" || record["msg"] != message {
		t.Fatalf("record = %#v, want the failure at ERROR with its own text", record)
	}
}

func TestNewCommandWriterRejectsNilLogger(t *testing.T) {
	_, err := NewCommandWriter(nil).Write([]byte("error"))
	if err == nil || !strings.Contains(err.Error(), "nil logger") {
		t.Fatalf("Write() error = %v, want nil logger error", err)
	}
}
