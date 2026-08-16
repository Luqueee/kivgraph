package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/procstat"
)

func statsFixtureProcesses() []procstat.Process {
	return []procstat.Process{
		{PID: 10, Args: []string{"/opt/kivgraph/bin/kivgraph", "serve"}},
		{PID: 11, Args: []string{"/opt/kivgraph/bin/kivgraph", "ui", "--addr", "127.0.0.1:7777"}},
		{PID: 12, Args: []string{"/opt/kivgraph/bin/kivgraph", "index", "--full", "--json"}},
		{PID: 13, Args: []string{"/usr/bin/node", "server.js"}},
		{PID: 14, Args: []string{"/opt/kivgraph/bin/kivgraph"}},
		{PID: 15, Args: []string{"/opt/kivgraph/bin/kivgraph", "stats"}},
	}
}

// TestCollectStatsSelectsAndOrders fixes what the table contains and in what
// order. Both halves are contracts: a row for something that is not a kivgraph
// command would be noise, and an order that changes between refreshes makes a
// live view unreadable, so ties fall back to the pid rather than to whatever the
// platform listed first.
func TestCollectStatsSelectsAndOrders(t *testing.T) {
	weights := map[int]int64{10: 100 << 20, 11: 300 << 20, 12: 900 << 20}
	report, err := collectStatsWith(
		func() ([]procstat.Process, error) { return statsFixtureProcesses(), nil },
		11,
		func(pid int) procstat.Sample {
			return procstat.Sample{Proportional: weights[pid], Resident: weights[pid] * 2}
		},
		true,
	)
	if err != nil {
		t.Fatalf("collectStatsWith() error = %v", err)
	}
	var got []int
	for _, row := range report.Rows {
		got = append(got, row.PID)
	}
	// 11 is this process, 13 is not kivgraph, 14 names no command and 15 is a
	// stats of its own -- a view that watched itself would report the watcher.
	if len(got) != 2 || got[0] != 12 || got[1] != 10 {
		t.Fatalf("rows = %v, want the index pass first and then serve", got)
	}
	if report.Total != (900<<20)+(100<<20) {
		t.Fatalf("total = %d, want the sum of the proportional sizes", report.Total)
	}

	// Equal weights must order by pid, so two identical servers never swap.
	tied, err := collectStatsWith(
		func() ([]procstat.Process, error) {
			return []procstat.Process{
				{PID: 30, Args: []string{"kivgraph", "serve"}},
				{PID: 20, Args: []string{"kivgraph", "serve"}},
			}, nil
		},
		0,
		func(int) procstat.Sample { return procstat.Sample{Proportional: 5 << 20} },
		true,
	)
	if err != nil {
		t.Fatalf("collectStatsWith() error = %v", err)
	}
	if tied.Rows[0].PID != 20 || tied.Rows[1].PID != 30 {
		t.Fatalf("tied rows = %d then %d, want the lower pid first", tied.Rows[0].PID, tied.Rows[1].PID)
	}
}

// TestStatsCostPrefersTheProportionalShare defends the number the whole command
// exists for. Three servers reading one mapped snapshot each count all of it as
// resident, so a table that summed resident sizes would report a machine
// spending three times what it spends.
func TestStatsCostPrefersTheProportionalShare(t *testing.T) {
	shared := statsRow{Resident: 140 << 20, Proportional: 79 << 20}
	if shared.cost() != 79<<20 {
		t.Fatalf("cost = %d, want the proportional share", shared.cost())
	}
	// Where the platform cannot divide pages there is no share, and resident is
	// the only answer available -- which is why the report says so out loud.
	undivided := statsRow{Resident: 140 << 20}
	if undivided.cost() != 140<<20 {
		t.Fatalf("cost = %d, want the resident size", undivided.cost())
	}
}

