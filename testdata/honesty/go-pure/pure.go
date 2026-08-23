// Package pure has nothing the index cannot read. It is the control: every
// answer about it must be COMPLETE, or the verdict is a constant rather than a
// measurement.
package pure

// Reachable is called from this package and from nowhere else.
func Reachable() string { return "reachable" }

// Caller is the one use of Reachable.
func Caller() string { return Reachable() }

// Lonely is declared and never used, so an answer about it is a real absence.
func Lonely() string { return "lonely" }
