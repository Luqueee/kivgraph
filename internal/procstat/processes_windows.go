//go:build windows

package procstat

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// listProcesses reads every process's command line out of its own memory.
//
// Windows keeps no argv anywhere a reader can ask for it. A process is handed
// one string and splits it itself, so the closest thing to the vector procfs
// hands over is that string plus the rules the process used on it: the string
// lives in the PEB, reachable with NtQueryInformationProcess and three reads,
// and DecomposeCommandLine applies exactly the rules CommandLineToArgvW does.
//
// A process this cannot open belongs to another user or exited between the
// snapshot and the read. Either way it is not one this command could signal,
// so it is skipped rather than reported as an error -- the same trade the
// Linux implementation makes with an unreadable cmdline.
func listProcesses() ([]Process, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	processes := make([]Process, 0, 64)
	for {
		if pid := int(entry.ProcessID); pid > 0 {
			if args, err := commandLine(entry.ProcessID); err == nil && len(args) > 0 {
				processes = append(processes, Process{PID: pid, Args: args})
			}
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				return processes, nil
			}
			return nil, err
		}
	}
}

// commandLine reads one process's command line and splits it.
//
// The access asked for is the least that can answer: the query right that does
// not require the process to be one this could debug, and the read right the
// three reads need. Anything broader would fail on processes this can still
// legitimately see.
func commandLine(pid uint32) ([]string, error) {
	handle, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_VM_READ, false, pid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	// A 32-bit process keeps a PEB of its own shape, and reading it with the
	// layout of this one yields a pointer into nothing. Kivgraph is never the
	// 32-bit process, so this skips rather than carrying a second set of
	// offsets for processes it has no business signalling anyway.
	var wow64 bool
	if err := windows.IsWow64Process(handle, &wow64); err != nil {
		return nil, err
	}
	if wow64 {
		return nil, windows.ERROR_NOT_SUPPORTED
	}

	var basic windows.PROCESS_BASIC_INFORMATION
	var returned uint32
	if err := windows.NtQueryInformationProcess(handle, windows.ProcessBasicInformation,
		unsafe.Pointer(&basic), uint32(unsafe.Sizeof(basic)), &returned); err != nil {
		return nil, err
	}
	var block windows.PEB
	if err := read(handle, uintptr(unsafe.Pointer(basic.PebBaseAddress)),
		unsafe.Pointer(&block), unsafe.Sizeof(block)); err != nil {
		return nil, err
	}
	var parameters windows.RTL_USER_PROCESS_PARAMETERS
	if err := read(handle, uintptr(unsafe.Pointer(block.ProcessParameters)),
		unsafe.Pointer(&parameters), unsafe.Sizeof(parameters)); err != nil {
		return nil, err
	}
	// Length counts bytes and the buffer is UTF-16, so an odd one is a
	// structure this did not read correctly rather than a short command line.
	length := parameters.CommandLine.Length
	if length == 0 || length%2 != 0 {
		return nil, windows.ERROR_INVALID_DATA
	}
	buffer := make([]uint16, length/2)
	if err := read(handle, uintptr(unsafe.Pointer(parameters.CommandLine.Buffer)),
		unsafe.Pointer(&buffer[0]), uintptr(length)); err != nil {
		return nil, err
	}
	return windows.DecomposeCommandLine(windows.UTF16ToString(buffer))
}

// read insists on the whole structure. A partial copy means the address was
// only partly mapped, which makes every field after the boundary a leftover of
// whatever the buffer held, and this is reading pointers.
func read(process windows.Handle, address uintptr, into unsafe.Pointer, size uintptr) error {
	var copied uintptr
	if err := windows.ReadProcessMemory(process, address, (*byte)(into), size, &copied); err != nil {
		return err
	}
	if copied != size {
		return windows.ERROR_PARTIAL_COPY
	}
	return nil
}
