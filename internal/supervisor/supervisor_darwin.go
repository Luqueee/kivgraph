//go:build darwin

package supervisor

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// launchAgentsDir is where a per-user launchd agent lives. A system-wide
// LaunchDaemon is deliberately not an option: the daemon reads a state
// directory under the user's home and answers with that user's source paths.
func launchAgentsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("supervisor: resolve home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

func planPath(spec Spec) (string, string, error) {
	label, err := spec.Label()
	if err != nil {
		return "", "", err
	}
	directory, err := launchAgentsDir()
	if err != nil {
		return "", "", err
	}
	return label, filepath.Join(directory, label+".plist"), nil
}

// plist renders the agent.
//
// KeepAlive with SuccessfulExit false is the whole point of the file: launchd
// restarts the daemon when it dies and leaves it alone when it exits cleanly,
// which is what `kivgraph daemon install` promises and what a hand-run
// `kivgraph daemon &` cannot do.
//
// It is encoded with encoding/xml rather than a format string so a path holding
// an ampersand or a quote cannot produce a plist launchd refuses to parse.
func plist(spec Spec, label string) ([]byte, error) {
	type entry struct {
		XMLName xml.Name
		Value   string `xml:",chardata"`
	}
	var body strings.Builder
	body.WriteString(xml.Header)
	body.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	body.WriteString(`<plist version="1.0">` + "\n<dict>\n")

	write := func(key, value string) error {
		encoded, err := xml.Marshal(entry{XMLName: xml.Name{Local: "string"}, Value: value})
		if err != nil {
			return fmt.Errorf("supervisor: encode plist value: %w", err)
		}
		keyed, err := xml.Marshal(entry{XMLName: xml.Name{Local: "key"}, Value: key})
		if err != nil {
			return fmt.Errorf("supervisor: encode plist key: %w", err)
		}
		body.WriteString("  " + string(keyed) + "\n  " + string(encoded) + "\n")
		return nil
	}
	if err := write("Label", label); err != nil {
		return nil, err
	}
	body.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, argument := range spec.arguments() {
		encoded, err := xml.Marshal(entry{XMLName: xml.Name{Local: "string"}, Value: argument})
		if err != nil {
			return nil, fmt.Errorf("supervisor: encode plist argument: %w", err)
		}
		body.WriteString("    " + string(encoded) + "\n")
	}
	body.WriteString("  </array>\n")
	body.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	body.WriteString("  <key>KeepAlive</key>\n  <dict>\n    <key>SuccessfulExit</key>\n    <false/>\n  </dict>\n")
	if err := write("WorkingDirectory", spec.StateDirectory); err != nil {
		return nil, err
	}
	// The daemon logs structurally to stderr, and launchd discards a stream
	// nobody redirects. Both go into the state directory next to the graph
	// they describe, so `kivgraph logs` and these files agree on location.
	if err := write("StandardOutPath", filepath.Join(spec.StateDirectory, "daemon.out.log")); err != nil {
		return nil, err
	}
	if err := write("StandardErrorPath", filepath.Join(spec.StateDirectory, "daemon.err.log")); err != nil {
		return nil, err
	}
	body.WriteString("</dict>\n</plist>\n")
	return []byte(body.String()), nil
}

func install(spec Spec) (Report, error) {
	label, path, err := planPath(spec)
	if err != nil {
		return Report{}, err
	}
	rendered, err := plist(spec, label)
	if err != nil {
		return Report{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Report{}, fmt.Errorf("supervisor: create %s: %w", filepath.Dir(path), err)
	}
	// A unit is rewritten rather than merged: it has one owner and one shape,
	// and a partial edit would leave launchd loading half of two versions.
	if err := writeFileAtomic(path, rendered, 0o644); err != nil {
		return Report{}, err
	}
	// bootout before bootstrap so a reinstall over a loaded agent takes the
	// new arguments. A bootout of something not loaded is not a failure.
	target := "gui/" + strconv.Itoa(os.Getuid())
	_ = run("launchctl", "bootout", target+"/"+label)
	if err := run("launchctl", "bootstrap", target, path); err != nil {
		return Report{State: StateStale, Label: label, Path: path},
			fmt.Errorf("supervisor: launchctl bootstrap: %w", err)
	}
	return Report{
		State:  StateInstalled,
		Label:  label,
		Path:   path,
		Detail: "launchd starts it at login and restarts it if it dies",
	}, nil
}

func remove(spec Spec) (Report, error) {
	label, path, err := planPath(spec)
	if err != nil {
		return Report{}, err
	}
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return Report{State: StateAbsent, Label: label, Path: path, Detail: "no agent was installed"}, nil
	}
	_ = run("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+label)
	if err := os.Remove(path); err != nil {
		return Report{}, fmt.Errorf("supervisor: remove %s: %w", path, err)
	}
	return Report{State: StateAbsent, Label: label, Path: path, Detail: "the agent was unloaded and removed"}, nil
}

func status(spec Spec) (Report, error) {
	label, path, err := planPath(spec)
	if err != nil {
		return Report{}, err
	}
	existing, readErr := os.ReadFile(path)
	if errors.Is(readErr, os.ErrNotExist) {
		return Report{State: StateAbsent, Label: label, Path: path,
			Detail: "no agent is installed for this state directory"}, nil
	}
	if readErr != nil {
		return Report{}, fmt.Errorf("supervisor: read %s: %w", path, readErr)
	}
	wanted, err := plist(spec, label)
	if err != nil {
		return Report{}, err
	}
	if string(existing) != string(wanted) {
		return Report{State: StateStale, Label: label, Path: path,
			Detail: "the installed agent describes a different daemon: reinstall to replace it"}, nil
	}
	return Report{State: StateInstalled, Label: label, Path: path,
		Detail: "launchd starts it at login and restarts it if it dies"}, nil
}
