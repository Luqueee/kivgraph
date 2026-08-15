package ladybug

import "os"

// The buffer pool a read-only open of a canonical graph is given, as a
// proportion of the graph it will read, bounded at both ends.
const (
	scanBufferPoolFactor  = 2
	minimumScanBufferPool = 256 << 20
	maximumScanBufferPool = 2 << 30
)

// scanBufferPoolBytes is the buffer pool one read-only open of the graph at
// path is given.
//
// The engine's own default is 80% of the machine's memory, which is the right
// size for a cache in a long-lived database process. A scan is the opposite of
// that: it reads every page once, in stable key order, and then closes the
// database. Sizing its cache from the machine means every `ladygraph serve`
// reserves gigabytes it cannot use while it loads its snapshot, and on a
// machine running one server per MCP client that reservation is the difference
// between fitting and swapping.
//
// The pool is proportional to what will be read, floored so a small graph still
// has room to sort, and capped so a large one cannot claim the machine. A path
// that cannot be measured gets the floor: the open that follows will report why
// the file could not be read, and guessing large is not a better answer than
// guessing small.
func scanBufferPoolBytes(path string) uint64 {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 0 {
		return minimumScanBufferPool
	}
	requested := uint64(info.Size()) * scanBufferPoolFactor
	if requested < minimumScanBufferPool {
		return minimumScanBufferPool
	}
	if requested > maximumScanBufferPool {
		return maximumScanBufferPool
	}
	return requested
}
