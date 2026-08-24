package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/eventlog"
	"github.com/Luqueee/kivgraph/internal/integrations"
	"github.com/Luqueee/kivgraph/internal/procstat"
	"github.com/Luqueee/kivgraph/internal/version"
)

// The command line is declared once, here, and read three times: by the
// dispatch that runs a command, by the help that lists it, and by the
// completion that suggests it.
//
// It used to be declared twice -- a linear if-chain for dispatch and a separate
// table for help -- and the two had already drifted: ten commands carried flags
// the help never named. A third copy for completion would have drifted the same
// way, and a completion that omits a third of the surface is worse than none,
// because a reader trusts it.
//
// Flags are deliberately NOT restated here. Each command owns one
// `<name>FlagSet` constructor, and `writeFlagList` already walks a real
// `flag.FlagSet`, so that constructor is the only place a flag is spelled. The
// `usage` string below is a curated summary for a person -- naming every flag
// of `logs` on one help line would make the help worse, not better -- and
// TestUsageNamesOnlyRealFlags keeps it from naming one that does not exist.

// dependencies are the seams the tests replace. They were threaded through six
// wrapper functions one parameter at a time; the wrappers still exist so no
// test callsite changes, but they now fill this struct instead of growing
// another argument.
type dependencies struct {
	diagnose  storageDiagnoser
	rebuilder graphRebuilder
	verify    graphVerifier
	roles     graphRoleResolver
	rollback  graphRollbacker
	build     snapshotBuilder
}

// flagHint is what completion knows about a flag's value beyond its name.
//
// A flag with neither a vocabulary nor paths still completes its own name; the
// hint only decides what comes after the space.
type flagHint struct {
	// values answers the accepted vocabulary. It is a function because some
	// vocabularies are only knowable at the moment of asking -- the
	// generations on disk, the clients installed on this machine.
	values func() []string
	// paths asks the shell to fall back to its own file completion, which
	// knows about quoting, symlinks and the current directory, and which no
	// list produced here could match.
	paths bool
}

// commandSpec is one invocable command.
type commandSpec struct {
	// words is the invocation, split. Two-word forms are matched before
	// one-word forms, which is what keeps `doctor storage` from being read
	// as `doctor` with a stray argument.
	words []string
	group string
	usage string
	// summary is the one line the help prints and `<command> --help` repeats.
	summary string
	// absence answers why this build cannot run the command. A nil absence
	// means nothing stands in the way.
	absence func() string
	// flags builds the command's flag set bound to throwaway values, for a
	// caller that wants to describe the command rather than run it.
	flags func() *flag.FlagSet
	// hints names the flags whose values complete to something.
	hints map[string]flagHint
	// run executes the command with the arguments that follow its words.
	run func(deps dependencies, args []string, stdout, stderr io.Writer) int
	// hidden keeps a command out of the help. Completion's own entry point is
	// not something a person invokes.
	hidden bool
}

// name answers the invocation as one string, which is how a command is spelled
// in a flag set name, in an error and in the help.
func (spec commandSpec) name() string { return strings.Join(spec.words, " ") }

// commandGroupOrder is the order an operator meets the surface: set up a graph,
// look at it, keep it, wire it into a client, and the two commands only a
// rebuild pipeline needs.
var commandGroupOrder = []string{
	"Getting started",
	"Diagnostics",
	"Maintenance",
	"Integrations",
	"Pipeline",
}

