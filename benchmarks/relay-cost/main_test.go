package main

import (
	"strings"
	"testing"
)

func TestParseClientCountsRejectsNonsense(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "1,two"} {
		if _, err := parseClientCounts(value); err == nil {
			t.Fatalf("parseClientCounts(%q) accepted a count it cannot measure", value)
		}
	}
	counts, err := parseClientCounts(" 1, 2 ,4,8 ")
	if err != nil {
		t.Fatalf("parseClientCounts: %v", err)
	}
	if len(counts) != 4 || counts[0] != 1 || counts[3] != 8 {
		t.Fatalf("parseClientCounts returned %v", counts)
	}
}

// TestCheckIdleRefusesAnAnsweredIdleRun is the one guard no live run would
// catch. The probes that would break it live inside the process starters, which
// need a real server, so deleting one breaks nothing on a laptop and produces a
// file whose name -- idle -- is the only thing about it that is wrong.
func TestCheckIdleRefusesAnAnsweredIdleRun(t *testing.T) {
	answered := 12.5
	cases := map[string]point{
		"a timed first answer": {Clients: 2, Arms: []arm{{Name: armServe, FirstAnswersMS: []float64{answered}}}},
		"an answered call":     {Clients: 2, Arms: []arm{{Name: armRelay, Latency: latency{Calls: 1}}}},
		"a timed new client":   {Clients: 2, Arms: []arm{{Name: armDaemon, NewClientMS: &answered}}},
	}
	for name, measured := range cases {
		if err := checkIdle(0, measured); err == nil {
			t.Fatalf("an idle run with %s was accepted", name)
		}
	}
	if err := checkIdle(0, point{Clients: 2, Arms: []arm{{Name: armServe}}}); err != nil {
		t.Fatalf("a genuinely idle run was refused: %v", err)
	}
	// A run that asked for calls is allowed to have answered them.
	if err := checkIdle(8, point{Clients: 2, Arms: []arm{{Name: armServe, Latency: latency{Calls: 8}}}}); err != nil {
		t.Fatalf("a run with calls was refused: %v", err)
	}
}

func TestCheckGenerationsRefusesArmsOnDifferentGraphs(t *testing.T) {
	measured := point{Clients: 1, Arms: []arm{
		{Name: armServe, SnapshotID: 91},
		{Name: armRelay, SnapshotID: 90},
	}}
	if err := checkGenerations(measured); err == nil {
		t.Fatal("two arms serving different generations were accepted as one measurement")
	}
}

// TestLeastSquaresRefusesALineThroughOnePoint keeps a single client count from
// publishing a slope. One point determines no line, and a zero would read as an
// arrangement that is flat in the number of clients.
func TestLeastSquaresRefusesALineThroughOnePoint(t *testing.T) {
	if slope, intercept := leastSquares([]float64{1}, []float64{100}); slope != 0 || intercept != 0 {
		t.Fatalf("one point fitted a line: slope %v intercept %v", slope, intercept)
	}
	slope, intercept := leastSquares([]float64{1, 2, 3}, []float64{10, 20, 30})
	if slope != 10 || intercept != 0 {
		t.Fatalf("slope %v intercept %v, want 10 and 0", slope, intercept)
	}
}

// TestVerdictReportsBothOutcomes fixes that a failing gate is a real answer.
// The ADR says in as many words that a floor which opens no gap closes the
// ficha, so `proceed: false` has to be reachable and has to say so.
func TestVerdictReportsBothOutcomes(t *testing.T) {
	computed := slopes{
		Arms: []armSlope{
			{Name: armServe, PerClientBytes: 9 << 20},
			{Name: armRelay, PerClientBytes: 8.5 * (1 << 20)},
		},
		SavedBytesPerClient: 0.5 * (1 << 20),
	}
	decided := verdictOf(computed, 1<<20)
	if decided.Proceed {
		t.Fatal("half a megabyte per client passed a one-megabyte gate")
	}
	if !strings.Contains(decided.Reason, "stdio server stays") {
		t.Fatalf("a failed gate did not name its consequence: %s", decided.Reason)
	}

	computed.SavedBytesPerClient = 4 << 20
	computed.Arms[1].PerClientBytes = 5 << 20
	if decided := verdictOf(computed, 1<<20); !decided.Proceed {
		t.Fatalf("four megabytes per client failed a one-megabyte gate: %s", decided.Reason)
	}
}

// TestLimitationsDeclareADirtyTree keeps an artifact from attributing its
// numbers to a revision that does not contain the code that produced them.
func TestLimitationsDeclareADirtyTree(t *testing.T) {
	notes := limitations(results{Commit: "abc1234-dirty", Calls: 8}, config{Warmup: 1})
	if !containsSubstring(notes, "-dirty") {
		t.Fatalf("a dirty tree was not declared: %v", notes)
	}
	notes = limitations(results{Commit: "abc1234", Calls: 0}, config{})
	if containsSubstring(notes, "-dirty") {
		t.Fatalf("a clean tree was declared dirty: %v", notes)
	}
	if !containsSubstring(notes, "48 of 51") {
		t.Fatalf("an idle run did not declare which load it measured: %v", notes)
	}
}

func containsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}
