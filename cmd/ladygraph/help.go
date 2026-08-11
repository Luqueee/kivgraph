package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/Luqueee/ladygraph/internal/version"
	"github.com/Luqueee/ladygraph/internal/webassets"
)

// The command line is the first thing an operator sees, so it is written for a
// person: a grouped list on request, one line on a mistake, and never a log
// record. Structured stderr belongs to the long-running commands, where a
// client reads it.

const helpTagline = "A canonical code graph for Go and TypeScript, served over MCP."

type commandEntry struct {
	invocation string
	summary    string
}

type commandGroup struct {
	title    string
	commands []commandEntry
}

// unavailableCommands answers why a build cannot run a command, keyed by the
// invocation the table above declares. TestHelpMarksEveryUnavailableCommand
// keeps the keys naming commands that exist.
var unavailableCommands = map[string]func() string{
	"ui [--addr HOST:PORT]": webBundleAbsence,
}

// commandAbsence answers why this build cannot run the command, and the empty
// string when nothing stands in the way.
func commandAbsence(invocation string) string {
	produce, exists := unavailableCommands[invocation]
	if !exists {
		return ""
	}
	return produce()
}

// webBundleAbsence is why `ui` cannot run: the published MCP bundle is built
// without web assets on purpose, so a binary that advertises a viewer it
// cannot serve is lying to the only person who reads the help.
func webBundleAbsence() string {
	if webassets.Available() {
		return ""
	}
	return "this build carries no web bundle"
}

// commandGroups is the whole surface of the command line, in the order an
// operator meets it: set up a graph, look at it, keep it, and the two commands
// that only a rebuild pipeline needs.
var commandGroups = []commandGroup{
	{
		title: "Getting started",
		commands: []commandEntry{
			{"init [--repository NAME=PATH] [--languages LIST]", "Write the configuration and register repositories"},
			{"index --full", "Index every registered repository and publish a generation"},
			{"serve", "Run the MCP server over stdio"},
			{"ui [--addr HOST:PORT]", "Serve the read-only graph viewer, loopback by default"},
		},
	},
	{
		title: "Diagnostics",
		commands: []commandEntry{
			{"doctor", "Check configuration, toolchains and the published graph"},
			{"doctor storage --database PATH", "Inspect one LadybugDB database file"},
			{"doctor graph --database PATH", "Validate the canonical graph of a database"},
			{"graph status --root PATH", "Report the active and backup generations"},
			{"version [--json]", "Print the release, with --json for full provenance"},
		},
	},
	{
		title: "Maintenance",
		commands: []commandEntry{
			{"upgrade", "Rebuild the graph after a schema change"},
			{"clean [--keep-active] [--yes]", "Remove published graph generations"},
			{"rollback --root PATH [--generation ID]", "Return to the previous generation"},
			{"snapshot --root PATH [--generation ID]", "Rebuild the hot snapshot of a generation"},
			{"update [--check]", "Install the latest published release"},
		},
	},
	{
		title: "Integrations",
		commands: []commandEntry{
			{"mcp install [--scope user|project]", "Detect and register one or more MCP clients"},
			{"mcp status --target TARGET [--scope user|project]", "Inspect a client MCP registration"},
			{"mcp remove --target TARGET [--scope user|project]", "Remove only Ladygraph's MCP registration"},
			{"skill install [--scope user|project]", "Detect and install the Agent Skill in one or more clients"},
			{"skill status --target TARGET [--scope user|project]", "Inspect the installed Agent Skill"},
			{"skill remove --target TARGET [--scope user|project]", "Remove only Ladygraph's Agent Skill"},
		},
	},
	{
		title: "Pipeline",
		commands: []commandEntry{
			{"rebuild --facts PATH --root PATH ...", "Publish a generation from a fact set"},
			{"benchmark generate-graph", "Generate a synthetic corpus"},
		},
	},
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
	width := 0
	for _, group := range commandGroups {
		for _, command := range group.commands {
			width = max(width, len(command.invocation))
		}
	}

	fmt.Fprintf(writer, "%sladygraph%s %s\n", paint.bold, paint.reset, version.Value)
	fmt.Fprintf(writer, "%s%s%s\n\n", paint.dim, helpTagline, paint.reset)
	fmt.Fprintf(writer, "%sUsage%s\n  %s <command> [flags]\n", paint.bold, paint.reset, program)
	for _, group := range commandGroups {
		fmt.Fprintf(writer, "\n%s%s%s\n", paint.bold, group.title, paint.reset)
		for _, command := range group.commands {
			fmt.Fprintf(writer, "  %-*s  %s", width, command.invocation, command.summary)
			if reason := commandAbsence(command.invocation); reason != "" {
				fmt.Fprintf(writer, " %s(unavailable: %s)%s", paint.dim, reason, paint.reset)
			}
			fmt.Fprintln(writer)
		}
	}
	fmt.Fprintf(writer, "\n%sRun \"%s <command> --help\" for the flags of one command.%s\n",
		paint.dim, program, paint.reset)
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
	fmt.Fprintf(writer, "%sUsage%s\n  ladygraph %s [flags]\n", paint.bold, paint.reset, name)
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

// summaryFor finds the one-line description of a command by the first word of
// its invocation, so the per-command help and the list cannot drift apart.
func summaryFor(name string) string {
	for _, group := range commandGroups {
		for _, command := range group.commands {
			if strings.HasPrefix(command.invocation, name+" ") || command.invocation == name {
				return command.summary
			}
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
