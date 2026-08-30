package indexing

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/filelock"
)

func TestFullIndexRefusesAConcurrentProfileHoldingSharedAnalyzerTargets(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "analyzer-targets.lock")
	held, acquired, err := filelock.Acquire(lockPath)
	if err != nil || !acquired {
		t.Fatalf("Acquire() = %v, %t, %v", held, acquired, err)
	}
	defer held.Release()

	_, err = RunFull(context.Background(), FullOptions{
		Root:                  t.TempDir(),
		ResolverVersion:       "test",
		SharedTargetsLockPath: lockPath,
	})
	if err == nil || !strings.Contains(err.Error(), "shared analyzer targets are busy") {
		t.Fatalf("RunFull() error = %v, want named cross-profile lock refusal", err)
	}
}
