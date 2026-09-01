package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/integrations"
	"github.com/Luqueee/kivgraph/internal/supervisor"
)

var errStaleDaemonUnit = errors.New("the daemon supervisor unit is stale")

type updateConfigLoader func(string) (config.Loaded, error)
type updateSupervisorOperation func(supervisor.Spec) (supervisor.Report, error)

// refreshInstalledRuntime repairs the installation-level processes and client
// integrations that an in-place bundle replacement leaves behind. It receives
// the executable path from before the replacement: on Unix, asking the running
// updater for its path afterwards can return the old `.previous` directory.
func refreshInstalledRuntime(executable string, stdout, stderr io.Writer) error {
	var failures []error
	restarted, err := refreshSupervisedDaemonWith(executable, config.Load, supervisor.Status,
		supervisor.Restart, stdout)
	if err != nil {
		writeWarning(stderr, "update.daemon: %v", err)
		failures = append(failures, err)
	}

	endpoint, hasEndpoint, endpointErr := installedDaemonEndpoint(restarted)
	if endpointErr != nil {
		failures = append(failures, endpointErr)
	}
	if err := refreshInstalledIntegrations(integrations.Options{Executable: executable},
		endpoint, hasEndpoint, stdout); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func installedDaemonEndpoint(waitForRestart bool) (integrations.Endpoint, bool, error) {
	loaded, err := config.Load("")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return integrations.Endpoint{}, false, nil
		}
		return integrations.Endpoint{}, false, fmt.Errorf("read the configuration for MCP refresh: %w", err)
	}
	directory := stateDirectory(loaded)
	var endpoint daemon.Endpoint
	if waitForRestart {
		endpoint, err = daemon.WaitReachable(context.Background(), directory, endpointDeadline)
	} else {
		endpoint, err = daemon.ReadEndpoint(directory)
	}
	if err != nil {
		if !waitForRestart {
			return integrations.Endpoint{}, false, nil
		}
		return integrations.Endpoint{}, false, fmt.Errorf("wait for the refreshed daemon endpoint: %w", err)
	}
	return integrations.Endpoint{URL: endpoint.URL, Token: endpoint.Token}, true, nil
}

// refreshSupervisedDaemonWith restarts an already installed supervisor. It
// never provisions a missing one: `update` must not create a background daemon
// on a machine that did not ask for one.
func refreshSupervisedDaemonWith(
	executable string,
	load updateConfigLoader,
	status updateSupervisorOperation,
	restart updateSupervisorOperation,
	stdout io.Writer,
) (bool, error) {
	loaded, err := load("")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read the configuration: %w", err)
	}
	spec := supervisor.Spec{
		Executable:     executable,
		StateDirectory: stateDirectory(loaded),
		ConfigPath:     loaded.ConfigPath,
	}
	report, err := status(spec)
	if err != nil {
		if errors.Is(err, supervisor.ErrUnsupportedPlatform) {
			return false, nil
		}
		return false, fmt.Errorf("inspect the daemon supervisor: %w", err)
	}
	switch report.State {
	case supervisor.StateAbsent, supervisor.StateUnsupported:
		return false, nil
	case supervisor.StateStale:
		return false, fmt.Errorf("%w: %s", errStaleDaemonUnit, report.Detail)
	case supervisor.StateInstalled:
		if _, err := restart(spec); err != nil {
			return false, fmt.Errorf("restart %s: %w", report.Label, err)
		}
		writeSuccess(stdout, "update.daemon: %s refreshed", report.Label)
		return true, nil
	default:
		return false, fmt.Errorf("inspect the daemon supervisor: unknown state %q", report.State)
	}
}

