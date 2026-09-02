package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/integrations"
	"github.com/Luqueee/kivgraph/internal/procstat"
	"github.com/Luqueee/kivgraph/internal/supervisor"
	"github.com/Luqueee/kivgraph/internal/update"
)

func TestUpdatePostInstallUsesExecutableCapturedBeforeBundleSwap(t *testing.T) {
	var runnerPath, postInstallPath string
	runner := func(_ context.Context, options update.Options) (update.Result, error) {
		runnerPath = options.ExecutablePath
		return update.Result{
			CurrentVersion:  "0.1.0",
			LatestVersion:   "0.2.0",
			UpdateAvailable: true,
			Updated:         true,
		}, nil
	}
	postInstall := func(executable string, _, _ io.Writer) error {
		postInstallPath = executable
		return nil
	}

	var stdout, stderr bytes.Buffer
	if code := runUpdateWithRunnerAndPostInstall(nil, nil, &stdout, &stderr,
		runner, noProcesses, nil, nil, true, postInstall); code != 0 {
		t.Fatalf("runUpdateWithRunnerAndPostInstall = %d, stderr=%q", code, stderr.String())
	}
	if runnerPath == "" || postInstallPath == "" || runnerPath != postInstallPath {
		t.Fatalf("captured executable paths differ: runner=%q post-install=%q", runnerPath, postInstallPath)
	}
}

func TestUpdateDoesNotRestartTheRefreshedDaemonAgain(t *testing.T) {
	fixture := &stopFixture{processes: []procstat.Process{kivgraphProcess(51, "daemon")}}
	postInstallCalls := 0
	staleRestartCalls := 0
	postInstall := func(string, io.Writer, io.Writer) updatePostInstallResult {
		postInstallCalls++
		return updatePostInstallResult{RefreshedDaemonPID: 51}
	}
	restart := func([]procstat.Process) (daemonRestart, error) {
		staleRestartCalls++
		return daemonRestart{PID: 51, Ownership: ownershipSupervised}, nil
	}

	var stdout, stderr bytes.Buffer
	if code := runUpdateWithRunnerAtExecutable(nil, nil, &stdout, &stderr,
		installedRunner(), fixture.list, fixture.signal, restart, true,
		"/bundle/bin/kivgraph", postInstall); code != 0 {
		t.Fatalf("runUpdateWithRunnerAtExecutable() = %d, stderr=%q", code, stderr.String())
	}
	if postInstallCalls != 1 {
		t.Fatalf("post-install calls = %d, want 1", postInstallCalls)
	}
	if staleRestartCalls != 0 {
		t.Fatalf("stale daemon restart calls = %d, want 0", staleRestartCalls)
	}
	if strings.Contains(stdout.String(), "update.stale") {
		t.Fatalf("refreshed daemon was still treated as stale:\n%s", stdout.String())
	}
}

func TestUpdatePostInstallResultTracksOnlyAReachableRestart(t *testing.T) {
	result := updatePostInstallResultFor(true, true, 91, nil)
	if result.RefreshedDaemonPID != 91 || result.Err != nil {
		t.Fatalf("reachable restart result = %#v, want pid 91 and no error", result)
	}
	if result := updatePostInstallResultFor(true, false, 91, errors.New("endpoint unavailable")); result.RefreshedDaemonPID != 0 || result.Err == nil {
		t.Fatalf("unreachable restart result = %#v, want no pid and an error", result)
	}
}

func TestUpdateFailsWhenPostInstallRefreshFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runUpdateWithRunnerAndPostInstall(nil, nil, &stdout, &stderr,
		installedRunner(), noProcesses, nil, nil, true,
		func(string, io.Writer, io.Writer) error { return errors.New("daemon did not restart") }); code != 1 {
		t.Fatalf("runUpdateWithRunnerAndPostInstall = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "refresh installed runtime integrations: daemon did not restart") {
		t.Fatalf("post-install failure was not reported: %q", stderr.String())
	}
}