// commandTable is the whole surface. It is a function rather than a package
// variable because several entries close over the hints they need, and because
// a table built on demand cannot be mutated by one caller for another.
func commandTable() []commandSpec {
	return []commandSpec{
		{
			words:   []string{"init"},
			group:   "Getting started",
			usage:   "init [--repository NAME=PATH] [--languages LIST]",
			summary: "Write the configuration and register repositories",
			flags:   func() *flag.FlagSet { var o initOptions; return initFlagSet(&o) },
			hints: map[string]flagHint{
				"config":       {paths: true},
				"repositories": {paths: true},
				"languages":    {values: config.SupportedLanguages},
			},
			run: func(_ dependencies, args []string, stdout, stderr io.Writer) int {
				return runInit(args, stdout, stderr)
			},
		},
		{
			words:   []string{"index", "--full"},
			group:   "Getting started",
			usage:   "index --full [--json]",
			summary: "Index every registered repository and publish a generation",
			flags:   func() *flag.FlagSet { var o indexFullOptions; return indexFullFlagSet(&o) },
			hints: map[string]flagHint{
				"config":       {paths: true},
				"repositories": {paths: true},
			},
			run: func(_ dependencies, args []string, stdout, stderr io.Writer) int {
				return runIndexFull(args, stdout, stderr)
			},
		},
		{
			words:   []string{"serve"},
			group:   "Getting started",
			usage:   "serve",
			summary: "Run the MCP server over stdio",
			flags:   func() *flag.FlagSet { var path string; return serveFlagSet(&path) },
			hints:   map[string]flagHint{"config": {paths: true}},
			// serve, daemon and ui never reach this table's dispatch:
			// main intercepts them before run, because they are the
			// commands that own a signal handler and log structurally
			// for the life of the process. They are declared here so
			// the help and the completion still describe them.
			run: nil,
		},
		{
			words:   []string{"daemon"},
			group:   "Getting started",
			usage:   "daemon [--addr HOST:PORT] [--allow-remote]",
			summary: "Serve MCP to many clients from one process, over HTTP and a unix socket",
			// The daemon's own flag set, not serve's: these two lines are
			// what the global help prints and what completion offers, and
			// serve has neither --addr nor --allow-remote. `daemon --help`
			// was already right, which is exactly why nobody noticed.
			flags: func() *flag.FlagSet {
				var path string
				var options daemonOptions
				return daemonFlagSet(&path, &options)
			},
			hints: map[string]flagHint{"config": {paths: true}},
			run:   nil,
		},
		{
			words:   []string{"ui"},
			group:   "Getting started",
			usage:   "ui [--addr HOST:PORT]",
			summary: "Serve the read-only graph viewer, every interface by default",
			absence: webBundleAbsence,
			flags: func() *flag.FlagSet {
				var path, address string
				return uiFlagSet(&path, &address)
			},
			hints: map[string]flagHint{"config": {paths: true}},
			run:   nil,
		},
		{
			words:   []string{"stop"},
			group:   "Getting started",
			usage:   "stop [--dry-run]",
			summary: "Stop every running serve, daemon and ui of this user",
			flags:   func() *flag.FlagSet { var o stopOptions; return stopFlagSet(&o) },
			run: func(_ dependencies, args []string, stdout, stderr io.Writer) int {
				return runStop(args, stdout, stderr, procstat.List, signalProcess)
			},
		},
		{
			words:   []string{"doctor"},
			group:   "Diagnostics",
			usage:   "doctor",
			summary: "Check configuration, toolchains and the published graph",
			flags:   func() *flag.FlagSet { var o doctorOptions; return doctorFlagSet(&o) },
			hints:   map[string]flagHint{"config": {paths: true}},
			run: func(_ dependencies, args []string, stdout, stderr io.Writer) int {
				return runDoctor(args, stdout, stderr)
			},
		},
		{
			words:   []string{"doctor", "storage"},
			group:   "Diagnostics",
			usage:   "doctor storage --database PATH",
			summary: "Inspect one LadybugDB database file",
			flags:   func() *flag.FlagSet { var o doctorStorageOptions; return doctorStorageFlagSet(&o) },
			hints:   map[string]flagHint{"database": {paths: true}},
			run: func(deps dependencies, args []string, stdout, stderr io.Writer) int {
				return runDoctorStorage(args, stdout, stderr, deps.diagnose)
			},
		},
		{
			words:   []string{"doctor", "graph"},
			group:   "Diagnostics",
			usage:   "doctor graph --database PATH",
			summary: "Validate the canonical graph of a database",
			flags:   func() *flag.FlagSet { var o doctorGraphOptions; return doctorGraphFlagSet(&o) },
			hints:   map[string]flagHint{"database": {paths: true}},
			run: func(deps dependencies, args []string, stdout, stderr io.Writer) int {
				return runDoctorGraph(args, stdout, stderr, deps.verify)
			},
		},
		{
			words:   []string{"graph", "status"},
			group:   "Diagnostics",
			usage:   "graph status --root PATH",
			summary: "Report the active and backup generations",
			flags:   func() *flag.FlagSet { var o graphStatusOptions; return graphStatusFlagSet(&o) },
			hints:   map[string]flagHint{"root": {paths: true}},
			run: func(deps dependencies, args []string, stdout, stderr io.Writer) int {
				return runGraphStatus(args, stdout, stderr, deps.roles)
			},
		},
		{
			words:   []string{"stats"},
			group:   "Diagnostics",
			usage:   "stats [--interval D] [--once] [--json]",
			summary: "Watch what every kivgraph process on this machine costs",
			flags:   func() *flag.FlagSet { var o statsOptions; return statsFlagSet(&o) },
			run: func(_ dependencies, args []string, stdout, stderr io.Writer) int {
				return runStats(args, stdout, stderr, procstat.List)
			},
		},
		{
			words:   []string{"logs"},
			group:   "Diagnostics",
			usage:   "logs [--follow] [--kind K] [--since D] [--json]",
			summary: "Read what this machine indexed, served and answered",
			flags:   func() *flag.FlagSet { var o logsOptions; return logsFlagSet(&o) },
			hints: map[string]flagHint{
				"config": {paths: true},
				"kind":   {values: eventlog.Kinds},
				"tool":   {values: recordedToolNames},
			},
			run: func(_ dependencies, args []string, stdout, stderr io.Writer) int {
				return runLogs(args, stdout, stderr)
			},
		},
		{
			words:   []string{"tool-stats"},
			group:   "Diagnostics",
			usage:   "tool-stats [--tool NAME] [--since D] [--json]",
			summary: "Report the cost and the failures of every tool",
			flags:   func() *flag.FlagSet { var o toolStatsOptions; return toolStatsFlagSet(&o) },
			hints: map[string]flagHint{
				"config": {paths: true},
				"tool":   {values: recordedToolNames},
			},
			run: func(_ dependencies, args []string, stdout, stderr io.Writer) int {
				return runToolStats(args, stdout, stderr)
			},
		},
		{
			words:   []string{"version"},
			group:   "Diagnostics",
			usage:   "version [--json]",
			summary: "Print the release, with --json for full provenance",
			flags:   func() *flag.FlagSet { var o versionOptions; return versionFlagSet(&o) },
			run: func(_ dependencies, args []string, stdout, stderr io.Writer) int {
				return runVersion(args, stdout, stderr)
			},
		},
		{
			words:   []string{"upgrade"},
			group:   "Maintenance",
			usage:   "upgrade",
			summary: "Rebuild the graph after a schema change",
			flags:   func() *flag.FlagSet { var o upgradeOptions; return upgradeFlagSet(&o) },
			hints: map[string]flagHint{
				"config":       {paths: true},
				"repositories": {paths: true},
			},
			run: func(_ dependencies, args []string, stdout, stderr io.Writer) int {
				return runUpgrade(args, stdout, stderr)
			},
		},
		{
			words:   []string{"clean"},
			group:   "Maintenance",
			usage:   "clean [--keep-active] [--yes]",
			summary: "Remove published graph generations",
			flags:   func() *flag.FlagSet { var o cleanOptions; return cleanFlagSet(&o) },
			hints:   map[string]flagHint{"config": {paths: true}},
			run: func(_ dependencies, args []string, stdout, stderr io.Writer) int {
				return runClean(args, stdout, stderr)
			},
		},
		{
			words:   []string{"rollback"},
			group:   "Maintenance",
			usage:   "rollback --root PATH [--generation ID]",
			summary: "Return to the previous generation",
			flags:   func() *flag.FlagSet { var o rollbackOptions; return rollbackFlagSet(&o) },
			hints: map[string]flagHint{
				"root":       {paths: true},
				"generation": {values: publishedGenerationIDs},
			},
			run: func(deps dependencies, args []string, stdout, stderr io.Writer) int {
				return runRollback(args, stdout, stderr, deps.rollback)
			},
		},
		{
			words:   []string{"snapshot"},
			group:   "Maintenance",
			usage:   "snapshot --root PATH [--generation ID]",
			summary: "Rebuild the hot snapshot of a generation",
			flags:   func() *flag.FlagSet { var o snapshotOptions; return snapshotFlagSet(&o) },
			hints: map[string]flagHint{
				"root":       {paths: true},
				"generation": {values: publishedGenerationIDs},
			},
			run: func(deps dependencies, args []string, stdout, stderr io.Writer) int {
				return runSnapshot(args, stdout, stderr, deps.build)
			},
		},
		{
			words:   []string{"update"},
			group:   "Maintenance",
			usage:   "update [--check] [--stop]",
			summary: "Install the latest published release",
			flags:   func() *flag.FlagSet { var o updateOptions; return updateFlagSet(&o) },
			run: func(_ dependencies, args []string, stdout, stderr io.Writer) int {
				return runUpdate(args, stdout, stderr)
			},
		},
		{
			words:   []string{"completion"},
			group:   "Integrations",
			usage:   "completion bash|zsh|fish",
			summary: "Print the shell completion script for one shell",
			run: func(_ dependencies, args []string, stdout, stderr io.Writer) int {
				return runCompletionScript(args, stdout, stderr)
			},
		},
		{
			words:   []string{"__complete"},
			hidden:  true,
			summary: "Answer the candidates for a partially typed command line",
			run: func(_ dependencies, args []string, stdout, stderr io.Writer) int {
				return runComplete(args, stdout, stderr)
			},
		},
		{
			words:   []string{"rebuild"},
			group:   "Pipeline",
			usage:   "rebuild --facts PATH --root PATH ...",
			summary: "Publish a generation from a fact set",
			flags:   func() *flag.FlagSet { var o rebuildOptions; return rebuildFlagSet(&o) },
			hints: map[string]flagHint{
				"facts":      {paths: true},
				"root":       {paths: true},
				"generation": {values: publishedGenerationIDs},
			},
			run: func(deps dependencies, args []string, stdout, stderr io.Writer) int {
				return runRebuild(args, stdout, stderr, deps.rebuilder)
			},
		},
		{
			words:   []string{"benchmark", "generate-graph"},
			group:   "Pipeline",
			usage:   "benchmark generate-graph",
			summary: "Generate a synthetic corpus",
			flags:   func() *flag.FlagSet { var o generateGraphOptions; return generateGraphFlagSet(&o) },
			hints:   map[string]flagHint{"output": {paths: true}},
			run: func(_ dependencies, args []string, stdout, stderr io.Writer) int {
				return runGenerateGraph(args, stdout, stderr)
			},
		},
	}
}

