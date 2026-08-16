package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/procstat"
)

// statsRow is one process as `stats` reports it.
type statsRow struct {
	PID     int             `json:"pid"`
	Command string          `json:"command"`
	Detail  string          `json:"detail,omitempty"`
	Sample  procstat.Sample `json:"-"`

	Resident      int64   `json:"resident_bytes"`
	Proportional  int64   `json:"proportional_bytes,omitempty"`
	SharedClean   int64   `json:"shared_clean_bytes,omitempty"`
	PrivateDirty  int64   `json:"private_dirty_bytes,omitempty"`
	Peak          int64   `json:"peak_bytes"`
	CPUSeconds    float64 `json:"cpu_seconds"`
	UptimeSeconds float64 `json:"uptime_seconds"`
}

// cost is what this process spends on the machine: its share of every page it
// holds where that can be divided, and its resident size where it cannot.
//
// The two are not interchangeable and the difference is the whole reason this
// command exists. Three servers reading one published snapshot each count all of
// it as resident, so their resident sizes sum to three times a file that is only
// there once.
func (row statsRow) cost() int64 {
	if row.Proportional > 0 {
		return row.Proportional
	}
	return row.Resident
}

// statsReport is what one refresh observed.
type statsReport struct {
	Rows []statsRow `json:"processes"`
	// Proportional says whether the machine could divide shared pages. When it
	// could not, Total is a sum of resident sizes and counts shared pages once
	// per process, which the report has to say rather than imply.
	Proportional bool      `json:"proportional_memory"`
	Total        int64     `json:"total_bytes"`
	At           time.Time `json:"observed_at"`
}

// processObserver is the sampling seam. It exists so a test can decide what a
// process weighs: the ordering of the table is a claim about the numbers, and a
// test that could not set them would only be checking that nothing panics.
type processObserver func(pid int) procstat.Sample

func collectStats(list processLister, self int) (statsReport, error) {
	return collectStatsWith(list, self, procstat.Observe, procstat.ProportionalSupported())
}

func collectStatsWith(list processLister, self int, observe processObserver, proportional bool) (statsReport, error) {
	processes, err := list()
	if err != nil {
		return statsReport{}, err
	}
	report := statsReport{Proportional: proportional, At: time.Now()}
	for _, process := range processes {
		if process.PID == self {
			continue
		}
		program, command := process.Invocation()
		if program != "kivgraph" || command == "" || command == "stats" {
			continue
		}
		sample := observe(process.PID)
		row := statsRow{
			PID:           process.PID,
			Command:       command,
			Detail:        statsDetail(process.Args),
			Sample:        sample,
			Resident:      sample.Resident,
			Proportional:  sample.Proportional,
			SharedClean:   sample.SharedClean,
			PrivateDirty:  sample.PrivateDirty,
			Peak:          sample.Peak,
			CPUSeconds:    sample.CPU.Seconds(),
			UptimeSeconds: sample.Uptime.Seconds(),
		}
		report.Rows = append(report.Rows, row)
		report.Total += row.cost()
	}
	// Heaviest first, then by pid so two identical rows never swap places between
	// refreshes: a table that reorders itself is unreadable.
	sort.Slice(report.Rows, func(left, right int) bool {
		if report.Rows[left].cost() != report.Rows[right].cost() {
			return report.Rows[left].cost() > report.Rows[right].cost()
		}
		return report.Rows[left].PID < report.Rows[right].PID
	})
	return report, nil
}

// statsDetail keeps the argument that distinguishes two processes of the same
// command, which is what a bare `serve` and a `serve --config /tmp/x` are.
func statsDetail(args []string) string {
	if len(args) < 3 {
		return ""
	}
	detail := strings.Join(args[2:], " ")
	const limit = 44
	if len(detail) > limit {
		return detail[:limit-1] + "…"
	}
	return detail
}

