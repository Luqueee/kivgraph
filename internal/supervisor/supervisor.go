// Package supervisor gives the daemon an owner.
//
// `kivgraph daemon` is a long-lived process that nothing starts and nothing
// restarts. That is why `serve` is still what a client registration names by
// default: a `command` entry is owned by the client that spawns it, while a
// `url` entry points at a process which, today, exists only for as long as the
// shell that launched it. Registering every client against a daemon nobody owns
// trades memory for a failure where every client loses its tools at once and
// nothing brings them back.
//
// Measured on a workspace of 108.737 symbols, with eight idle clients, one
// daemon holds 10-13 MB of private pages against 77-81 for eight servers, and
// peaks at 26-29 MB against 179-186. That is the saving this package exists to
// make safe to depend on.
//
// What it installs is the platform's own supervisor -- a launchd agent on
// darwin, a systemd user unit on linux -- because writing a second process
// manager inside a code-graph tool is not a thing this project should own.
//
// The unit is per state directory, not per machine or per user. A daemon serves
// one state directory (internal/daemon.SocketName says why: two configurations
// pointing at different directories get different daemons, so a client never
// reaches a graph built from someone else's repositories), so two configurations
// must be able to hold two supervised daemons at once. The label carries a
// digest of the directory to make that true, and Status prints it rather than
// leaving the operator to guess which unit serves what.
package supervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnsupportedPlatform reports a platform with no supervisor this package
// knows how to drive. It is a declared limitation and never a silent no-op: a
// caller that cannot install a supervisor must be able to say so.
var ErrUnsupportedPlatform = errors.New("supervisor: this platform has no supported process supervisor")

// ErrIncompleteSpec reports a spec that names no executable or no state
// directory. Both are absolute paths in the installed file, so neither can be
// inferred later.
var ErrIncompleteSpec = errors.New("supervisor: a spec needs an executable and a state directory")

// labelPrefix is the reverse-DNS root of a launchd label and the stem of a
// systemd unit name. It is not the module path: a label is an operator-facing
// identifier that appears in `launchctl list` and `systemctl --user status`.
const labelPrefix = "com.kivgraph.daemon"

// Spec is the daemon a supervisor should keep running.
type Spec struct {
	// Executable is the absolute path of the kivgraph binary to run. It is
	// recorded rather than resolved at start time because an installed
	// supervisor outlives the shell that installed it, and `kivgraph update`
	// replaces the bundle in place at the same path.
	Executable string
	// StateDirectory is the directory whose daemon this is. It is the
	// identity: it decides the label, so two configurations get two units.
	StateDirectory string
	// ConfigPath, when set, is passed as --config. Empty means the daemon
	// resolves its own configuration, which is what a default install wants.
	ConfigPath string
	// Address, when set, is passed as --addr. Empty means the daemon's own
	// default bind.
	Address string
	// AllowRemote is passed as --allow-remote, and it travels with Address for
	// a reason: the daemon refuses a non-loopback bind without it, so a unit
	// recording one and not the other would start a daemon that exits.
	AllowRemote bool
}

// daemonPath is the PATH an installed unit records for the daemon.
//
// Neither supervisor gives a process the environment its owner works in. A
// systemd user unit that declares none inherits systemd's own -- /usr/local/bin,
// /usr/bin, /bin and a pair of games directories -- and a launchd agent gets
// /usr/bin:/bin:/usr/sbin:/sbin. What both lists have in common is that nothing
// the user installed for themselves is on them, and neither reads a shell
// profile to find out.
//
// That breaks the daemon in two different ways, one loud and one silent.
// `kivgraph-ts-worker` ends in `exec node`, and a node installed through nvm
// lives under ~/.nvm and reaches PATH through .bashrc -- so the worker exits 127
// and every TypeScript repository fails to index. Homebrew's /opt/homebrew/bin
// is missing for the same reason. Go fails quietly instead: a current toolchain
// in ~/.local/go/bin loses to the distribution's in /usr/bin, and the daemon
// builds a different graph with nothing to report.
//
// What makes this hard to see from the outside is that it is invisible from the
// shell. `kivgraph index --full` typed by hand works, because the shell has the
// PATH the unit lacks, and the logs point at the indexer rather than at the
// environment. Recording that shell's PATH is the fix, and it is recorded rather
// than resolved because the shell that ran `daemon install` is the one place
// where the user's toolchains demonstrably resolve: they typed the command
// there. It also makes the unit self-describing -- `systemctl cat` or the plist
// answers what the daemon can reach, where ambient state answers nothing.
//
// The cost is that it is a snapshot. An nvm upgrade moves node to a new
// versioned directory and the recorded PATH goes on naming the old one, so the
// remedy is `kivgraph daemon install` again -- which is the remedy an operator
// would reach for anyway, and the one `daemon status` already names.
func daemonPath() string {
	return os.Getenv("PATH")
}

// State is what a supervisor knows about a spec.
type State string

