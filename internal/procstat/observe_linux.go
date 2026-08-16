//go:build linux

package procstat

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// userHz is the unit /proc reports process times in.
//
// It is 100 on every Linux port regardless of the kernel's own tick rate: the
// kernel scales what it exports, precisely so a reader does not have to ask.
const userHz = 100

func observe(pid int) Sample {
	sample := Sample{Resident: residentBytes(pid)}
	readStatusFields(pid, &sample)
	readRollupFields(pid, &sample)
	readTimes(pid, &sample)
	return sample
}

// readStatusFields takes the peak from /proc/<pid>/status, which is the only
// place that remembers it: the high-water mark is not derivable from what the
// process holds now.
func readStatusFields(pid int, sample *Sample) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return
	}
	for line := range strings.Lines(string(data)) {
		name, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		if name == "VmHWM" {
			sample.Peak = kilobytesToBytes(value)
			return
		}
	}
}

// readRollupFields takes the shared-memory split from smaps_rollup, which the
// kernel added in 4.14. An older kernel leaves the three fields at zero, which a
// caller reads as "this machine cannot divide shared pages" rather than as a
// process that shares nothing.
func readRollupFields(pid int, sample *Sample) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/smaps_rollup")
	if err != nil {
		return
	}
	for line := range strings.Lines(string(data)) {
		name, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch name {
		case "Pss":
			sample.Proportional = kilobytesToBytes(value)
		case "Shared_Clean":
			sample.SharedClean = kilobytesToBytes(value)
		case "Private_Dirty":
			sample.PrivateDirty = kilobytesToBytes(value)
		}
	}
}

// readTimes takes processor time and start time from /proc/<pid>/stat, and turns
// the second into an uptime with /proc/uptime rather than with the boot time: the
// two are expressed in the same units against the same origin, so subtracting
// them needs no clock.
//
// The comm field can contain spaces and parentheses, so the fields are counted
// from the last ')' and never by splitting the whole line.
func readTimes(pid int, sample *Sample) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return
	}
	line := string(data)
	end := strings.LastIndex(line, ")")
	if end < 0 || end+2 >= len(line) {
		return
	}
	fields := strings.Fields(line[end+2:])
	// The slice starts at field 3 (state), so utime and stime are 11 and 12, and
	// starttime is 19.
	if len(fields) <= 19 {
		return
	}
	user, userErr := strconv.ParseInt(fields[11], 10, 64)
	system, systemErr := strconv.ParseInt(fields[12], 10, 64)
	if userErr == nil && systemErr == nil {
		sample.CPU = time.Duration(user+system) * time.Second / userHz
	}
	started, startedErr := strconv.ParseInt(fields[19], 10, 64)
	if startedErr != nil {
		return
	}
	booted, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return
	}
	seconds, err := strconv.ParseFloat(strings.Fields(string(booted))[0], 64)
	if err != nil {
		return
	}
	uptime := time.Duration(seconds*float64(time.Second)) - time.Duration(started)*time.Second/userHz
	if uptime > 0 {
		sample.Uptime = uptime
	}
}

func kilobytesToBytes(value string) int64 {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0
	}
	kilobytes, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || kilobytes < 0 {
		return 0
	}
	return kilobytes * 1024
}

func proportionalSupported() bool { return true }