func TestRefreshSupervisedDaemonUsesCapturedExecutable(t *testing.T) {
	stateDirectory := t.TempDir()
	wantedExecutable := filepath.Join(t.TempDir(), "kivgraph", "bin", "kivgraph")
	loaded := config.Loaded{
		Config:     config.Config{Storage: config.StorageConfig{DatabasePath: filepath.Join(stateDirectory, "graph.lbdb")}},
		ConfigPath: filepath.Join(t.TempDir(), "config.yaml"),
	}
	var statusSpec, restartSpec supervisor.Spec
	status := func(spec supervisor.Spec) (supervisor.Report, error) {
		statusSpec = spec
		return supervisor.Report{State: supervisor.StateInstalled, Label: "test.daemon"}, nil
	}
	restart := func(spec supervisor.Spec) (supervisor.Report, error) {
		restartSpec = spec
		return supervisor.Report{State: supervisor.StateInstalled, Label: "test.daemon"}, nil
	}

	var stdout bytes.Buffer
	restarted, err := refreshSupervisedDaemonWith(wantedExecutable,
		func(string) (config.Loaded, error) { return loaded, nil }, status, restart,
		&stdout)
	if err != nil {
		t.Fatalf("refreshSupervisedDaemonWith() error = %v", err)
	}
	if !restarted {
		t.Fatal("refreshSupervisedDaemonWith() reported no restart")
	}
	if statusSpec.Executable != wantedExecutable || restartSpec.Executable != wantedExecutable {
		t.Fatalf("supervisor executable paths: status=%q restart=%q, want %q",
			statusSpec.Executable, restartSpec.Executable, wantedExecutable)
	}
	if statusSpec.StateDirectory != stateDirectory || restartSpec.StateDirectory != stateDirectory {
		t.Fatalf("supervisor state directories: status=%q restart=%q, want %q",
			statusSpec.StateDirectory, restartSpec.StateDirectory, stateDirectory)
	}
	if !strings.Contains(stdout.String(), "update.daemon") {
		t.Fatalf("daemon refresh was not reported: %q", stdout.String())
	}
}

func TestRefreshSupervisedDaemonDoesNotProvisionAbsentDaemon(t *testing.T) {
	consulted := false
	status := func(supervisor.Spec) (supervisor.Report, error) {
		consulted = true
		return supervisor.Report{}, nil
	}
	restart := func(supervisor.Spec) (supervisor.Report, error) {
		t.Fatal("restart called for an absent daemon")
		return supervisor.Report{}, nil
	}

	var stdout bytes.Buffer
	restarted, err := refreshSupervisedDaemonWith("/bundle/bin/kivgraph",
		func(string) (config.Loaded, error) { return config.Loaded{}, os.ErrNotExist }, status, restart,
		&stdout)
	if err != nil {
		t.Fatalf("refreshSupervisedDaemonWith() error = %v", err)
	}
	if restarted {
		t.Fatal("refreshSupervisedDaemonWith() reported a restart without configuration")
	}
	if consulted {
		t.Fatal("refresh consulted a supervisor without configuration")
	}
}

func TestRefreshInstalledIntegrationsRefreshesOwnedUserArtifacts(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	executable := filepath.Join(t.TempDir(), "kivgraph", "bin", "kivgraph")
	manager, err := integrations.New(integrations.Options{
		HomeDir: home, ProjectDir: project, Executable: executable, GOOS: "linux",
	})
	if err != nil {
		t.Fatalf("integrations.New() error = %v", err)
	}
	if _, err := manager.InstallHook(integrations.TargetClaudeCode, integrations.ScopeUser, false, false); err != nil {
		t.Fatalf("InstallHook() error = %v", err)
	}
	if _, err := manager.InstallSkill(integrations.TargetCodex, integrations.ScopeUser, false, false); err != nil {
		t.Fatalf("InstallSkill() error = %v", err)
	}
	if err := os.Remove(filepath.Join(home, ".config", "kivgraph", "skills", "kivgraph", "SKILL.md")); err != nil {
		t.Fatalf("remove canonical skill: %v", err)
	}
	if _, err := manager.InstallMCP(integrations.TargetClaudeCode, integrations.ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP() error = %v", err)
	}

	var stdout bytes.Buffer
	if err := refreshInstalledIntegrations(integrations.Options{
		HomeDir: home, ProjectDir: project, Executable: executable, GOOS: "linux",
	}, integrations.Endpoint{}, false, &stdout); err != nil {
		t.Fatalf("refreshInstalledIntegrations() error = %v", err)
	}
	for _, want := range []string{"update.hook", "update.skill", "update.mcp"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("refresh output lost %q: %s", want, stdout.String())
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "kivgraph", "skills", "kivgraph", "SKILL.md")); err != nil {
		t.Fatalf("refresh did not repair the canonical skill: %v", err)
	}
}

