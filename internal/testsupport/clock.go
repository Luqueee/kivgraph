package testsupport

import "runtime"

// ClockResolvesShortDurations reports whether this platform's monotonic clock
// can measure an interval shorter than the work a fast unit test does.
//
// It cannot everywhere. Go's monotonic clock on Windows reads the interrupt
// time, whose tick is the scheduler's rather than the counter's, and the
// difference is four orders of magnitude. Measured on the host this branch was
// developed against:
//
//	                                  linux/amd64   windows/amd64
//	smallest non-zero time.Since             9ns         52.6µs
//	short work measured as exactly 0     0 / 1000      996 / 1000
//
// So an assertion that some duration is greater than zero is a claim about the
// clock and not about the code, and on Windows it is a claim the clock refuses
// almost always. A test whose subject is a counter keeps its counter and drops
// the duration beside it, the way a test whose subject is privacy drops a mode
// bit -- see ModeBitsHonoured, which is the same shape for the same reason.
//
// This is not a licence to stop measuring duration. It is a statement that a
// duration under 52.6µs is unobservable there, and a test that needs one has
// to make the work long enough to see rather than assert its way past the
// tick.
func ClockResolvesShortDurations() bool { return runtime.GOOS != "windows" }
