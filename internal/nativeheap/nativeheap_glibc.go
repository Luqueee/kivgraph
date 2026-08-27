//go:build linux && cgo

package nativeheap

/*
#include <malloc.h>
*/
import "C"

// Return hands back the free top of every arena the allocator holds, and
// reports whether this build has an allocator that can do it.
//
// glibc gives every thread that contends for the allocator its own arena and
// grows each before it reuses anything, so a process that rebuilds its
// snapshot on a fresh set of threads keeps another arena's worth of resident
// memory each time. Nothing leaks: every arena is reachable and reused, and
// the allocator counts it as free. That is precisely why the kernel still
// counts it as resident and why a Go scavenge cannot reach it.
//
// Measured on devlabs against its own 189 MB graph, 102 894 symbols, four
// builds in one process on a warm page cache, with the Go heap parking at
// 176-180 MB against a 173 MB live snapshot either way:
//
//	Go scavenge alone   RSS 309.5 -> 400.7 -> 453.7 -> 511.1 MB   (+67 MB each)
//	and malloc_trim     RSS 241.5 -> 243.6 -> 244.5 -> 249.6 MB   (+2.7 MB each)
//
// Capping the arenas instead does not work from here, and the measurement is
// what says so: mallopt(M_ARENA_MAX, 1) after start left resident size
// climbing 334 -> 396 -> 467 -> 553 MB, because glibc reads arena_max when it
// creates an arena and allocations already spread across secondary, mmap'd
// heaps stay where they are. The same cap in the environment, read before the
// first allocation, was flat at 256-259 MB -- still worse than trimming, and
// not ours to set: an MCP server is launched by its client.
//
// malloc_trim walks every arena, so it does not care which one holds the freed
// slabs, and it costs no measurable time: 1.63-1.77 s per build without it,
// 1.67-1.84 s with it. An earlier reading of this as a speedup was a cold page
// cache on a 189 MB file, not the allocator.
func Return() bool {
	C.malloc_trim(0)
	return true
}