func TestRefreshInstalledIntegrationsPreservesDaemonTransport(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	executable := filepath.Join(t.TempDir(), "kivgraph", "bin", "kivgraph")
	oldEndpoint := integrations.Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "old-token"}
	currentEndpoint := integrations.Endpoint{URL: "http://127.0.0.1:7789/mcp", Token: "current-token"}
	manager, err := integrations.New(integrations.Options{
		HomeDir: home, ProjectDir: project, Executable: executable, GOOS: "linux", Endpoint: oldEndpoint,
	})
	if err != nil {
		t.Fatalf("integrations.New() error = %v", err)
	}
	if _, err := manager.InstallMCP(integrations.TargetClaudeCode, integrations.ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP() error = %v", err)
	}

	var stdout bytes.Buffer
	if err := refreshInstalledIntegrations(integrations.Options{
		HomeDir: home, ProjectDir: project, Executable: executable, GOOS: "linux",
		PreviousEndpoint: oldEndpoint,
	}, currentEndpoint, true, &stdout); err != nil {
		t.Fatalf("refreshInstalledIntegrations() error = %v", err)
	}
	currentManager, err := integrations.New(integrations.Options{
		HomeDir: home, ProjectDir: project, Executable: executable, GOOS: "linux", Endpoint: currentEndpoint,
	})
	if err != nil {
		t.Fatalf("integrations.New(current endpoint) error = %v", err)
	}
	status, err := currentManager.StatusMCP(integrations.TargetClaudeCode, integrations.ScopeUser)
	if err != nil {
		t.Fatalf("StatusMCP() error = %v", err)
	}
	if status.Status != "managed" {
		t.Fatalf("daemon MCP entry status = %q, want managed", status.Status)
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("read refreshed MCP config: %v", err)
	}
	if content := string(data); strings.Contains(content, oldEndpoint.URL) ||
		!strings.Contains(content, currentEndpoint.URL) {
		t.Fatalf("MCP endpoint was not refreshed from old=%q to current=%q: %s",
			oldEndpoint.URL, currentEndpoint.URL, data)
	}
}

func TestRefreshSupervisedDaemonReportsStaleUnit(t *testing.T) {
	status := func(supervisor.Spec) (supervisor.Report, error) {
		return supervisor.Report{State: supervisor.StateStale, Detail: "reinstall the unit"}, nil
	}
	restart := func(supervisor.Spec) (supervisor.Report, error) {
		t.Fatal("restart called for a stale unit")
		return supervisor.Report{}, nil
	}

	var stdout bytes.Buffer
	_, err := refreshSupervisedDaemonWith("/bundle/bin/kivgraph",
		func(string) (config.Loaded, error) {
			return config.Loaded{Config: config.Config{Storage: config.StorageConfig{
				DatabasePath: filepath.Join(t.TempDir(), "graph.lbdb"),
			}}}, nil
		}, status, restart, &stdout)
	if err == nil || !errors.Is(err, errStaleDaemonUnit) {
		t.Fatalf("refreshSupervisedDaemonWith() error = %v, want stale unit error", err)
	}
}

func TestRefreshSupervisedDaemonRepairsManagedStaleUnit(t *testing.T) {
	loaded := config.Loaded{Config: config.Config{Storage: config.StorageConfig{
		DatabasePath: filepath.Join(t.TempDir(), "graph.lbdb"),
	}}}
	restarted := false
	status := func(supervisor.Spec) (supervisor.Report, error) {
		return supervisor.Report{
			State:      supervisor.StateStale,
			Label:      "test.daemon",
			Managed:    true,
			Repairable: true,
			Detail:     "the installed unit has the legacy supervisor format",
		}, nil
	}
	restart := func(supervisor.Spec) (supervisor.Report, error) {
		restarted = true
		return supervisor.Report{State: supervisor.StateInstalled, Label: "test.daemon"}, nil
	}

	var stdout bytes.Buffer
	refreshed, err := refreshSupervisedDaemonWith("/bundle/bin/kivgraph", func(string) (config.Loaded, error) {
		return loaded, nil
	}, status, restart, &stdout)
	if err != nil {
		t.Fatalf("%s: refreshSupervisedDaemonWith() error = %v", t.Name(), err)
	}
	if !refreshed || !restarted {
		t.Fatalf("%s: refreshSupervisedDaemonWith() = refreshed %t, restarted %t; want both", t.Name(), refreshed, restarted)
	}
}

