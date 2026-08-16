//go:build darwin && cgo

package procstat

/*
#include <stdint.h>
#include <libproc.h>
#include <sys/resource.h>

// resident_size reports the resident set size of pid. proc_pid_rusage is the
// interface ps and top use; it answers for another process of the same user
// without a task port, which task_info would require. RUSAGE_INFO_V2 is
// requested by name because the layout of the current flavour changes between
// SDKs.
static int kivgraph_resident_size(int pid, uint64_t *out) {
	struct rusage_info_v2 info;
	int result = proc_pid_rusage(pid, RUSAGE_INFO_V2, (rusage_info_t *)&info);
	if (result != 0) {
		return result;
	}
	*out = info.ri_resident_size;
	return 0;
}
*/
import "C"

const maxResidentBytes = int64(^uint64(0) >> 1)

func residentBytes(pid int) int64 {
	var resident C.uint64_t
	if C.kivgraph_resident_size(C.int(pid), &resident) != 0 {
		return 0
	}
	if uint64(resident) > uint64(maxResidentBytes) {
		return 0
	}
	return int64(resident)
}

// supported reports whether this build can answer ResidentBytes.
func supported() bool { return true }
