package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/toolchain"
)

type toolchainStatusOptions struct {
	ConfigPath string
	JSONOutput bool
}

func toolchainStatusFlagSet(options *toolchainStatusOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("toolchain status", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.ConfigPath, "config", "", "configuration file")
	flags.BoolVar(&options.JSONOutput, "json", false, "print the status as JSON")
	return flags
}

type toolchainInstallOptions struct {
	ConfigPath string
	Version    string
	JSONOutput bool
}

func toolchainInstallFlagSet(options *toolchainInstallOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("toolchain install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.ConfigPath, "config", "", "configuration file")
	flags.StringVar(&options.Version, "version", toolchain.DefaultPyrightVersion, "exact tool version")
	flags.BoolVar(&options.JSONOutput, "json", false, "print the result as JSON")
	return flags
}

type toolchainRemoveOptions struct {
	ConfigPath string
	Yes        bool
	JSONOutput bool
}

func toolchainRemoveFlagSet(options *toolchainRemoveOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("toolchain remove", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.ConfigPath, "config", "", "configuration file")
	flags.BoolVar(&options.Yes, "yes", false, "confirm removing the managed tool")
	flags.BoolVar(&options.JSONOutput, "json", false, "print the result as JSON")
	return flags
}

type toolchainPythonStatus struct {
	AnalyzerCommand string `json:"analyzer_command"`
	AnalyzerMode    string `json:"analyzer_mode"`
	Managed         bool   `json:"managed"`
}

type toolchainStatusOutput struct {
	StateDirectory string                 `json:"state_directory"`
	ConfigPath     string                 `json:"config_path,omitempty"`
	Python         toolchainPythonStatus  `json:"python"`
	Tools          []toolchain.ToolStatus `json:"tools"`
}

type toolchainInstallOutput struct {
	Tool            string `json:"tool"`
	State           string `json:"state"`
	Version         string `json:"version"`
	Executable      string `json:"executable"`
	ConfigPath      string `json:"config_path"`
	AnalyzerMode    string `json:"analyzer_mode"`
	AnalyzerCommand string `json:"analyzer_command"`
}

type toolchainRemoveOutput struct {
	Tool             string `json:"tool"`
	State            string `json:"state"`
	ConfigPath       string `json:"config_path"`
	AnalyzerMode     string `json:"analyzer_mode"`
	AnalyzerDisabled bool   `json:"analyzer_disabled"`
}

func runToolchainStatus(args []string, stdout, stderr io.Writer) int {
	var options toolchainStatusOptions
	flags := toolchainStatusFlagSet(&options)
	if parsed, code := parseCommandFlags("toolchain status", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "toolchain status: unexpected arguments: %v", flags.Args())
		return 2
	}
	stateDirectory, configPath, python, err := toolchainContext(options.ConfigPath)
	if err != nil {
		writeCommandError(stderr, "toolchain status: %v", err)
		return 1
	}
	tools, err := toolchain.Status(stateDirectory)
	if err != nil {
		writeCommandError(stderr, "toolchain status: %v", err)
		return 1
	}
	output := toolchainStatusOutput{
		StateDirectory: stateDirectory,
		ConfigPath:     configPath,
		Python:         python,
		Tools:          tools,
	}
	if options.JSONOutput {
		return writeToolchainJSON(stdout, stderr, output)
	}
	fmt.Fprintf(stdout, "toolchain.state: %s\n", stateDirectory)
	if configPath == "" {
		fmt.Fprintln(stdout, "python.analyzer: fallback (no configuration file yet)")
	} else {
		fmt.Fprintf(stdout, "python.analyzer: %s (%s)\n", python.AnalyzerMode, configPath)
	}
	for _, status := range tools {
		if status.State == "installed" {
			writeSuccess(stdout, "toolchain.%s: installed %s (%s)", status.Name, status.Version, status.Executable)
			continue
		}
		writeInfo(stdout, "toolchain.%s: %s (%s)", status.Name, status.State, status.Detail)
	}
	return 0
}