func TestRefreshSupervisedDaemonReportsOperationFailures(t *testing.T) {
	loaded := config.Loaded{
		Config: config.Config{Storage: config.StorageConfig{
			DatabasePath: filepath.Join(t.TempDir(), "graph.lbdb"),
		}},
		ConfigPath: filepath.Join(t.TempDir(), "config.yaml"),
	}
	tests := []struct {
		name         string
		loadErr      error
		statusReport supervisor.Report
		statusErr    error
		restartErr   error
		wantErr      bool
	}{
		{name: "configuration", loadErr: errors.New("bad configuration"), wantErr: true},
		{name: "unsupported", statusErr: supervisor.ErrUnsupportedPlatform},
		{name: "status", statusErr: errors.New("status failed"), wantErr: true},
		{name: "unknown state", statusReport: supervisor.Report{State: "mystery"}, wantErr: true},
		{
			name:         "restart",
			statusReport: supervisor.Report{State: supervisor.StateInstalled, Label: "test.daemon"},
			restartErr:   errors.New("restart failed"),
			wantErr:      true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			load := func(string) (config.Loaded, error) {
				if test.loadErr != nil {
					return config.Loaded{}, test.loadErr
				}
				return loaded, nil
			}
			status := func(supervisor.Spec) (supervisor.Report, error) {
				return test.statusReport, test.statusErr
			}
			restart := func(supervisor.Spec) (supervisor.Report, error) {
				return supervisor.Report{}, test.restartErr
			}
			_, err := refreshSupervisedDaemonWith("/bundle/bin/kivgraph", load, status,
				restart, io.Discard)
			if (err != nil) != test.wantErr {
				t.Fatalf("refreshSupervisedDaemonWith() error = %v, wantErr=%t", err, test.wantErr)
			}
		})
	}
}

func TestRefreshClientIntegrationReportsInspectionAndInstallFailures(t *testing.T) {
	targets := []integrations.Target{"inspect-error", "absent", "incompatible", "install-error", "unchanged"}
	status := func(target integrations.Target, scope integrations.Scope) (integrations.Plan, error) {
		if scope != integrations.ScopeUser {
			t.Fatalf("status scope = %q, want user", scope)
		}
		switch target {
		case "inspect-error":
			return integrations.Plan{}, errors.New("inspection failed")
		case "absent":
			return integrations.Plan{Status: "absent"}, nil
		case "incompatible":
			return integrations.Plan{Status: "incompatible"}, nil
		default:
			return integrations.Plan{Status: "managed"}, nil
		}
	}
	install := func(target integrations.Target, scope integrations.Scope, dryRun, force bool) (integrations.Plan, error) {
		if scope != integrations.ScopeUser || dryRun || force {
			t.Fatalf("install arguments = scope %q, dryRun=%t, force=%t", scope, dryRun, force)
		}
		if target == "install-error" {
			return integrations.Plan{}, errors.New("install failed")
		}
		return integrations.Plan{Target: target, Changed: false}, nil
	}

	var stdout bytes.Buffer
	err := refreshClientIntegration(integrations.Manager{}, "hook", targets, status, install, &stdout)
	if err == nil || !strings.Contains(err.Error(), "inspect-error") || !strings.Contains(err.Error(), "install-error") {
		t.Fatalf("refreshClientIntegration() error = %v, want both failures", err)
	}
	if !strings.Contains(stdout.String(), "already current") {
		t.Fatalf("unchanged integration was not reported: %q", stdout.String())
	}
}