// integrationCommands are the mcp and skill operations. They are a family
// rather than seven hand-written entries: the two kinds accept the same
// operations with the same flags, and only the writers take --dry-run and
// --force.
func integrationCommands() []commandSpec {
	specs := make([]commandSpec, 0, 6)
	for _, kind := range []string{"mcp", "skill"} {
		for _, operation := range []string{"install", "status", "remove"} {
			specs = append(specs, integrationCommand(kind, operation))
		}
	}
	return specs
}

func integrationCommand(kind, operation string) commandSpec {
	writes := operation != "status"
	// Only the MCP side has a transport to choose, so only it takes --daemon.
	endpoint := kind == "mcp"
	usage := kind + " " + operation + " [--scope user|project]"
	if endpoint {
		usage = kind + " " + operation + " [--scope user|project] [--daemon]"
	}
	summary := "Detect and register one or more MCP clients"
	switch {
	case kind == "mcp" && operation == "status":
		usage = "mcp status --target TARGET [--scope user|project] [--daemon]"
		summary = "Inspect a client MCP registration"
	case kind == "mcp" && operation == "remove":
		usage = "mcp remove --target TARGET [--scope user|project] [--daemon]"
		summary = "Remove only Kivgraph's MCP registration"
	case kind == "skill" && operation == "install":
		summary = "Detect and install the Agent Skill in one or more clients"
	case kind == "skill" && operation == "status":
		usage = "skill status --target TARGET [--scope user|project]"
		summary = "Inspect the installed Agent Skill"
	case kind == "skill" && operation == "remove":
		usage = "skill remove --target TARGET [--scope user|project]"
		summary = "Remove only Kivgraph's Agent Skill"
	}
	return commandSpec{
		words:   []string{kind, operation},
		group:   "Integrations",
		usage:   usage,
		summary: summary,
		flags: func() *flag.FlagSet {
			var options integrationOptions
			return integrationFlagSet(kind+" "+operation, &options, io.Discard, writes, endpoint)
		},
		hints: map[string]flagHint{
			"target": {values: integrationTargetNames},
			"scope":  {values: integrationScopeNames},
		},
		run: func(_ dependencies, args []string, stdout, stderr io.Writer) int {
			if kind == "mcp" {
				return runMCPCommand(append([]string{operation}, args...), stdout, stderr)
			}
			return runSkillCommand(append([]string{operation}, args...), stdout, stderr)
		},
	}
}