func runToolchainInstall(tool string, args []string, stdout, stderr io.Writer) int {
	if err := toolchain.ValidateName(tool); err != nil {
		writeCommandError(stderr, "toolchain install: %v", err)
		return 2
	}
	var options toolchainInstallOptions
	flags := toolchainInstallFlagSet(&options)
	if parsed, code := parseCommandFlags("toolchain install", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "toolchain install: unexpected arguments: %v", flags.Args())
		return 2
	}
	loaded, err := ensureLoadedConfiguration(options.ConfigPath)
	if err != nil {
		writeCommandError(stderr, "toolchain install: %v", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	status, err := toolchain.Install(ctx, stateDirectory(loaded), tool, options.Version, "")
	if err != nil {
		writeCommandError(stderr, "toolchain install: %v", err)
		return 1
	}
	command := toolchain.PyrightAnalyzerCommand(status.Executable)
	if previous := loaded.Config.Python.AnalyzerCommand; previous != "" && previous != config.DefaultPythonAnalyzerCommand && previous != command {
		writeWarning(stderr, "python.analyzer: replacing configured command %q with managed Pyright %q", previous, command)
	}
	if err := config.SetPythonAnalyzer(loaded.ConfigPath, command, "exact"); err != nil {
		writeCommandError(stderr, "toolchain install: activate Python analyzer: %v", err)
		return 1
	}
	if options.JSONOutput {
		return writeToolchainJSON(stdout, stderr, toolchainInstallOutput{
			Tool:            status.Name,
			State:           status.State,
			Version:         status.Version,
			Executable:      status.Executable,
			ConfigPath:      loaded.ConfigPath,
			AnalyzerMode:    "exact",
			AnalyzerCommand: command,
		})
	}
	writeSuccess(stdout, "toolchain.%s: installed %s", status.Name, status.Version)
	writeSuccess(stdout, "python.analyzer: exact mode enabled in %s", loaded.ConfigPath)
	return 0
}

func runToolchainRemove(tool string, args []string, stdout, stderr io.Writer) int {
	if err := toolchain.ValidateName(tool); err != nil {
		writeCommandError(stderr, "toolchain remove: %v", err)
		return 2
	}
	var options toolchainRemoveOptions
	flags := toolchainRemoveFlagSet(&options)
	if parsed, code := parseCommandFlags("toolchain remove", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "toolchain remove: unexpected arguments: %v", flags.Args())
		return 2
	}
	if !options.Yes {
		writeCommandError(stderr, "toolchain remove: --yes is required because this deletes managed tool state")
		return 2
	}
	stateRoot, configPath, python, err := toolchainContext(options.ConfigPath)
	if err != nil {
		writeCommandError(stderr, "toolchain remove: %v", err)
		return 1
	}
	statuses, err := toolchain.Status(stateRoot)
	if err != nil {
		writeCommandError(stderr, "toolchain remove: inspect managed tool: %v", err)
		return 1
	}
	stateBeforeRemoval := "missing"
	for _, status := range statuses {
		if status.Name == tool {
			stateBeforeRemoval = status.State
			break
		}
	}
	wasManaged := python.Managed
	managed := wasManaged
	if wasManaged {
		var disabled bool
		disabled, err = config.SetPythonAnalyzerIfCurrent(configPath, python.AnalyzerCommand, config.DefaultPythonAnalyzerCommand, "fallback")
		if err != nil {
			writeCommandError(stderr, "toolchain remove: disable Python analyzer: %v", err)
			return 1
		}
		managed = disabled
	}
	if err := toolchain.Remove(stateRoot, tool); err != nil {
		if managed {
			restored, restoreErr := config.SetPythonAnalyzerIfCurrent(configPath, config.DefaultPythonAnalyzerCommand, python.AnalyzerCommand, python.AnalyzerMode)
			if restoreErr != nil {
				writeCommandError(stderr, "toolchain remove: restore Python analyzer after failed removal: %v", restoreErr)
			} else if !restored {
				writeWarning(stderr, "python.analyzer: configuration changed after fallback activation; verify analyzer mode and command in %s", configPath)
			}
		}
		writeCommandError(stderr, "toolchain remove: %v", err)
		return 1
	}
	removalState := "removed"
	if stateBeforeRemoval == "missing" {
		removalState = "missing"
	}
	if wasManaged && !managed {
		writeWarning(stderr, "python.analyzer: configuration changed during removal; verify analyzer mode and command in %s", configPath)
	}
	if options.JSONOutput {
		analyzerMode := python.AnalyzerMode
		if managed {
			analyzerMode = "fallback"
		} else if wasManaged {
			// The compare-and-set found a different command; report the mode now on disk.
			_, _, refreshed, refreshErr := toolchainContext(configPath)
			if refreshErr != nil {
				writeWarning(stderr, "python.analyzer: could not verify configuration after concurrent change: %v", refreshErr)
				analyzerMode = ""
			} else {
				analyzerMode = refreshed.AnalyzerMode
			}
		}
		return writeToolchainJSON(stdout, stderr, toolchainRemoveOutput{
			Tool:             tool,
			State:            removalState,
			ConfigPath:       configPath,
			AnalyzerMode:     analyzerMode,
			AnalyzerDisabled: managed,
		})
	}
	if stateBeforeRemoval == "missing" {
		writeInfo(stdout, "toolchain.%s: missing (nothing to remove)", tool)
	} else {
		writeSuccess(stdout, "toolchain.%s: removed", tool)
	}
	if managed {
		writeSuccess(stdout, "python.analyzer: fallback mode restored in %s", configPath)
	}
	return 0
}

