// Package procstat reads live process statistics from the operating system.
//
// The observations are best effort: a zero value means the platform could not
// answer, never that the process used no memory. Callers that publish a metric
// must treat zero as "unknown" and say so.
package procstat

import "time"

// ResidentBytes returns the current resident set size of pid in bytes, or 0
// when the platform cannot report it.
func ResidentBytes(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	return residentBytes(pid)
}

// Sample is one observation of a live process.
//
// Every field is best effort and zero means the platform could not answer, never
// that the process used none. The distinction matters most for the three
// shared-memory fields: a process that maps a published snapshot counts every
// page it touched as resident, so three servers reading one generation each
// report all of it. Proportional is that page divided by the processes holding
// it, which is what the machine actually spends.
type Sample struct {
	// Resident is what the process has in memory right now, shared pages
	// included, which is what `ps` and `top` show and what misleads.
	Resident int64
	// Proportional is the process's share of every page it holds: a page mapped
	// by three processes counts a third here. Zero on a platform that cannot
	// divide it.
	Proportional int64
	// SharedClean is memory the process reads and does not own -- the executable,
	// the shared libraries, a mapped snapshot.
	SharedClean int64
	// PrivateDirty is memory only this process holds, which is what it would
	// stop costing if it exited.
	PrivateDirty int64
	// Peak is the high-water mark of resident memory over the process's life. It
	// is what sizes a machine, because a server that peaks at a gigabyte needs
	// that gigabyte even if it parks at a tenth of it.
	Peak int64
	// CPU is processor time consumed, user and system together.
	CPU time.Duration
	// Uptime is how long the process has been running.
	Uptime time.Duration
}

// Observe answers one sample of pid.
func Observe(pid int) Sample {
	if pid <= 0 {
		return Sample{}
	}
	return observe(pid)
}

// ProportionalSupported reports whether this build can divide shared memory
// between the processes holding it.
//
// A reader has to be told, because the alternative is reading a resident size as
// a cost and concluding that mapping a file made a server more expensive when it
// made the machine cheaper.
func ProportionalSupported() bool { return proportionalSupported() }