func TestRefreshInstalledIntegrationsRejectsUnsupportedClientPlatform(t *testing.T) {
	directory := t.TempDir()
	err := refreshInstalledIntegrations(integrations.Options{
		HomeDir: directory, ProjectDir: directory, Executable: filepath.Join(directory, "kivgraph"), GOOS: "plan9",
	}, integrations.Endpoint{}, false, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "create integration manager") {
		t.Fatalf("refreshInstalledIntegrations() error = %v, want unsupported platform", err)
	}
}

func TestRefreshInstalledIntegrationsContinuesAfterClientReadFailures(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	executable := filepath.Join(t.TempDir(), "kivgraph", "bin", "kivgraph")
	if err := os.MkdirAll(filepath.Join(home, ".claude", "skills", "kivgraph", "SKILL.md"), 0o700); err != nil {
		t.Fatalf("create invalid skill path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("write invalid hook file: %v", err)
	}

	err := refreshInstalledIntegrations(integrations.Options{
		HomeDir: home, ProjectDir: project, Executable: executable, GOOS: "linux",
	}, integrations.Endpoint{}, false, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "hook --target claude-code") || !strings.Contains(err.Error(), "skill --target claude-code") {
		t.Fatalf("refreshInstalledIntegrations() error = %v, want hook and skill failures", err)
	}
}

func TestRefreshMCPIntegrationsRejectsInvalidDaemonEndpoint(t *testing.T) {
	directory := t.TempDir()
	err := refreshMCPIntegrations(integrations.Options{
		HomeDir: directory, ProjectDir: directory, Executable: filepath.Join(directory, "kivgraph"), GOOS: "linux",
	}, integrations.Endpoint{URL: "http://127.0.0.1:7788/mcp"}, true, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "create daemon integration manager") {
		t.Fatalf("refreshMCPIntegrations() error = %v, want invalid endpoint", err)
	}
	err = refreshInstalledIntegrations(integrations.Options{
		HomeDir: directory, ProjectDir: directory, Executable: filepath.Join(directory, "kivgraph"), GOOS: "linux",
	}, integrations.Endpoint{URL: "http://127.0.0.1:7788/mcp"}, true, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "create daemon integration manager") {
		t.Fatalf("refreshInstalledIntegrations() error = %v, want invalid endpoint", err)
	}
}

func TestRefreshMCPIntegrationsRejectsUnsupportedClientPlatform(t *testing.T) {
	directory := t.TempDir()
	err := refreshMCPIntegrations(integrations.Options{
		HomeDir: directory, ProjectDir: directory, Executable: filepath.Join(directory, "kivgraph"), GOOS: "plan9",
	}, integrations.Endpoint{}, false, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "create stdio integration manager") {
		t.Fatalf("refreshMCPIntegrations() error = %v, want unsupported platform", err)
	}
}

type fakeMCPIntegrationManager struct {
	statuses     map[integrations.Target]integrations.Plan
	statusErrs   map[integrations.Target]error
	installErrs  map[integrations.Target]error
	installCalls []integrations.Target
}

func (manager *fakeMCPIntegrationManager) StatusMCP(target integrations.Target, _ integrations.Scope) (integrations.Plan, error) {
	if err := manager.statusErrs[target]; err != nil {
		return integrations.Plan{}, err
	}
	if plan, ok := manager.statuses[target]; ok {
		return plan, nil
	}
	return integrations.Plan{Status: "absent"}, nil
}

func (manager *fakeMCPIntegrationManager) InstallMCP(target integrations.Target, _ integrations.Scope, dryRun, force bool) (integrations.Plan, error) {
	if dryRun || force {
		return integrations.Plan{}, fmt.Errorf("unexpected dryRun=%t force=%t", dryRun, force)
	}
	manager.installCalls = append(manager.installCalls, target)
	if err := manager.installErrs[target]; err != nil {
		return integrations.Plan{}, err
	}
	return integrations.Plan{Target: target, Changed: false}, nil
}

func TestRefreshMCPIntegrationsRoutesAndReportsFailures(t *testing.T) {
	stdioManager := &fakeMCPIntegrationManager{statuses: map[integrations.Target]integrations.Plan{
		integrations.TargetClaudeCode:    {Status: "managed"},
		integrations.TargetClaudeDesktop: {Status: "absent"},
		integrations.TargetCodex:         {Status: "superseded"},
		integrations.TargetOpenCode:      {Status: "absent"},
		integrations.TargetOhMyPi:        {Status: "unknown"},
	}}
	endpointManager := &fakeMCPIntegrationManager{
		statuses: map[integrations.Target]integrations.Plan{
			integrations.TargetClaudeDesktop: {Status: "absent"},
			integrations.TargetCodex:         {Status: "managed"},
		},
		statusErrs:  map[integrations.Target]error{integrations.TargetOpenCode: errors.New("endpoint inspection failed")},
		installErrs: map[integrations.Target]error{integrations.TargetCodex: errors.New("endpoint install failed")},
	}

	var stdout bytes.Buffer
	err := refreshMCPIntegrationsWith(stdioManager, endpointManager, true, &stdout)
	if err == nil || !strings.Contains(err.Error(), "opencode") || !strings.Contains(err.Error(), "codex") || !strings.Contains(err.Error(), "oh-my-pi") {
		t.Fatalf("refreshMCPIntegrationsWith() error = %v, want all routing failures", err)
	}
	if len(stdioManager.installCalls) != 1 || stdioManager.installCalls[0] != integrations.TargetClaudeCode {
		t.Fatalf("stdio install calls = %v, want only claude-code", stdioManager.installCalls)
	}
	if len(endpointManager.installCalls) != 1 || endpointManager.installCalls[0] != integrations.TargetCodex {
		t.Fatalf("endpoint install calls = %v, want only codex", endpointManager.installCalls)
	}
	if !strings.Contains(stdout.String(), "already current") {
		t.Fatalf("managed stdio integration was not reported: %q", stdout.String())
	}

	stdioManager = &fakeMCPIntegrationManager{
		statusErrs: map[integrations.Target]error{integrations.TargetClaudeCode: errors.New("stdio inspection failed")},
	}
	if err := refreshMCPIntegrationsWith(stdioManager, endpointManager, true, io.Discard); err == nil || !strings.Contains(err.Error(), "inspect stdio") {
		t.Fatalf("refreshMCPIntegrationsWith() error = %v, want stdio inspection failure", err)
	}

	stdioManager = &fakeMCPIntegrationManager{}
	if err := refreshMCPIntegrationsWith(stdioManager, nil, false, io.Discard); err != nil {
		t.Fatalf("refreshMCPIntegrationsWith() without endpoint error = %v", err)
	}
	if len(stdioManager.installCalls) != 0 {
		t.Fatalf("stdio installs without an endpoint = %v, want none", stdioManager.installCalls)
	}
}

func TestRefreshInstalledRuntimeReportsFilesystemFailures(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(home, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write home file: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	var stdout, stderr bytes.Buffer
	err := refreshInstalledRuntime(filepath.Join(t.TempDir(), "bin", "kivgraph"), &stdout, &stderr)
	if err == nil {
		t.Fatal("refreshInstalledRuntime() succeeded with an invalid home directory")
	}
	if !strings.Contains(stderr.String(), "update.daemon") {
		t.Fatalf("daemon failure was not reported: %q", stderr.String())
	}
}

func TestRefreshInstalledRuntimeDoesNotCreateMissingConfiguration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	if err := refreshInstalledRuntime(filepath.Join(t.TempDir(), "bin", "kivgraph"), &stdout, &stderr); err != nil {
		t.Fatalf("refreshInstalledRuntime() error = %v", err)
	}
	configPath, err := config.DefaultConfigPath()
	if err != nil {
		t.Fatalf("config.DefaultConfigPath() error = %v", err)
	}
	if _, err := os.Stat(configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refresh created the missing configuration at %q: %v", configPath, err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("missing configuration wrote stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRefreshInstalledRuntimeUsesThePersistedEndpointAsOwnershipEvidence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	executable := filepath.Join(t.TempDir(), "bin", "kivgraph")
	oldEndpoint := integrations.Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "old-token"}
	currentEndpoint := integrations.Endpoint{URL: "http://127.0.0.1:7799/mcp", Token: "current-token"}
	manager, err := integrations.New(integrations.Options{
		HomeDir: home, ProjectDir: t.TempDir(), Executable: executable, GOOS: "linux", Endpoint: oldEndpoint,
	})
	if err != nil {
		t.Fatalf("integrations.New() error = %v", err)
	}
	if _, err := manager.InstallMCP(integrations.TargetClaudeCode, integrations.ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP() error = %v", err)
	}

	var stdout, stderr bytes.Buffer
	refresh := refreshInstalledRuntimeWith(executable, &stdout, &stderr, runtimeRefreshDependencies{
		readEndpoint: func(waitForRestart bool) (installedDaemonEndpointResult, bool, error) {
			if waitForRestart {
				return installedDaemonEndpointResult{Endpoint: currentEndpoint, PID: 123}, true, nil
			}
			return installedDaemonEndpointResult{Endpoint: oldEndpoint}, true, nil
		},
		refreshDaemon: func(string, io.Writer) (daemonRefreshResult, error) {
			return daemonRefreshResult{Restarted: true}, nil
		},
		refreshIntegrations: refreshInstalledIntegrations,
	})
	if refresh.Err != nil {
		t.Fatalf("refreshInstalledRuntimeWith() error = %v", refresh.Err)
	}
	updated, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("read refreshed MCP config: %v", err)
	}
	content := string(updated)
	if strings.Contains(content, oldEndpoint.URL) || !strings.Contains(content, currentEndpoint.URL) ||
		strings.Contains(content, oldEndpoint.Token) || !strings.Contains(content, currentEndpoint.Token) {
		t.Fatalf("MCP endpoint was not refreshed from old=%q to current=%q: %s",
			oldEndpoint.URL, currentEndpoint.URL, updated)
	}
}

