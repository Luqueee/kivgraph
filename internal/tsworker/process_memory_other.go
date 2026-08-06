//go:build !linux

package tsworker

func processMemoryBytes(int) int64 { return 0 }
