package indexing

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/filelock"
	"github.com/Luqueee/kivgraph/internal/workspace"
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

func TestFreshnessFailureReleasesSharedAnalyzerTargets(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "analyzer-targets.lock")
	_, err := RunFull(t.Context(), FullOptions{
		Root: root, ResolverVersion: "test", SharedTargetsLockPath: lockPath,
		Repositories: []workspace.Repository{{Name: "missing", Path: filepath.Join(root, "missing")}},
	})
	if err == nil || !strings.Contains(err.Error(), "capture source inventory") {
		t.Fatalf("RunFull() error = %v, want inventory refusal", err)
	}
	lock, acquired, err := filelock.Acquire(lockPath)
	if err != nil || !acquired {
		t.Fatalf("inventory failure retained profile lock: %v, %t", err, acquired)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}
