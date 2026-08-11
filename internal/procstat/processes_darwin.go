//go:build darwin

package procstat

import (
	"encoding/binary"
	"strings"

	"golang.org/x/sys/unix"
)

// listProcesses asks the kernel for the process table and then for each
// process's argument vector.
//
// `kern.procargs2` answers for a process of this user and refuses for anyone
// else's, which is exactly the set this command could signal, so a refusal is
// a skip rather than an error.
func listProcesses() ([]Process, error) {
	table, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	processes := make([]Process, 0, len(table))
	for index := range table {
		pid := int(table[index].Proc.P_pid)
		if pid <= 0 {
			continue
		}
		raw, err := unix.SysctlRaw("kern.procargs2", pid)
		if err != nil {
			continue
		}
		args := parseProcArgs2(raw)
		if len(args) == 0 {
			continue
		}
		processes = append(processes, Process{PID: pid, Args: args})
	}
	return processes, nil
}

// parseProcArgs2 reads the buffer `kern.procargs2` returns: the argument
// count, the executable path, alignment padding, and then that many NUL
// terminated arguments before the environment.
//
// The count is what separates the arguments from the environment; walking
// NUL terminated strings to the end of the buffer would report every
// environment variable as an argument.
func parseProcArgs2(raw []byte) []string {
	if len(raw) < 4 {
		return nil
	}
	count := int(binary.NativeEndian.Uint32(raw[:4]))
	if count <= 0 {
		return nil
	}
	rest := raw[4:]

	// The executable path comes first and is not one of the count
	// arguments; the kernel pads it to an alignment boundary with NULs.
	end := indexNUL(rest)
	if end < 0 {
		return nil
	}
	rest = rest[end:]
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}

	args := make([]string, 0, count)
	for range count {
		end := indexNUL(rest)
		if end < 0 {
			if trimmed := strings.TrimSpace(string(rest)); trimmed != "" {
				args = append(args, trimmed)
			}
			break
		}
		args = append(args, string(rest[:end]))
		rest = rest[end+1:]
	}
	return args
}

func indexNUL(data []byte) int {
	for index, value := range data {
		if value == 0 {
			return index
		}
	}
	return -1
}
