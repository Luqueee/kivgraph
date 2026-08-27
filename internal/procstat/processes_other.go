//go:build !linux && !darwin && !windows

package procstat

// listProcesses reports that this build cannot see the process table. The
// distribution targets linux/amd64 and darwin/arm64; anything else says so
// instead of answering that nothing is running.
func listProcesses() ([]Process, error) { return nil, ErrProcessListUnsupported }
