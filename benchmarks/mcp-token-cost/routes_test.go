package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
	"time"
)

func TestRouteComparisonPreservesPaginationAndCoverage(t *testing.T) {
	page := `{"coverage":{"exact":2},"next_cursor":"abc"}`
	for _, other := range []string{`{"coverage":{"exact":1},"next_cursor":"def"}`, `{"coverage":{"exact":2}}`} {
		if samePage(page, other) {
			t.Errorf("accepted different page: %s", other)
		}
	}
	if !samePage(page, `{"coverage":{"exact":2},"next_cursor":"def"}`) {
		t.Fatal("opaque cursor contents are not reference facts")
	}
}

func TestRouteRowsKeepEachSampleBeforePooling(t *testing.T) {
	var buf bytes.Buffer
	samples := [2]routeSample{
		{answer: 3 * time.Millisecond, full: 5 * time.Millisecond},
		{answer: 2 * time.Millisecond, full: 4 * time.Millisecond},
	}
	// Direct ran first; the emitted JSONL must reflect that chronology.
	if err := writeRouteRows(json.NewEncoder(&buf), "GetActiveHelpConfig", 0, [2]int{2, 1}, samples); err != nil {
		t.Fatal(err)
	}
	want := []routeRow{
		{Question: "GetActiveHelpConfig", Pair: 0, Arm: "direct", Position: 1, AnswerNS: 2e6, SessionNS: 4e6},
		{Question: "GetActiveHelpConfig", Pair: 0, Arm: "lookup-first", Position: 2, AnswerNS: 3e6, SessionNS: 5e6},
	}
	var got []routeRow
	dec := json.NewDecoder(&buf)
	for dec.More() {
		var row routeRow
		if err := dec.Decode(&row); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("rows = %+v, want %+v", got, want)
	}
}
