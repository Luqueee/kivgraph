package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/procstat"
	"github.com/Luqueee/kivgraph/internal/supervisor"
)

// supervisorOptions carries the flags of `kivgraph daemon install`.
//
// They mirror the daemon's own, because what the supervisor records is a daemon
// invocation: a supervised daemon bound somewhere other than the default has to
// be installable, or the operator is left starting it by hand -- which is the
// state this command exists to end.
type supervisorOptions struct {
	Address     string
	AllowRemote bool
}

// supervisorFlagSet declares them once, so the help and the completion describe
// the flags the command really parses.
func supervisorFlagSet(operation string, configPath *string, options *supervisorOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("daemon "+operation, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(configPath, "config", "", "configuration file")
	if operation == "install" {
		flags.StringVar(&options.Address, "addr", "", "HTTP address the supervised daemon binds")
		flags.BoolVar(&options.AllowRemote, "allow-remote", false,
			"record a bind outside loopback, which sends names, file paths and source metadata off this host")
	}
	return flags
}

// supervisorSpec resolves which daemon the supervisor should own.
//
// The state directory is the identity, and it is read from the configuration
// rather than assumed: two configurations get two daemons, so they must get two
// units. The executable is resolved to an absolute path because the installed
// file outlives the shell that wrote it -- a relative path would resolve against
// whatever directory launchd or systemd happened to start in.
//
// The configuration comes back with the spec because the caller needs it too:
// `daemon install` asks which languages are registered before deciding whether
// a missing node runtime is worth naming, and reloading it there would be the
// same file read twice with a second failure path for the same error.
func supervisorSpec(configPath string, options supervisorOptions) (supervisor.Spec, config.Loaded, error) {
	loaded, err := config.Load(configPath)
	if err != nil {
		return supervisor.Spec{}, config.Loaded{}, fmt.Errorf("read the configuration: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return supervisor.Spec{}, loaded, fmt.Errorf("resolve this executable: %w", err)
	}
	address := options.Address
	if options.AllowRemote && address == "" {
		// --allow-remote without an address would record a loopback bind and
		// a permission that changes nothing, which reads as a remote daemon
		// and is not one.
		return supervisor.Spec{}, loaded, errors.New("--allow-remote needs --addr: it permits a bind, it does not choose one")
	}
	return supervisor.Spec{
		Executable:     executable,
		StateDirectory: stateDirectory(loaded),
		ConfigPath:     loaded.ConfigPath,
		Address:        address,
		AllowRemote:    options.AllowRemote,
	}, loaded, nil
}

// warnAboutAnUnreachableNode says so when the PATH an install just recorded
// cannot resolve a node that runs.
//
// The unit records the PATH of the shell that ran `daemon install`, so this is
// the same question the daemon will ask later -- which makes it the one moment
// where the answer is cheap and the remedy is in front of the person who can
// apply it. Left unsaid, the daemon fails at `exec node` with a 127 that names
// the worker and not the environment, and it fails on every rebuild until
// something stops it.
//
// `exec.LookPath` alone only proves a file is there and marked executable: a
// broken install -- a binary for the wrong architecture, a shim left behind by
// an nvm uninstall -- resolves and still cannot run, and the same 127 follows.
// So the check runs it, bounded by a timeout short enough that a machine with
// no node at all does not make `daemon install` noticeably slower for the
// common case that already returned from `LookPath`.
//
// It is a warning and not a refusal, and the install still succeeds. A
// workspace with no TypeScript or JavaScript in it never runs the worker, and
// refusing to supervise a daemon that would have worked would be a worse
// failure than the one being reported. That is also why it is asked only when a
// registered repository declares one of those languages: `doctor` reports a
// toolchain nobody needs as "not configured" rather than as a problem, and this
// says nothing at all.
func warnAboutAnUnreachableNode(stderr io.Writer, loaded config.Loaded) {
	needed := false
	for _, repository := range loaded.Repositories.Repositories {
		for _, language := range repository.Languages {
			switch strings.ToLower(strings.TrimSpace(language)) {
			case "typescript", "ts", "javascript", "js", "node":
				needed = true
			}
		}
	}
	if !needed {
		return
	}
	if nodeRuns() {
		return
	}
	writeWarning(stderr, "daemon install: node is not on the PATH this unit recorded, "+
		"so the TypeScript worker will fail and every rebuild with it")
	writeWarning(stderr, "daemon install: install node, then run `kivgraph daemon install` "+
		"again from a shell that can reach it")
}

// nodeRuns reports whether node both resolves and executes.
func nodeRuns() bool {
	if _, err := exec.LookPath("node"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "node", "--version").Run() == nil
}

// restartSupervisedDaemon puts the supervised daemon of the default
// configuration back on the executable that is now on disk, and says which pid
// was answering from the replaced one.
//
// `update` replaces the bundle in place, so the path in the installed unit
// still resolves and the image behind it does not. ADR 0069 sells "an `update`
// that restarts instead of killing eight" as one of the two things only a
// daemon allows, and until this existed the command did neither: it listed the
// daemon beside the `serve` processes and offered to stop it.
//
// The daemon is identified by the pid it published, never by its command line.
// Two state directories have two daemons, and restarting the wrong one would
// take a graph down that this update never touched.
//
// Nothing here is an error, and what the outcomes differ in is what they let
// the caller say afterwards.
//
// Three establish nothing and answer `ownershipUnknown`: no configuration to
// locate this daemon by, no readable endpoint to identify it with, and a stale
// daemon that is not the one this configuration published. In all three the
// `kivgraph daemon` in the list may well be another state directory's, already
// supervised, and advising its operator to install a supervisor would be a
// guess.
//
// Two establish that nobody has it -- no unit, or a platform with none to
// install -- which is the only case the caller's advice fits.
//
// And one is supervised without being restarted: a unit that exists and was
// edited by hand. Nothing is restarted through it, because that would start
// whatever the operator wrote instead of what this spec describes.
//
// The default configuration and not a flag: `update` takes no `--config`, and
// neither does `stop`. A daemon installed against another configuration
// produces a unit whose contents differ, `Status` reports it stale, and this
// falls back to what the command did before -- conservative, and it says so.
func restartSupervisedDaemon(targets []procstat.Process) (daemonRestart, error) {
	loaded, err := config.Load("")
	if err != nil {
		// A machine with no configuration of its own has no daemon of this
		// state directory either: the stale process in the list was started
		// with a --config somewhere else, and its graph is not the one this
		// update is about. A configuration that exists and cannot be read is a
		// different thing and is reported.
		if errors.Is(err, fs.ErrNotExist) {
			return daemonRestart{Ownership: ownershipUnknown}, nil
		}
		return daemonRestart{Ownership: ownershipUnknown}, fmt.Errorf("read the configuration: %w", err)
	}
	endpoint, err := daemon.ReadEndpoint(stateDirectory(loaded))
	if err != nil {
		// A daemon writes this before it serves, so no endpoint means this
		// configuration has none running and the stale process belongs to
		// another one. A file that exists and cannot be read lands here too,
		// and answers the same: unknown, not unowned.
		return daemonRestart{Ownership: ownershipUnknown}, nil
	}
	if !slices.ContainsFunc(targets, func(target procstat.Process) bool {
		return target.PID == endpoint.PID
	}) {
		return daemonRestart{Ownership: ownershipUnknown}, nil
	}
	spec, _, err := supervisorSpec("", supervisorOptions{})
	if err != nil {
		return daemonRestart{Ownership: ownershipUnknown}, err
	}
	report, err := supervisor.Restart(spec)
	if err != nil {
		return daemonRestart{
			Label: report.Label, PID: endpoint.PID, Ownership: ownershipSupervised,
		}, err
	}
	switch report.State {
	case supervisor.StateInstalled:
		return daemonRestart{
			Label: report.Label, PID: endpoint.PID, Ownership: ownershipSupervised,
		}, nil
	case supervisor.StateStale:
		return daemonRestart{
			Label: report.Label, Ownership: ownershipSupervised, Detail: report.Detail,
		}, nil
	}
	// Absent or unsupported, over a daemon this configuration published and
	// this update found stale: nobody owns it, and the caution meant for a
	// process a client spawned is the right one here after all.
	return daemonRestart{Ownership: ownershipNone}, nil
}

// runSupervisorCommand executes `daemon install`, `daemon remove` and
// `daemon status`.
func runSupervisorCommand(operation string, args []string, stdout, stderr io.Writer) int {
	configPath := ""
	var options supervisorOptions
	flags := supervisorFlagSet(operation, &configPath, &options)
	if ok, code := parseCommandFlags("daemon "+operation, flags, args, stdout, stderr); !ok {
		return code
	}
	if flags.NArg() > 0 {
		writeCommandError(stderr, "daemon %s: unexpected argument %q", operation, flags.Arg(0))
		return 1
	}
	spec, loaded, err := supervisorSpec(configPath, options)
	if err != nil {
		writeCommandError(stderr, "daemon %s: %v", operation, err)
		return 1
	}

	var report supervisor.Report
	switch operation {
	case "install":
		report, err = supervisor.Install(spec)
	case "remove":
		report, err = supervisor.Remove(spec)
	case "status":
		report, err = supervisor.Status(spec)
	default:
		writeCommandError(stderr, "daemon: unknown operation %q", operation)
		return 1
	}
	if err != nil {
		// An unsupported platform is a declared limitation, not a crash: it is
		// named on stderr with what the operator has to do instead, and it
		// still exits non-zero so a script cannot read it as success.
		if errors.Is(err, supervisor.ErrUnsupportedPlatform) {
			writeWarning(stderr, "daemon %s: %s", operation, report.Detail)
			writeWarning(stderr, "daemon %s: start it yourself with `kivgraph daemon`", operation)
			return 1
		}
		writeCommandError(stderr, "daemon %s: %v", operation, err)
		return 1
	}

	if operation == "install" {
		announceSupervisorInstall(loaded)
		warnAboutAnUnreachableNode(stderr, loaded)
	}
	writeSupervisorReport(stdout, stderr, operation, spec, report)
	return 0
}

func writeSupervisorReport(stdout, stderr io.Writer, operation string, spec supervisor.Spec, report supervisor.Report) {
	var endpointText string
	var endpointErr error
	if operation == "install" || operation == "status" {
		endpoint, err := daemon.ReadEndpoint(spec.StateDirectory)
		endpointErr = err
		switch {
		case err == nil:
			endpointText = endpoint.URL
		case errors.Is(err, os.ErrNotExist):
			endpointText = "not published yet"
		default:
			writeWarning(stderr, "daemon.endpoint: %v", err)
			endpointText = "unreadable"
		}
	}

	if !integrationTUIIsInteractive(stdout) {
		fmt.Fprintf(stdout, "daemon.supervisor: state=%s label=%s\n", report.State, report.Label)
		if report.Path != "" {
			fmt.Fprintf(stdout, "daemon.supervisor: unit=%s\n", report.Path)
		}
		if report.Detail != "" {
			fmt.Fprintf(stdout, "daemon.supervisor: %s\n", report.Detail)
		}
		if operation == "status" && (report.State == supervisor.StateAbsent || report.State == supervisor.StateStale) {
			fmt.Fprintln(stdout, "daemon.supervisor: install one with `kivgraph daemon install`")
		}
		if operation == "install" {
			switch {
			case endpointErr == nil:
				fmt.Fprintf(stdout, "daemon.endpoint: %s\n", endpointText)
			case errors.Is(endpointErr, os.ErrNotExist):
				fmt.Fprintln(stdout, "daemon.endpoint: not published yet: run `kivgraph daemon status` once it is up")
			}
		}
		return
	}

	paint := styleFor(stdout)
	rows := []keyValueRow{
		{Key: "State", Value: string(report.State), ValueStyle: supervisorStateStyle(report.State, paint)},
		{Key: "Label", Value: report.Label},
	}
	if report.Path != "" {
		rows = append(rows, keyValueRow{Key: "Unit", Value: report.Path})
	}
	if report.Detail != "" {
		rows = append(rows, keyValueRow{Key: "Detail", Value: report.Detail})
	}
	if operation == "status" && (report.State == supervisor.StateAbsent || report.State == supervisor.StateStale) {
		rows = append(rows, keyValueRow{Key: "Action", Value: "install one with `kivgraph daemon install`", ValueStyle: paint.accent})
	}
	if operation == "install" || operation == "status" {
		rows = append(rows, keyValueRow{Key: "Endpoint", Value: endpointText})
	}
	writeKeyValueTable(stdout, "Daemon supervisor", rows)
}

func supervisorStateStyle(state supervisor.State, paint style) string {
	switch state {
	case supervisor.StateInstalled:
		return paint.success
	case supervisor.StateStale, supervisor.StateUnsupported:
		return paint.warning
	default:
		return paint.dim
	}
}

// reportDoctorSupervisor adds the daemon's ownership to the doctor report.
//
// It never fails the run. An absent supervisor is the state of a machine that
// never asked for one, and an unsupported platform is a declared limitation --
// turning either into a FAIL would put doctor in red on every installation that
// registers clients against `serve`, which is exactly how a real failure stops
// being noticed.
func reportDoctorSupervisor(result func(name string, passed bool, detail string), loaded config.Loaded) {
	executable, err := os.Executable()
	if err != nil {
		result("daemon.supervisor", true, "this executable cannot be resolved, so no unit can be described")
		return
	}
	report, err := supervisor.Status(supervisor.Spec{
		Executable:     executable,
		StateDirectory: stateDirectory(loaded),
		ConfigPath:     loaded.ConfigPath,
	})
	if err != nil {
		result("daemon.supervisor", true, err.Error())
		return
	}
	detail := string(report.State)
	if report.Path != "" {
		detail += " " + report.Path
	}
	switch report.State {
	case supervisor.StateAbsent, supervisor.StateStale:
		detail += ": install one with `kivgraph daemon install`"
	case supervisor.StateUnsupported:
		// The daemon still runs here; what it lacks is something to start it
		// again. Saying that is the whole report, and it is not a remedy.
		detail += ": the daemon will run but nothing will restart it"
	}
	result("daemon.supervisor", true, detail)
}