// runToolchainInstallParent and runToolchainRemoveParent are hidden dispatch
// fallbacks. They make an unsupported tool produce a useful validation error,
// while the visible exact forms keep completion honest about what this build
// can actually install.
func runToolchainInstallParent(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		writeCommandError(stderr, "toolchain install: a tool name is required (want %s)", toolchain.Pyright)
		return 2
	}
	return runToolchainInstall(args[0], args[1:], stdout, stderr)
}

func runToolchainRemoveParent(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		writeCommandError(stderr, "toolchain remove: a tool name is required (want %s)", toolchain.Pyright)
		return 2
	}
	return runToolchainRemove(args[0], args[1:], stdout, stderr)
}

func writeToolchainJSON(stdout, stderr io.Writer, value any) int {
	if err := json.NewEncoder(stdout).Encode(value); err != nil {
		writeCommandError(stderr, "toolchain: write JSON: %v", err)
		return 1
	}
	return 0
}

func toolchainContext(configPath string) (string, string, toolchainPythonStatus, error) {
	defaultPath, err := config.DefaultConfigPath()
	if err != nil {
		return "", "", toolchainPythonStatus{}, err
	}
	if strings.TrimSpace(configPath) == "" {
		if _, statErr := os.Stat(defaultPath); errors.Is(statErr, os.ErrNotExist) {
			state, stateErr := config.DefaultStateDirectory()
			if stateErr != nil {
				return "", "", toolchainPythonStatus{}, stateErr
			}
			defaults := config.DefaultConfig().Python
			return state, "", toolchainPythonStatus{
				AnalyzerCommand: defaults.AnalyzerCommand,
				AnalyzerMode:    defaults.AnalyzerMode,
			}, nil
		} else if statErr != nil {
			return "", "", toolchainPythonStatus{}, fmt.Errorf("inspect config %q: %w", defaultPath, statErr)
		}
	}
	loaded, err := config.LoadProfile(configPath, "")
	if err != nil {
		return "", "", toolchainPythonStatus{}, err
	}
	state := stateDirectory(loaded)
	return state, loaded.ConfigPath, toolchainPythonStatus{
		AnalyzerCommand: loaded.Config.Python.AnalyzerCommand,
		AnalyzerMode:    loaded.Config.Python.AnalyzerMode,
		Managed:         toolchain.IsManagedPyrightCommand(loaded.Config.Python.AnalyzerCommand, state),
	}, nil
}
