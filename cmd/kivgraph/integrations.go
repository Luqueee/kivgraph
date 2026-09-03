package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Luqueee/kivgraph/internal/integrations"
)

func runMCPCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || helpRequested(args) {
		writeIntegrationHelp(stdout, "mcp", "Manage local MCP client registrations", []string{
			"mcp install [--target TARGET] [--scope user|project] [--stdio|--daemon] [--dry-run] [--force]",
			"mcp status --target TARGET [--scope user|project] [--stdio|--daemon]",
			"mcp remove --target TARGET [--scope user|project] [--stdio|--daemon] [--dry-run] [--force]",
		}, integrations.KnownTargets())
		return 0
	}
	switch args[0] {
	case "install":
		return runMCPChange(integrations.ActionInstall, args[1:], stdout, stderr)
	case "remove":
		return runMCPChange(integrations.ActionRemove, args[1:], stdout, stderr)
	case "status":
		return runMCPStatus(args[1:], stdout, stderr)
	default:
		writeCommandError(stderr, "mcp: unknown operation %q", args[0])
		return 2
	}
}

func runSkillCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || helpRequested(args) {
		writeIntegrationHelp(stdout, "skill", "Manage the Kivgraph Agent Skill", []string{
			"skill install [--target TARGET] [--scope user|project] [--dry-run] [--force]",
			"skill status --target TARGET [--scope user|project]",
			"skill remove --target TARGET [--scope user|project] [--dry-run] [--force]",
		}, integrations.SkillTargets())
		return 0
	}
	switch args[0] {
	case "install":
		return runSkillChange(integrations.ActionInstall, args[1:], stdout, stderr)
	case "remove":
		return runSkillChange(integrations.ActionRemove, args[1:], stdout, stderr)
	case "status":
		return runSkillStatus(args[1:], stdout, stderr)
	default:
		writeCommandError(stderr, "skill: unknown operation %q", args[0])
		return 2
	}
}

func runHookCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || helpRequested(args) {
		writeIntegrationHelp(stdout, "hook", "Manage the pre-tool-use gate", []string{
			"hook install [--target TARGET] [--scope user|project] [--dry-run] [--force]",
			"hook status --target TARGET [--scope user|project]",
			"hook remove --target TARGET [--scope user|project] [--dry-run] [--force]",
		}, integrations.HookTargets())
		return 0
	}
	switch args[0] {
	case "install":
		return runHookChange(integrations.ActionInstall, args[1:], stdout, stderr)
	case "remove":
		return runHookChange(integrations.ActionRemove, args[1:], stdout, stderr)
	case "status":
		return runHookStatus(args[1:], stdout, stderr)
	default:
		writeCommandError(stderr, "hook: unknown operation %q", args[0])
		return 2
	}
}

func runMCPChange(action integrations.Action, args []string, stdout, stderr io.Writer) int {
	return runMCPChangeWithInput(action, args, os.Stdin, stdout, stderr)
}

func runMCPChangeWithInput(action integrations.Action, args []string, input io.Reader, stdout, stderr io.Writer) int {
	options, ok := parseIntegrationFlags("mcp "+string(action), args, stdout, stderr, true, true)
	if !ok {
		return 2
	}
	target, scope, dryRun, force := options.Target, options.Scope, options.DryRun, options.Force
	selectedTargets := []integrations.Target{}
	if target != "" {
		selectedTargets = append(selectedTargets, integrations.Target(target))
	} else if action == integrations.ActionInstall {
		// Detection has no transport side effect. Select the clients first so a
		// later consent prompt is about a real install, not a background daemon
		// started before the operator has chosen any target.
		detector, err := integrations.New(integrations.Options{})
		if err != nil {
			writeCommandError(stderr, "mcp %s: %v", action, err)
			return 1
		}
		selectedTargets, err = selectIntegrationTargets(input, stdout, detector, "mcp", integrations.Scope(scope))
		if err != nil {
			writeCommandError(stderr, "mcp %s: %v", action, err)
			return 2
		}
	} else {
		writeCommandError(stderr, "mcp %s: --target is required", action)
		return 2
	}
	// Only an install may bring a daemon up. A remove deletes our entry and
	// needs no endpoint to do it: starting a daemon in order to unregister from
	// it would be the opposite of what was asked.
	managerOptions, err := integrationManagerOptionsWithInput(options, action == integrations.ActionInstall, input, stdout)
	if err != nil {
		writeCommandError(stderr, "mcp %s: %v", action, err)
		return 1
	}
	manager, err := integrations.New(managerOptions)
	if err != nil {
		writeCommandError(stderr, "mcp %s: %v", action, err)
		return 1
	}

	failed := false
	for _, selectedTarget := range selectedTargets {
		var plan integrations.Plan
		switch action {
		case integrations.ActionInstall:
			plan, err = manager.InstallMCP(selectedTarget, integrations.Scope(scope), dryRun, force)
		case integrations.ActionRemove:
			plan, err = manager.RemoveMCP(selectedTarget, integrations.Scope(scope), dryRun, force)
		default:
			err = fmt.Errorf("unsupported MCP operation %q", action)
		}
		if err != nil {
			writeCommandError(stderr, "mcp %s --target %s: %v", action, selectedTarget, err)
			failed = true
			continue
		}
		writeIntegrationPlan(stdout, "mcp", plan)
	}
	if failed {
		return 1
	}
	return 0
}

