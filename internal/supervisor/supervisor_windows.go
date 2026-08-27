//go:build windows

package supervisor

import (
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// definitionDir is where the task definition this package wrote is kept.
//
// Task Scheduler is not a directory of files the way systemd and launchd are:
// registering a task copies the definition into a store of its own, and the
// file left behind is a record rather than the thing being read. It is written
// anyway, and for the same reason the other two platforms have a path -- an
// operator asking what is installed gets something to open, and `status` has
// something to compare against that does not depend on how the scheduler
// chooses to print XML back.
func definitionDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("supervisor: resolve the configuration directory: %w", err)
	}
	return filepath.Join(base, "kivgraph", "supervisor"), nil
}

func planPath(spec Spec) (string, string, error) {
	label, err := spec.Label()
	if err != nil {
		return "", "", err
	}
	directory, err := definitionDir()
	if err != nil {
		return "", "", err
	}
	return label, filepath.Join(directory, label+".xml"), nil
}

// taskAction is the Exec element, rendered through encoding/xml so a path
// holding an ampersand cannot end the document early.
type taskAction struct {
	Command          string `xml:"Command"`
	Arguments        string `xml:"Arguments,omitempty"`
	WorkingDirectory string `xml:"WorkingDirectory"`
}

// unit renders the task.
//
// A logon trigger and LeastPrivilege, which together are the faithful
// translation of what the other two platforms install: a systemd *user* unit
// and a launchd *agent* are both per-user and neither needs root, so this is
// per-user and needs no elevation either. A service through the SCM would
// start at boot and outlive the logout, and would run as another identity --
// which would change where the daemon looks for the configuration whose whole
// content is one user's paths.
//
// Four of these settings are not decoration:
//
//   - ExecutionTimeLimit PT0S, because the default ends a task after 72 hours
//     and a daemon that disappears every third day is worse than one that was
//     never installed.
//   - MultipleInstancesPolicy IgnoreNew, so a second logon does not start a
//     second daemon against the same state directory.
//   - DisallowStartIfOnBatteries false, or a laptop never starts it.
//   - RestartOnFailure, which is the counterpart of systemd's Restart and
//     launchd's KeepAlive. One minute is the scheduler's floor.
func unit(spec Spec) string {
	account, err := currentAccount()
	if err != nil {
		// The renderer has no error channel because the other platforms need
		// none, and an empty UserId is a definition the scheduler rejects
		// loudly rather than one it registers wrongly.
		account = ""
	}
	arguments := spec.arguments()
	action := taskAction{WorkingDirectory: spec.StateDirectory}
	if len(arguments) > 0 {
		action.Command = arguments[0]
		action.Arguments = strings.Join(quoteAll(arguments[1:]), " ")
	}
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-16"?>` + "\n")
	body.WriteString(`<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">` + "\n")
	body.WriteString("  <RegistrationInfo>\n")
	body.WriteString("    <Description>" + escapeXML("Kivgraph MCP daemon for "+spec.StateDirectory) + "</Description>\n")
	body.WriteString("  </RegistrationInfo>\n")
	body.WriteString("  <Triggers>\n    <LogonTrigger>\n      <Enabled>true</Enabled>\n")
	body.WriteString("      <UserId>" + escapeXML(account) + "</UserId>\n")
	body.WriteString("    </LogonTrigger>\n  </Triggers>\n")
	body.WriteString("  <Principals>\n    <Principal id=\"Author\">\n")
	body.WriteString("      <UserId>" + escapeXML(account) + "</UserId>\n")
	body.WriteString("      <LogonType>InteractiveToken</LogonType>\n")
	body.WriteString("      <RunLevel>LeastPrivilege</RunLevel>\n")
	body.WriteString("    </Principal>\n  </Principals>\n")
	body.WriteString("  <Settings>\n")
	body.WriteString("    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>\n")
	body.WriteString("    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>\n")
	body.WriteString("    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>\n")
	body.WriteString("    <AllowHardTerminate>true</AllowHardTerminate>\n")
	body.WriteString("    <StartWhenAvailable>true</StartWhenAvailable>\n")
	body.WriteString("    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>\n")
	body.WriteString("    <IdleSettings>\n      <StopOnIdleEnd>false</StopOnIdleEnd>\n      <RestartOnIdle>false</RestartOnIdle>\n    </IdleSettings>\n")
	body.WriteString("    <AllowStartOnDemand>true</AllowStartOnDemand>\n")
	body.WriteString("    <Enabled>true</Enabled>\n")
	body.WriteString("    <Hidden>false</Hidden>\n")
	body.WriteString("    <RunOnlyIfIdle>false</RunOnlyIfIdle>\n")
	body.WriteString("    <WakeToRun>false</WakeToRun>\n")
	body.WriteString("    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>\n")
	body.WriteString("    <Priority>7</Priority>\n")
	body.WriteString("    <RestartOnFailure>\n      <Interval>PT1M</Interval>\n      <Count>3</Count>\n    </RestartOnFailure>\n")
	body.WriteString("  </Settings>\n")
	body.WriteString("  <Actions Context=\"Author\">\n    <Exec>\n")
	body.WriteString("      <Command>" + escapeXML(action.Command) + "</Command>\n")
	if action.Arguments != "" {
		body.WriteString("      <Arguments>" + escapeXML(action.Arguments) + "</Arguments>\n")
	}
	body.WriteString("      <WorkingDirectory>" + escapeXML(action.WorkingDirectory) + "</WorkingDirectory>\n")
	body.WriteString("    </Exec>\n  </Actions>\n</Task>\n")
	return body.String()
}

