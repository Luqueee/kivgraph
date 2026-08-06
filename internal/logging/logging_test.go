package logging

import (
	"bytes"
	"encoding/json"
	"errors"
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

func TestNewErrorWriterConvertsLegacyTextToJSON(t *testing.T) {
	var output bytes.Buffer
	writer := NewErrorWriter(New(&output))
	message := "doctor storage: invalid database"
	if _, err := writer.Write([]byte(message + "\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("JSON log = %q: %v", output.String(), err)
	}
	if record["level"] != "ERROR" || record["msg"] != "command stderr" || record["message"] != message {
		t.Fatalf("record = %#v, want structured legacy message", record)
	}
}

func TestNewErrorWriterRejectsNilLogger(t *testing.T) {
	_, err := NewErrorWriter(nil).Write([]byte("error"))
	if err == nil || !strings.Contains(err.Error(), "nil logger") {
		t.Fatalf("Write() error = %v, want nil logger error", err)
	}
}