const (
	// StateAbsent means no unit for this spec is installed.
	StateAbsent State = "absent"
	// StateInstalled means the unit exists and the supervisor loaded it.
	StateInstalled State = "installed"
	// StateStale means the unit exists but describes a different executable
	// or a different daemon than the spec asks for. It is reported rather
	// than repaired silently: an operator who edited a unit by hand should be
	// told, not overruled.
	StateStale State = "stale"
	// StateUnsupported means this platform has no supervisor this package
	// drives. It is not a failure of the installation.
	StateUnsupported State = "unsupported"
)

// Report is what an operation observed. Path is empty when the platform has no
// supervisor, and Detail always says something a reader can act on.
type Report struct {
	State  State
	Label  string
	Path   string
	Detail string
	// Managed means the installed definition matches a Kivgraph-rendered
	// definition, apart from a supported migration such as the recorded PATH.
	// It is ownership evidence for an update; a path or label alone is not.
	Managed bool
	// Repairable means the definition is a known Kivgraph version that can be
	// rewritten safely by an update. Hand-edited and foreign definitions are
	// never repairable.
	Repairable bool
}

// Label returns the supervisor identifier for a spec.
//
// The digest is of the cleaned absolute state directory, so the same daemon
// always gets the same label and two daemons never collide. Eight hex
// characters is enough to separate the handful of state directories one user
// has, and short enough to read in `launchctl list`.
func (spec Spec) Label() (string, error) {
	if err := spec.validate(); err != nil {
		return "", err
	}
	directory, err := filepath.Abs(filepath.Clean(spec.StateDirectory))
	if err != nil {
		return "", fmt.Errorf("supervisor: resolve state directory: %w", err)
	}
	digest := sha256.Sum256([]byte(directory))
	return labelPrefix + "." + hex.EncodeToString(digest[:4]), nil
}

func (spec Spec) validate() error {
	if strings.TrimSpace(spec.Executable) == "" || strings.TrimSpace(spec.StateDirectory) == "" {
		return ErrIncompleteSpec
	}
	return nil
}

// arguments returns the daemon invocation a supervisor records, executable
// first. It is built here rather than in each platform file so both platforms
// start the same daemon.
func (spec Spec) arguments() []string {
	arguments := []string{spec.Executable, "daemon"}
	if spec.ConfigPath != "" {
		arguments = append(arguments, "--config", spec.ConfigPath)
	}
	if spec.Address != "" {
		arguments = append(arguments, "--addr", spec.Address)
	}
	if spec.AllowRemote {
		arguments = append(arguments, "--allow-remote")
	}
	return arguments
}

// Install writes the unit and asks the supervisor to load it.
//
// It is idempotent: an install over a unit that already describes this spec
// reloads it rather than reporting a change, because a caller that runs it
// twice should not have to know which time was the first.
func Install(spec Spec) (Report, error) {
	if err := spec.validate(); err != nil {
		return Report{}, err
	}
	return install(spec)
}

// Remove takes the unit out and asks the supervisor to unload it. Removing an
// absent unit is not an error: it is the state the caller asked for.
func Remove(spec Spec) (Report, error) {
	if err := spec.validate(); err != nil {
		return Report{}, err
	}
	return remove(spec)
}

// Restart asks the supervisor to bring the daemon back on the executable that
// is on disk now.
//
// It exists because `kivgraph update` replaces the bundle in place: the path
// in the installed unit is the same, but the image behind it is not, and a
// daemon that was already running keeps answering from the one that was
// swapped out.
//
// Stopping it instead is worse than doing nothing, and gets worse the better
// the daemon behaves. Both supervisors are configured on purpose to leave a
// clean exit alone -- systemd's `Restart=on-failure`, launchd's `KeepAlive`
// with `SuccessfulExit` false -- so a daemon asked politely to stop shuts down
// properly, exits zero, and stays down. Only the supervisor puts it back, and
// only this asks it to.
//
// A spec whose unit is absent or unsupported is not restarted and is not an
// error. A stale unit is repaired only when Status provides explicit evidence
// that it is a Kivgraph-rendered legacy definition; hand-edited and foreign
// definitions remain untouched. The Report says which case was observed.
func Restart(spec Spec) (Report, error) {
	return restartWith(spec, status, install, restart)
}

type operation func(Spec) (Report, error)

func restartWith(spec Spec, inspect, repair, bringBack operation) (Report, error) {
	if err := spec.validate(); err != nil {
		return Report{}, err
	}
	report, err := inspect(spec)
	if err != nil {
		return report, err
	}
	if report.State == StateStale && report.Managed && report.Repairable {
		report, err = repair(spec)
		if err != nil {
			return report, err
		}
	}
	if report.State != StateInstalled {
		return report, nil
	}
	return bringBack(spec)
}

// Status reports what is installed without changing anything.
func Status(spec Spec) (Report, error) {
	if err := spec.validate(); err != nil {
		return Report{}, err
	}
	return status(spec)
}
