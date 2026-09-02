//go:build linux

package supervisor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// unitDir is where a systemd user unit lives.
//
// A user unit rather than a system one, for the same reason launchd gets an
// agent and not a daemon: the graph is the user's, built from paths under the
// user's home, and a system service would answer for whoever asked.
//
// XDG_CONFIG_HOME is honoured because systemd itself honours it: writing to
// ~/.config unconditionally would install a unit systemd does not read.
func unitDir() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); configured != "" {
		return filepath.Join(configured, "systemd", "user"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("supervisor: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

func planPath(spec Spec) (string, string, error) {
	label, err := spec.Label()
	if err != nil {
		return "", "", err
	}
	directory, err := unitDir()
	if err != nil {
		return "", "", err
	}
	return label, filepath.Join(directory, label+".service"), nil
}

// unit renders the service.
//
// Restart=on-failure is the counterpart of launchd's KeepAlive: systemd brings
// the daemon back when it dies and leaves a clean exit alone. RestartSec keeps a
// daemon that cannot bind from spinning.
//
// StartLimitIntervalSec is here because the default cannot trip against this
// RestartSec, and the default is what "on-failure" quietly means without it.
// Measured on 2026-08-28 against a unit whose ExecStart names a binary that no
// longer exists: with the defaults -- 5 starts per 10s -- systemd reached
// NRestarts=140 and was still going, one exec attempt every two seconds and two
// journal lines each, with no end. Five starts at two-second spacing is exactly
// ten seconds, so the limit sits on its own boundary and never fires. Widening
// the window to 30s makes the same unit give up at NRestarts=5 and land in
// `failed`, which costs nothing and shows up in `systemctl --user status`.
//
// A daemon that failed to start five times in half a minute will not be fixed
// by a sixth attempt, so this is the better behaviour on its own terms. It is
// also what makes it safe to install a unit naming a binary somebody else can
// delete: a `.mcpb` extension removes its own directory and the format has no
// uninstall hook, so nothing here gets to clean up first.
//
// Type=simple, not notify: the daemon does not speak sd_notify, and claiming it
// does would make systemd wait for a readiness signal that never arrives.
//
// Environment=PATH is the one directive here that is not about the daemon's
// lifecycle, and daemonPath says why it has to be written down rather than
// inherited.
func unit(spec Spec, path string) string {
	arguments := spec.arguments()
	var quoted []string
	for _, argument := range arguments {
		quoted = append(quoted, quoteUnitArgument(argument))
	}
	var body strings.Builder
	body.WriteString("[Unit]\n")
	body.WriteString("Description=Kivgraph MCP daemon for " + spec.StateDirectory + "\n")
	body.WriteString("Documentation=https://kivgraph.dev/docs/cli\n")
	body.WriteString("StartLimitIntervalSec=30\n")
	body.WriteString("StartLimitBurst=5\n\n")
	body.WriteString("[Service]\n")
	body.WriteString("Type=simple\n")
	// A machine with no PATH at all records none: an empty one would give the
	// daemon a worse environment than systemd's default, which is the thing
	// this exists to replace.
	if path != "" {
		body.WriteString(pathDirective + quoteUnitEnvironment(path) + "\n")
	}
	body.WriteString("ExecStart=" + strings.Join(quoted, " ") + "\n")
	body.WriteString("WorkingDirectory=" + spec.StateDirectory + "\n")
	body.WriteString("Restart=on-failure\n")
	body.WriteString("RestartSec=2\n\n")
	body.WriteString("[Install]\n")
	body.WriteString("WantedBy=default.target\n")
	return body.String()
}

// pathDirective is how a recorded PATH opens. The quote is part of it because
// quoteUnitEnvironment always writes one, and matching on the whole opening is
// what keeps `status` from mistaking some other operator-added Environment=
// line for the one this package writes.
const pathDirective = `Environment="PATH=`

// quoteUnitEnvironment renders the value of a recorded PATH.
//
// Three characters would not survive being written plainly. systemd splits
// Environment= on whitespace, so an entry holding a space -- which any path
// under an "Application Support" or a "my apps" produces -- would become a
// second assignment. It resolves % specifiers in this setting, so a literal
// percent has to be doubled or a directory named %h expands to the home
// directory. And a newline would end the line, leaving whatever followed it to
// be read as a directive of its own: the only one of the three that could put
// something in the unit that nobody wrote.
//
// The value is always quoted rather than only when it needs to be. A PATH is
// one long line either way, so there is no legibility in `systemctl cat` to
// protect, and a rule with no exception is one fewer case for `status` to
// compare wrongly.
func quoteUnitEnvironment(value string) string {
	escaped := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"%", "%%",
	).Replace(value)
	return escaped + `"`
}

// withoutRecordedPath returns a rendered unit with its PATH directive removed,
// and whether it had one.
//
// It is what lets `status` compare the daemon rather than the shell it is
// asked from. The recorded PATH belongs to the terminal that ran `daemon
// install`, so comparing it would report every unit stale as soon as the
// operator opened a shell with a different one -- and send them to reinstall a
// daemon that is working.
//
// Whether a PATH is recorded at all is compared, and that is deliberate: a unit
// written before this existed carries none, and the daemon under it is exactly
// the one that cannot resolve node. Reporting it stale is what puts the remedy
// in front of the person who needs it.
func withoutRecordedPath(rendered string) (string, bool) {
	var kept strings.Builder
	recorded := false
	for line := range strings.SplitSeq(rendered, "\n") {
		if strings.HasPrefix(line, pathDirective) {
			recorded = true
			continue
		}
		kept.WriteString(line)
		kept.WriteString("\n")
	}
	return strings.TrimSuffix(kept.String(), "\n"), recorded
}

// quoteUnitArgument quotes an ExecStart word.
//
// systemd splits ExecStart on whitespace, so a path holding a space would become
// two arguments. Its quoting accepts a double-quoted string with backslash
// escapes, which is what this produces -- and only when needed, so the common
// case stays readable in `systemctl cat`.
func quoteUnitArgument(argument string) string {
	if !strings.ContainsAny(argument, " \t\"\\'") {
		return argument
	}
	replaced := strings.ReplaceAll(argument, `\`, `\\`)
	replaced = strings.ReplaceAll(replaced, `"`, `\"`)
	return `"` + replaced + `"`
}

func install(spec Spec) (Report, error) {
	label, path, err := planPath(spec)
	if err != nil {
		return Report{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Report{}, fmt.Errorf("supervisor: create %s: %w", filepath.Dir(path), err)
	}
	if err := writeFileAtomic(path, []byte(unit(spec, daemonPath())), 0o644); err != nil {
		return Report{}, err
	}
	// daemon-reload before enable so systemd reads the file just written; a
	// reinstall over a running unit otherwise enables the previous contents.
	if err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return Report{State: StateStale, Label: label, Path: path},
			fmt.Errorf("supervisor: systemctl daemon-reload: %w", err)
	}
	if err := run("systemctl", "--user", "enable", "--now", label+".service"); err != nil {
		return Report{State: StateStale, Label: label, Path: path},
			fmt.Errorf("supervisor: systemctl enable: %w", err)
	}
	return Report{
		State:  StateInstalled,
		Label:  label,
		Path:   path,
		Detail: "systemd starts it with the session and restarts it if it dies",
	}, nil
}

func remove(spec Spec) (Report, error) {
	label, path, err := planPath(spec)
	if err != nil {
		return Report{}, err
	}
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return Report{State: StateAbsent, Label: label, Path: path, Detail: "no unit was installed"}, nil
	}
	_ = run("systemctl", "--user", "disable", "--now", label+".service")
	if err := os.Remove(path); err != nil {
		return Report{}, fmt.Errorf("supervisor: remove %s: %w", path, err)
	}
	// The unit file is gone, so the reload is what stops systemd reporting a
	// unit whose file no longer exists.
	_ = run("systemctl", "--user", "daemon-reload")
	return Report{State: StateAbsent, Label: label, Path: path, Detail: "the unit was disabled and removed"}, nil
}

func restart(spec Spec) (Report, error) {
	label, path, err := planPath(spec)
	if err != nil {
		return Report{}, err
	}
	// restart and not stop-then-start: systemd does both without a window in
	// which the unit is loaded and nothing is running, and a `start` that
	// raced the shutdown would be refused rather than queued.
	//
	// No daemon-reload first. The unit file is not what changed -- `update`
	// replaced the executable it points at -- and reloading would hide an
	// operator's hand edit by picking it up silently, which is exactly what
	// status reports as stale rather than repairing.
	if err := run("systemctl", "--user", "restart", label+".service"); err != nil {
		return Report{State: StateInstalled, Label: label, Path: path},
			fmt.Errorf("supervisor: systemctl restart: %w", err)
	}
	return Report{
		State:  StateInstalled,
		Label:  label,
		Path:   path,
		Detail: "systemd restarted it on the executable now on disk",
	}, nil
}

func status(spec Spec) (Report, error) {
	label, path, err := planPath(spec)
	if err != nil {
		return Report{}, err
	}
	existing, readErr := os.ReadFile(path)
	if errors.Is(readErr, os.ErrNotExist) {
		return Report{State: StateAbsent, Label: label, Path: path,
			Detail: "no unit is installed for this state directory"}, nil
	}
	if readErr != nil {
		return Report{}, fmt.Errorf("supervisor: read %s: %w", path, readErr)
	}
	if hasDropIns, err := hasDropIns(path + ".d"); err != nil {
		return Report{}, err
	} else if hasDropIns {
		return Report{State: StateStale, Label: label, Path: path,
			Detail: "the installed unit has user drop-ins; update leaves operator-managed supervisor configuration untouched"}, nil
	}
	installed, recordsPath := withoutRecordedPath(string(existing))
	wanted, wantsPath := withoutRecordedPath(unit(spec, daemonPath()))
	if installed != wanted {
		return Report{State: StateStale, Label: label, Path: path,
			Detail: "the installed unit describes a different daemon: reinstall to replace it"}, nil
	}
	if recordsPath != wantsPath {
		return Report{State: StateStale, Label: label, Path: path,
			Detail: "the installed unit records no PATH, so the daemon cannot reach the toolchains " +
				"this shell can: update can repair this legacy Kivgraph unit", Managed: true,
			Repairable: true}, nil
	}
	return Report{State: StateInstalled, Label: label, Path: path,
		Detail: "systemd starts it with the session and restarts it if it dies", Managed: true}, nil
}

func hasDropIns(directory string) (bool, error) {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("supervisor: read drop-ins %s: %w", directory, err)
	}
	return len(entries) > 0, nil
}
