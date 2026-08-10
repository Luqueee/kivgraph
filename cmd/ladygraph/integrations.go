package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/Luqueee/ladygraph/internal/integrations"
)

func runMCPCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || helpRequested(args) {
		writeIntegrationHelp(stdout, "mcp", "Manage local MCP client registrations", []string{
			"mcp install --target TARGET [--scope user|project] [--dry-run] [--force]",
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
			"skill install --target TARGET [--scope user|project] [--dry-run] [--force]",
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
	target, scope, dryRun, force, ok := parseIntegrationFlags("mcp "+string(action), args, stdout, stderr, true)
	if !ok {
		return 2
	}
	manager, err := integrations.New(integrations.Options{})
	if err != nil {
		writeCommandError(stderr, "mcp %s: %v", action, err)
		return 1
	}
	var plan integrations.Plan
	switch action {
	case integrations.ActionInstall:
		plan, err = manager.InstallMCP(integrations.Target(target), integrations.Scope(scope), dryRun, force)
	case integrations.ActionRemove:
		plan, err = manager.RemoveMCP(integrations.Target(target), integrations.Scope(scope), dryRun, force)
	default:
		err = fmt.Errorf("unsupported MCP operation %q", action)
	}
	if err != nil {
		writeCommandError(stderr, "mcp %s: %v", action, err)
		return 1
	}
	writeIntegrationPlan(stdout, "mcp", plan)
	return 0
}

func runMCPStatus(args []string, stdout, stderr io.Writer) int {
	target, scope, _, _, ok := parseIntegrationFlags("mcp status", args, stdout, stderr, false)
	if !ok {
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
	target, scope, dryRun, force, ok := parseIntegrationFlags("skill "+string(action), args, stdout, stderr, true)
	if !ok {
		return 2
	}
	manager, err := integrations.New(integrations.Options{})
	if err != nil {
		writeCommandError(stderr, "skill %s: %v", action, err)
		return 1
	}
	var plan integrations.Plan
	switch action {
	case integrations.ActionInstall:
		plan, err = manager.InstallSkill(integrations.Target(target), integrations.Scope(scope), dryRun, force)
	case integrations.ActionRemove:
		plan, err = manager.RemoveSkill(integrations.Target(target), integrations.Scope(scope), dryRun, force)
	default:
		err = fmt.Errorf("unsupported skill operation %q", action)
	}
	if err != nil {
		writeCommandError(stderr, "skill %s: %v", action, err)
		return 1
	}
	writeIntegrationPlan(stdout, "skill", plan)
	return 0
}

func runSkillStatus(args []string, stdout, stderr io.Writer) int {
	target, scope, _, _, ok := parseIntegrationFlags("skill status", args, stdout, stderr, false)
	if !ok {
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
	flags.StringVar(&target, "target", "", "client target")
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
	if target == "" {
		writeCommandError(stderr, "%s: --target is required", name)
		return "", "", false, false, false
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "%s: unexpected arguments", name)
		return "", "", false, false, false
	}
	return target, scope, dryRun, force, true
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
}
