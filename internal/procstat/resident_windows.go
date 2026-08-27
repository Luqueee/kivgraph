//go:build windows

package procstat

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processMemoryCounters is psapi's PROCESS_MEMORY_COUNTERS.
//
// It is declared here because golang.org/x/sys/windows does not bind
// GetProcessMemoryInfo, which is the one call in this file that has no
// binding. The layout is fixed and documented; the field this package needs is
// the working set, and the peak beside it is the only place the high-water
// mark is remembered at all.
type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

var (
	psapi                   = windows.NewLazySystemDLL("psapi.dll")
	procGetProcessMemoryInf = psapi.NewProc("GetProcessMemoryInfo")
)

// supported reports whether this build can answer ResidentBytes. It can.
func supported() bool { return true }

// proportionalSupported reports whether a page can be divided among the
// processes that map it. Windows keeps no counter that does: the working set
// counts a shared page in full for every process holding it, exactly like the
// resident size this package already warns is the number that misleads.
func proportionalSupported() bool { return false }

func residentBytes(pid int) int64 {
	counters, ok := memoryCounters(pid)
	if !ok {
		return 0
	}
	return int64(counters.WorkingSetSize)
}

// observe fills what this platform can measure and leaves the rest at zero,
// which a caller reads as "not measured here" and never as a process that used
// none.
//
// Proportional, SharedClean and PrivateDirty stay zero deliberately. Windows
// offers PrivateUsage, and it is a commit charge rather than resident private
// memory -- close enough to look like an answer and different enough to be the
// wrong one in the report this feeds.
func observe(pid int) Sample {
	sample := Sample{}
	if counters, ok := memoryCounters(pid); ok {
		sample.Resident = int64(counters.WorkingSetSize)
		sample.Peak = int64(counters.PeakWorkingSetSize)
	}
	readTimes(pid, &sample)
	return sample
}

func memoryCounters(pid int) (processMemoryCounters, bool) {
	var counters processMemoryCounters
	handle, err := openForQuery(pid)
	if err != nil {
		return counters, false
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	counters.CB = uint32(unsafe.Sizeof(counters))
	status, _, _ := procGetProcessMemoryInf.Call(
		uintptr(handle), uintptr(unsafe.Pointer(&counters)), uintptr(counters.CB))
	if status == 0 {
		return processMemoryCounters{}, false
	}
	return counters, true
}

// readTimes takes the processor time and the age from the kernel's own clock.
// Creation time is absolute, so the uptime is derived rather than reported.
func readTimes(pid int, sample *Sample) {
	handle, err := openForQuery(pid)
	if err != nil {
		return
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return
	}
	// A Filetime of processor time is a duration in 100-nanosecond units, not
	// an instant, so it is read as a count and not through Nanoseconds().
	sample.CPU = filetimeDuration(kernel) + filetimeDuration(user)
	if started := time.Unix(0, creation.Nanoseconds()); !started.IsZero() {
		if uptime := time.Since(started); uptime > 0 {
			sample.Uptime = uptime
		}
	}
}

func filetimeDuration(value windows.Filetime) time.Duration {
	ticks := int64(value.HighDateTime)<<32 | int64(value.LowDateTime)
	return time.Duration(ticks) * 100 * time.Nanosecond
}

// openForQuery asks for the least that answers: the right to be told about the
// process, and nothing that would fail on one this can still legitimately see.
func openForQuery(pid int) (windows.Handle, error) {
	if pid <= 0 {
		return 0, windows.ERROR_INVALID_PARAMETER
	}
	return windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
}
