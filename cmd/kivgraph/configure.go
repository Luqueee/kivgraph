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
	flags.BoolVar(&options.Force, "force", false, "replace incompatible or edited Kivgraph-owned entries")
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

	if !options.DryRun {
		result, initErr := config.Initialize(config.InitOptions{})
		if initErr != nil {
			writeCommandError(stderr, "configure: initialize Kivgraph: %v", initErr)
			return 1
		}
		if result.ConfigCreated || result.RepositoriesCreated {
			writeInfo(stdout, "configure: initialized Kivgraph configuration")
		}
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
	manager, err := integrations.New(managerOptions)
	if err != nil {
		writeCommandError(stderr, "configure: create integration manager: %v", err)
		return 1
	}

	failed := mcpFailed
	if !mcpFailed {
		for _, target := range selectedTargets {
			plan, installErr := manager.InstallMCP(target, integrations.ScopeUser, options.DryRun, options.Force)
			if installErr != nil {
				writeCommandError(stderr, "configure mcp --target %s: %v", target, installErr)
				failed = true
				continue
			}
			writeIntegrationPlan(stdout, "mcp", plan)
		}
	}

	for _, target := range selectedTargets {
		if !containsIntegrationTarget(integrations.SkillTargets(), target) {
			writeInfo(stdout, "configure: skill skipped for %s: this client has no local skill directory", integrationTargetLabel(target))
			continue
		}
		plan, installErr := manager.InstallSkill(target, integrations.ScopeUser, options.DryRun, options.Force)
		if installErr != nil {
			writeCommandError(stderr, "configure skill --target %s: %v", target, installErr)
			failed = true
			continue
		}
		writeIntegrationPlan(stdout, "skill", plan)
	}

	if installConfigureHooks(
		manager, selectedTargets, integrations.HookTargets(), options.DryRun, options.Force, stdout, stderr) {
		failed = true
	}

	instructionTargets := supportedConfigureTargets(selectedTargets, integrations.InstructionsTargets())
	if len(instructionTargets) == 0 {
		writeInfo(stdout, "configure: instructions skipped: the selected clients have no user instruction file")
	} else {
		destinations, destinationErr := instructionsDestinations(manager, instructionsOptions{}, instructionTargets)
		if destinationErr != nil {
			writeCommandError(stderr, "configure instructions: %v", destinationErr)
			failed = true
		} else {
			for _, destination := range destinations {
				plan, installErr := manager.InstallInstructionsForTarget(destination.target, options.DryRun, options.Force)
				if installErr != nil {
					writeCommandError(stderr, "configure instructions %s: %v", destination.selector(), installErr)
					failed = true
					continue
				}
				writeInstructionsPlan(stdout, plan, destination.agent)
			}
		}
	}

	if failed {
		return 1
	}
	return 0
}

func installConfigureHooks(
	manager integrations.Manager,
	selectedTargets, supportedTargets []integrations.Target,
	dryRun, force bool,
	stdout, stderr io.Writer,
) (failed bool) {
	for _, target := range selectedTargets {
		if !containsIntegrationTarget(supportedTargets, target) {
			writeInfo(stdout, "configure: hook skipped for %s: this client has no hook integration", integrationTargetLabel(target))
			continue
		}
		plan, installErr := manager.InstallHook(target, integrations.ScopeUser, dryRun, force)
		if installErr != nil {
			writeCommandError(stderr, "configure hook --target %s: %v", target, installErr)
			failed = true
			continue
		}
		writeIntegrationPlan(stdout, "hook", plan)
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
