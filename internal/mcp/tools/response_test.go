package tools

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestResponseJSONIncludesEveryStandardField(t *testing.T) {
	response := Response[[]string]{Results: []string{}}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal(response) error = %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v", err)
	}
	for _, name := range []string{
		"snapshot_id",
		"snapshot_age_ms",
		"total",
		"returned",
		"truncated",
		"next_cursor",
		"coverage",
		"results",
	} {
		if _, ok := fields[name]; !ok {
			t.Errorf("response is missing standard field %q: %s", name, data)
		}
	}
	if string(fields["snapshot_id"]) != "null" || string(fields["snapshot_age_ms"]) != "null" || string(fields["next_cursor"]) != "null" {
		t.Fatalf("nil metadata = snapshot_id %s, snapshot_age_ms %s, next_cursor %s; want JSON nulls", fields["snapshot_id"], fields["snapshot_age_ms"], fields["next_cursor"])
	}
	if string(fields["results"]) != "[]" {
		t.Fatalf("results = %s, want an empty JSON array", fields["results"])
	}
}

func TestToolErrorIsSerializableAndClassified(t *testing.T) {
	cause := errors.New("database unavailable")
	err := WrapToolError(CodeSnapshotUnavailable, "snapshot cannot be read", cause)

	if got := ErrorCode(err); got != CodeSnapshotUnavailable {
		t.Fatalf("ErrorCode(err) = %q, want %q", got, CodeSnapshotUnavailable)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is(err, cause) = false")
	}

	data, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("json.Marshal(err) error = %v", marshalErr)
	}
	var wire ToolError
	if unmarshalErr := json.Unmarshal(data, &wire); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(err) error = %v", unmarshalErr)
	}
	if wire.Code != CodeSnapshotUnavailable || wire.Message != "snapshot cannot be read" || wire.cause != nil {
		t.Fatalf("serialized tool error = %#v, want code/message without internal cause", wire)
	}
}
