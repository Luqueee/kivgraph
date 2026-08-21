package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/eventlog"
)

// runToolStats answers what every tool cost and whether it answered.
//
// It reads the durable store rather than asking a server, and that is the whole
// reason it can answer at all: the per-tool counters a server keeps are minted
// when it starts and gone when it stops, so a question asked from another
// process -- which is what a command is -- would always find an empty
// registry. Reading the file also makes the answer span every server that ever
// ran, which is the span the question implies.
func runToolStats(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("tool-stats", flag.ContinueOnError)
	configPath := ""
	tool := ""
	since := time.Duration(0)
	jsonOutput := false
	flags.StringVar(&configPath, "config", "", "read this configuration instead of the default one")
	flags.StringVar(&tool, "tool", "", "report only this tool")
	flags.DurationVar(&since, "since", 0, "report only what happened within this duration")
	flags.BoolVar(&jsonOutput, "json", false, "write the summary as JSON")
	if parsed, code := parseCommandFlags("tool-stats", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "tool-stats: unexpected arguments: %v", flags.Args())
		return 2
	}
	if since < 0 {
		writeCommandError(stderr, "tool-stats: --since must not be negative, got %s", since)
		return 2
	}

	configuration, err := config.LoadConfig(configPath)
	if err != nil {
		writeCommandError(stderr, "tool-stats: load configuration: %v", err)
		return 1
	}
	options := eventlog.ReadOptions{Kinds: []eventlog.Kind{eventlog.KindTool}, Tool: tool}
	if since > 0 {
		options.Since = time.Now().Add(-since)
	}
	events, err := eventlog.Read(configuration.Logging.EventLogPath, options)
	if err != nil {
		writeCommandError(stderr, "tool-stats: %v", err)
		return 1
	}
	summary := eventlog.Summarize(events)

	if jsonOutput {
		encoded, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			writeCommandError(stderr, "tool-stats: encode summary: %v", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\n", encoded)
		return 0
	}
	writeToolStats(stdout, summary, configuration.Logging.EventLogPath)
	return 0
}

func writeToolStats(stdout io.Writer, summary eventlog.Summary, path string) {
	styles := styleFor(stdout)
	if len(summary.Tools) == 0 {
		writeInfo(stdout, "tool-stats: no tool call recorded yet in %s", path)
		return
	}

	const header = "%-30s %7s %7s %7s %8s %9s %9s %9s\n"
	fmt.Fprintf(stdout, styles.dim+header+styles.reset,
		"TOOL", "CALLS", "OK", "FAIL", "OK%", "MEAN", "P95", "MAX")
	for _, entry := range summary.Tools {
		rate := "-"
		if share, known := entry.SuccessRate(); known {
			rate = fmt.Sprintf("%.1f%%", share*100)
		}
		// Only a failing row is coloured. A table where every line is
		// painted tells a reader nothing about which line to read.
		prefix, suffix := "", ""
		if entry.Failed > 0 {
			prefix, suffix = styles.warning, styles.reset
		}
		fmt.Fprintf(stdout, prefix+"%-30s %7d %7d %7d %8s %9s %9s %9s"+suffix+"\n",
			entry.Tool,
			entry.Calls,
			entry.OK,
			entry.Failed,
			rate,
			formatLogDuration(entry.Mean),
			formatLogDuration(entry.P95),
			formatLogDuration(entry.Max),
		)
	}

	fmt.Fprintln(stdout, "")
	writeInfo(stdout, "tool-stats: calls=%d ok=%d failed=%d window=%s",
		summary.Calls, summary.OK, summary.Failed, toolStatsWindow(summary))
	// The last failure of each tool is the line that turns a count into
	// something actionable, and a count alone has sent people to grep for it.
	for _, entry := range summary.Tools {
		if entry.LastFail == "" {
			continue
		}
		writeWarning(stdout, "tool-stats.failure: %s: %s", entry.Tool, entry.LastFail)
	}
}

// toolStatsWindow describes the span the summary covers, because a mean over
// ten minutes and a mean over three weeks are different claims.
func toolStatsWindow(summary eventlog.Summary) string {
	if summary.First.IsZero() || summary.Last.IsZero() {
		return "none"
	}
	span := summary.Last.Sub(summary.First)
	if span <= 0 {
		return "a single instant"
	}
	return formatLogDuration(span) + " to " + strconv.Quote(summary.Last.Format(time.RFC3339))
}