// TestRenderStatsPlainSaysWhatTheTotalMeans keeps the two platforms
// distinguishable in the redirected output. A sum that counts shared pages once
// per process is not the same number as one that divides them, and a reader with
// only the digits cannot tell.
func TestRenderStatsPlainSaysWhatTheTotalMeans(t *testing.T) {
	rows := []statsRow{{PID: 7, Command: "serve", Resident: 140 << 20, Proportional: 79 << 20,
		SharedClean: 90 << 20, PrivateDirty: 50 << 20, Peak: 171 << 20,
		Sample: procstat.Sample{CPU: 3 * time.Second, Uptime: 90 * time.Minute}}}

	divided := renderStatsPlain(statsReport{Rows: rows, Proportional: true, Total: 79 << 20})
	for _, want := range []string{"COST", "PRIVATE", "SHARED", "79.0 MiB", "50.0 MiB", "1h30m", "total 79.0 MiB across 1 process"} {
		if !strings.Contains(divided, want) {
			t.Fatalf("proportional output missing %q:\n%s", want, divided)
		}
	}
	if strings.Contains(divided, "RESIDENT") {
		t.Fatalf("a platform that divides pages must not headline resident size:\n%s", divided)
	}
	// A correct number needs no apology: the caveat exists to qualify a total
	// that counts shared pages twice, and printing it either way would teach a
	// reader to skip the line that matters.
	if strings.Contains(divided, "cannot divide") {
		t.Fatalf("a divided total must carry no caveat:\n%s", divided)
	}

	undivided := renderStatsPlain(statsReport{Rows: rows, Proportional: false, Total: 140 << 20})
	for _, want := range []string{"RESIDENT", "140.0 MiB", "shared pages counted once per process", "cannot divide them"} {
		if !strings.Contains(undivided, want) {
			t.Fatalf("undivided output missing %q:\n%s", want, undivided)
		}
	}

	empty := renderStatsPlain(statsReport{})
	if !strings.Contains(empty, "no kivgraph process is running") {
		t.Fatalf("empty output = %q", empty)
	}
}

// TestStatsViewRendersWithoutColour is the live view's contract in the one form a
// test can read: NO_COLOR output has to carry every number and no escape.
func TestStatsViewRendersWithoutColour(t *testing.T) {
	model := &statsModel{
		list:     func() ([]procstat.Process, error) { return nil, nil },
		interval: time.Second,
		styles:   newStatsStyles(false),
		report: statsReport{Proportional: true, Total: 79 << 20, Rows: []statsRow{{
			PID: 7, Command: "serve", Detail: "--config /tmp/x", Proportional: 79 << 20,
			PrivateDirty: 50 << 20, SharedClean: 90 << 20, Peak: 171 << 20,
			Sample: procstat.Sample{CPU: time.Second, Uptime: time.Hour},
		}}},
	}
	view := model.View()
	for _, want := range []string{"kivgraph stats", "serve", "79.0 MiB", "--config /tmp/x", "q quit", "█"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "\x1b[") {
		t.Fatalf("NO_COLOR view emitted an escape sequence:\n%q", view)
	}

	model.quitting = true
	if model.View() != "" {
		t.Fatalf("a quitting view must leave the terminal clean, got %q", model.View())
	}
}

// TestStatsBarScalesToTheHeaviestRow keeps the bar a comparison between
// processes. Scaling it to the machine's memory would flatten every row to
// nothing, and a bar that is always empty is a decoration.
func TestStatsBarScalesToTheHeaviestRow(t *testing.T) {
	full := statsBar(100, 100)
	if strings.Contains(full, "·") {
		t.Fatalf("the heaviest row should fill its bar, got %q", full)
	}
	half := statsBar(50, 100)
	if strings.Count(half, "█") == 0 || strings.Count(half, "·") == 0 {
		t.Fatalf("half of the heaviest should be half a bar, got %q", half)
	}
	// A row that weighs almost nothing still gets a mark: a blank line reads as
	// a process that is not there.
	if strings.Count(statsBar(1, 1<<40), "█") != 1 {
		t.Fatalf("a tiny row lost its mark: %q", statsBar(1, 1<<40))
	}
	if strings.Contains(statsBar(0, 0), "█") {
		t.Fatalf("an unmeasured row must not draw a bar: %q", statsBar(0, 0))
	}
}

// TestFormatBytesAndDurationSayNothingForZero holds the package rule at the
// surface: zero means the platform could not answer, so it prints as an absence
// and never as a measurement of none.
func TestFormatBytesAndDurationSayNothingForZero(t *testing.T) {
	if formatBytes(0) != "—" || formatBytes(-1) != "—" {
		t.Fatalf("formatBytes(0) = %q, formatBytes(-1) = %q", formatBytes(0), formatBytes(-1))
	}
	if formatDuration(0) != "—" {
		t.Fatalf("formatDuration(0) = %q", formatDuration(0))
	}
	for value, want := range map[int64]string{512: "512 B", 2048: "2.0 KiB", 3 << 20: "3.0 MiB", 5 << 30: "5.0 GiB"} {
		if got := formatBytes(value); got != want {
			t.Fatalf("formatBytes(%d) = %q, want %q", value, got, want)
		}
	}
}