func runMCPStatus(args []string, stdout, stderr io.Writer) int {
	options, ok := parseIntegrationFlags("mcp status", args, stdout, stderr, false, true)
	if !ok {
		return 2
	}
	target, scope := options.Target, options.Scope
	if target == "" {
		writeCommandError(stderr, "mcp status: --target is required")
		return 2
	}
	managerOptions, err := integrationManagerOptions(options, false, stdout)
	if err != nil {
		writeCommandError(stderr, "mcp status: %v", err)
		return 1
	}
	manager, err := integrations.New(managerOptions)
	if err != nil {
		writeCommandError(stderr, "mcp status: %v", err)
		return 1
	}
	plan, err := manager.StatusMCP(integrations.Target(target), integrations.Scope(scope))
	if err != nil {
		writeCommandError(stderr, "mcp status: %v", err)
		return 1
	}
	writeIntegrationPlan(stdout, "mcp", plan)
	return 0
}

// clientIntegration is one of the two integrations that write a client-native
// file: the Agent Skill and the pre-tool-use gate.
//
// They are a struct of three methods rather than two copies of the same
// function because that is all that ever differed between them -- the flags,
// the target selection, the per-target error handling and the plan printing
// were identical, and a third copy for the gate would have been the point at
// which the three started to drift.
type clientIntegration struct {
	kind    string
	install func(integrations.Manager, integrations.Target, integrations.Scope, bool, bool) (integrations.Plan, error)
	remove  func(integrations.Manager, integrations.Target, integrations.Scope, bool, bool) (integrations.Plan, error)
	status  func(integrations.Manager, integrations.Target, integrations.Scope) (integrations.Plan, error)
}

func skillIntegration() clientIntegration {
	return clientIntegration{
		kind:    "skill",
		install: integrations.Manager.InstallSkill,
		remove:  integrations.Manager.RemoveSkill,
		status:  integrations.Manager.StatusSkill,
	}
}

func hookIntegration() clientIntegration {
	return clientIntegration{
		kind:    "hook",
		install: integrations.Manager.InstallHook,
		remove:  integrations.Manager.RemoveHook,
		status:  integrations.Manager.StatusHook,
	}
}

func runSkillChange(action integrations.Action, args []string, stdout, stderr io.Writer) int {
	return runClientChangeWithInput(skillIntegration(), action, args, os.Stdin, stdout, stderr)
}

func runHookChange(action integrations.Action, args []string, stdout, stderr io.Writer) int {
	return runClientChangeWithInput(hookIntegration(), action, args, os.Stdin, stdout, stderr)
}

func runSkillChangeWithInput(action integrations.Action, args []string, input io.Reader, stdout, stderr io.Writer) int {
	return runClientChangeWithInput(skillIntegration(), action, args, input, stdout, stderr)
}