// refreshInstalledIntegrations updates only user-scoped entries that already
// belong to Kivgraph. Missing entries stay missing, incompatible entries stay
// untouched, and project files are never changed by a user-level update.
func refreshInstalledIntegrations(
	options integrations.Options,
	endpoint integrations.Endpoint,
	hasEndpoint bool,
	stdout io.Writer,
) error {
	manager, err := integrations.New(options)
	if err != nil {
		return fmt.Errorf("create integration manager: %w", err)
	}
	var failures []error
	if err := refreshClientIntegration(manager, "hook", integrations.HookTargets(),
		manager.StatusHook, manager.InstallHook, stdout); err != nil {
		failures = append(failures, err)
	}
	if err := refreshClientIntegration(manager, "skill", integrations.SkillTargets(),
		manager.StatusSkill, manager.InstallSkill, stdout); err != nil {
		failures = append(failures, err)
	}
	if err := refreshMCPIntegrations(options, endpoint, hasEndpoint, stdout); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

type integrationStatus func(integrations.Target, integrations.Scope) (integrations.Plan, error)
type integrationInstall func(integrations.Target, integrations.Scope, bool, bool) (integrations.Plan, error)

type mcpIntegrationManager interface {
	StatusMCP(integrations.Target, integrations.Scope) (integrations.Plan, error)
	InstallMCP(integrations.Target, integrations.Scope, bool, bool) (integrations.Plan, error)
}

func refreshClientIntegration(
	manager integrations.Manager,
	kind string,
	targets []integrations.Target,
	status integrationStatus,
	install integrationInstall,
	stdout io.Writer,
) error {
	var failures []error
	for _, target := range targets {
		plan, err := status(target, integrations.ScopeUser)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s --target %s: inspect: %w", kind, target, err))
			continue
		}
		if plan.Status == "absent" || plan.Status == "incompatible" {
			continue
		}
		refreshed, err := install(target, integrations.ScopeUser, false, false)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s --target %s: refresh: %w", kind, target, err))
			continue
		}
		writeUpdateIntegrationPlan(stdout, kind, refreshed)
	}
	return errors.Join(failures...)
}

func refreshMCPIntegrations(
	options integrations.Options,
	endpoint integrations.Endpoint,
	hasEndpoint bool,
	stdout io.Writer,
) error {
	stdioOptions := options
	stdioOptions.Endpoint = integrations.Endpoint{}
	stdioManager, err := integrations.New(stdioOptions)
	if err != nil {
		return fmt.Errorf("create stdio integration manager: %w", err)
	}
	var endpointManager integrations.Manager
	if hasEndpoint {
		endpointOptions := options
		endpointOptions.Endpoint = endpoint
		endpointManager, err = integrations.New(endpointOptions)
		if err != nil {
			return fmt.Errorf("create daemon integration manager: %w", err)
		}
	}

	return refreshMCPIntegrationsWith(stdioManager, endpointManager, hasEndpoint, stdout)
}

func refreshMCPIntegrationsWith(
	stdioManager mcpIntegrationManager,
	endpointManager mcpIntegrationManager,
	hasEndpoint bool,
	stdout io.Writer,
) error {
	var failures []error
	for _, target := range integrations.KnownTargets() {
		stdioPlan, err := stdioManager.StatusMCP(target, integrations.ScopeUser)
		if err != nil {
			failures = append(failures, fmt.Errorf("mcp --target %s: inspect stdio: %w", target, err))
			continue
		}
		manager := stdioManager
		switch stdioPlan.Status {
		case "managed":
			// Keep an existing stdio registration on stdio, even when a
			// daemon endpoint happens to be available.
		case "absent", "incompatible", "superseded":
			if !hasEndpoint {
				continue
			}
			endpointPlan, endpointErr := endpointManager.StatusMCP(target, integrations.ScopeUser)
			if endpointErr != nil {
				failures = append(failures, fmt.Errorf("mcp --target %s: inspect daemon: %w", target, endpointErr))
				continue
			}
			if endpointPlan.Status != "managed" && endpointPlan.Status != "superseded" {
				continue
			}
			manager = endpointManager
		default:
			failures = append(failures, fmt.Errorf("mcp --target %s: unknown status %q", target, stdioPlan.Status))
			continue
		}
		refreshed, err := manager.InstallMCP(target, integrations.ScopeUser, false, false)
		if err != nil {
			failures = append(failures, fmt.Errorf("mcp --target %s: refresh: %w", target, err))
			continue
		}
		writeUpdateIntegrationPlan(stdout, "mcp", refreshed)
	}
	return errors.Join(failures...)
}

func writeUpdateIntegrationPlan(stdout io.Writer, kind string, plan integrations.Plan) {
	if plan.Changed {
		writeSuccess(stdout, "update.%s: %s refreshed", kind, plan.Target)
		return
	}
	writeInfo(stdout, "update.%s: %s already current", kind, plan.Target)
}
