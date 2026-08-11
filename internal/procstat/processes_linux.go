//go:build linux

package procstat

import (
	"os"
	"strconv"
	"strings"
)

// listProcesses reads argv from procfs.
//
// A process whose cmdline cannot be read belongs to another user or exited
// between the directory listing and the read; either way it is not one this
// command could signal, so it is skipped rather than reported as an error.
func listProcesses() ([]Process, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	processes := make([]Process, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/cmdline")
		if err != nil {
			continue
		}
		args := splitArgv(string(data))
		if len(args) == 0 {
			continue
		}
		processes = append(processes, Process{PID: pid, Args: args})
	}
	return processes, nil
}

// splitArgv splits the NUL separated argv procfs stores, dropping the
// trailing separator the kernel writes after the last argument.
func splitArgv(raw string) []string {
	fields := strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00")
	args := make([]string, 0, len(fields))
	for _, field := range fields {
		if field == "" {
			continue
		}
		args = append(args, field)
	}
	return args
}
