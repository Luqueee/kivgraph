package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Luqueee/ladygraph/internal/integrations"
)

func runMCPCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || helpRequested(args) {
		writeIntegrationHelp(stdout, "mcp", "Manage local MCP client registrations", []string{
			"mcp install [--target TARGET] [--scope user|project] [--dry-run] [--force]",
			"mcp status --target TARGET [--scope user|project]",
			"mcp remove --target TARGET [--scope user|project] [--dry-run] [--force]",
		})
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
		writeIntegrationHelp(stdout, "skill", "Manage the Ladygraph Agent Skill", []string{
			"skill install [--target TARGET] [--scope user|project] [--dry-run] [--force]",
			"skill status --target TARGET [--scope user|project]",
			"skill remove --target TARGET [--scope user|project] [--dry-run] [--force]",
		})
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

func runMCPChange(action integrations.Action, args []string, stdout, stderr io.Writer) int {
	return runMCPChangeWithInput(action, args, os.Stdin, stdout, stderr)
}

func runMCPChangeWithInput(action integrations.Action, args []string, input io.Reader, stdout, stderr io.Writer) int {
	target, scope, dryRun, force, ok := parseIntegrationFlags("mcp "+string(action), args, stdout, stderr, true)
	if !ok {
		return 2
	}
	manager, err := integrations.New(integrations.Options{})
	if err != nil {
		writeCommandError(stderr, "mcp %s: %v", action, err)
		return 1
	}

	selectedTargets := []integrations.Target{}
	if target != "" {
		selectedTargets = append(selectedTargets, integrations.Target(target))
	} else if action == integrations.ActionInstall {
		selectedTargets, err = selectIntegrationTargets(input, stdout, manager, "mcp", integrations.Scope(scope))
		if err != nil {
			writeCommandError(stderr, "mcp %s: %v", action, err)
			return 2
		}
	} else {
		writeCommandError(stderr, "mcp %s: --target is required", action)
		return 2
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
	target, scope, _, _, ok := parseIntegrationFlags("mcp status", args, stdout, stderr, false)
	if !ok {
		return 2
	}
	if target == "" {
		writeCommandError(stderr, "mcp status: --target is required")
		return 2
	}
	manager, err := integrations.New(integrations.Options{})
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

func runSkillChange(action integrations.Action, args []string, stdout, stderr io.Writer) int {
	return runSkillChangeWithInput(action, args, os.Stdin, stdout, stderr)
}

func runSkillChangeWithInput(action integrations.Action, args []string, input io.Reader, stdout, stderr io.Writer) int {
	target, scope, dryRun, force, ok := parseIntegrationFlags("skill "+string(action), args, stdout, stderr, true)
	if !ok {
		return 2
	}
	manager, err := integrations.New(integrations.Options{})
	if err != nil {
		writeCommandError(stderr, "skill %s: %v", action, err)
		return 1
	}

	selectedTargets := []integrations.Target{}
	if target != "" {
		selectedTargets = append(selectedTargets, integrations.Target(target))
	} else if action == integrations.ActionInstall {
		selectedTargets, err = selectIntegrationTargets(input, stdout, manager, "skill", integrations.Scope(scope))
		if err != nil {
			writeCommandError(stderr, "skill %s: %v", action, err)
			return 2
		}
	} else {
		writeCommandError(stderr, "skill %s: --target is required", action)
		return 2
	}

	failed := false
	for _, selectedTarget := range selectedTargets {
		var plan integrations.Plan
		switch action {
		case integrations.ActionInstall:
			plan, err = manager.InstallSkill(selectedTarget, integrations.Scope(scope), dryRun, force)
		case integrations.ActionRemove:
			plan, err = manager.RemoveSkill(selectedTarget, integrations.Scope(scope), dryRun, force)
		default:
			err = fmt.Errorf("unsupported skill operation %q", action)
		}
		if err != nil {
			writeCommandError(stderr, "skill %s --target %s: %v", action, selectedTarget, err)
			failed = true
			continue
		}
		writeIntegrationPlan(stdout, "skill", plan)
	}
	if failed {
		return 1
	}
	return 0
}

func runSkillStatus(args []string, stdout, stderr io.Writer) int {
	target, scope, _, _, ok := parseIntegrationFlags("skill status", args, stdout, stderr, false)
	if !ok {
		return 2
	}
	if target == "" {
		writeCommandError(stderr, "skill status: --target is required")
		return 2
	}
	manager, err := integrations.New(integrations.Options{})
	if err != nil {
		writeCommandError(stderr, "skill status: %v", err)
		return 1
	}
	plan, err := manager.StatusSkill(integrations.Target(target), integrations.Scope(scope))
	if err != nil {
		writeCommandError(stderr, "skill status: %v", err)
		return 1
	}
	writeIntegrationPlan(stdout, "skill", plan)
	return 0
}

func parseIntegrationFlags(name string, args []string, stdout, stderr io.Writer, changes bool) (string, string, bool, bool, bool) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	target := ""
	scope := integrations.ScopeUser
	dryRun := false
	force := false
	flags.SetOutput(stderr)
	flags.StringVar(&target, "target", "", "client target (optional for interactive install)")
	flags.StringVar(&scope, "scope", integrations.ScopeUser, "configuration scope: user or project")
	if changes {
		flags.BoolVar(&dryRun, "dry-run", false, "show the change without writing")
		flags.BoolVar(&force, "force", false, "replace or remove an incompatible entry")
	}
	if parsed, code := parseCommandFlags(name, flags, args, stdout, stderr); !parsed {
		return "", "", false, false, false
	} else if code != 0 {
		return "", "", false, false, false
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "%s: unexpected arguments", name)
		return "", "", false, false, false
	}
	return target, scope, dryRun, force, true
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

func writeIntegrationHelp(stdout io.Writer, command, summary string, commands []string) {
	paint := styleFor(stdout)
	fmt.Fprintf(stdout, "%sUsage%s: ladygraph %s <operation> [flags]\n\n", paint.bold, paint.reset, command)
	fmt.Fprintf(stdout, "%s%s%s\n\n", paint.dim, summary, paint.reset)
	for _, usage := range commands {
		fmt.Fprintf(stdout, "  %s\n", usage)
	}
	fmt.Fprintf(stdout, "\n%sTargets%s: claude-code, claude-desktop, codex, opencode, oh-my-pi\n", paint.bold, paint.reset)
	fmt.Fprintln(stdout, "For install operations, omit --target to open the selector. Use arrows or j/k to move, space to toggle, a/n for all or none, Enter to confirm, and q or Esc to cancel. Pass --target for scripted, non-interactive use.")
}
