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
// Type=simple, not notify: the daemon does not speak sd_notify, and claiming it
// does would make systemd wait for a readiness signal that never arrives.
func unit(spec Spec) string {
	arguments := spec.arguments()
	var quoted []string
	for _, argument := range arguments {
		quoted = append(quoted, quoteUnitArgument(argument))
	}
	var body strings.Builder
	body.WriteString("[Unit]\n")
	body.WriteString("Description=Kivgraph MCP daemon for " + spec.StateDirectory + "\n")
	body.WriteString("Documentation=https://kivgraph.dev/docs/cli\n\n")
	body.WriteString("[Service]\n")
	body.WriteString("Type=simple\n")
	body.WriteString("ExecStart=" + strings.Join(quoted, " ") + "\n")
	body.WriteString("WorkingDirectory=" + spec.StateDirectory + "\n")
	body.WriteString("Restart=on-failure\n")
	body.WriteString("RestartSec=2\n\n")
	body.WriteString("[Install]\n")
	body.WriteString("WantedBy=default.target\n")
	return body.String()
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
	if err := writeFileAtomic(path, []byte(unit(spec)), 0o644); err != nil {
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
	if string(existing) != unit(spec) {
		return Report{State: StateStale, Label: label, Path: path,
			Detail: "the installed unit describes a different daemon: reinstall to replace it"}, nil
	}
	return Report{State: StateInstalled, Label: label, Path: path,
		Detail: "systemd starts it with the session and restarts it if it dies"}, nil
}
