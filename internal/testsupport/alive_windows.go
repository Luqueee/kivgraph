//go:build windows

package testsupport

import "golang.org/x/sys/windows"

// stillActive is what GetExitCodeProcess reports for a process that has not
// exited. It is STATUS_PENDING wearing another name, and x/sys/windows binds
// neither under this one.
const stillActive = 259

// ProcessAlive reports whether a process exists and is still running.
//
// There is no signal 0 here -- os.Process.Signal answers "not supported by
// windows" for everything but Kill, so the Unix probe does not merely fail, it
// reports every live process as gone. The question is asked of the process
// object instead: a handle can be opened for one that has already exited, so
// the exit code is what separates the two, and STILL_ACTIVE is the sentinel
// the kernel uses to say there is not one yet.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}