func TestRefreshInstalledRuntimeProtectsTheSupervisedPIDAfterFailure(t *testing.T) {
	oldEndpoint := integrations.Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "old-token"}
	var stdout, stderr bytes.Buffer
	refresh := refreshInstalledRuntimeWith("/bundle/bin/kivgraph", &stdout, &stderr, runtimeRefreshDependencies{
		readEndpoint: func(waitForRestart bool) (installedDaemonEndpointResult, bool, error) {
			if waitForRestart {
				return installedDaemonEndpointResult{}, false, errors.New("daemon did not come back")
			}
			return installedDaemonEndpointResult{
				Endpoint: oldEndpoint,
				PID:      321,
			}, true, nil
		},
		refreshDaemon: func(string, io.Writer) (daemonRefreshResult, error) {
			return daemonRefreshResult{Supervised: true}, errors.New("stale unit could not be repaired")
		},
		refreshIntegrations: func(integrations.Options, integrations.Endpoint, bool, io.Writer) error {
			return nil
		},
	})
	if refresh.SupervisedDaemonPID != 321 {
		t.Fatalf("SupervisedDaemonPID = %d, want 321", refresh.SupervisedDaemonPID)
	}
	if refresh.RefreshedDaemonPID != 0 {
		t.Fatalf("RefreshedDaemonPID = %d after a failed refresh, want 0", refresh.RefreshedDaemonPID)
	}
	if refresh.Err == nil {
		t.Fatal("refreshInstalledRuntimeWith() succeeded after the daemon refresh failed")
	}
}