// allCommands is the table every reader walks, longest invocation first so a
// prefix match cannot claim a command that has a more specific form.
func allCommands() []commandSpec {
	specs := append(commandTable(), integrationCommands()...)
	// A stable sort on word count alone keeps the declared order within each
	// length, which is the order the help prints.
	for index := 1; index < len(specs); index++ {
		for position := index; position > 0 && len(specs[position].words) > len(specs[position-1].words); position-- {
			specs[position], specs[position-1] = specs[position-1], specs[position]
		}
	}
	return specs
}

// findCommand answers the command the arguments name and how many words it
// consumed.
func findCommand(args []string) (commandSpec, int, bool) {
	for _, spec := range allCommands() {
		if len(args) < len(spec.words) {
			continue
		}
		matched := true
		for index, word := range spec.words {
			if args[index] != word {
				matched = false
				break
			}
		}
		if matched {
			return spec, len(spec.words), true
		}
	}
	return commandSpec{}, 0, false
}

// versionOptions carries the flags of `kivgraph version`.
type versionOptions struct {
	JSONOutput bool
}

// versionFlagSet declares them in one place. version parses its own arguments
// rather than handing them straight to the flag package, but the declaration
// still lives here so the help and the completion can describe --json instead
// of pretending the command takes nothing.
func versionFlagSet(options *versionOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&options.JSONOutput, "json", false, "print full provenance as JSON")
	return flags
}