func runClientChangeWithInput(integration clientIntegration, action integrations.Action, args []string, input io.Reader, stdout, stderr io.Writer) int {
	label := integration.kind + " " + string(action)
	options, ok := parseIntegrationFlags(label, args, stdout, stderr, true, false)
	if !ok {
		return 2
	}
	target, scope, dryRun, force := options.Target, options.Scope, options.DryRun, options.Force
	manager, err := integrations.New(integrations.Options{})
	if err != nil {
		writeCommandError(stderr, "%s: %v", label, err)
		return 1
	}

	selectedTargets := []integrations.Target{}
	if target != "" {
		selectedTargets = append(selectedTargets, integrations.Target(target))
	} else if action == integrations.ActionInstall {
		selectedTargets, err = selectIntegrationTargets(input, stdout, manager, integration.kind, integrations.Scope(scope))
		if err != nil {
			writeCommandError(stderr, "%s: %v", label, err)
			return 2
		}
	} else {
		writeCommandError(stderr, "%s: --target is required", label)
		return 2
	}

	failed := false
	for _, selectedTarget := range selectedTargets {
		var plan integrations.Plan
		switch action {
		case integrations.ActionInstall:
			plan, err = integration.install(manager, selectedTarget, integrations.Scope(scope), dryRun, force)
		case integrations.ActionRemove:
			plan, err = integration.remove(manager, selectedTarget, integrations.Scope(scope), dryRun, force)
		default:
			err = fmt.Errorf("unsupported %s operation %q", integration.kind, action)
		}
		if err != nil {
			writeCommandError(stderr, "%s --target %s: %v", label, selectedTarget, err)
			failed = true
			continue
		}
		writeIntegrationPlan(stdout, integration.kind, plan)
	}
	if failed {
		return 1
	}
	return 0
}

func runSkillStatus(args []string, stdout, stderr io.Writer) int {
	return runClientStatus(skillIntegration(), args, stdout, stderr)
}

func runHookStatus(args []string, stdout, stderr io.Writer) int {
	return runClientStatus(hookIntegration(), args, stdout, stderr)
}

func runClientStatus(integration clientIntegration, args []string, stdout, stderr io.Writer) int {
	label := integration.kind + " status"
	options, ok := parseIntegrationFlags(label, args, stdout, stderr, false, false)
	if !ok {
		return 2
	}
	target, scope := options.Target, options.Scope
	if target == "" {
		writeCommandError(stderr, "%s: --target is required", label)
		return 2
	}
	manager, err := integrations.New(integrations.Options{})
	if err != nil {
		writeCommandError(stderr, "%s: %v", label, err)
		return 1
	}
	plan, err := integration.status(manager, integrations.Target(target), integrations.Scope(scope))
	if err != nil {
		writeCommandError(stderr, "%s: %v", label, err)
		return 1
	}
	writeIntegrationPlan(stdout, integration.kind, plan)
	return 0
}

// integrationOptions carries the flags of the mcp and skill operations.
type integrationOptions struct {
	Target string
	Scope  string
	DryRun bool
	Force  bool
	// Daemon asks for the url entry explicitly. Since the daemon became the
	// default it changes no outcome on its own, and it is still accepted
	// because it was valid yesterday: a flag that started erroring would break
	// every script that already passes it. What it does change is the failure
	// -- an explicit ask refuses where the default would fall back to stdio.
	Daemon bool
	// Stdio asks for the command entry: one `serve` per client, spawned by the
	// client, with no daemon involved.
	Stdio bool
}

// integrationFlagSet declares them in one place. The changes flag is what
// separates an operation that writes from one that only reports: only the
// writers accept --dry-run and --force.
//
// endpoint separates the two kinds of integration rather than the two kinds of
// operation: a skill is a file with no transport, so --daemon means nothing
// there. It is accepted by `status` as well as by the writers, because a status
// that compared against the wrong shape would call our own entry unmanaged.
//
// The output writer is a parameter because these flag sets report parse errors
// themselves rather than discarding them, and the destination is the caller's.
func integrationFlagSet(name string, options *integrationOptions, output io.Writer, changes, endpoint bool) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&options.Target, "target", "", "client target (optional for interactive install)")
	flags.StringVar(&options.Scope, "scope", integrations.ScopeUser, "configuration scope: user or project")
	if changes {
		flags.BoolVar(&options.DryRun, "dry-run", false, "show the change without writing")
		flags.BoolVar(&options.Force, "force", false, "replace or remove an incompatible entry")
	}
	if endpoint {
		flags.BoolVar(&options.Daemon, "daemon", false,
			"require the supervised daemon, installing it without asking")
		flags.BoolVar(&options.Stdio, "stdio", false,
			"write a `serve` command entry instead: one process per client")
	}
	return flags
}

