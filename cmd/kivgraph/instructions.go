package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Luqueee/kivgraph/internal/integrations"
)

// instructionsOptions carries the flags for the user-context installer.
type instructionsOptions struct {
	Agent  string
	File   string
	DryRun bool
	Force  bool
}

func instructionsFlagSet(options *instructionsOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("instructions install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.Agent, "agent", "", "coding agent: use --help for supported agents")
	flags.StringVar(&options.File, "file", "",
		"deprecated global file selector; prefer --agent")
	flags.BoolVar(&options.DryRun, "dry-run", false, "show the change without writing")
	flags.BoolVar(&options.Force, "force", false, "replace an edited Kivgraph block")
	return flags
}

func instructionsFileNames() []string {
	return []string{
		integrations.InstructionsFileAgents,
		integrations.InstructionsFileClaude,
		integrations.InstructionsFileOhMyPi,
	}
}

func instructionsAgentNames() []string {
	return integrations.InstructionsAgentNames()
}

func runInstructionsCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || helpRequested(args) {
		writeInstructionsHelp(stdout)
		return 0
	}
	writeCommandError(stderr, "instructions: unknown operation %q", args[0])
	return 2
}

func runInstructionsInstall(args []string, stdout, stderr io.Writer) int {
	return runInstructionsInstallWithInput(args, os.Stdin, stdout, stderr)
}

func runInstructionsInstallWithInput(args []string, input io.Reader, stdout, stderr io.Writer) int {
	var options instructionsOptions
	flags := instructionsFlagSet(&options)
	if parsed, code := parseCommandFlags("instructions install", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "instructions install: unexpected arguments: %v", flags.Args())
		return 2
	}

	manager, err := integrations.New(integrations.Options{})
	if err != nil {
		writeCommandError(stderr, "instructions install: %v", err)
		return 1
	}
	if options.Agent != "" && options.File != "" {
		writeCommandError(stderr, "instructions install: --agent and --file cannot be combined")
		return 2
	}

	selectedTargets := []integrations.Target{}
	if options.Agent == "" && options.File == "" {
		selectedTargets, err = selectIntegrationTargets(
			input, stdout, manager, "instructions", integrations.ScopeUser)
		if err != nil {
			writeCommandError(stderr, "instructions install: %v", err)
			return 2
		}
	}
	destinations, err := instructionsDestinations(manager, options, selectedTargets)
	if err != nil {
		writeCommandError(stderr, "instructions install: %v", err)
		return 1
	}

	failed := false
	for _, destination := range destinations {
		plan, err := manager.InstallInstructionsForTarget(destination.target, options.DryRun, options.Force)
		if err != nil {
			writeCommandError(stderr, "instructions install %s: %v", destination.selector(), err)
			failed = true
			continue
		}
		writeInstructionsPlan(stdout, plan, destination.agent)
	}
	if failed {
		return 1
	}
	return 0
}

type instructionsDestination struct {
	target integrations.Target
	agent  string
	file   string
}

func (destination instructionsDestination) selector() string {
	if destination.agent != "" {
		return "--agent " + destination.agent
	}
	if destination.file != "" {
		return "--file " + destination.file
	}
	return "--agent " + string(destination.target)
}

func instructionsDestinations(
	manager integrations.Manager,
	options instructionsOptions,
	targets []integrations.Target,
) ([]instructionsDestination, error) {
	if options.File != "" {
		targets, err := instructionsTargetsForFile(options.File)
		if err != nil {
			return nil, err
		}
		destinations, err := instructionDestinationsForTargets(manager, targets)
		if err != nil {
			return nil, err
		}
		for index := range destinations {
			destinations[index].agent = ""
			destinations[index].file = options.File
		}
		return destinations, nil
	}
	if options.Agent != "" {
		target := integrations.Target(options.Agent)
		if _, _, err := manager.InstructionsDestinationForTarget(target); err != nil {
			return nil, err
		}
		return []instructionsDestination{{target: target, agent: options.Agent}}, nil
	}
	return instructionDestinationsForTargets(manager, targets)
}

func instructionsTargetsForFile(file string) ([]integrations.Target, error) {
	targets := make([]integrations.Target, 0, len(integrations.InstructionsTargets()))
	for _, target := range integrations.InstructionsTargets() {
		name, err := integrations.InstructionsFileForTarget(target)
		if err != nil {
			return nil, err
		}
		if name == file {
			targets = append(targets, target)
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("unsupported instructions file %q (want %s)", file,
			strings.Join(instructionsFileNames(), ", "))
	}
	return targets, nil
}

func instructionDestinationsForTargets(
	manager integrations.Manager,
	targets []integrations.Target,
) ([]instructionsDestination, error) {
	destinations := make([]instructionsDestination, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		_, path, err := manager.InstructionsDestinationForTarget(target)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		destinations = append(destinations, instructionsDestination{
			target: target,
			agent:  string(target),
		})
	}
	return destinations, nil
}

func writeInstructionsPlan(stdout io.Writer, plan integrations.InstructionsPlan, agent string) {
	selector := fmt.Sprintf("--file %s", plan.File)
	if agent != "" {
		selector = fmt.Sprintf("--agent %s", agent)
	}
	location := plan.Path
	if plan.SourcePath != "" {
		location += " ← " + plan.SourcePath
	}
	message := fmt.Sprintf("instructions install %s: %s (%s) — %s",
		selector, plan.Status, location, plan.Detail)
	switch plan.Status {
	case "installed":
		writeSuccess(stdout, "%s", message)
	case "would-install":
		writeInfo(stdout, "%s", message)
	default:
		writeInfo(stdout, "%s", message)
	}
}

func writeInstructionsHelp(stdout io.Writer) {
	paint := styleFor(stdout)
	fmt.Fprintf(stdout, "%sUsage%s: kivgraph instructions <operation> [flags]\n\n", paint.bold, paint.reset)
	fmt.Fprintf(stdout, "%sManage user-level instructions loaded by coding agents%s\n\n", paint.dim, paint.reset)
	fmt.Fprintln(stdout, "  instructions install [--agent AGENT] [--file AGENTS.md|CLAUDE.md|.omp/AGENTS.md] [--dry-run] [--force]")
	fmt.Fprintln(stdout, "  without --agent or --file, choose one or more coding agents interactively")
	fmt.Fprintln(stdout, "  --file is kept for compatibility and selects every matching user-level client")
	fmt.Fprintln(stdout, "\nSupported agents: "+strings.Join(instructionsAgentNames(), ", "))
	fmt.Fprintln(stdout, "Supported files: "+strings.Join(instructionsFileNames(), ", "))
}
