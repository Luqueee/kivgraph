package nativeheap_test

import (
	"testing"

	"github.com/Luqueee/kivgraph/internal/nativeheap"
)

// TestReturnIsRepeatable defends the contract every caller depends on: handing
// the native heap back is an economy, never a precondition, so it answers the
// same thing however many times it is asked and a build with no such allocator
// is not a failure. ReturnBuildMemory calls it after every snapshot build, and
// a server publishes several an hour.
func TestReturnIsRepeatable(t *testing.T) {
	first := nativeheap.Return()
	for range 3 {
		if nativeheap.Return() != first {
			t.Fatalf("Return changed its answer between calls, first was %v", first)
		}
	}
	if !first {
		t.Log("this build has no native allocator to return memory to")
	}
}
