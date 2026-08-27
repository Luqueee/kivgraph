//go:build !linux && !windows && !(darwin && cgo)

package procstat

func residentBytes(int) int64 { return 0 }

// supported reports whether this build can answer ResidentBytes.
func supported() bool { return false }

// observe answers nothing, which a caller reads as "this platform could not
// measure it" and never as a process that used none.
func observe(int) Sample { return Sample{} }

func proportionalSupported() bool { return false }
