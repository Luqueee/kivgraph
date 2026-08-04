//go:build ladybug && cgo && linux

package ladybug

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

func externalStorageLocks(path string) ([]int, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, true, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, true, errors.New("database stat has no Linux inode data")
	}
	data, err := os.ReadFile("/proc/locks")
	if err != nil {
		if os.IsNotExist(err) || os.IsPermission(err) {
			return nil, false, nil
		}
		return nil, true, err
	}
	identity := linuxLockIdentity(uint64(stat.Dev), stat.Ino)
	return parseExternalLockPIDs(string(data), identity, os.Getpid()), true, nil
}

func linuxLockIdentity(device, inode uint64) string {
	major := ((device & 0x00000000000fff00) >> 8) | ((device & 0xfffff00000000000) >> 32)
	minor := (device & 0x00000000000000ff) | ((device & 0x00000ffffff00000) >> 12)
	return fmt.Sprintf("%02x:%02x:%d", major, minor, inode)
}

func parseExternalLockPIDs(data, identity string, currentPID int) []int {
	seen := make(map[int]struct{})
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		for index, field := range fields {
			if field != identity || index == 0 {
				continue
			}
			pid, err := strconv.Atoi(fields[index-1])
			if err == nil && pid > 0 && pid != currentPID {
				seen[pid] = struct{}{}
			}
		}
	}
	pids := make([]int, 0, len(seen))
	for pid := range seen {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	return pids
}
