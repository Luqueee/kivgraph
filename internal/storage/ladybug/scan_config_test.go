package ladybug

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScanBufferPoolBytesIsBoundedAtBothEnds fixes the rule that keeps a server
// from reserving a share of the machine to read a file. The engine's own default
// is 80% of system memory, which is a cache size for a database process that
// lives; a scan reads every page once and closes.
func TestScanBufferPoolBytesIsBoundedAtBothEnds(t *testing.T) {
	directory := t.TempDir()

	small := filepath.Join(directory, "small.db")
	if err := os.WriteFile(small, []byte("x"), 0o600); err != nil {
		t.Fatalf("write small graph: %v", err)
	}
	if got := scanBufferPoolBytes(small); got != minimumScanBufferPool {
		t.Fatalf("scanBufferPoolBytes(small) = %d, want the floor %d: a graph still has to sort",
			got, uint64(minimumScanBufferPool))
	}

	proportional := filepath.Join(directory, "proportional.db")
	const size = 400 << 20
	if err := os.WriteFile(proportional, nil, 0o600); err != nil {
		t.Fatalf("create proportional graph: %v", err)
	}
	if err := os.Truncate(proportional, size); err != nil {
		t.Fatalf("size proportional graph: %v", err)
	}
	if got := scanBufferPoolBytes(proportional); got != size*scanBufferPoolFactor {
		t.Fatalf("scanBufferPoolBytes(400 MiB) = %d, want %d", got, uint64(size*scanBufferPoolFactor))
	}

	large := filepath.Join(directory, "large.db")
	if err := os.WriteFile(large, nil, 0o600); err != nil {
		t.Fatalf("create large graph: %v", err)
	}
	if err := os.Truncate(large, maximumScanBufferPool); err != nil {
		t.Fatalf("size large graph: %v", err)
	}
	if got := scanBufferPoolBytes(large); got != maximumScanBufferPool {
		t.Fatalf("scanBufferPoolBytes(large) = %d, want the cap %d: one scan cannot claim the machine",
			got, uint64(maximumScanBufferPool))
	}
}

// TestScanBufferPoolBytesGuessesSmallWhenItCannotMeasure keeps an unmeasurable
// path from claiming memory. The open that follows reports why the file could
// not be read; guessing large would only add a reservation to that failure.
func TestScanBufferPoolBytesGuessesSmallWhenItCannotMeasure(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent.db")
	if got := scanBufferPoolBytes(absent); got != minimumScanBufferPool {
		t.Fatalf("scanBufferPoolBytes(absent) = %d, want the floor %d", got, uint64(minimumScanBufferPool))
	}
}