func parseIntegrationFlags(name string, args []string, stdout, stderr io.Writer, changes, endpoint bool) (integrationOptions, bool) {
	options := integrationOptions{Scope: integrations.ScopeUser}
	flags := integrationFlagSet(name, &options, stderr, changes, endpoint)
	if parsed, code := parseCommandFlags(name, flags, args, stdout, stderr); !parsed || code != 0 {
		return integrationOptions{}, false
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "%s: unexpected arguments", name)
		return integrationOptions{}, false
	}
	return options, true
}

func selectIntegrationTargets(input io.Reader, stdout io.Writer, manager integrations.Manager, kind string, scope integrations.Scope) ([]integrations.Target, error) {
	var (
		detections []integrations.TargetDetection
		err        error
	)
	switch kind {
	case "mcp":
		detections, err = manager.DetectMCPTargets(scope)
	case "skill":
		detections, err = manager.DetectSkillTargets(scope)
	case "hook":
		detections, err = manager.DetectHookTargets(scope)
	default:
		return nil, fmt.Errorf("unsupported interactive integration %q", kind)
	}
	if err != nil {
		return nil, fmt.Errorf("detect coding agents: %w", err)
	}

	defaultSelection := make([]int, 0, len(detections))
	for index, detection := range detections {
		if detection.Detected {
			defaultSelection = append(defaultSelection, index)
		}
	}
	selected, err := runIntegrationSelection(input, stdout, detections, defaultSelection, scope)
	if err != nil {
		return nil, err
	}
	targets := make([]integrations.Target, 0, len(selected))
	for _, index := range selected {
		targets = append(targets, detections[index].Target)
	}
	return targets, nil
}

func integrationTargetLabel(target integrations.Target) string {
	switch target {
	case integrations.TargetClaudeCode:
		return "Claude Code"
	case integrations.TargetClaudeDesktop:
		return "Claude Desktop"
	case integrations.TargetCodex:
		return "Codex"
	case integrations.TargetOpenCode:
		return "OpenCode"
	case integrations.TargetOhMyPi:
		return "Oh My Pi"
	default:
		return string(target)
	}
}

func writeIntegrationPlan(stdout io.Writer, kind string, plan integrations.Plan) {
	message := fmt.Sprintf("%s %s --target %s --scope %s: %s (%s) — %s",
		kind, plan.Action, plan.Target, plan.Scope, plan.Status, plan.Path, plan.Detail)
	switch plan.Status {
	case "installed", "removed":
		writeSuccess(stdout, "%s", message)
	case "incompatible":
		writeWarning(stdout, "%s", message)
	default:
		writeInfo(stdout, "%s", message)
	}
}

// writeIntegrationHelp prints one integration family's operations.
//
// The targets are a parameter and not a constant because they are not the same
// for every family: Claude Desktop has no project scope, while MCP and skill
// integrations support different client sets.
func writeIntegrationHelp(stdout io.Writer, command, summary string, commands []string, targets []integrations.Target) {
	paint := styleFor(stdout)
	fmt.Fprintf(stdout, "%sUsage%s: kivgraph %s <operation> [flags]\n\n", paint.bold, paint.reset, command)
	fmt.Fprintf(stdout, "%s%s%s\n\n", paint.dim, summary, paint.reset)
	for _, usage := range commands {
		fmt.Fprintf(stdout, "  %s\n", usage)
	}
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, string(target))
	}
	fmt.Fprintf(stdout, "\n%sTargets%s: %s\n", paint.bold, paint.reset, strings.Join(names, ", "))
	fmt.Fprintln(stdout, "For install operations, omit --target to open the selector. Use arrows or j/k to move, space to toggle, a/n for all or none, Enter to confirm, and q or Esc to cancel. If no supervised daemon exists, install asks before creating one; answer no or pass --stdio to use serve. Pass --daemon to require the daemon without asking, and --target for scripted use.")
}