// runVersion keeps the shape `version` has always had: the bare form prints the
// release, `--json` prints the provenance, and anything else is not a version
// invocation at all. That third case reports an unknown command rather than an
// unknown flag, so the guard comes before the parse.
func runVersion(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 || (len(args) == 1 && args[0] != "--json") {
		writeUsageError(stderr, "kivgraph", fmt.Sprintf("unknown command %q", "version"))
		return 2
	}
	var options versionOptions
	if err := versionFlagSet(&options).Parse(args); err != nil {
		writeCommandError(stderr, "version: %v", err)
		return 2
	}
	if options.JSONOutput {
		return runVersionJSON(stdout, stderr)
	}
	fmt.Fprintln(stdout, version.Value)
	return 0
}

// recordedToolNames answers the tools that appear in the durable event log,
// which is a better answer than a hardcoded list: it names the tools this
// installation has actually been asked for, newest first, and it stays right
// when the tool surface changes.
func recordedToolNames() []string {
	configuration, err := config.LoadConfig("")
	if err != nil {
		return nil
	}
	events, err := eventlog.Read(configuration.Logging.EventLogPath,
		eventlog.ReadOptions{Kinds: []eventlog.Kind{eventlog.KindTool}})
	if err != nil {
		return nil
	}
	names := make([]string, 0, 8)
	seen := make(map[string]bool)
	for _, entry := range eventlog.Summarize(events).Tools {
		if entry.Tool == "" || seen[entry.Tool] {
			continue
		}
		seen[entry.Tool] = true
		names = append(names, entry.Tool)
	}
	return names
}

// integrationTargetNames answers the clients this machine could register,
// which is the whole point of completing this flag: the vocabulary is long,
// hyphenated and easy to misspell.
func integrationTargetNames() []string {
	targets := integrations.KnownTargets()
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, string(target))
	}
	return names
}

func integrationScopeNames() []string {
	return []string{integrations.ScopeUser, integrations.ScopeProject}
}

// updateShellNames is the vocabulary of `completion`.
func updateShellNames() []string { return []string{"bash", "zsh", "fish"} }
