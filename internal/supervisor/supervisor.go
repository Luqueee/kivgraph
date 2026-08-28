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
// A spec whose unit is absent, stale or unsupported is not restarted and is
// not an error: the Report says which, and a caller that has something else to
// do about an unsupervised process needs to be told rather than failed. That
// is the same contract Status has, for the same reason -- unlike Install and
// Remove, there is nothing here to refuse to do.
func Restart(spec Spec) (Report, error) {
	if err := spec.validate(); err != nil {
		return Report{}, err
	}
	report, err := status(spec)
	if err != nil || report.State != StateInstalled {
		return report, err
	}
	return restart(spec)
}

// Status reports what is installed without changing anything.
func Status(spec Spec) (Report, error) {
	if err := spec.validate(); err != nil {
		return Report{}, err
	}
	return status(spec)
}
