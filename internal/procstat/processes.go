package procstat

import (
	"errors"
	"strings"

	"github.com/Luqueee/kivgraph/internal/executable"
)

// ErrProcessListUnsupported reports that this build cannot enumerate
// processes. It is a declared limitation rather than an empty answer: a
// command that stops processes must not report success because it could not
// see any.
var ErrProcessListUnsupported = errors.New("procstat: this platform cannot enumerate processes")

// Process is one live process, named by the arguments it was started with.
//
// Only processes this user can read are listed. Enumerating another user's
// arguments needs privileges this tool does not want, and a process it cannot
// read is one it could not signal either.
type Process struct {
	PID  int
	Args []string
}

// Command answers the process's argv joined for a report, or its identifier
// when the platform gave no arguments.
func (process Process) Command() string {
	if len(process.Args) == 0 {
		return ""
	}
	return strings.Join(process.Args, " ")
}

// Invocation reports the executable's base name and its first argument, which
// is what distinguishes `kivgraph serve` from `kivgraph index`.
func (process Process) Invocation() (program, command string) {
	if len(process.Args) == 0 {
		return "", ""
	}
	program = executable.BaseName(strings.TrimSpace(process.Args[0]))
	if len(process.Args) > 1 {
		command = strings.TrimSpace(process.Args[1])
	}
	return program, command
}

// List returns every process of this user that the platform can describe.
func List() ([]Process, error) {
	return listProcesses()
}