// quoteAll quotes the words that would otherwise be split.
//
// The scheduler hands Arguments to the process as one string and the process
// splits it with the usual rules, so a path holding a space needs the quotes
// it would need on a command line.
func quoteAll(arguments []string) []string {
	quoted := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		if strings.ContainsAny(argument, " \t\"") {
			quoted = append(quoted, `"`+strings.ReplaceAll(argument, `"`, `\"`)+`"`)
			continue
		}
		quoted = append(quoted, argument)
	}
	return quoted
}

func escapeXML(value string) string {
	var out strings.Builder
	_ = xml.EscapeText(&out, []byte(value))
	return out.String()
}

// currentAccount names the logon this task belongs to, as DOMAIN\user.
func currentAccount() (string, error) {
	current, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("supervisor: resolve the current user: %w", err)
	}
	return current.Username, nil
}

// encode writes the definition as UTF-16 with a byte order mark.
//
// `schtasks /XML` reads what the declaration says, and the declaration says
// UTF-16 because that is what the scheduler itself writes and the only
// encoding every version of the tool accepts without argument.
func encode(definition string) []byte {
	units := utf16.Encode([]rune(definition))
	out := make([]byte, 0, 2+len(units)*2)
	out = append(out, 0xFF, 0xFE)
	for _, unit := range units {
		out = binary.LittleEndian.AppendUint16(out, unit)
	}
	return out
}

func install(spec Spec) (Report, error) {
	label, path, err := planPath(spec)
	if err != nil {
		return Report{}, err
	}
	if _, err := currentAccount(); err != nil {
		return Report{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Report{}, fmt.Errorf("supervisor: create %s: %w", filepath.Dir(path), err)
	}
	if err := writeFileAtomic(path, encode(unit(spec)), 0o600); err != nil {
		return Report{}, err
	}
	// /F replaces a task of the same name, which is what a reinstall means;
	// without it the scheduler refuses and the operator is told to delete
	// something they did not know existed.
	if err := run("schtasks", "/Create", "/TN", label, "/XML", path, "/F"); err != nil {
		return Report{State: StateStale, Label: label, Path: path},
			fmt.Errorf("supervisor: schtasks /Create: %w", err)
	}
	// Registering a logon task does not start it, and the operator asked for a
	// daemon rather than for a daemon at the next sign-in.
	if err := run("schtasks", "/Run", "/TN", label); err != nil {
		return Report{State: StateStale, Label: label, Path: path},
			fmt.Errorf("supervisor: schtasks /Run: %w", err)
	}
	return Report{
		State:  StateInstalled,
		Label:  label,
		Path:   path,
		Detail: "the task scheduler starts it at logon and restarts it if it dies",
	}, nil
}

func remove(spec Spec) (Report, error) {
	label, path, err := planPath(spec)
	if err != nil {
		return Report{}, err
	}
	registered := run("schtasks", "/Query", "/TN", label) == nil
	_, statErr := os.Stat(path)
	if errors.Is(statErr, os.ErrNotExist) && !registered {
		return Report{State: StateAbsent, Label: label, Path: path, Detail: "no task was installed"}, nil
	}
	// The task is ended before it is deleted: deleting a registered task
	// leaves whatever it started running, and `daemon remove` means the daemon
	// is gone rather than merely unowned.
	_ = run("schtasks", "/End", "/TN", label)
	if registered {
		if err := run("schtasks", "/Delete", "/TN", label, "/F"); err != nil {
			return Report{State: StateStale, Label: label, Path: path},
				fmt.Errorf("supervisor: schtasks /Delete: %w", err)
		}
	}
	if statErr == nil {
		if err := os.Remove(path); err != nil {
			return Report{}, fmt.Errorf("supervisor: remove %s: %w", path, err)
		}
	}
	return Report{State: StateAbsent, Label: label, Path: path, Detail: "the task was ended and deleted"}, nil
}

func status(spec Spec) (Report, error) {
	label, path, err := planPath(spec)
	if err != nil {
		return Report{}, err
	}
	existing, readErr := os.ReadFile(path)
	if errors.Is(readErr, os.ErrNotExist) {
		return Report{State: StateAbsent, Label: label, Path: path,
			Detail: "no task is installed for this state directory"}, nil
	}
	if readErr != nil {
		return Report{}, fmt.Errorf("supervisor: read %s: %w", path, readErr)
	}
	// The definition on disk and the task in the scheduler are two facts and
	// either can be the one that is missing. A definition with no task is what
	// an operator who deleted the task by hand leaves behind, and reporting it
	// as installed would promise a restart nothing will perform.
	if run("schtasks", "/Query", "/TN", label) != nil {
		return Report{State: StateAbsent, Label: label, Path: path,
			Detail: "the definition is here but the scheduler has no task for it: reinstall to register it"}, nil
	}
	if string(existing) != string(encode(unit(spec))) {
		return Report{State: StateStale, Label: label, Path: path,
			Detail: "the installed task describes a different daemon: reinstall to replace it"}, nil
	}
	return Report{State: StateInstalled, Label: label, Path: path,
		Detail: "the task scheduler starts it at logon and restarts it if it dies"}, nil
}
