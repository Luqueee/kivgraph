package indexing

import (
	"os"
	"strconv"
	"strings"
)

// cgroupMemoryLimit is the memory the cgroup of this process is allowed, which
// a container sets and a bare machine does not. The unified hierarchy keeps it
// in `memory.max`, the older one in `memory.limit_in_bytes`.
func cgroupMemoryLimit() (int64, bool) {
	for _, path := range []string{
		"/sys/fs/cgroup/memory.max",
		"/sys/fs/cgroup/memory/memory.limit_in_bytes",
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return parseCgroupLimit(string(content))
	}
	return 0, false
}

// parseCgroupLimit reads a cgroup limit file. `max` is no limit, and so is the
// page-rounded maximum an old kernel reports for one: a value no machine has.
func parseCgroupLimit(content string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(content), 10, 64)
	if err != nil || value <= 0 || value >= 1<<60 {
		return 0, false
	}
	return value, true
}
