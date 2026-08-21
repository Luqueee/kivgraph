package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/eventlog"
)

// defaultLogLines is how much history a bare `kivgraph logs` shows. It is a
// screenful of context rather than the whole store: a reader asking what
// happened almost always means what happened last.
const defaultLogLines = 200

// followInterval is how often --follow looks for new records. The store is a
// file, so a poll costs a read of what has been appended since the last one.
const followInterval = 500 * time.Millisecond

func runLogs(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("logs", flag.ContinueOnError)
	configPath := ""
	kinds := ""
	tool := ""
	since := time.Duration(0)
	limit := defaultLogLines
	follow := false
	failures := false
	jsonOutput := false
	flags.StringVar(&configPath, "config", "", "read this configuration instead of the default one")
	flags.StringVar(&kinds, "kind", "", "keep only these comma-separated kinds: serve, tool, index")
	flags.StringVar(&tool, "tool", "", "keep only the calls of this tool")
	flags.DurationVar(&since, "since", 0, "keep only what happened within this duration")
	flags.IntVar(&limit, "limit", defaultLogLines, "show at most this many of the newest records")
	flags.BoolVar(&follow, "follow", false, "keep printing records as they arrive")
	flags.BoolVar(&failures, "failures", false, "keep only what did not answer")
	flags.BoolVar(&jsonOutput, "json", false, "write the records as JSON instead of rendering them")
	if parsed, code := parseCommandFlags("logs", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "logs: unexpected arguments: %v", flags.Args())
		return 2
	}
	selected, err := parseLogKinds(kinds)
	if err != nil {
		writeCommandError(stderr, "logs: %v", err)
		return 2
	}
	if limit < 0 {
		writeCommandError(stderr, "logs: --limit must not be negative, got %d", limit)
		return 2
	}
	if since < 0 {
		writeCommandError(stderr, "logs: --since must not be negative, got %s", since)
		return 2
	}

	configuration, err := config.LoadConfig(configPath)
	if err != nil {
		writeCommandError(stderr, "logs: load configuration: %v", err)
		return 1
	}
	path := configuration.Logging.EventLogPath
	options := eventlog.ReadOptions{
		Kinds:        selected,
		Tool:         tool,
		FailuresOnly: failures,
		Limit:        limit,
	}
	if since > 0 {
		options.Since = time.Now().Add(-since)
	}

	events, err := eventlog.Read(path, options)
	if err != nil {
		writeCommandError(stderr, "logs: %v", err)
		return 1
	}
	if jsonOutput {
		// A follower emitting a JSON array could never close it, so the
		// two flags describe different documents and cannot be combined.
		if follow {
			writeCommandError(stderr, "logs: --json and --follow cannot be combined")
			return 2
		}
		encoded, err := json.MarshalIndent(events, "", "  ")
		if err != nil {
			writeCommandError(stderr, "logs: encode records: %v", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\n", encoded)
		return 0
	}

	styles := newLogStyles(stdout)
	if len(events) == 0 && !follow {
		writeInfo(stdout, "logs: nothing recorded yet in %s", path)
		return 0
	}
	writeLogLines(stdout, styles, collapseLogEvents(events))
	if !follow {
		return 0
	}
	return followLogs(stdout, stderr, styles, path, options, events)
}

// followLogs prints records as they are appended. It keeps the newest instant
// it has shown and the identities it showed at that instant, because several
// processes write this store and two of them can land on the same nanosecond;
// filtering by time alone would either repeat those records forever or drop
// one of them.
func followLogs(
	stdout, stderr io.Writer,
	styles logStyles,
	path string,
	options eventlog.ReadOptions,
	shown []eventlog.Event,
) int {
	// Following is a live view, so the window that bounded the first page
	// must not keep truncating every later one.
	options.Limit = 0
	newest, seen := logWatermark(shown)
	ticker := time.NewTicker(followInterval)
	defer ticker.Stop()
	for range ticker.C {
		if !newest.IsZero() {
			options.Since = newest
		}
		events, err := eventlog.Read(path, options)
		if err != nil {
			writeCommandError(stderr, "logs: %v", err)
			return 1
		}
		fresh := make([]eventlog.Event, 0, len(events))
		for _, event := range events {
			if seen[logIdentity(event)] {
				continue
			}
			fresh = append(fresh, event)
		}
		if len(fresh) == 0 {
			continue
		}
		writeLogLines(stdout, styles, collapseLogEvents(fresh))
		latest, latestSeen := logWatermark(fresh)
		if latest.After(newest) {
			newest, seen = latest, latestSeen
			continue
		}
		for identity := range latestSeen {
			seen[identity] = true
		}
	}
	return 0
}

// logWatermark answers the newest instant among events and the identities
// recorded at exactly that instant.
func logWatermark(events []eventlog.Event) (time.Time, map[string]bool) {
	newest := time.Time{}
	for _, event := range events {
		if event.Time.After(newest) {
			newest = event.Time
		}
	}
	seen := make(map[string]bool)
	for _, event := range events {
		if event.Time.Equal(newest) {
			seen[logIdentity(event)] = true
		}
	}
	return newest, seen
}

func logIdentity(event eventlog.Event) string {
	return strings.Join([]string{
		event.Time.Format(time.RFC3339Nano),
		string(event.Kind),
		event.Message,
		event.Tool,
		event.Status,
		strconv.Itoa(event.PID),
	}, "\x00")
}

func parseLogKinds(value string) ([]eventlog.Kind, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var kinds []eventlog.Kind
	for _, field := range strings.Split(value, ",") {
		switch kind := eventlog.Kind(strings.TrimSpace(field)); kind {
		case eventlog.KindServe, eventlog.KindTool, eventlog.KindIndex:
			kinds = append(kinds, kind)
		default:
			return nil, fmt.Errorf("unknown kind %q, want serve, tool or index", field)
		}
	}
	return kinds, nil
}

// logLine is one rendered row: an event, how many identical ones it stands
// for, and the mean duration of that run.
type logLine struct {
	event  eventlog.Event
	repeat int
	mean   time.Duration
}

// collapseLogEvents folds a run of adjacent records that say the same thing
// into one row.
//
// Duration is deliberately not part of the identity. Every tool call has its
// own elapsed time, so including it would mean nothing ever collapsed -- and
// twelve consecutive find_references calls are exactly what a reader wants to
// see as one row carrying their mean, not as twelve rows to scroll past.
func collapseLogEvents(events []eventlog.Event) []logLine {
	lines := make([]logLine, 0, len(events))
	for _, event := range events {
		if len(lines) > 0 {
			previous := &lines[len(lines)-1]
			if logRunIdentity(previous.event) == logRunIdentity(event) {
				elapsed, timed := event.Duration()
				if timed {
					previous.mean += elapsed
				}
				previous.repeat++
				// The row reports the newest occurrence, because the
				// question a repeated line raises is whether it is
				// still happening.
				previous.event.Time = event.Time
				continue
			}
		}
		line := logLine{event: event, repeat: 1}
		if elapsed, timed := event.Duration(); timed {
			line.mean = elapsed
		}
		lines = append(lines, line)
	}
	for index := range lines {
		if lines[index].repeat > 1 {
			lines[index].mean /= time.Duration(lines[index].repeat)
		}
	}
	return lines
}

func logRunIdentity(event eventlog.Event) string {
	return strings.Join([]string{
		string(event.Level),
		string(event.Kind),
		event.Message,
		event.Tool,
		event.Status,
		event.Stage,
		event.Repository,
		event.Generation,
		event.Error,
		strconv.Itoa(event.PID),
	}, "\x00")
}

func writeLogLines(stdout io.Writer, styles logStyles, lines []logLine) {
	for _, line := range lines {
		fmt.Fprintln(stdout, renderLogLine(styles, line))
	}
}

func renderLogLine(styles logStyles, line logLine) string {
	badge := logBadgeFor(line.event)
	rendered := strings.Builder{}
	rendered.WriteString(styles.dim(line.event.Time.Format("15:04:05")))
	rendered.WriteString(" ")
	rendered.WriteString(styles.badge(badge))
	rendered.WriteString(" ")
	rendered.WriteString(styles.message(line.event.Message))
	if fields := logFields(line); fields != "" {
		rendered.WriteString(" ")
		rendered.WriteString(styles.dim(fields))
	}
	if line.repeat > 1 {
		rendered.WriteString(" ")
		rendered.WriteString(styles.dim(fmt.Sprintf("(×%d)", line.repeat)))
	}
	return rendered.String()
}

// logFields renders the attributes of an event in a fixed order, so two runs
// over the same store produce the same text and a reader learns where to look.
func logFields(line logLine) string {
	event := line.event
	var fields []string
	appendField := func(key, value string) {
		if value == "" {
			return
		}
		fields = append(fields, key+"="+oneLine(value))
	}
	// The message of a tool event is the tool name, so repeating it would
	// spend a column on nothing.
	if event.Tool != "" && event.Tool != event.Message {
		appendField("tool", event.Tool)
	}
	if event.Stage != "" && !strings.Contains(event.Message, event.Stage) {
		appendField("stage", event.Stage)
	}
	if line.mean > 0 || line.event.DurationMS != nil {
		key := "took"
		if line.repeat > 1 {
			key = "mean"
		}
		appendField(key, formatLogDuration(line.mean))
	}
	if event.Results != nil {
		appendField("results", strconv.FormatInt(*event.Results, 10))
	}
	if event.Symbols != nil {
		appendField("symbols", strconv.FormatInt(*event.Symbols, 10))
	}
	appendField("repository", event.Repository)
	appendField("generation", event.Generation)
	if event.PID != 0 {
		appendField("pid", strconv.Itoa(event.PID))
	}
	appendField("error", event.Error)
	return strings.Join(fields, " ")
}

// maxLogFieldRunes bounds one rendered field. A loader error can carry a whole
// subprocess transcript, and one record is one line.
const maxLogFieldRunes = 160

// oneLine folds a value onto the single line its record occupies. A Go loader
// failure arrives with embedded newlines and a stderr transcript, and printing
// it verbatim breaks the one-record-per-line contract every filter and every
// reader of this output depends on -- observed on the first real failing pass.
func oneLine(value string) string {
	folded := strings.Join(strings.Fields(value), " ")
	runes := []rune(folded)
	if len(runes) <= maxLogFieldRunes {
		return folded
	}
	return string(runes[:maxLogFieldRunes-1]) + "…"
}

// formatLogDuration keeps a column narrow without lying about its scale: a
// sub-millisecond call reads in microseconds, a rebuild stage in seconds.
func formatLogDuration(elapsed time.Duration) string {
	switch {
	case elapsed <= 0:
		return "0ms"
	case elapsed < time.Millisecond:
		return fmt.Sprintf("%dµs", elapsed.Microseconds())
	case elapsed < time.Second:
		return fmt.Sprintf("%.0fms", float64(elapsed)/float64(time.Millisecond))
	case elapsed < time.Minute:
		return fmt.Sprintf("%.2fs", elapsed.Seconds())
	default:
		return fmt.Sprintf("%dm%02ds", int(elapsed.Minutes()), int(elapsed.Seconds())%60)
	}
}

// logBadge is the coloured column that carries the level. Its width is fixed so
// the messages of consecutive lines align regardless of what happened.
type logBadge struct {
	text       string
	foreground string
	background string
}

const logBadgeWidth = 7

// The badge names what happened, not only how badly. A failure is ERROR and a
// degraded answer is WARN, but a call that answered is TOOL and a pass is
// INDEX: a store where every routine line said INFO would leave the reader
// doing the classification the writer already knew.
var (
	logBadgeInfo  = logBadge{text: "INFO", foreground: "15", background: "26"}
	logBadgeWarn  = logBadge{text: "WARN", foreground: "16", background: "214"}
	logBadgeError = logBadge{text: "ERROR", foreground: "15", background: "160"}
	logBadgeTool  = logBadge{text: "TOOL", foreground: "16", background: "35"}
	logBadgeIndex = logBadge{text: "INDEX", foreground: "15", background: "99"}
)

func logBadgeFor(event eventlog.Event) logBadge {
	switch {
	case event.Level == eventlog.LevelError || event.Status == eventlog.StatusError:
		return logBadgeError
	case event.Level == eventlog.LevelWarn:
		return logBadgeWarn
	case event.Kind == eventlog.KindTool:
		return logBadgeTool
	case event.Kind == eventlog.KindIndex:
		return logBadgeIndex
	default:
		return logBadgeInfo
	}
}

// logStyles renders a row. It writes raw SGR sequences rather than going
// through lipgloss because this is printed output, not a Bubble Tea view, and
// because the solid-background badge has no equivalent in the foreground-only
// palette the rest of the non-TUI surface uses.
//
// A destination that is not a terminal gets none of it, which is what makes the
// command safe to pipe and what the rendering test asserts.
type logStyles struct{ color bool }

func newLogStyles(writer io.Writer) logStyles {
	return logStyles{color: styleFor(writer).reset != ""}
}

func (styles logStyles) dim(value string) string {
	if !styles.color || value == "" {
		return value
	}
	return "\x1b[2;38;5;245m" + value + "\x1b[0m"
}

func (styles logStyles) message(value string) string {
	if !styles.color {
		return value
	}
	return "\x1b[1m" + value + "\x1b[0m"
}

func (styles logStyles) badge(badge logBadge) string {
	text := badge.text
	if len(text) > logBadgeWidth-1 {
		text = text[:logBadgeWidth-1]
	}
	padded := fmt.Sprintf(" %-*s", logBadgeWidth-1, text)
	if !styles.color {
		return padded
	}
	return "\x1b[1;38;5;" + badge.foreground + ";48;5;" + badge.background + "m" + padded + "\x1b[0m"
}
