//go:build darwin && cgo

package procstat

/*
#include <stdint.h>
#include <libproc.h>
#include <mach/mach_time.h>
#include <sys/resource.h>

// kivgraph_process_sample reports what proc_pid_rusage knows about pid: what it
// holds now, the most it ever held, the processor time it spent, and how long ago
// it started.
//
// proc_pid_rusage is the interface ps and top use; it answers for another process
// of the same user without a task port, which task_info would require.
//
// The flavour is V4 rather than the V2 the resident-size reader beside this one
// asks for, and the difference is not an inconsistency to tidy away: the peak
// footprint field only exists from V4, which macOS has carried since 10.13,
// while a reader that needs nothing but the resident size has no reason to
// require it. Neither asks for the current flavour, whose layout changes between
// SDKs.
//
// The start time is a mach absolute time, which is not nanoseconds on every
// machine, so it is converted through the timebase rather than assumed.
static int kivgraph_process_sample(int pid, uint64_t *resident, uint64_t *peak,
                                   uint64_t *cpu_ns, uint64_t *uptime_ns) {
	struct rusage_info_v4 info;
	int result = proc_pid_rusage(pid, RUSAGE_INFO_V4, (rusage_info_t *)&info);
	if (result != 0) {
		return result;
	}
	*resident = info.ri_resident_size;
	*peak = info.ri_lifetime_max_phys_footprint;
	*cpu_ns = info.ri_user_time + info.ri_system_time;

	mach_timebase_info_data_t timebase;
	if (mach_timebase_info(&timebase) != KERN_SUCCESS || timebase.denom == 0) {
		*uptime_ns = 0;
		return 0;
	}
	uint64_t now = mach_absolute_time();
	if (now <= info.ri_proc_start_abstime) {
		*uptime_ns = 0;
		return 0;
	}
	*uptime_ns = (now - info.ri_proc_start_abstime) * timebase.numer / timebase.denom;
	return 0;
}
*/
import "C"

import "time"

func observe(pid int) Sample {
	var resident, peak, cpu, uptime C.uint64_t
	if C.kivgraph_process_sample(C.int(pid), &resident, &peak, &cpu, &uptime) != 0 {
		return Sample{}
	}
	return Sample{
		Resident: clampBytes(uint64(resident)),
		Peak:     clampBytes(uint64(peak)),
		CPU:      time.Duration(clampBytes(uint64(cpu))),
		Uptime:   time.Duration(clampBytes(uint64(uptime))),
	}
}

func clampBytes(value uint64) int64 {
	if value > uint64(maxResidentBytes) {
		return 0
	}
	return int64(value)
}

// proportionalSupported is false here, and saying so is the point.
//
// Dividing a shared page between the processes holding it is what /proc's
// smaps_rollup does; this platform reports a footprint per process and no such
// split, so a caller must not read a resident size as a cost. There is no
// approximation that would be better than the declared absence: guessing which
// pages are shared is how a mapped file starts looking like a leak.
func proportionalSupported() bool { return false }
