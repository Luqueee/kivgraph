package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"

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
func supervisorSpec(configPath string, options supervisorOptions) (supervisor.Spec, error) {
	loaded, err := config.Load(configPath)
	if err != nil {
		return supervisor.Spec{}, fmt.Errorf("read the configuration: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return supervisor.Spec{}, fmt.Errorf("resolve this executable: %w", err)
	}
	address := options.Address
	if options.AllowRemote && address == "" {
		// --allow-remote without an address would record a loopback bind and
		// a permission that changes nothing, which reads as a remote daemon
		// and is not one.
		return supervisor.Spec{}, errors.New("--allow-remote needs --addr: it permits a bind, it does not choose one")
	}
	return supervisor.Spec{
		Executable:     executable,
		StateDirectory: stateDirectory(loaded),
		ConfigPath:     configPath,
		Address:        address,
		AllowRemote:    options.AllowRemote,
	}, nil
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
// Four outcomes are "nothing to do here", and all four return a zero pid
// rather than an error, because each leaves the caller's existing behaviour
// correct: no configuration at all, no endpoint file, a daemon that is not
// among the stale processes, and a unit that is absent, stale or on a platform
// with no supervisor.
//
// The default configuration and not a flag: `update` takes no `--config`, and
// neither does `stop`. A daemon installed against another configuration
// produces a unit whose contents differ, `Status` reports it stale, and this
// falls back to what the command did before -- conservative, and it says so.
func restartSupervisedDaemon(targets []procstat.Process) (string, int, error) {
	loaded, err := config.Load("")
	if err != nil {
		// A machine with no configuration of its own has no daemon of this
		// state directory either: the stale process in the list was started
		// with a --config somewhere else, and its graph is not the one this
		// update is about. A configuration that exists and cannot be read is a
		// different thing and is reported.
		if errors.Is(err, fs.ErrNotExist) {
			return "", 0, nil
		}
		return "", 0, fmt.Errorf("read the configuration: %w", err)
	}
	endpoint, err := daemon.ReadEndpoint(stateDirectory(loaded))
	if err != nil {
		// The endpoint is written before the daemon serves and removed when it
		// stops, so its absence is the answer rather than a failure to get one.
		return "", 0, nil
	}
	if !slices.ContainsFunc(targets, func(target procstat.Process) bool {
		return target.PID == endpoint.PID
	}) {
		return "", 0, nil
	}
	spec, err := supervisorSpec("", supervisorOptions{})
	if err != nil {
		return "", 0, err
	}
	report, err := supervisor.Restart(spec)
	if err != nil {
		return report.Label, endpoint.PID, err
	}
	if report.State != supervisor.StateInstalled {
		return "", 0, nil
	}
	return report.Label, endpoint.PID, nil
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
		fmt.Fprintf(stderr, "daemon %s: unexpected argument %q\n", operation, flags.Arg(0))
		return 1
	}
	spec, err := supervisorSpec(configPath, options)
	if err != nil {
		fmt.Fprintf(stderr, "daemon %s: %v\n", operation, err)
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
		fmt.Fprintf(stderr, "daemon: unknown operation %q\n", operation)
		return 1
	}
	if err != nil {
		// An unsupported platform is a declared limitation, not a crash: it is
		// named on stderr with what the operator has to do instead, and it
		// still exits non-zero so a script cannot read it as success.
		if errors.Is(err, supervisor.ErrUnsupportedPlatform) {
			fmt.Fprintf(stderr, "daemon %s: %s\n", operation, report.Detail)
			fmt.Fprintf(stderr, "daemon %s: start it yourself with `kivgraph daemon`\n", operation)
			return 1
		}
		fmt.Fprintf(stderr, "daemon %s: %v\n", operation, err)
		return 1
	}

	fmt.Fprintf(stdout, "daemon.supervisor: state=%s label=%s\n", report.State, report.Label)
	if report.Path != "" {
		fmt.Fprintf(stdout, "daemon.supervisor: unit=%s\n", report.Path)
	}
	if report.Detail != "" {
		fmt.Fprintf(stdout, "daemon.supervisor: %s\n", report.Detail)
	}
	// A status that only names the state leaves the reader to guess the
	// remedy, and the remedy is the reason they asked. Where there is no
	// supervisor to install, the remedy is not that command -- naming it there
	// sends an operator to run something that can only answer that it will not
	// work, which is worse than the silence it was written to avoid.
	if operation == "status" && (report.State == supervisor.StateAbsent || report.State == supervisor.StateStale) {
		fmt.Fprintf(stdout, "daemon.supervisor: install one with `kivgraph daemon install`\n")
	}
	// After an install the operator's next question is where clients should
	// point, and the answer is the endpoint the daemon publishes. It is read
	// rather than predicted: a daemon that has not come up yet has not written
	// one, and saying so beats printing an address nothing is listening on.
	if operation == "install" {
		endpoint, endpointErr := daemon.ReadEndpoint(spec.StateDirectory)
		switch {
		case endpointErr == nil:
			fmt.Fprintf(stdout, "daemon.endpoint: %s\n", endpoint.URL)
		case errors.Is(endpointErr, os.ErrNotExist):
			fmt.Fprintf(stdout, "daemon.endpoint: not published yet: run `kivgraph daemon status` once it is up\n")
		default:
			fmt.Fprintf(stderr, "daemon.endpoint: %v\n", endpointErr)
		}
	}
	return 0
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
