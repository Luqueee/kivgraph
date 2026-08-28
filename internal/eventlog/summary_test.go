package eventlog

import (
	"testing"
	"time"
)

func toolCall(offset time.Duration, tool string, elapsed time.Duration, failure string) Event {
	base := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	event := Event{
		Time:    base.Add(offset),
		Level:   LevelInfo,
		Kind:    KindTool,
		Message: tool,
		Tool:    tool,
		Status:  StatusOK,
	}
	if failure != "" {
		event.Level = LevelError
		event.Status = StatusError
		event.Error = failure
	}
	return event.WithDuration(elapsed)
}

func TestSummarizeCountsAnswersAndFailures(t *testing.T) {
	events := []Event{
		toolCall(0, "find_references", 10*time.Millisecond, ""),
		toolCall(time.Second, "find_references", 30*time.Millisecond, ""),
		toolCall(2*time.Second, "find_references", 20*time.Millisecond, "CURSOR_INVALID: stale"),
		toolCall(3*time.Second, "find_symbol", 5*time.Millisecond, ""),
		// A record of another kind must not be counted as a call.
		{Time: time.Now(), Kind: KindIndex, Message: "index --full finished"},
	}
	summary := Summarize(events)

	if summary.Calls != 4 || summary.OK != 3 || summary.Failed != 1 {
		t.Fatalf("summary totals = calls %d ok %d failed %d, want 4/3/1",
			summary.Calls, summary.OK, summary.Failed)
	}
	if len(summary.Tools) != 2 {
		t.Fatalf("Summarize() reported %d tools, want 2", len(summary.Tools))
	}
	// The busiest tool leads, because that is the row a reader came for.
	references := summary.Tools[0]
	if references.Tool != "find_references" {
		t.Fatalf("first row = %q, want find_references", references.Tool)
	}
	want := ToolSummary{
		Tool:     "find_references",
		Calls:    3,
		OK:       2,
		Failed:   1,
		Mean:     20 * time.Millisecond,
		Median:   20 * time.Millisecond,
		P95:      30 * time.Millisecond,
		Max:      30 * time.Millisecond,
		Last:     references.Last,
		LastFail: "CURSOR_INVALID: stale",
	}
	if references != want {
		t.Fatalf("find_references summary = %+v, want %+v", references, want)
	}
	if share, known := references.SuccessRate(); !known || share < 0.66 || share > 0.67 {
		t.Fatalf("SuccessRate() = %v %v, want about 0.666 and true", share, known)
	}
}

// A mean hides the tail the percentiles exist to show; this is the case where
// the two answers differ, which is the only case worth a test. Two slow calls
// in twenty, not one: with a single outlier the 95th percentile of twenty
// samples is still the nineteenth value, and the maximum is what reports it.
func TestSummarizePercentilesFollowTheTail(t *testing.T) {
	var events []Event
	for index := range 18 {
		events = append(events, toolCall(time.Duration(index)*time.Second, "get_source", time.Millisecond, ""))
	}
	events = append(events,
		toolCall(19*time.Second, "get_source", time.Second, ""),
		toolCall(20*time.Second, "get_source", time.Second, ""),
	)

	entry := Summarize(events).Tools[0]
	if entry.Median != time.Millisecond {
		t.Fatalf("median = %s, want 1ms", entry.Median)
	}
	if entry.P95 != time.Second {
		t.Fatalf("p95 = %s, want 1s: the slow call is the twentieth of twenty", entry.P95)
	}
	if entry.Max != time.Second {
		t.Fatalf("max = %s, want 1s", entry.Max)
	}
	if entry.Mean <= time.Millisecond || entry.Mean >= entry.P95 {
		t.Fatalf("mean = %s, want it between the median and the p95", entry.Mean)
	}
}

func TestSummarizeWithoutCallsIsEmptyRatherThanZeroed(t *testing.T) {
	summary := Summarize(nil)
	if len(summary.Tools) != 0 || summary.Calls != 0 {
		t.Fatalf("Summarize(nil) = %+v, want no tools and no calls", summary)
	}
	if !summary.First.IsZero() || !summary.Last.IsZero() {
		t.Fatalf("Summarize(nil) reported a window: %s to %s", summary.First, summary.Last)
	}
	if _, known := (ToolSummary{}).SuccessRate(); known {
		t.Fatal("SuccessRate() claimed to know the rate of a tool with no calls")
	}
}

// An untimed call still counts: the store may hold a record from a producer
// that observed the outcome but not the duration.
func TestSummarizeCountsUntimedCalls(t *testing.T) {
	events := []Event{{
		Time: time.Now(), Kind: KindTool, Message: "graph_status",
		Tool: "graph_status", Status: StatusOK,
	}}
	entry := Summarize(events).Tools[0]
	if entry.Calls != 1 || entry.OK != 1 {
		t.Fatalf("summary = %+v, want one answered call", entry)
	}
	if entry.Mean != 0 || entry.P95 != 0 || entry.Max != 0 {
		t.Fatalf("summary invented a latency: %+v", entry)
	}
}