func TestUpdateDoesNotOfferASupervisedDaemonForStoppingAfterRefreshFailure(t *testing.T) {
	fixture := &stopFixture{processes: []procstat.Process{kivgraphProcess(121, "daemon")}}
	var stdout, stderr bytes.Buffer
	if code := runUpdateWithRunnerAtExecutable(nil, nil, &stdout, &stderr,
		installedRunner(), fixture.list, fixture.signal, func([]procstat.Process) (daemonRestart, error) {
			t.Fatal("the supervised daemon was offered to stale-process cleanup")
			return daemonRestart{}, nil
		}, true, "/bundle/bin/kivgraph", func(string, io.Writer, io.Writer) updatePostInstallResult {
			return updatePostInstallResult{SupervisedDaemonPID: 121, Err: errors.New("stale unit could not be repaired")}
		}); code != 1 {
		t.Fatalf("runUpdateWithRunnerAtExecutable() = %d, want 1; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "update.stale") || strings.Contains(stdout.String(), "stop them now") {
		t.Fatalf("supervised daemon was offered for stopping after refresh failure: %s", stdout.String())
	}
}

func TestInstalledDaemonEndpointReportsRestartTimeout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := config.Initialize(config.InitOptions{}); err != nil {
		t.Fatalf("config.Initialize() error = %v", err)
	}
	previousDeadline := endpointDeadline
	endpointDeadline = 10 * time.Millisecond
	t.Cleanup(func() { endpointDeadline = previousDeadline })

	_, ok, err := installedDaemonEndpoint(true)
	if err == nil || ok || !strings.Contains(err.Error(), "wait for the refreshed daemon endpoint") {
		t.Fatalf("installedDaemonEndpoint() = ok=%t, err=%v; want timeout", ok, err)
	}
}

