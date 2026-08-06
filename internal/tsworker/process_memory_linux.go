//go:build linux

package tsworker

import (
	"os"
	"strconv"
	"strings"
)

func processMemoryBytes(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	residentPages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || residentPages < 0 {
		return 0
	}
	pageSize := int64(os.Getpagesize())
	if pageSize <= 0 || residentPages > (int64(^uint64(0)>>1)/pageSize) {
		return 0
	}
	return residentPages * pageSize
}