// TestErrorCodeReadsTheCodeTheWriterEncoded is what makes a log written before
// anything classified refusals still classifiable. The writer renders a tool
// failure as "CODE: message" on purpose -- its own comment says the
// classification survives without a field of its own -- and this is the other
// half of that decision.
func TestErrorCodeReadsTheCodeTheWriterEncoded(t *testing.T) {
	for _, testCase := range []struct {
		rendered string
		want     string
	}{
		{`AMBIGUOUS_SYMBOL: "Status" has 71 declarations`, "AMBIGUOUS_SYMBOL"},
		{`SYMBOL_NOT_FOUND: name "posthog" was not found`, "SYMBOL_NOT_FOUND"},
		// Nothing to read, and each for a different reason: no separator, a
		// message that is not a code, and a colon with nothing before it. A
		// loose parser would turn every one of these into a code that some
		// future RefusalCodes could match.
		{"", ""},
		{"something went wrong", ""},
		{"read config: no such file", ""},
		{": leading", ""},
		{"lower_case: message", ""},
	} {
		if got := (Event{Error: testCase.rendered}).ErrorCode(); got != testCase.want {
			t.Fatalf("Event{Error: %q}.ErrorCode() = %q, want %q", testCase.rendered, got, testCase.want)
		}
	}
}

// TestSummarizeSeparatesARefusalFromAFailure is the arithmetic LUQUE-2235 was
// opened over. The two live in one column until a caller names the codes, and
// summed they made find_references read 22.2% when its real failure rate was
// 42% -- a number that invites a hunt for a bug that is not there.
func TestSummarizeSeparatesARefusalFromAFailure(t *testing.T) {
	base := time.Date(2026, 8, 21, 13, 10, 0, 0, time.UTC)
	failed := func(offset time.Duration, rendered string) Event {
		return Event{
			Time: base.Add(offset), Kind: KindTool, Tool: "find_references",
			Level: LevelError, Status: StatusError, Error: rendered,
		}
	}
	events := []Event{
		{Time: base, Kind: KindTool, Tool: "find_references", Status: StatusOK},
		failed(time.Second, `AMBIGUOUS_SYMBOL: "Status" has 71 declarations`),
		failed(2*time.Second, `SYMBOL_NOT_FOUND: name "posthog" was not found`),
	}

	// Without the vocabulary the two are one number, which is what every
	// caller got before this and what the measurement reported.
	if summed := Summarize(events); summed.Failed != 2 || summed.Refused != 0 {
		t.Fatalf("Summarize() without codes = failed %d refused %d, want 2 and 0",
			summed.Failed, summed.Refused)
	}

	summary := Summarize(events, "AMBIGUOUS_SYMBOL")
	if summary.Calls != 3 || summary.OK != 1 || summary.Refused != 1 || summary.Failed != 1 {
		t.Fatalf("Summarize() = calls %d ok %d refused %d failed %d, want 3, 1, 1 and 1",
			summary.Calls, summary.OK, summary.Refused, summary.Failed)
	}
	entry := summary.Tools[0]
	if entry.OK != 1 || entry.Refused != 1 || entry.Failed != 1 {
		t.Fatalf("the tool row = ok %d refused %d failed %d, want 1, 1 and 1",
			entry.OK, entry.Refused, entry.Failed)
	}
	// The three columns still account for every call: a refusal moved out of
	// Failed rather than being counted twice.
	if entry.OK+entry.Refused+entry.Failed != entry.Calls {
		t.Fatalf("the columns do not sum to the calls: %+v", entry)
	}
	// LastFail points at what to act on. A refusal there sends a reader
	// looking for a bug in the answer the tool was designed to give.
	if entry.LastFail != `SYMBOL_NOT_FOUND: name "posthog" was not found` {
		t.Fatalf("LastFail = %q, want the failure and not the refusal", entry.LastFail)
	}
}

// A tool whose only non-OK call was a refusal has failed nothing, so it must
// offer nothing to act on: the last-failure line is the one that turns a count
// into a search.
func TestSummarizeLeavesNoFailureToActOnWhenOnlyARefusalHappened(t *testing.T) {
	base := time.Date(2026, 8, 21, 13, 10, 0, 0, time.UTC)
	entry := Summarize([]Event{{
		Time: base, Kind: KindTool, Tool: "find_references",
		Level: LevelError, Status: StatusError,
		Error: `AMBIGUOUS_SYMBOL: "Status" has 71 declarations`,
	}}, "AMBIGUOUS_SYMBOL").Tools[0]
	if entry.Failed != 0 || entry.Refused != 1 {
		t.Fatalf("the row = refused %d failed %d, want 1 and 0", entry.Refused, entry.Failed)
	}
	if entry.LastFail != "" {
		t.Fatalf("a row that failed nothing offered %q to act on", entry.LastFail)
	}
}
