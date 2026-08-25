package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/Luqueee/kivgraph/internal/version"
	"github.com/Luqueee/kivgraph/internal/webassets"
)

// The command line is the first thing an operator sees, so it is written for a
// person: a grouped list on request, one line on a mistake, and never a log
// record. Structured stderr belongs to the long-running commands, where a
// client reads it.

const helpTagline = "A canonical polyglot code graph, served over MCP."

// webBundleAbsence is why `ui` cannot run, and it is empty whenever the assets
// are linked in: the published release always carries them and the release
// workflow fails if it does not, but a `--mcp-only` bundle or a plain
// `go build` has none, and a binary that advertises a viewer it cannot serve is
// lying to the only person who reads the help.
func webBundleAbsence() string {
	if webassets.Available() {
		return ""
	}
	return "this build carries no web bundle"
}

// style carries the escape sequences to use, empty when the destination is not
// a terminal, so a redirected or piped help stays plain text.
type style struct {
	bold    string
	dim     string
	accent  string
	success string
	warning string
	error   string
	reset   string
}

func styleFor(writer io.Writer) style {
	file, ok := writer.(*os.File)
	if !ok || !isTerminal(file) {
		return style{}
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return style{}
	}
	return style{
		bold:    "\x1b[1m",
		dim:     "\x1b[2m",
		accent:  "\x1b[36m",
		success: "\x1b[32m",
		warning: "\x1b[33m",
		error:   "\x1b[31m",
		reset:   "\x1b[0m",
	}
}

// isTerminal reports whether a person is reading this stream.
func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// writeHelp renders the grouped command list. The summary column is aligned
// across every group, not per group, so the eye can run straight down it.
func writeHelp(writer io.Writer, program string) {
	paint := styleFor(writer)
	specs := allCommands()
	width := 0
	for _, spec := range specs {
		if spec.hidden {
			continue
		}
		width = max(width, len(spec.usage))
	}

	fmt.Fprintf(writer, "%skivgraph%s %s\n", paint.bold, paint.reset, version.Value)
	fmt.Fprintf(writer, "%s%s%s\n\n", paint.dim, helpTagline, paint.reset)
	fmt.Fprintf(writer, "%sUsage%s\n  %s <command> [flags]\n", paint.bold, paint.reset, program)
	for _, title := range commandGroupOrder {
		fmt.Fprintf(writer, "\n%s%s%s\n", paint.bold, title, paint.reset)
		// The table is ordered longest-invocation-first for dispatch, so
		// the help restores the declared order of each group here rather
		// than printing `doctor storage` above `doctor`.
		for _, spec := range helpOrder(title) {
			fmt.Fprintf(writer, "  %-*s  %s", width, spec.usage, spec.summary)
			if spec.absence != nil {
				if reason := spec.absence(); reason != "" {
					fmt.Fprintf(writer, " %s(unavailable: %s)%s", paint.dim, reason, paint.reset)
				}
			}
			fmt.Fprintln(writer)
		}
	}
	fmt.Fprintf(writer, "\n%sRun \"%s <command> --help\" for the flags of one command.%s\n",
		paint.dim, program, paint.reset)
}

// helpOrder answers one group's commands in declaration order.
func helpOrder(group string) []commandSpec {
	declared := commandTable()
	declared = append(declared, integrationCommands()...)
	ordered := make([]commandSpec, 0, len(declared))
	for _, spec := range declared {
		if spec.hidden || spec.group != group {
			continue
		}
		ordered = append(ordered, spec)
	}
	return ordered
}

// writeUsageError states the mistake in one line and points at the help,
// instead of repeating the whole surface at someone who mistyped.
func writeUsageError(writer io.Writer, program, problem string) {
	if levelled, ok := writer.(levelledWriter); ok {
		levelled.WriteLevel(slog.LevelError, fmt.Sprintf("%s: %s", program, problem))
		return
	}
	paint := styleFor(writer)
	fmt.Fprintf(writer, "%s%s%s: %s%s\n", paint.error, program, paint.reset, problem, paint.reset)
	fmt.Fprintf(writer, "%sRun \"%s --help\" to see the available commands.%s\n",
		paint.dim, program, paint.reset)
}

// levelledWriter is a stderr that records the severity a caller states,
// rather than deciding one for every line it receives.
type levelledWriter interface {
	WriteLevel(level slog.Level, message string)
}

func writeInfo(writer io.Writer, format string, arguments ...any) {
	paint := styleFor(writer)
	fmt.Fprintf(writer, "%s%s%s\n", paint.accent, fmt.Sprintf(format, arguments...), paint.reset)
}