func TestRefreshInstalledRuntimeLeavesMissingSurfacesMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	if _, err := config.Initialize(config.InitOptions{}); err != nil {
		t.Fatalf("config.Initialize() error = %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := refreshInstalledRuntime(filepath.Join(t.TempDir(), "bin", "kivgraph"),
		&stdout, &stderr); err != nil {
		t.Fatalf("refreshInstalledRuntime() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "update.daemon: no installed supervisor") || stderr.Len() != 0 {
		t.Fatalf("refresh of missing runtime surfaces wrote stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	manager, err := integrations.New(integrations.Options{
		HomeDir: home, ProjectDir: t.TempDir(), Executable: filepath.Join(t.TempDir(), "bin", "kivgraph"), GOOS: "linux",
	})
	if err != nil {
		t.Fatalf("integrations.New() error = %v", err)
	}
	for _, target := range integrations.HookTargets() {
		plan, err := manager.StatusHook(target, integrations.ScopeUser)
		if err != nil {
			t.Fatalf("StatusHook(%s) error = %v", target, err)
		}
		assertPathAbsent(t, plan.Path)
	}
	for _, target := range integrations.SkillTargets() {
		plan, err := manager.StatusSkill(target, integrations.ScopeUser)
		if err != nil {
			t.Fatalf("StatusSkill(%s) error = %v", target, err)
		}
		assertPathAbsent(t, plan.Path)
	}
	for _, target := range integrations.KnownTargets() {
		plan, err := manager.StatusMCP(target, integrations.ScopeUser)
		if err != nil {
			t.Fatalf("StatusMCP(%s) error = %v", target, err)
		}
		assertPathAbsent(t, plan.Path)
	}
}

func assertPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime refresh created %q: %v", path, err)
	}
}

func TestInstalledDaemonEndpointReadsPublishedEndpoint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	result, err := config.Initialize(config.InitOptions{})
	if err != nil {
		t.Fatalf("config.Initialize() error = %v", err)
	}
	loaded, err := config.Load(result.ConfigPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	directory := stateDirectory(loaded)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("create state directory: %v", err)
	}
	encoded, err := json.Marshal(daemon.Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "token"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(daemon.EndpointPath(directory), encoded, 0o600); err != nil {
		t.Fatalf("write endpoint: %v", err)
	}

	endpoint, ok, err := installedDaemonEndpoint(false)
	if err != nil {
		t.Fatalf("installedDaemonEndpoint() error = %v", err)
	}
	if !ok || endpoint.URL != "http://127.0.0.1:7788/mcp" || endpoint.Token != "token" {
		t.Fatalf("installedDaemonEndpoint() = %#v, %t; want published endpoint", endpoint, ok)
	}
}

func TestInstalledDaemonEndpointWaitsForReachableEndpoint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	result, err := config.Initialize(config.InitOptions{})
	if err != nil {
		t.Fatalf("config.Initialize() error = %v", err)
	}
	loaded, err := config.Load(result.ConfigPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	directory := stateDirectory(loaded)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("create state directory: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()
	endpoint := daemon.Endpoint{
		URL:   fmt.Sprintf("http://%s/mcp", listener.Addr()),
		Token: "token",
	}
	encoded, err := json.Marshal(endpoint)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(daemon.EndpointPath(directory), encoded, 0o600); err != nil {
		t.Fatalf("write endpoint: %v", err)
	}

	got, ok, err := installedDaemonEndpoint(true)
	if err != nil {
		t.Fatalf("installedDaemonEndpoint() error = %v", err)
	}
	if !ok || got.URL != endpoint.URL || got.Token != endpoint.Token {
		t.Fatalf("installedDaemonEndpoint() = %#v, %t; want %#v, true", got, ok, endpoint)
	}
}