func runStats(args []string, stdout, stderr io.Writer, list processLister) int {
	flags := flag.NewFlagSet("stats", flag.ContinueOnError)
	interval := time.Second
	once := false
	asJSON := false
	flags.DurationVar(&interval, "interval", time.Second, "refresh interval for the live view")
	flags.BoolVar(&once, "once", false, "print one observation and exit")
	flags.BoolVar(&asJSON, "json", false, "print one observation as JSON and exit")
	if parsed, code := parseCommandFlags("stats", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "stats: unexpected arguments: %v", flags.Args())
		return 2
	}
	if interval < 100*time.Millisecond {
		writeCommandError(stderr, "stats: --interval must be at least 100ms")
		return 2
	}
	report, err := collectStats(list, os.Getpid())
	if err != nil {
		writeCommandError(stderr, "stats: %v", err)
		return 1
	}
	if asJSON {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			writeCommandError(stderr, "stats: %v", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\n", encoded)
		return 0
	}
	// A live view that nobody is watching is a command that never ends, so a
	// redirected stats stream is one observation and an exit.
	if once || !integrationTUIIsInteractive(stdout) {
		fmt.Fprint(stdout, renderStatsPlain(report))
		return 0
	}
	if err := runStatsTUI(stdout, list, interval); err != nil {
		writeCommandError(stderr, "stats: %v", err)
		return 1
	}
	return 0
}

// renderStatsPlain is the redirected form: aligned columns, no colour, no
// cursor movement, one observation.
func renderStatsPlain(report statsReport) string {
	if len(report.Rows) == 0 {
		return "stats: no kivgraph process is running\n"
	}
	var out strings.Builder
	if report.Proportional {
		fmt.Fprintf(&out, "%-7s %-8s %10s %10s %10s %10s %8s %9s\n",
			"PID", "COMMAND", "COST", "PRIVATE", "SHARED", "PEAK", "CPU", "UPTIME")
	} else {
		fmt.Fprintf(&out, "%-7s %-8s %10s %10s %8s %9s\n",
			"PID", "COMMAND", "RESIDENT", "PEAK", "CPU", "UPTIME")
	}
	for _, row := range report.Rows {
		if report.Proportional {
			fmt.Fprintf(&out, "%-7d %-8s %10s %10s %10s %10s %8s %9s\n",
				row.PID, row.Command,
				formatBytes(row.Proportional), formatBytes(row.PrivateDirty),
				formatBytes(row.SharedClean), formatBytes(row.Peak),
				formatDuration(row.Sample.CPU), formatDuration(row.Sample.Uptime))
			continue
		}
		fmt.Fprintf(&out, "%-7d %-8s %10s %10s %8s %9s\n",
			row.PID, row.Command, formatBytes(row.Resident), formatBytes(row.Peak),
			formatDuration(row.Sample.CPU), formatDuration(row.Sample.Uptime))
	}
	fmt.Fprintf(&out, "%s\n", statsTotalLine(report))
	return out.String()
}

// statsTotalLine says what the sum means, because on a machine that cannot
// divide shared pages it does not mean what it looks like.
func statsTotalLine(report statsReport) string {
	if report.Proportional {
		return fmt.Sprintf("total %s across %d process(es), shared pages counted once",
			formatBytes(report.Total), len(report.Rows))
	}
	return fmt.Sprintf("total %s across %d process(es); this platform cannot divide shared pages, so anything they share is counted once per process",
		formatBytes(report.Total), len(report.Rows))
}

// formatBytes writes a size the way a person reads one, and a dash for the zero
// that means the platform could not answer.
func formatBytes(value int64) string {
	if value <= 0 {
		return "—"
	}
	const unit = 1024.0
	size := float64(value)
	for _, suffix := range []string{"B", "KiB", "MiB", "GiB", "TiB"} {
		if size < unit || suffix == "TiB" {
			if suffix == "B" {
				return fmt.Sprintf("%d B", value)
			}
			return fmt.Sprintf("%.1f %s", size, suffix)
		}
		size /= unit
	}
	return fmt.Sprintf("%d B", value)
}

func formatDuration(value time.Duration) string {
	if value <= 0 {
		return "—"
	}
	switch {
	case value < time.Minute:
		return fmt.Sprintf("%.1fs", value.Seconds())
	case value < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(value.Minutes()), int(value.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(value.Hours()), int(value.Minutes())%60)
	}
}