func writeSuccess(writer io.Writer, format string, arguments ...any) {
	paint := styleFor(writer)
	fmt.Fprintf(writer, "%s%s%s\n", paint.success, fmt.Sprintf(format, arguments...), paint.reset)
}

func writeWarning(writer io.Writer, format string, arguments ...any) {
	paint := styleFor(writer)
	fmt.Fprintf(writer, "%s%s%s\n", paint.warning, fmt.Sprintf(format, arguments...), paint.reset)
}

// writeCommandError states a failure on the command's stderr.
//
// When that stderr is the structured writer -- stderr is a pipe or a file,
// so a program is reading it -- the line is recorded at ERROR. Everything
// else a command writes there is progress, and progress logged as an error
// tells a reader nothing about whether anything went wrong.
func writeCommandError(writer io.Writer, format string, arguments ...any) {
	message := fmt.Sprintf(format, arguments...)
	if levelled, ok := writer.(levelledWriter); ok {
		levelled.WriteLevel(slog.LevelError, message)
		return
	}
	paint := styleFor(writer)
	fmt.Fprintf(writer, "%s%s%s\n", paint.error, message, paint.reset)
}

func writeResult(writer io.Writer, passed bool, format string, arguments ...any) {
	if passed {
		writeSuccess(writer, format, arguments...)
		return
	}
	writeCommandError(writer, format, arguments...)
}

// parseCommandFlags parses one subcommand's flags.
//
// An explicit --help is a request, not a mistake: the usage goes to stdout and
// the command exits 0. A real parse error names itself on stderr, once; the
// flag package is silenced so it cannot print a second, unformatted copy.
func parseCommandFlags(name string, flags *flag.FlagSet, args []string, stdout, stderr io.Writer) (bool, int) {
	flags.SetOutput(io.Discard)
	err := flags.Parse(args)
	switch {
	case err == nil:
		return true, 0
	case errors.Is(err, flag.ErrHelp):
		writeCommandHelp(stdout, name, flags)
		return false, 0
	default:
		writeCommandError(stderr, "%s: %v", name, err)
		writeCommandHelp(stderr, name, flags)
		return false, 2
	}
}

func writeCommandHelp(writer io.Writer, name string, flags *flag.FlagSet) {
	paint := styleFor(writer)
	fmt.Fprintf(writer, "%sUsage%s\n  kivgraph %s [flags]\n", paint.bold, paint.reset, name)
	if summary := summaryFor(name); summary != "" {
		fmt.Fprintf(writer, "\n%s%s%s\n", paint.dim, summary, paint.reset)
	}
	writeFlagList(writer, flags, paint)
}

// writeFlagList renders the flags the way the rest of the help spells them,
// with two dashes and one aligned column. The flag package prints a single
// dash across two lines, which reads like a different program.
func writeFlagList(writer io.Writer, flags *flag.FlagSet, paint style) {
	type row struct{ invocation, usage, fallback string }
	rows := make([]row, 0, 8)
	width := 0
	flags.VisitAll(func(entry *flag.Flag) {
		name, usage := flag.UnquoteUsage(entry)
		invocation := "--" + entry.Name
		if name != "" {
			invocation += " " + strings.ToUpper(name)
		}
		fallback := ""
		if entry.DefValue != "" && entry.DefValue != "false" && entry.DefValue != "0" {
			fallback = fmt.Sprintf(" (default %s)", entry.DefValue)
		}
		width = max(width, len(invocation))
		rows = append(rows, row{invocation, usage, fallback})
	})
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(writer, "\n%sFlags%s\n", paint.bold, paint.reset)
	for _, entry := range rows {
		fmt.Fprintf(writer, "  %-*s  %s%s\n", width, entry.invocation, entry.usage, entry.fallback)
	}
}

// summaryFor finds the one-line description of a command, so the per-command
// help and the list cannot drift apart. The name is the invocation as the
// command's own flag set spells it, which is not always how the table spells it
// -- `benchmark generate-graph` names its flag set "generate-graph".
func summaryFor(name string) string {
	for _, spec := range allCommands() {
		if spec.name() == name || strings.HasSuffix(spec.name(), " "+name) {
			return spec.summary
		}
	}
	return ""
}

// boundedReportLines caps how many detail lines a report prints, and says how
// many it withheld. A pass over a broken workspace can produce hundreds, and
// a report nobody reads to the end hides the first one.
func boundedReportLines(lines []string, limit int) []string {
	if len(lines) <= limit {
		return lines
	}
	bounded := make([]string, 0, limit+1)
	bounded = append(bounded, lines[:limit]...)
	return append(bounded, fmt.Sprintf("and %d more", len(lines)-limit))
}
