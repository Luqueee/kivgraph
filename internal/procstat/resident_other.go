//go:build !linux && !(darwin && cgo)

package procstat

func residentBytes(int) int64 { return 0 }

// supported reports whether this build can answer ResidentBytes.
func supported() bool { return false }
