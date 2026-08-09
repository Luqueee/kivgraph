// Package procstat reads live process statistics from the operating system.
//
// The observations are best effort: a zero value means the platform could not
// answer, never that the process used no memory. Callers that publish a metric
// must treat zero as "unknown" and say so.
package procstat

// ResidentBytes returns the current resident set size of pid in bytes, or 0
// when the platform cannot report it.
func ResidentBytes(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	return residentBytes(pid)
}
