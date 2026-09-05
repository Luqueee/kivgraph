package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/integrations"
)

// configureOptions carries the flags for the first-run integration wizard.
// Targets are repeatable so an installer or a script can skip the selector
// without having to encode terminal input.
type configureOptions struct {
	Targets stringList
	DryRun  bool
	Force   bool
	Daemon  bool
	Stdio   bool
}

func configureFlagSet(options *configureOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("configure", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Var(&options.Targets, "target", "coding agent target (repeatable; omit to choose interactively)")
	flags.BoolVar(&options.DryRun, "dry-run", false, "show the changes without writing")
	flags.BoolVar(&options.Force, "force", false, "replace all Kivgraph-owned entries, including matching ones")
	flags.BoolVar(&options.Daemon, "daemon", false, "require and install the supervised daemon without asking")
	flags.BoolVar(&options.Stdio, "stdio", false, "write per-client serve entries instead of using the daemon")
	return flags
}

func configureTargetNames() []string {
	names := []string{"claude", "omp"}
	for _, target := range integrations.KnownTargets() {
		names = append(names, string(target))
	}
	return names
}

func runConfigure(args []string, stdout, stderr io.Writer) int {
	return runConfigureWithInput(args, os.Stdin, stdout, stderr)
}

func runConfigureWithInput(args []string, input io.Reader, stdout, stderr io.Writer) int {
	return runConfigureWithResolver(args, input, stdout, stderr, integrationManagerOptionsWithInput)
}

// configureManagerOptionsResolver is the daemon boundary of the wizard. It
// keeps the filesystem integrations independent from supervisor failures and
// lets the command continue with skills, hooks and instructions when MCP
// transport setup is unavailable.
type configureManagerOptionsResolver func(integrationOptions, bool, io.Reader, io.Writer) (integrations.Options, error)

func runConfigureWithResolver(
	args []string,
	input io.Reader,
	stdout, stderr io.Writer,
	resolve configureManagerOptionsResolver,
) int {
	return runConfigureWithResolverAndPrompt(args, input, stdout, stderr, resolve, promptYes)
}

type configureReplacementPrompt func(io.Reader, io.Writer, string) bool

func runConfigureWithResolverAndPrompt(
	args []string,
	input io.Reader,
	stdout, stderr io.Writer,
	resolve configureManagerOptionsResolver,
	prompt configureReplacementPrompt,
) int {
	var options configureOptions
	flags := configureFlagSet(&options)
	if parsed, code := parseCommandFlags("configure", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "configure: unexpected arguments: %v", flags.Args())
		return 2
	}
	if options.Daemon && options.Stdio {
		writeCommandError(stderr, "configure: --stdio and --daemon ask for opposite entries: pass one")
		return 2
	}

	detector, err := integrations.New(integrations.Options{})
	if err != nil {
		writeCommandError(stderr, "configure: detect coding agents: %v", err)
		return 1
	}

	selectedTargets, err := configureTargets(input, stdout, detector, options.Targets)
	if err != nil {
		writeCommandError(stderr, "configure: %v", err)
		return 2
	}
	report := newConfigureReport(selectedTargets, options.DryRun)

	if !options.DryRun {
		result, initErr := config.Initialize(config.InitOptions{})
		if initErr != nil {
			writeCommandError(stderr, "configure: initialize Kivgraph: %v", initErr)
			return 1
		}
		report.initialized = result.ConfigCreated || result.RepositoriesCreated
	}

	integration := integrationOptions{
		Scope:  integrations.ScopeUser,
		DryRun: options.DryRun,
		Force:  options.Force,
		Daemon: options.Daemon,
		Stdio:  options.Stdio,
	}
	managerOptions, err := resolve(
		integration, !options.DryRun, input, stdout)
	mcpFailed := false
	if err != nil {
		writeCommandError(stderr, "configure mcp: %v", err)
		mcpFailed = true
		managerOptions = integrations.Options{}
	}
	report.transport = configureTransport(managerOptions, options, mcpFailed)
	manager, err := integrations.New(managerOptions)
	if err != nil {
		writeCommandError(stderr, "configure: create integration manager: %v", err)
		return 1
	}
	if !options.DryRun && !options.Force && prompt != nil &&
		configureHasManagedComponents(manager, selectedTargets, !mcpFailed) {
		options.Force = prompt(input, stdout,
			"Some selected Kivgraph components are already configured. Replace all selected components?")
	}

	failed := mcpFailed
	if !mcpFailed {
		for _, target := range selectedTargets {
			plan, installErr := manager.InstallMCP(target, integrations.ScopeUser, options.DryRun, options.Force)
			if installErr != nil {
				writeCommandError(stderr, "configure mcp --target %s: %v", target, installErr)
				report.failed(target, configureSurfaceMCP)
				failed = true
				continue
			}
			report.plan(target, configureSurfaceMCP, plan)
		}
	} else {
		for _, target := range selectedTargets {
			report.failed(target, configureSurfaceMCP)
		}
	}

	for _, target := range selectedTargets {
		if !containsIntegrationTarget(integrations.SkillTargets(), target) {
			report.notSupported(target, configureSurfaceSkill)
			continue
		}
		plan, installErr := manager.InstallSkill(target, integrations.ScopeUser, options.DryRun, options.Force)
		if installErr != nil {
			writeCommandError(stderr, "configure skill --target %s: %v", target, installErr)
			report.failed(target, configureSurfaceSkill)
			failed = true
			continue
		}
		report.plan(target, configureSurfaceSkill, plan)
	}

	if installConfigureHooks(
		manager, selectedTargets, integrations.HookTargets(), options.DryRun, options.Force, report, stderr) {
		failed = true
	}

	instructionTargets := supportedConfigureTargets(selectedTargets, integrations.InstructionsTargets())
	for _, target := range selectedTargets {
		if !containsIntegrationTarget(instructionTargets, target) {
			report.notSupported(target, configureSurfaceInstructions)
		}
	}
	if len(instructionTargets) == 0 {
	} else {
		destinations, destinationErr := instructionsDestinations(manager, instructionsOptions{}, instructionTargets)
		if destinationErr != nil {
			writeCommandError(stderr, "configure instructions: %v", destinationErr)
			for _, target := range instructionTargets {
				report.failed(target, configureSurfaceInstructions)
			}
			failed = true
		} else {
			for _, destination := range destinations {
				plan, installErr := manager.InstallInstructionsForTarget(destination.target, options.DryRun, options.Force)
				if installErr != nil {
					writeCommandError(stderr, "configure instructions %s: %v", destination.selector(), installErr)
					if reportErr := report.instructionsFailed(manager, instructionTargets, destination.target); reportErr != nil {
						writeCommandError(stderr, "configure instructions %s: %v", destination.selector(), reportErr)
						report.failed(destination.target, configureSurfaceInstructions)
					}
					failed = true
					continue
				}
				if recordErr := report.instructions(manager, instructionTargets, destination.target, plan); recordErr != nil {
					writeCommandError(stderr, "configure instructions %s: %v", destination.selector(), recordErr)
					report.failed(destination.target, configureSurfaceInstructions)
					failed = true
				}
			}
		}
	}
	writeConfigureReport(stdout, report)

	if failed {
		return 1
	}
	return 0
}

