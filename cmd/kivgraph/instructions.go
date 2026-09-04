package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Luqueee/kivgraph/internal/integrations"
)

// instructionsOptions carries the flags for the project context installer.
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
		"override the agent's project context file")
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

	projectRoot, err := currentProjectRoot()
	if err != nil {
		writeCommandError(stderr, "instructions install: %v", err)
		return 1
	}
	manager, err := integrations.New(integrations.Options{ProjectDir: projectRoot})
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
			input, stdout, manager, "instructions", integrations.ScopeProject)
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
		plan, err := manager.InstallInstructions(destination.file, options.DryRun, options.Force)
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
	file  string
	agent string
}

func (destination instructionsDestination) selector() string {
	if destination.agent != "" {
		return "--agent " + destination.agent
	}
	return "--file " + destination.file
}

func instructionsDestinations(
	manager integrations.Manager,
	options instructionsOptions,
	targets []integrations.Target,
) ([]instructionsDestination, error) {
	if options.File != "" {
		return []instructionsDestination{{file: options.File}}, nil
	}
	if options.Agent != "" {
		file, err := integrations.InstructionsFileForTarget(integrations.Target(options.Agent))
		if err != nil {
			return nil, err
		}
		if _, err := manager.InstructionsDestination(file); err != nil {
			return nil, err
		}
		return []instructionsDestination{{file: file, agent: options.Agent}}, nil
	}

	destinations := make([]instructionsDestination, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		file, err := integrations.InstructionsFileForTarget(target)
		if err != nil {
			return nil, err
		}
		path, err := manager.InstructionsDestination(file)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		destinations = append(destinations, instructionsDestination{
			file:  file,
			agent: string(target),
		})
	}
	return destinations, nil
}

func writeInstructionsPlan(stdout io.Writer, plan integrations.InstructionsPlan, agent string) {
	selector := fmt.Sprintf("--file %s", plan.File)
	if agent != "" {
		selector = fmt.Sprintf("--agent %s", agent)
	}
	message := fmt.Sprintf("instructions install %s: %s (%s) — %s",
		selector, plan.Status, plan.Path, plan.Detail)
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
	fmt.Fprintf(stdout, "%sManage project instructions loaded by coding agents%s\n\n", paint.dim, paint.reset)
	fmt.Fprintln(stdout, "  instructions install [--agent AGENT] [--file AGENTS.md|CLAUDE.md|.omp/AGENTS.md] [--dry-run] [--force]")
	fmt.Fprintln(stdout, "  without --agent or --file, choose one or more coding agents interactively")
	fmt.Fprintln(stdout, "\nSupported agents: "+strings.Join(instructionsAgentNames(), ", "))
	fmt.Fprintln(stdout, "Supported files: "+strings.Join(instructionsFileNames(), ", "))
}