func configureHasManagedComponents(
	manager integrations.Manager,
	targets []integrations.Target,
	mcpAvailable bool,
) bool {
	for _, target := range targets {
		if mcpAvailable {
			plan, err := manager.StatusMCP(target, integrations.ScopeUser)
			if err == nil && plan.Status == "managed" {
				return true
			}
		}
		if containsIntegrationTarget(integrations.SkillTargets(), target) {
			plan, err := manager.StatusSkill(target, integrations.ScopeUser)
			if err == nil && plan.Status == "managed" {
				return true
			}
		}
		if containsIntegrationTarget(integrations.HookTargets(), target) {
			plan, err := manager.StatusHook(target, integrations.ScopeUser)
			if err == nil && plan.Status == "managed" {
				return true
			}
		}
		if containsIntegrationTarget(integrations.InstructionsTargets(), target) {
			plan, err := manager.InstallInstructionsForTarget(target, true, false)
			if err == nil && plan.Status == "managed" {
				return true
			}
		}
	}
	return false
}

func installConfigureHooks(
	manager integrations.Manager,
	selectedTargets, supportedTargets []integrations.Target,
	dryRun, force bool,
	report *configureReport,
	stderr io.Writer,
) (failed bool) {
	for _, target := range selectedTargets {
		if !containsIntegrationTarget(supportedTargets, target) {
			report.notSupported(target, configureSurfaceHook)
			continue
		}
		plan, installErr := manager.InstallHook(target, integrations.ScopeUser, dryRun, force)
		if installErr != nil {
			writeCommandError(stderr, "configure hook --target %s: %v", target, installErr)
			report.failed(target, configureSurfaceHook)
			failed = true
			continue
		}
		report.plan(target, configureSurfaceHook, plan)
	}
	return failed
}

func configureTargets(
	input io.Reader,
	stdout io.Writer,
	detector integrations.Manager,
	explicit []string,
) ([]integrations.Target, error) {
	if len(explicit) == 0 {
		return selectIntegrationTargets(input, stdout, detector, "mcp", integrations.ScopeUser)
	}

	selected := make([]integrations.Target, 0, len(explicit))
	seen := make(map[integrations.Target]struct{}, len(explicit))
	for _, value := range explicit {
		target, ok := configureTarget(value)
		if !ok {
			return nil, fmt.Errorf("unsupported target %q (want %s)", value, strings.Join(configureTargetNames(), ", "))
		}
		if _, exists := seen[target]; exists {
			return nil, fmt.Errorf("target %q was specified more than once", value)
		}
		seen[target] = struct{}{}
		selected = append(selected, target)
	}
	return selected, nil
}

func configureTarget(value string) (integrations.Target, bool) {
	switch value {
	case "claude":
		return integrations.TargetClaudeCode, true
	case "omp":
		return integrations.TargetOhMyPi, true
	}
	for _, target := range integrations.KnownTargets() {
		if value == string(target) {
			return target, true
		}
	}
	return "", false
}

func containsIntegrationTarget(targets []integrations.Target, wanted integrations.Target) bool {
	for _, target := range targets {
		if target == wanted {
			return true
		}
	}
	return false
}

func supportedConfigureTargets(targets, supported []integrations.Target) []integrations.Target {
	filtered := make([]integrations.Target, 0, len(targets))
	for _, target := range targets {
		if containsIntegrationTarget(supported, target) {
			filtered = append(filtered, target)
		}
	}
	return filtered
}
